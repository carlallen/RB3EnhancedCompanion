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

	var BLANK_ALBUM_ART = '/static/art/blank_album_art_keep.png';

	function albumArtURL(shortname) {
		return '/static/art/' + encodeURIComponent(shortname || '') + '_keep.png';
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
					currentSongOrigin.title = song.origin || '';
				}
				if (state.songShortName !== lastAlbumArtShortname) {
					lastAlbumArtShortname = state.songShortName;
					currentSongArt.src = albumArtURL(state.songShortName);
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

	// DIFFICULTY_PARTS lists the song-level difficulty fields (each 0-6,
	// from the song's metadata file), in display order: two rows of five,
	// each identified by its instrument icon rather than a text label.
	// vocals is special-cased (see vocalsIconName) since its icon depends
	// on the song's vocal part count.
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
	// its difficulty (0-6). If the song's metadata didn't chart that part
	// at all (value is null/undefined), there's no difficulty to show a
	// ring for, so it's just the icon on its own, dimmed.
	function diffCellHTML(song, part) {
		var value = song[part.key];
		var iconName = part.icon || vocalsIconName(song);
		if (value == null) {
			return '<div class="diff-cell"><div class="diff-ring">' +
				'<img class="diff-icon diff-icon-dim" src="' + INSTRUMENT_ICON_DIR + iconName + '.png" alt="' + escapeHTML(part.label) + '" title="' + escapeHTML(part.label) + ' - not charted">' +
				'</div></div>';
		}
		var ring = Math.max(0, Math.min(6, value));
		return '<div class="diff-cell"><div class="diff-ring">' +
			'<img class="diff-ring-bg" src="' + RING_ICON_DIR + 'ring_' + ring + '.png" alt="">' +
			'<img class="diff-icon" src="' + INSTRUMENT_ICON_DIR + iconName + '.png" alt="' + escapeHTML(part.label) + '" title="' + escapeHTML(part.label) + ' - ' + ring + '/6">' +
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

	function renderSongList(songs) {
		currentSongList = songs;
		songlist.innerHTML = '';
		expandedRow = null;

		if (songs.length === 0) {
			var msg = document.createElement('li');
			msg.className = 'song-message';
			msg.textContent = 'Waiting for the song list… enter the Music Library to load it.';
			songlist.appendChild(msg);
			return;
		}

		songs.forEach(function (song) {
			var li = document.createElement('li');
			li.className = 'song-row';
			li.dataset.search = (song.title + ' ' + song.artist + ' ' + song.album).toLowerCase();
			// Stashed so expandRow can build this row's panel/extra elements
			// without needing its own closure over song (it's also used by
			// the search box to auto-expand a single remaining result).
			li._song = song;
			li.innerHTML =
				'<div class="song-info">' +
				'<img class="album-art" src="' + albumArtURL(song.shortname) + '" alt="" loading="lazy">' +
				'<div class="song-details">' +
				'<span class="song-title">' + escapeHTML(song.title || '(untitled)') + '</span>' +
				'<span class="song-artist">' + escapeHTML(song.artist) + '</span>' +
				'</div>' +
				'</div>' +
				'<div class="song-actions">' +
				'<img class="origin-icon" src="' + originIconURL(song.origin) + '" alt="' + escapeHTML(song.origin) + '" title="' + escapeHTML(song.origin) + '" loading="lazy">' +
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

			songlist.appendChild(li);
		});

		var countRow = document.createElement('li');
		countRow.className = 'song-count';
		songlist.appendChild(countRow);

		applyFilter();
	}

	function jumpToSong(shortname) {
		fetch('/jump?shortname=' + encodeURIComponent(shortname));
	}

	// applyFilter shows/hides rows matching the search box, and returns the
	// sole matching row if exactly one matched (null otherwise).
	function applyFilter() {
		var term = searchbox.value.trim().toLowerCase();
		var rows = songlist.querySelectorAll('.song-row');
		var visible = 0;
		var soleMatch = null;
		rows.forEach(function (row) {
			var match = term.length < 3 || (row.dataset.search || '').indexOf(term) !== -1;
			row.style.display = match ? '' : 'none';
			if (match) {
				visible++;
				soleMatch = row;
			}
		});
		var countRow = songlist.querySelector('.song-count');
		if (countRow) {
			countRow.textContent = term.length < 3
				? 'Showing ' + rows.length + ' songs'
				: 'Showing ' + visible + ' of ' + rows.length + ' songs';
		}
		return visible === 1 ? soleMatch : null;
	}

	// Searching closes whatever song is currently open, then re-opens it
	// automatically if the search now narrows the list down to exactly one
	// song.
	searchbox.addEventListener('input', function () {
		collapseExpandedRow();
		var soleMatch = applyFilter();
		if (soleMatch) expandRow(soleMatch);
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
