/*
** Zabbix
** Copyright (C) 2001-2019 Zabbix SIA
**
** This program is free software; you can redistribute it and/or modify
** it under the terms of the GNU General Public License as published by
** the Free Software Foundation; either version 2 of the License, or
** (at your option) any later version.
**
** This program is distributed in the hope that it will be useful,
** but WITHOUT ANY WARRANTY; without even the implied warranty of
** MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
** GNU General Public License for more details.
**
** You should have received a copy of the GNU General Public License
** along with this program; if not, write to the Free Software
** Foundation, Inc., 51 Franklin Street, Fifth Floor, Boston, MA  02110-1301, USA.
**/

package scheduler

import (
	"container/heap"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"
	"zabbix/internal/agent"
	"zabbix/internal/monitor"
	"zabbix/pkg/glexpr"
	"zabbix/pkg/itemutil"
	"zabbix/pkg/log"
	"zabbix/pkg/plugin"
)

type Manager struct {
	input   chan interface{}
	plugins map[string]*pluginAgent
	queue   pluginHeap
	clients map[uint64]*client
}

type updateRequest struct {
	clientID           uint64
	sink               plugin.ResultWriter
	requests           []*plugin.Request
	refreshUnsupported int
	expressions        []*glexpr.Expression
}

type queryRequest struct {
	command string
	sink    chan string
}

type Scheduler interface {
	UpdateTasks(clientID uint64, writer plugin.ResultWriter, refreshUnsupported int, expressions []*glexpr.Expression,
		requests []*plugin.Request)
	FinishTask(task performer)
	PerformTask(key string, timeout time.Duration) (result string, err error)
	Query(command string) (status string)
}

func (m *Manager) cleanupClient(c *client, now time.Time) {
	released := c.cleanup(m.plugins, now)
	for _, p := range released {
		if p.refcount != 0 {
			continue
		}
		log.Debugf("deactivate unused plugin %s", p.name())

		// deactivate recurring tasks
		for deactivate := true; deactivate; {
			deactivate = false
			for _, t := range p.tasks {
				if t.isRecurring() {
					t.deactivate()
					// deactivation can change tasks ordering, so repeat the iteration if task was deactivated
					deactivate = true
					break
				}
			}
		}

		// queue stopper task if plugin has Runner interface
		if _, ok := p.impl.(plugin.Runner); ok {
			task := &stopperTask{
				taskBase: taskBase{plugin: p, active: true},
			}
			if err := task.reschedule(now); err != nil {
				log.Debugf("cannot schedule stopper task for plugin %s", p.name())
				continue
			}
			p.enqueueTask(task)
			log.Debugf("created stopper task for plugin %s", p.name())

			if p.queued() {
				m.queue.Update(p)
			}
		}

		// queue plugin if there are still some tasks left to be finished before deactivating
		if len(p.tasks) != 0 {
			if !p.queued() {
				heap.Push(&m.queue, p)
			}
		}
	}
}

func (m *Manager) processUpdateRequest(update *updateRequest, now time.Time) {
	log.Debugf("processing update request (%d requests)", len(update.requests))

	// TODO: client expiry - remove unused owners after timeout (day+?)
	var requestClient *client
	var ok bool
	if requestClient, ok = m.clients[update.clientID]; !ok {
		log.Debugf("registering new client ID:%d", update.clientID)
		requestClient = newClient(update.clientID, update.sink)
		m.clients[update.clientID] = requestClient
	}

	requestClient.refreshUnsupported = update.refreshUnsupported
	requestClient.updateExpressions(update.expressions)

	for _, r := range update.requests {
		var key string
		var err error
		var p *pluginAgent
		if key, _, err = itemutil.ParseKey(r.Key); err == nil {
			if p, ok = m.plugins[key]; !ok {
				err = fmt.Errorf("Unknown metric %s", key)
			}
		}
		if err == nil {
			err = requestClient.addRequest(p, r, update.sink, now)
		}
		if err != nil {
			if tacc, ok := requestClient.exporters[r.Itemid]; ok {
				log.Debugf("deactivate exporter task for item %d because of error: %s", r.Itemid, err)
				tacc.task().deactivate()
			}
			update.sink.Write(&plugin.Result{Itemid: r.Itemid, Error: err, Ts: now})
			log.Debugf("cannot monitor metric \"%s\": %s", r.Key, err.Error())
			continue
		}

		if !p.queued() {
			heap.Push(&m.queue, p)
		} else {
			m.queue.Update(p)
		}
	}

	m.cleanupClient(requestClient, now)
}

