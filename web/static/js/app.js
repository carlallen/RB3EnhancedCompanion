(function () {
	'use strict';

	var statusDot = document.getElementById('topbar-status');
	var playerCard = document.getElementById('player-card');
	var currentSong = document.getElementById('current-song');
	var currentSongTitle = document.getElementById('current-song-title');
	var currentSongArtist = document.getElementById('current-song-artist');
	var currentSongOrigin = document.getElementById('current-song-origin');
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

	function applyState(state) {
		statusDot.classList.toggle('on', state.connected);
		statusDot.classList.toggle('off', !state.connected);
		statusDot.title = state.connected
			? 'Connected' + (state.platform ? ' (' + state.platform + ')' : '')
			: 'Waiting for RB3Enhanced...';

		document.body.classList.toggle('on-song-select', state.screenName === 'song_select_screen');

		var inGame = state.connected && state.inGame;
		playerCard.hidden = !inGame;

		if (state.connected && state.songName) {
			currentSong.hidden = false;
			currentSongTitle.textContent = state.songName;
			currentSongArtist.textContent = state.songArtist || '';
			currentSongArtist.hidden = !inGame;
			currentSongOrigin.hidden = !inGame;
			if (inGame) {
				var song = findSong(state.songShortName);
				if (song) {
					currentSongOrigin.src = originIconURL(song.origin);
					currentSongOrigin.title = song.origin || '';
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

	function renderSongList(songs) {
		currentSongList = songs;
		songlist.innerHTML = '';

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
			li.innerHTML =
				'<div class="song-info">' +
				'<div class="song-details">' +
				'<span class="song-title">' + escapeHTML(song.title || '(untitled)') + '</span>' +
				'<span class="song-artist">' + escapeHTML(song.artist) + '</span>' +
				'</div>' +
				'</div>' +
				'<div class="song-actions">' +
				'<img class="origin-icon" src="' + originIconURL(song.origin) + '" alt="' + escapeHTML(song.origin) + '" title="' + escapeHTML(song.origin) + '" loading="lazy">' +
				'<button class="button play-button">Play</button>' +
				'</div>';
			li.querySelector('.play-button').addEventListener('click', function () {
				jumpToSong(song.shortname);
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

	function applyFilter() {
		var term = searchbox.value.trim().toLowerCase();
		var rows = songlist.querySelectorAll('.song-row');
		var visible = 0;
		rows.forEach(function (row) {
			var match = term.length < 3 || (row.dataset.search || '').indexOf(term) !== -1;
			row.style.display = match ? '' : 'none';
			if (match) visible++;
		});
		var countRow = songlist.querySelector('.song-count');
		if (countRow) {
			countRow.textContent = term.length < 3
				? 'Showing ' + rows.length + ' songs'
				: 'Showing ' + visible + ' of ' + rows.length + ' songs';
		}
	}

	searchbox.addEventListener('input', applyFilter);

	function connect() {
		var proto = location.protocol === 'https:' ? 'wss://' : 'ws://';
		var ws = new WebSocket(proto + location.host + '/ws');
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
