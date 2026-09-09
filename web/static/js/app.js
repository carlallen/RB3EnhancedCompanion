(function () {
	'use strict';

	// ?test=1 keeps the current-song section (song info, venue, band,
	// stage lights) visible even when not actually in a song, so the
	// layout can be checked on a device without an active RB3 session.
	var testMode = /(?:^|[?&])test=1(?:&|$)/.test(location.search);

	var statusDot = document.getElementById('topbar-status');
	var playerCard = document.getElementById('player-card');
	var currentSong = document.getElementById('current-song');
	var currentSongTitle = document.getElementById('current-song-title');
	var currentSongArtist = document.getElementById('current-song-artist');
	var currentSongOrigin = document.getElementById('current-song-origin');
	var currentSongArt = document.getElementById('current-song-art');
	var venueInfo = document.getElementById('venue-info');
	var venueName = document.getElementById('venue-name');
	var bandTable = document.getElementById('band-table');
	var bandRows = document.getElementById('band-rows');
	var songlist = document.getElementById('songlist');
	var searchbox = document.getElementById('searchbox');
	var filtersButton = document.getElementById('filters-button');
	var filtersModal = document.getElementById('filters-modal');
	var filtersClose = document.getElementById('filters-close');
	var filterOptionsModal = document.getElementById('filter-options-modal');
	var filterOptionsTitle = document.getElementById('filter-options-title');
	var filterOptionsList = document.getElementById('filter-options-list');
	var filterOptionsClose = document.getElementById('filter-options-close');
	var clearFiltersButton = document.getElementById('clear-filters-button');

	var stagekitDots = buildStagekit();
	var strobeEl = document.getElementById('sk-strobe');

	function buildStagekit() {
		var container = document.getElementById('stagekit');
		var dots = [];
		for (var pos = 0; pos < 8; pos++) {
			var posEl = document.createElement('div');
			posEl.className = 'sk-pos';
			var posDots = [];
			['r', 'g', 'b', 'y'].forEach(function (colour) {
				var dot = document.createElement('span');
				dot.className = 'sk-dot ' + colour;
				posEl.appendChild(dot);
				posDots.push(dot);
			});
			container.appendChild(posEl);
			dots.push(posDots);
		}
		return dots;
	}

	function originIconURL(origin) {
		return '/static/images/origin/' + encodeURIComponent(origin || 'generic') + '.png';
	}

	var BLANK_ALBUM_ART = '/static/images/blank_album_art_keep.png';

	// albumArtURL picks song's art URL from the data the server pushed:
	// its own default image unless the song list said this song has custom
	// art downloaded (song.hasArt) - the server doesn't guess/fall back.
	function albumArtURL(song) {
		if (!song || !song.hasArt) return BLANK_ALBUM_ART;
		return '/static/art/' + encodeURIComponent(song.shortname) + '_keep.png';
	}

	var lastAlbumArtShortname = null;

	currentSongArt.addEventListener('error', function () {
		this.onerror = null;
		this.src = BLANK_ALBUM_ART;
	});

	function applyState(state) {
		statusDot.classList.toggle('on', state.connected);
		statusDot.classList.toggle('off', !state.connected);
		statusDot.title = state.connected
			? 'Connected' + (state.platform ? ' (' + state.platform + ')' : '')
			: 'Waiting for RB3Enhanced...';

		document.body.classList.toggle('on-song-select', state.screenName === 'song_select_screen');

		var inGame = testMode || (state.connected && state.inGame);
		playerCard.hidden = !inGame;

		if (testMode || (state.connected && state.songName)) {
			currentSong.hidden = false;
			currentSongTitle.textContent = state.songName;
			currentSongArtist.textContent = state.songArtist || '';
			currentSongArtist.hidden = !inGame;
			currentSongOrigin.hidden = !inGame;
			currentSongArt.hidden = !inGame;
			if (inGame) {
				var song = findSong(state.songShortName);
				if (song) {
					currentSongOrigin.src = originIconURL(song.origin);
					currentSongOrigin.title = song.source || '';
				}
				if (state.songShortName !== lastAlbumArtShortname) {
					lastAlbumArtShortname = state.songShortName;
					currentSongArt.src = albumArtURL(song);
				}
			}
		} else {
			currentSong.hidden = true;
		}

		if (inGame && state.venueName) {
			venueInfo.hidden = false;
			venueName.textContent = state.venueName;
		} else {
			venueInfo.hidden = true;
		}

		if (inGame) {
			bandTable.hidden = false;
			bandRows.innerHTML = '';
			state.band.forEach(function (member, i) {
				if (!member.exists) return;
				var row = document.createElement('tr');
				row.innerHTML =
					'<td>' + (i + 1) + '</td>' +
					'<td>' + escapeHTML(member.trackType) + '</td>' +
					'<td>' + escapeHTML(member.difficulty) + '</td>';
				bandRows.appendChild(row);
			});
		} else {
			bandTable.hidden = true;
		}

		applyStagekit(state.stageKit);

		if (state.songList) {
			renderSongList(state.songList);
			lastSongListVersion = state.songListVersion || 0;
		}
	}

	function applyStagekit(sk) {
		if (!sk) return;
		for (var pos = 0; pos < 8; pos++) {
			for (var c = 0; c < 4; c++) {
				stagekitDots[pos][c].classList.toggle('on', !!(sk.led[pos] && sk.led[pos][c]));
			}
		}
		strobeEl.classList.remove('speed-1', 'speed-2', 'speed-3', 'speed-4');
		strobeEl.classList.toggle('on', !!sk.strobe);
		if (sk.strobe) {
			strobeEl.classList.add('speed-' + sk.strobe);
		}
	}

	var currentSongList = [];
	// lastSongListVersion survives across websocket reconnects (screen lock,
	// backgrounding, network handoff) within the same page load, so a
	// reconnect can tell the server it already has the current list.
	var lastSongListVersion = 0;
	// The song row currently expanded (or null) - shared between the
	// per-row click handlers and the search box, so searching can close
	// whatever's open and re-expand a single remaining result.
	var expandedRow = null;

	function findSong(shortname) {
		for (var i = 0; i < currentSongList.length; i++) {
			if (currentSongList[i].shortname === shortname) return currentSongList[i];
		}
		return null;
	}

	function escapeHTML(s) {
		var div = document.createElement('div');
		div.textContent = s || '';
		return div.innerHTML;
	}

	// formatLength renders a song length in milliseconds as "m:ss", or null
	// if ms isn't a usable number.
	function formatLength(ms) {
		if (typeof ms !== 'number' || !isFinite(ms) || ms < 0) return null;
		var totalSeconds = Math.round(ms / 1000);
		var m = Math.floor(totalSeconds / 60);
		var s = totalSeconds % 60;
		return m + ':' + (s < 10 ? '0' : '') + s;
	}

	var INSTRUMENT_ICON_DIR = '/static/icons/instruments/';
	var RING_ICON_DIR = '/static/icons/rings/';

	// DIFFICULTY_PARTS lists the song-level difficulty fields (each 0-7,
	// from the song's metadata file - 0 means the song has no chart for
	// that part), in display order: two rows of five, each identified by
	// its instrument icon rather than a text label. vocals is
	// special-cased (see vocalsIconName) since its icon depends on the
	// song's vocal part count.
	var DIFFICULTY_PARTS = [
		{ key: 'difficultyGuitar', icon: 'guitar', label: 'Guitar' },
		{ key: 'difficultyBass', icon: 'bass', label: 'Bass' },
		{ key: 'difficultyDrum', icon: 'drums', label: 'Drums' },
		{ key: 'difficultyKeys', icon: 'keys', label: 'Keys' },
		{ key: 'difficultyVocals', icon: null, label: 'Vocals' },
		{ key: 'difficultyProGuitar', icon: 'real_guitar', label: 'Pro Guitar' },
		{ key: 'difficultyProBass', icon: 'real_bass', label: 'Pro Bass' },
		{ key: 'difficultyProDrum', icon: 'real_drums', label: 'Pro Drums' },
		{ key: 'difficultyProKeys', icon: 'real_keys', label: 'Pro Keys' },
		{ key: 'difficultyBand', icon: 'band', label: 'Band' }
	];

	// vocalsIconName picks the vocals icon variant matching how many vocal
	// parts (1-3) the song's metadata reported harmony parts for.
	function vocalsIconName(song) {
		if (song.vocalParts === 3) return 'vocals3';
		if (song.vocalParts === 2) return 'vocals2';
		return 'vocals';
	}

	// diffCellHTML renders one difficulty-grid cell: the part's instrument
	// icon, sized to sit inside the ring_0.png-ring_6.png image matching
	// its difficulty. Difficulty values are 0-7, with 0 meaning the song
	// has no chart for that part at all - shown as just the icon on its
	// own, dimmed, with no ring. Any other value (1-7) maps to a ring
	// image one lower (0-6), since ring_0.png is the lowest chart tier,
	// not "not charted".
	function diffCellHTML(song, part) {
		var value = song[part.key];
		var iconName = part.icon || vocalsIconName(song);
		if (!value) {
			return '<div class="diff-cell"><div class="diff-ring">' +
				'<img class="diff-icon diff-icon-dim" src="' + INSTRUMENT_ICON_DIR + iconName + '.png" alt="' + escapeHTML(part.label) + '" title="' + escapeHTML(part.label) + ' - not charted">' +
				'</div></div>';
		}
		var ring = Math.max(0, Math.min(6, value - 1));
		return '<div class="diff-cell"><div class="diff-ring">' +
			'<img class="diff-ring-bg" src="' + RING_ICON_DIR + 'ring_' + ring + '.png" alt="">' +
			'<img class="diff-icon" src="' + INSTRUMENT_ICON_DIR + iconName + '.png" alt="' + escapeHTML(part.label) + '" title="' + escapeHTML(part.label) + ' - ' + value + '/7">' +
			'</div></div>';
	}

	// buildSongExtra renders the album/year+length/genre lines shown next
	// to the album art, below the title/artist, only while the row is
	// expanded. Album comes from the live console fetch and is shown
	// whenever known; year/length/genre come from the song's metadata file
	// and are each shown only if that file supplied them.
	function buildSongExtra(song) {
		var el = document.createElement('div');
		el.className = 'song-extra';

		var lines = '';
		if (song.album) lines += '<span class="song-extra-line">' + escapeHTML(song.album) + '</span>';
		var bits = [];
		if (song.year) bits.push(song.year);
		var length = formatLength(song.lengthMs);
		if (length) bits.push(length);
		if (bits.length) lines += '<span class="song-extra-line">' + bits.join(' · ') + '</span>';
		if (song.genre) lines += '<span class="song-extra-line">' + escapeHTML(song.genre) + '</span>';

		el.innerHTML = '<div class="song-extra-inner">' + lines + '</div>';
		return el;
	}

	// buildSongPanel renders the expanded detail panel for song:
	// per-instrument difficulty, when the song has a metadata file -
	// song.genre is null otherwise, since that's only ever set alongside
	// the rest. (Album/genre/year/length appear next to the album art
	// instead - see buildSongExtra.)
	function buildSongPanel(song) {
		var panel = document.createElement('div');
		panel.className = 'song-panel';

		var html = '<div class="song-meta">';

		if (song.genre == null && song.year == null && song.lengthMs == null && song.vocalParts == null) {
			html += '<p class="muted">No additional metadata available for this song.</p>';
		} else {
			html += '<div class="diff-grid">';
			DIFFICULTY_PARTS.forEach(function (part) {
				html += diffCellHTML(song, part);
			});
			html += '</div>';
		}

		html += '</div>';
		panel.innerHTML = html;

		return panel;
	}

	// setRowExpanded shows/hides a song row's panel and extra-info elements
	// by animating max-height to their actual measured content height
	// (scrollHeight), rather than relying on a CSS-only intrinsic-sizing
	// trick (0fr grid rows) to collapse them - that approach kept leaving
	// the row taller than it should be, since flex/grid items' default
	// "automatic minimum size" fought the collapse in ways that were hard
	// to fully pin down without a real browser to check against. Measuring
	// the height directly sidesteps that entirely.
	function setRowExpanded(li, expand) {
		li.classList.toggle('expanded', expand);
		['.song-panel', '.song-extra'].forEach(function (selector) {
			var el = li.querySelector(selector);
			if (el) el.style.maxHeight = expand ? el.scrollHeight + 'px' : '0px';
		});
	}

	// expandRow opens li (building its panel/extra elements the first time
	// it's expanded, via the song stashed on it by renderSongList), closing
	// whatever row was previously expanded first, since only one song's
	// details are ever open at once.
	function expandRow(li) {
		if (li === expandedRow) return;
		if (!li.querySelector('.song-panel')) {
			li.appendChild(buildSongPanel(li._song));
			li.querySelector('.song-details').appendChild(buildSongExtra(li._song));
		}
		if (expandedRow) setRowExpanded(expandedRow, false);
		setRowExpanded(li, true);
		expandedRow = li;
	}

	function collapseExpandedRow() {
		if (!expandedRow) return;
		setRowExpanded(expandedRow, false);
		expandedRow = null;
	}

	// sortOrder is which field the song list is sorted by - changed via the
	// filters modal. It's either 'title', 'artist', 'source', or one of the
	// difficultyX song fields (see DIFFICULTY_PARTS). The list from the
	// server already comes sorted by title, so re-sorting only actually
	// reorders anything when this is something else.
	var sortOrder = 'title';

	// SORT_STRING_FIELDS lists the sortOrder values that compare as
	// case-insensitive strings - everything else is a numeric difficultyX
	// field, sorted lowest-first with title as a tiebreaker.
	var SORT_STRING_FIELDS = { title: true, artist: true, source: true };

	function sortedSongList() {
		var list = currentSongList.slice();
		var isString = !!SORT_STRING_FIELDS[sortOrder];
		list.sort(function (a, b) {
			if (isString) {
				var av = (a[sortOrder] || '').toLowerCase();
				var bv = (b[sortOrder] || '').toLowerCase();
				if (av < bv) return -1;
				if (av > bv) return 1;
				return 0;
			}
			var diff = (a[sortOrder] || 0) - (b[sortOrder] || 0);
			if (diff !== 0) return diff;
			var at = (a.title || '').toLowerCase();
			var bt = (b.title || '').toLowerCase();
			if (at < bt) return -1;
			if (at > bt) return 1;
			return 0;
		});
		return list;
	}

	// renderSongList records the latest song list from the server and
	// (re)renders it in the current sort/filter/search state. Call
	// recomputeFilteredSongs directly instead when only the sort order,
	// search box, or filter selections changed, since currentSongList
	// itself hasn't.
	function renderSongList(songs) {
		currentSongList = songs;
		recomputeFilteredSongs();
	}

	// buildSongRow creates one song-list <li>, wired up the same way
	// regardless of whether it's part of the initial page or a later batch
	// appended while scrolling (see appendSongBatch).
	function buildSongRow(song) {
		var li = document.createElement('li');
		li.className = 'song-row';
		// Stashed so expandRow can build this row's panel/extra elements
		// without needing its own closure over song.
		li._song = song;
		li.innerHTML =
			'<div class="song-info">' +
			'<img class="album-art" src="' + albumArtURL(song) + '" alt="" loading="lazy">' +
			'<div class="song-details">' +
			'<span class="song-title">' + escapeHTML(song.title || '(untitled)') + '</span>' +
			'<span class="song-artist">' + escapeHTML(song.artist) + '</span>' +
			'</div>' +
			'</div>' +
			'<div class="song-actions">' +
			'<img class="origin-icon" src="' + originIconURL(song.origin) + '" alt="' + escapeHTML(song.source) + '" title="' + escapeHTML(song.source) + '" loading="lazy">' +
			'<button class="button play-button">Play</button>' +
			'</div>';
		li.querySelector('.album-art').addEventListener('error', function () {
			this.onerror = null;
			this.src = BLANK_ALBUM_ART;
		});
		li.querySelector('.play-button').addEventListener('click', function (ev) {
			ev.stopPropagation();
			jumpToSong(song.shortname);
		});

		li.querySelector('.song-info').addEventListener('click', function () {
			if (li === expandedRow) {
				collapseExpandedRow();
			} else {
				expandRow(li);
			}
		});

		return li;
	}

	// SONG_PAGE_SIZE is how many song rows are rendered up front, and how
	// many more get appended each time the scroll sentinel comes into view -
	// the DOM only ever holds as many rows as have actually been scrolled
	// to, rather than the whole (possibly huge) matching list at once.
	var SONG_PAGE_SIZE = 10;

	// filteredSongs is currentSongList, sorted per sortOrder and narrowed by
	// the search box and filter modal - recomputed by recomputeFilteredSongs
	// whenever any of those change. renderedSongCount is how many of its
	// entries currently have a rendered <li>.
	var filteredSongs = [];
	var renderedSongCount = 0;

	// SONG_LOAD_MARGIN_PX is how far below the viewport the sentinel can be
	// and still count as "near the bottom" - both as songScrollObserver's
	// rootMargin and, below, as a manual re-check after each batch.
	var SONG_LOAD_MARGIN_PX = 600;

	// songSentinel is an always-present, invisible row whose only job is to
	// tell songScrollObserver when the user has scrolled near the bottom of
	// what's rendered so far, so the next batch can be appended before they
	// actually hit the end of the list.
	var songSentinel = document.createElement('li');
	songSentinel.className = 'song-sentinel';
	var songScrollObserver = new IntersectionObserver(function (entries) {
		if (entries[0].isIntersecting) appendSongBatch();
	}, { rootMargin: SONG_LOAD_MARGIN_PX + 'px 0px' });

	// appendSongBatch renders the next SONG_PAGE_SIZE not-yet-rendered
	// songs from filteredSongs (if any) and moves the sentinel/count row
	// back to the end, past what was just added.
	//
	// IntersectionObserver only notifies on enter/exit transitions, not on
	// every check - so if the sentinel is still within SONG_LOAD_MARGIN_PX
	// of the viewport after this batch (a tall viewport, or short rows,
	// versus a small SONG_PAGE_SIZE), it never "re-enters" and no further
	// notification would ever fire, silently stalling the list well short
	// of its full length. Checking its position directly and looping keeps
	// loading until it's actually out of range or everything's rendered.
	function appendSongBatch() {
		var next = filteredSongs.slice(renderedSongCount, renderedSongCount + SONG_PAGE_SIZE);
		next.forEach(function (song) {
			songlist.insertBefore(buildSongRow(song), songSentinel);
		});
		renderedSongCount += next.length;
		if (renderedSongCount >= filteredSongs.length) {
			songScrollObserver.unobserve(songSentinel);
			return;
		}
		if (songSentinel.getBoundingClientRect().top <= window.innerHeight + SONG_LOAD_MARGIN_PX) {
			appendSongBatch();
		}
	}

	function updateSongCountRow() {
		var countRow = songlist.querySelector('.song-count');
		if (!countRow) return;
		var total = currentSongList.length;
		var matches = filteredSongs.length;
		countRow.textContent = matches === total
			? 'Showing ' + total + ' songs'
			: 'Showing ' + matches + ' of ' + total + ' songs';
	}

	// resetSongListView rebuilds #songlist from scratch against the current
	// filteredSongs: the waiting message if there's no song list at all yet,
	// otherwise the first page of rows plus the scroll sentinel and count
	// row, ready for appendSongBatch to add more as the user scrolls.
	function resetSongListView() {
		songlist.innerHTML = '';
		expandedRow = null;
		renderedSongCount = 0;
		songScrollObserver.unobserve(songSentinel);

		if (currentSongList.length === 0) {
			var msg = document.createElement('li');
			msg.className = 'song-message';
			msg.textContent = 'Waiting for the song list… enter the Music Library to load it.';
			songlist.appendChild(msg);
			return;
		}

		songlist.appendChild(songSentinel);
		var countRow = document.createElement('li');
		countRow.className = 'song-count';
		songlist.appendChild(countRow);

		appendSongBatch();
		updateSongCountRow();
		if (renderedSongCount < filteredSongs.length) songScrollObserver.observe(songSentinel);
	}

	function jumpToSong(shortname) {
		fetch('/jump?shortname=' + encodeURIComponent(shortname));
	}

	// songDecade buckets song.year into a two-digit decade label ("90s",
	// "00s", "10s", ...), or null if the song has no known year.
	function songDecade(song) {
		if (!song.year) return null;
		var twoDigit = Math.floor(song.year / 10) * 10 % 100;
		return (twoDigit < 10 ? '0' : '') + twoDigit + 's';
	}

	// decadeSortKey orders decade labels chronologically (50s, 60s, ..., 90s,
	// 00s, 10s, 20s) rather than alphabetically, treating any two-digit
	// value under 50 as 2000+ and the rest as 1900+ - the range RB3 songs
	// actually span.
	function decadeSortKey(label) {
		var n = parseInt(label, 10);
		return n < 50 ? n + 100 : n;
	}

	// keysSupport reports whether song has a keys chart at all (difficultyKeys
	// 1-7), rather than being uncharted for that part (0).
	function keysSupport(song) {
		return song.difficultyKeys ? 'Yes' : 'No';
	}

	var KEYS_SUPPORT_ORDER = { Yes: 0, No: 1 };
	function keysSupportSortKey(label) {
		return KEYS_SUPPORT_ORDER[label];
	}

	// FILTER_DEFS lists the filter categories shown in the filters modal, in
	// display order. Each has a getter returning the song's value for that
	// category (null/undefined if the song has none), used both to build
	// the option list (from currentSongList) and to test a song against the
	// user's selections. sortKey, if set, orders that category's values by
	// the key it returns instead of alphabetically.
	var FILTER_DEFS = [
		{ key: 'genre', label: 'Genre', get: function (song) { return song.genre || null; } },
		{ key: 'decade', label: 'Decade', get: songDecade, sortKey: decadeSortKey },
		{ key: 'keysSupport', label: 'Keys Support', get: keysSupport, sortKey: keysSupportSortKey },
		{ key: 'source', label: 'Song Source', get: function (song) { return song.source || null; } }
	];

	// filterSelections holds the set of values selected for each filter
	// category (value -> true); an empty set means "All" (no filtering on
	// that category).
	var filterSelections = {};
	FILTER_DEFS.forEach(function (def) { filterSelections[def.key] = {}; });

	var currentFilterDef = null;

	// sortFilterValues orders values per def.sortKey if it has one,
	// alphabetically otherwise.
	function sortFilterValues(def, values) {
		if (def.sortKey) {
			return values.slice().sort(function (a, b) { return def.sortKey(a) - def.sortKey(b); });
		}
		return values.slice().sort();
	}

	// distinctFilterValues returns the de-duplicated values def.get finds
	// across the current song list, in display order - the option list
	// shown when the user opens that filter category.
	function distinctFilterValues(def) {
		var seen = {};
		currentSongList.forEach(function (song) {
			var value = def.get(song);
			if (value) seen[value] = true;
		});
		return sortFilterValues(def, Object.keys(seen));
	}

	function filterButtonLabel(def) {
		var selected = sortFilterValues(def, Object.keys(filterSelections[def.key]));
		return selected.length ? selected.join(', ') : 'All';
	}

	function updateFilterButton(def) {
		var btn = document.getElementById('filter-' + def.key + '-button');
		if (btn) btn.textContent = filterButtonLabel(def);
	}

	// songPassesFilters reports whether song matches every filter category's
	// selection (categories left as "All" always match).
	function songPassesFilters(song) {
		return FILTER_DEFS.every(function (def) {
			var selected = filterSelections[def.key];
			if (Object.keys(selected).length === 0) return true;
			var value = def.get(song);
			return value != null && !!selected[value];
		});
	}

	function openFilterOptionsModal(def) {
		currentFilterDef = def;
		filterOptionsTitle.textContent = def.label;
		var values = distinctFilterValues(def);
		var selected = filterSelections[def.key];
		filterOptionsList.innerHTML = '';
		if (values.length === 0) {
			var empty = document.createElement('p');
			empty.className = 'muted';
			empty.textContent = 'No ' + def.label.toLowerCase() + ' values found in the song list.';
			filterOptionsList.appendChild(empty);
		} else {
			values.forEach(function (value) {
				var row = document.createElement('label');
				row.className = 'filter-option-row';
				var checkbox = document.createElement('input');
				checkbox.type = 'checkbox';
				checkbox.checked = !!selected[value];
				checkbox.addEventListener('change', function () {
					if (checkbox.checked) {
						selected[value] = true;
					} else {
						delete selected[value];
					}
				});
				row.appendChild(checkbox);
				row.appendChild(document.createTextNode(' ' + value));
				filterOptionsList.appendChild(row);
			});
		}
		filtersModal.hidden = true;
		filterOptionsModal.hidden = false;
	}

	function closeFilterOptionsModal() {
		filterOptionsModal.hidden = true;
		filtersModal.hidden = false;
		if (currentFilterDef) {
			updateFilterButton(currentFilterDef);
			currentFilterDef = null;
		}
		collapseExpandedRow();
		recomputeFilteredSongs();
	}

	FILTER_DEFS.forEach(function (def) {
		var btn = document.getElementById('filter-' + def.key + '-button');
		if (btn) btn.addEventListener('click', function () { openFilterOptionsModal(def); });
	});

	// clearFiltersButton resets every filter category back to "All" without
	// touching sortOrder, which isn't itself a filter.
	clearFiltersButton.addEventListener('click', function () {
		FILTER_DEFS.forEach(function (def) {
			filterSelections[def.key] = {};
			updateFilterButton(def);
		});
		collapseExpandedRow();
		recomputeFilteredSongs();
	});

	filterOptionsClose.addEventListener('click', closeFilterOptionsModal);
	filterOptionsModal.addEventListener('click', function (ev) {
		if (ev.target === filterOptionsModal) closeFilterOptionsModal();
	});

	// computeFilteredSongs returns currentSongList, sorted per sortOrder and
	// narrowed to whatever matches both the search box and the filter
	// modal's category selections.
	function computeFilteredSongs() {
		var term = searchbox.value.trim().toLowerCase();
		return sortedSongList().filter(function (song) {
			var searchMatch = term.length < 3 ||
				(song.title + ' ' + song.artist + ' ' + song.album).toLowerCase().indexOf(term) !== -1;
			return searchMatch && songPassesFilters(song);
		});
	}

	// recomputeFilteredSongs re-runs the search/sort/filter pipeline and
	// resets the rendered list back down to the first page - called
	// whenever sortOrder, the search box, or a filter selection changes.
	function recomputeFilteredSongs() {
		filteredSongs = computeFilteredSongs();
		resetSongListView();
	}

	// Searching closes whatever song is currently open, then re-opens it
	// automatically if the search now narrows the list down to exactly one
	// song (which, being the only rendered row, is always songlist's first
	// .song-row after recomputeFilteredSongs resets the view).
	searchbox.addEventListener('input', function () {
		collapseExpandedRow();
		recomputeFilteredSongs();
		if (filteredSongs.length === 1) expandRow(songlist.querySelector('.song-row'));
	});

	function openFiltersModal() {
		filtersModal.hidden = false;
	}

	function closeFiltersModal() {
		filtersModal.hidden = true;
	}

	filtersButton.addEventListener('click', openFiltersModal);
	filtersClose.addEventListener('click', closeFiltersModal);
	filtersModal.addEventListener('click', function (ev) {
		if (ev.target === filtersModal) closeFiltersModal();
	});
	document.addEventListener('keydown', function (ev) {
		if (ev.key !== 'Escape') return;
		if (!filterOptionsModal.hidden) {
			closeFilterOptionsModal();
		} else if (!filtersModal.hidden) {
			closeFiltersModal();
		}
	});

	// Sort order applies immediately (no separate "Apply" step) and
	// re-renders the list in place, without needing a fresh copy from the
	// server.
	document.getElementById('sort-order-select').addEventListener('change', function (ev) {
		sortOrder = ev.target.value;
		recomputeFilteredSongs();
	});

	function connect() {
		var proto = location.protocol === 'https:' ? 'wss://' : 'ws://';
		var ws = new WebSocket(proto + location.host + '/ws?songVersion=' + lastSongListVersion);
		ws.onmessage = function (ev) {
			applyState(JSON.parse(ev.data));
		};
		ws.onclose = function () {
			statusDot.classList.remove('on');
			statusDot.classList.add('off');
			statusDot.title = 'Disconnected from server - retrying...';
			setTimeout(connect, 2000);
		};
	}
	connect();
})();
