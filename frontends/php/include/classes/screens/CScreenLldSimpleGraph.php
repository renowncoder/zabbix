<?php
/*
** Zabbix
** Copyright (C) 2001-2014 Zabbix SIA
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


class CScreenLldSimpleGraph extends CScreenLldGraphBase {

	/**
	 * @var array
	 */
	protected $createdItemIds = array();

	/**
	 * @var array
	 */
	protected $itemPrototype = null;

	/**
	 * Adds simple graphs to surrogate screen.
	 */
	protected function addSurrogateScreenItems() {
		$createdItemIds = $this->getCreatedItemIds();
		$this->addSimpleGraphsToSurrogateScreen($createdItemIds);
	}

	/**
	 * Retrieves items created for item prototype given as resource for this screen item
	 * and returns array of the item IDs.
	 *
	 * @return array
	 */
	protected function getCreatedItemIds() {
		if (!$this->createdItemIds) {
			$itemPrototypeId = $this->screenitem['resourceid'];

			$hostId = $this->getCurrentHostId();

			// get all created (discovered) items for current host
			$allCreatedItems = API::Item()->get(array(
				'hostids' => array($hostId),
				'output' => array('itemid'),
				'selectItemDiscovery' => array('itemid', 'parent_itemid'),
				'filter' => array('flags' => ZBX_FLAG_DISCOVERY_CREATED)
			));

			// collect those item IDs where parent item is item prototype selected for this screen item as resource
			foreach ($allCreatedItems as $item) {
				if ($item['itemDiscovery']['parent_itemid'] == $itemPrototypeId) {
					$this->createdItemIds[] = $item['itemid'];
				}
			}
		}

		return $this->createdItemIds;
	}

	/**
	 * Makes and adds simple item graph items to surrogate screen from given item IDs.
	 *
	 * @param array $itemIds
	 */
	protected function addSimpleGraphsToSurrogateScreen(array $itemIds) {
		$screenItemTemplate = $this->getScreenItemTemplate(SCREEN_RESOURCE_SIMPLE_GRAPH);

		$screenItems = array();
		foreach ($itemIds as $itemId) {
			$screenItem = $screenItemTemplate;

			$screenItem['resourceid'] = $itemId;
			$screenItem['screenitemid'] = 'z' . $itemId;
			$screenItem['url'] = $this->screenitem['url'];

			$screenItems[] = $screenItem;
		}

		$this->addItemsToSurrogateScreen($screenItems);
	}

	/**
	 * @return mixed
	 */
	protected function getHostIdFromScreenItemResource() {
		$itemPrototype = $this->getItemPrototype();

		return $itemPrototype['discoveryRule']['hostid'];
	}

	/**
	 * @return CTag
	 */
	protected function getPreview() {
		$itemPrototype = $this->getItemPrototype();

		$queryParameters = array(
			'items' => array($itemPrototype),
			'period' => 3600,
			'legend' => 1,
			'width' => $this->screenitem['width'],
			'height' => $this->screenitem['height'],
			'name' => $itemPrototype['hosts'][0]['name'].NAME_DELIMITER.$itemPrototype['name']
		);

		$src = 'chart3.php?'.http_build_query($queryParameters);

		$img = new CImg($src);

		return $img;
	}

	/**
	 * @return integer
	 */
	protected function getItemPrototypeId() {
		return $this->screenitem['resourceid'];
	}

	/**
	 * @return boolean
	 */
	protected function mustShowPreview() {
		$createdItemIds = $this->getCreatedItemIds();

		if ($createdItemIds) {
			return false;
		}
		else {
			return true;
		}
	}

	/**
	 * Return item prototype used by this screen element
	 *
	 * @return array
	 */
	protected function getItemPrototype() {
		if (!$this->itemPrototype) {
			$itemPrototype = API::ItemPrototype()->get(array(
				'output' => array('name'),
				'itemids' => array($this->getItemPrototypeId()),
				'selectHosts' => array('name'),
				'selectDiscoveryRule' => array('hostid')
			));
			$this->itemPrototype = reset($itemPrototype);
		}

		return $this->itemPrototype;
	}
}