func (m *Manager) processQueue(now time.Time) {
	seconds := now.Unix()
	for p := m.queue.Peek(); p != nil; p = m.queue.Peek() {
		if task := p.peekTask(); task != nil {
			if task.getScheduled().Unix() > seconds {
				break
			}
			heap.Pop(&m.queue)
			if p.hasCapacity() {
				p.popTask()
				p.reserveCapacity(task)
				task.perform(m)
				if p.hasCapacity() {
					heap.Push(&m.queue, p)
				}
			} else {
				log.Debugf("cannot perform task for plugin %s: no capacity", p.name())
			}
		} else {
			// plugins with empty task queue should not be in Manager queue
			heap.Pop(&m.queue)
		}
	}
}

func (m *Manager) processFinishRequest(task performer) {
	p := task.getPlugin()
	p.releaseCapacity(task)
	if p.active() && task.isActive() && task.isRecurring() {
		if err := task.reschedule(time.Now()); err != nil {
			log.Warningf("cannot reschedule plugin %s: %s", p.impl.Name(), err)
		} else {
			p.enqueueTask(task)
		}
	}
	if !p.queued() && p.hasCapacity() {
		heap.Push(&m.queue, p)
	}
}

// rescheduleQueue reschedules all queued tasks. This is done whenever time
// difference between ticks exceeds limits (for example during daylight saving changes).
func (m *Manager) rescheduleQueue(now time.Time) {
	// easier to rebuild queues than update each element
	queue := make(pluginHeap, 0, len(m.queue))
	for _, p := range m.queue {
		tasks := p.tasks
		p.tasks = make(performerHeap, 0, len(tasks))
		for _, t := range tasks {
			if err := t.reschedule(now); err == nil {
				p.enqueueTask(t)
			}
		}
		heap.Push(&queue, p)
	}
	m.queue = queue
}

func (m *Manager) run() {
	defer log.PanicHook()
	log.Debugf("starting manager")
	// Adjust ticker creation at the 0 nanosecond timestamp. In reality it will have at least
	// some microseconds, which will be enough to include all scheduled tasks at this second
	// even with nanosecond priority adjustment.
	lastTick := time.Now()
	cleaned := lastTick
	time.Sleep(time.Duration(1e9 - lastTick.Nanosecond()))
	ticker := time.NewTicker(time.Second)
run:
	for {
		select {
		case <-ticker.C:
			now := time.Now()
			diff := now.Sub(lastTick)
			interval := time.Second * 10
			if diff <= -interval || diff >= interval {
				log.Warningf("detected %d time difference between queue checks, rescheduling tasks",
					int(math.Abs(float64(diff))/1e9))
				m.rescheduleQueue(now)
			}
			lastTick = now
			m.processQueue(now)
			// cleanup plugins used by passive checks
			if now.Sub(cleaned) >= time.Hour {
				if passive, ok := m.clients[0]; ok {
					m.cleanupClient(passive, now)
				}
				cleaned = now
			}
		case v := <-m.input:
			if v == nil {
				break run
			}
			switch v.(type) {
			case *updateRequest:
				m.processUpdateRequest(v.(*updateRequest), time.Now())
				m.processQueue(time.Now())
			case performer:
				m.processFinishRequest(v.(performer))
				m.processQueue(time.Now())
			case *queryRequest:
				r := v.(*queryRequest)
				if response, err := m.processQuery(r); err != nil {
					r.sink <- "cannot process request: " + err.Error()
				} else {
					r.sink <- response
				}
			}
		}
	}
	close(m.input)
	log.Debugf("manager has been stopped")
	monitor.Unregister()
}

