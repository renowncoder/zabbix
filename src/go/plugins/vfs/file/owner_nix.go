// +build !windows

/*
** Zabbix
** Copyright (C) 2001-2021 Zabbix SIA
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

package file

import (
	"errors"
	"fmt"
	"os/user"
	"strconv"
	"syscall"
)

// Export -
func (p *Plugin) exportOwner(params []string) (result interface{}, err error) {
	if len(params) > 3 {
		return nil, errors.New("Too many parameters.")
	}
	if len(params) == 0 || params[0] == "" {
		return nil, errors.New("Invalid first parameter.")
	}
	ownertype := "user"
	if len(params) > 1 && params[1] != "" {
		if params[1] != "user" && params[1] != "group" {
			return nil, fmt.Errorf("Invalid second parameter: %s", params[1])
		}
		ownertype = params[1]
	}
	resulttype := "name"
	if len(params) > 2 && params[2] != "" {
		if params[2] != "name" && params[2] != "id" {
			return nil, fmt.Errorf("Invalid second parameter: %s", params[2])
		}
		resulttype = params[2]
	}

	info, err := stdOs.Stat(params[0])
	if err != nil {
		return nil, fmt.Errorf("Cannot obtain file %s information: %s", params[0], err)
	}
	stat := info.Sys().(*syscall.Stat_t)
	if stat == nil {
		return nil, fmt.Errorf("Cannot obtain %s owner information", params[0])
	}
	var ret string
	switch ownertype + resulttype {
	case "userid":
		u := strconv.FormatUint(uint64(stat.Uid), 10)
		g := strconv.FormatUint(uint64(stat.Gid), 10)
		ret = u + "/" + g
	case "groupid":
		ret = strconv.FormatUint(uint64(stat.Gid), 10)
	case "username":
		u := strconv.FormatUint(uint64(stat.Uid), 10)
		usr, err := user.LookupId(u)
		if err != nil {
			return nil, fmt.Errorf("Cannot obtain %s user information: %s", params[0], err)
		}
		ret = usr.Name
	case "groupname":
		g := strconv.FormatUint(uint64(stat.Gid), 10)
		group, err := user.LookupGroupId(g)
		if err != nil {
			return nil, fmt.Errorf("Cannot obtain %s group information: %s", params[0], err)
		}
		ret = group.Name
	}

	return ret, nil
}
