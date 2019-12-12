<?php
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


/**
 * Pager helper for data pagination.
 */
class CPagerHelper {

	/**
	 * Number of page buttons (use odd number).
	 */
	const RANGE = 11;

	/**
	 * Create pagination line based on number of rows.
	 *
	 * @param int   $page      page to display
	 * @param int   $num_rows  number of rows to paginate
	 * @param CUrl  $url       data list URL
	 *
	 * @return CTag  paging line
	 */
	public static function paginate($page, $num_rows, CUrl $url) {
		$data = self::prepareData($page, $num_rows);

		return self::createPaging($data['page'], $data['num_rows'], $data['num_pages'], clone $url,
			$data['limit_exceeded'], $data['rows_per_page']
		);
	}

	/**
	 * Create pagination line based on data rows and trim data rows accordingly.
	 *
	 * @param int     $page        page to display
	 * @param array   $rows        data rows
	 * @param string  $sort_order  data sort order: ZBX_SORT_UP or ZBX_SORT_DOWN
	 * @param CUrl    $url         data list URL
	 *
	 * @return CTag  paging line
	 */
	public static function paginateRows($page, &$rows, $sort_order, CUrl $url) {
		$data = self::prepareData($page, count($rows));

		$paging = self::createPaging($data['page'], $data['num_rows'], $data['num_pages'], clone $url,
			$data['limit_exceeded'], $data['rows_per_page']
		);

		$start = ($data['page'] - 1) * $data['rows_per_page'];
		$end = $start + $data['rows_per_page'];
		$offset = ($sort_order == ZBX_SORT_DOWN)
			? $data['offset_down']
			: $data['offset_up'];

		// Trim given rows for the current page.
		$rows = array_slice($rows, $start + $offset, $end - $start, true);

		return $paging;
	}

	/**
	 * Store page number for given entity.
	 *
	 * @param string  $entity
	 * @param int     $page
	 */
	public static function store($entity, $page) {
		CProfile::update('web.pager.entity', $entity, PROFILE_TYPE_STR);
		CProfile::update('web.pager.page', $page, PROFILE_TYPE_INT);
	}

	/**
	 * Fetch stored page number for given entity.
	 *
	 * @param string  $entity
	 * @param mixed   $first_page  substitute return value for the first page
	 *
	 * @return mixed  page number (or the $first_page if wasn't stored or first page was stored)
	 */
	public static function fetch($entity, $first_page = 1) {
		if ($entity !== CProfile::get('web.pager.entity')) {
			return $first_page;
		}

		$page = CProfile::get('web.pager.page', 1);

		return ($page == 1 ? $first_page : $page);
	}

	/**
	 * Prepare data for a given page number and the number of data rows.
	 *
	 * @param int  $page
	 * @param int  $num_rows
	 *
	 * @return array
	 */
	private static function prepareData($page, $num_rows) {
		$rows_per_page = (int) CWebUser::$data['rows_per_page'];
		$config = select_config();

		$offset_down = 0;
		$limit_exceeded = ($num_rows > $config['search_limit']);

		if ($limit_exceeded) {
			$offset_down = $num_rows - $config['search_limit'];
			$num_rows = $config['search_limit'];
		}

		$num_pages = max(1, (int) ceil($num_rows / $rows_per_page));
		$page = max(1, min($num_pages, $page));

		return [
			'page' => $page,
			'num_rows' => $num_rows,
			'num_pages' => $num_pages,
			'offset_up' => 0,
			'offset_down' => $offset_down,
			'rows_per_page' => $rows_per_page,
			'limit_exceeded' => $limit_exceeded
		];
	}

	/**
	 * Create paging line.
	 *
	 * @param int   $page            page number
	 * @param int   $num_rows        number of rows
	 * @param int   $num_pages       number of pages
	 * @param CUrl  $url             data list URL
	 * @param bool  $limit_exceeded  true, if data list size exceeded the configuration search limit
	 * @param int   $rows_per_page   number of rows per page
	 *
	 * @return CTag
	 */
	private static function createPaging($page, $num_rows, $num_pages, CUrl $url, $limit_exceeded, $rows_per_page) {
		$tags = self::createPagingTags($page, $num_rows, $num_pages, $url);

		$total = $limit_exceeded ? $num_rows.'+' : $num_rows;
		$start = ($page - 1) * $rows_per_page;
		$end = min($num_rows, $start + $rows_per_page);

		if ($num_pages == 1) {
			$table_stats = _s('Displaying %1$s of %2$s found', $num_rows, $total);
		}
		else {
			$table_stats = _s('Displaying %1$s to %2$s of %3$s found', $start + 1, $end, $total);
		}

		return (new CDiv())
			->addClass(ZBX_STYLE_TABLE_PAGING)
			->addItem(
				(new CDiv())
					->addClass(ZBX_STYLE_PAGING_BTN_CONTAINER)
					->addItem($tags)
					->addItem(
						(new CDiv())
							->addClass(ZBX_STYLE_TABLE_STATS)
							->addItem($table_stats)
					)
			);
	}

	/**
	 * Create paging tags for paging line.
	 *
	 * @param int   $page       page number
	 * @param int   $num_rows   number of rows
	 * @param int   $num_pages  number of pages
	 * @param CUrl  $url        data list URL
	 *
	 * @return array
	 */
	private static function createPagingTags($page, $num_rows, $num_pages, CUrl $url) {
		$tags = [];

		if ($num_pages > 1) {
			$end_page = min($num_pages, max(self::RANGE, $page + floor(self::RANGE / 2)));
			$start_page = ($end_page > self::RANGE) ? $end_page - self::RANGE + 1 : 1;

			if ($start_page > 1) {
				$url->removeArgument('page');
				$tags[] = new CLink(_x('First', 'page navigation'), $url->getUrl());
			}

			if ($page > 1) {
				if ($page == 2) {
					$url->removeArgument('page');
				}
				else {
					$url->setArgument('page', $page - 1);
				}
				$tags[] = new CLink((new CSpan())->addClass(ZBX_STYLE_ARROW_LEFT), $url->getUrl());
			}

			for ($i = $start_page; $i <= $end_page; $i++) {
				if ($i == 1) {
					$url->removeArgument('page');
				}
				else {
					$url->setArgument('page', $i);
				}

				$link = new CLink($i, $url->getUrl());
				if ($i == $page) {
					$link->addClass(ZBX_STYLE_PAGING_SELECTED);
				}

				$tags[] = $link;
			}

			if ($page < $num_pages) {
				$url->setArgument('page', $page + 1);
				$tags[] = new CLink((new CSpan())->addClass(ZBX_STYLE_ARROW_RIGHT), $url->getUrl());
			}

			if ($i < $num_pages) {
				$url->setArgument('page', $num_pages);
				$tags[] = new CLink(_x('Last', 'page navigation'), $url->getUrl());
			}
		}

		return $tags;
	}
}