func (m *Manager) init() {
	m.input = make(chan interface{}, 10)
	m.queue = make(pluginHeap, 0, len(plugin.Metrics))
	m.clients = make(map[uint64]*client)
	m.plugins = make(map[string]*pluginAgent)

	metrics := make([]*plugin.Metric, 0, len(plugin.Metrics))

	for _, metric := range plugin.Metrics {
		metrics = append(metrics, metric)
	}
	sort.Slice(metrics, func(i, j int) bool {
		return metrics[i].Plugin.Name() < metrics[j].Plugin.Name()
	})

	pagent := &pluginAgent{}
	for _, metric := range metrics {
		if metric.Plugin != pagent.impl {
			capacity := metric.Plugin.Capacity()
			if options, ok := agent.Options.Plugins[metric.Plugin.Name()]; ok {
				if cap, ok := options["Capacity"]; ok {
					var err error
					if capacity, err = strconv.Atoi(cap); err != nil {
						log.Warningf("invalid configuration parameter Plugins.%s.Capacity value '%s', using default %d",
							metric.Plugin.Name(), cap, plugin.DefaultCapacity)
					}
				}
			}
			if capacity > metric.Plugin.Capacity() {
				log.Warningf("lowering the plugin %s capacity to %d as the configured capacity %d exceeds limits",
					metric.Plugin.Name(), metric.Plugin.Capacity(), capacity)
				capacity = metric.Plugin.Capacity()
			}

			pagent = &pluginAgent{
				impl:         metric.Plugin,
				tasks:        make(performerHeap, 0),
				capacity:     capacity,
				usedCapacity: 0,
				index:        -1,
				refcount:     0,
			}

			interfaces := ""
			if _, ok := metric.Plugin.(plugin.Exporter); ok {
				interfaces += "exporter, "
			}
			if _, ok := metric.Plugin.(plugin.Collector); ok {
				interfaces += "collector, "
			}
			if _, ok := metric.Plugin.(plugin.Runner); ok {
				interfaces += "runner, "
			}
			if _, ok := metric.Plugin.(plugin.Watcher); ok {
				interfaces += "watcher, "
			}
			if _, ok := metric.Plugin.(plugin.Configurator); ok {
				interfaces += "configurator, "
			}
			interfaces = interfaces[:len(interfaces)-2]
			log.Infof("using plugin '%s' providing following interfaces: %s", metric.Plugin.Name(), interfaces)
		}
		m.plugins[metric.Key] = pagent
	}
}
func (m *Manager) Start() {
	monitor.Register()
	go m.run()
}

func (m *Manager) Stop() {
	m.input <- nil
}

func (m *Manager) UpdateTasks(clientID uint64, writer plugin.ResultWriter, refreshUnsupported int,
	expressions []*glexpr.Expression, requests []*plugin.Request) {

	m.input <- &updateRequest{clientID: clientID,
		sink:               writer,
		requests:           requests,
		refreshUnsupported: refreshUnsupported,
		expressions:        expressions,
	}
}

type resultWriter chan *plugin.Result

func (r resultWriter) Write(result *plugin.Result) {
	r <- result
}

func (r resultWriter) Flush() {
}

func (r resultWriter) SlotsAvailable() int {
	return 1
}

func (r resultWriter) PersistSlotsAvailable() int {
	return 1
}

func (m *Manager) PerformTask(key string, timeout time.Duration) (result string, err error) {
	var lastLogsize uint64
	var mtime int

	w := make(resultWriter)
	m.UpdateTasks(0, w, 0, nil, []*plugin.Request{{Key: key, LastLogsize: &lastLogsize, Mtime: &mtime}})

	select {
	case r := <-w:
		if r.Error == nil {
			if r.Value != nil {
				result = *r.Value
			} else {
				// TODO: check what must be returned on empty result
			}
		} else {
			err = r.Error
		}
	case <-time.After(timeout):
		err = fmt.Errorf("timeout occurred")
	}
	return
}

func (m *Manager) FinishTask(task performer) {
	m.input <- task
}

func (m *Manager) Query(command string) (status string) {
	request := &queryRequest{command: command, sink: make(chan string)}
	m.input <- request
	return <-request.sink
}

func NewManager() *Manager {
	var m Manager
	m.init()
	return &m
}
