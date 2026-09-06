(function () {
	'use strict';

	var STORAGE_KEY = 'rb3ec-theme';
	var buttons = document.querySelectorAll('#theme-options .theme-option');

	function currentTheme() {
		try {
			var stored = localStorage.getItem(STORAGE_KEY);
			return stored === 'light' || stored === 'dark' ? stored : 'system';
		} catch (e) {
			return 'system';
		}
	}

	function applyTheme(theme) {
		if (theme === 'light' || theme === 'dark') {
			document.documentElement.setAttribute('data-theme', theme);
		} else {
			document.documentElement.removeAttribute('data-theme');
		}
	}

	function setTheme(theme) {
		try {
			if (theme === 'system') {
				localStorage.removeItem(STORAGE_KEY);
			} else {
				localStorage.setItem(STORAGE_KEY, theme);
			}
		} catch (e) {
			// localStorage unavailable - theme still applies for this page view.
		}
		applyTheme(theme);
		highlightSelected(theme);
	}

	function highlightSelected(theme) {
		buttons.forEach(function (btn) {
			btn.classList.toggle('selected', btn.dataset.themeValue === theme);
		});
	}

	buttons.forEach(function (btn) {
		btn.addEventListener('click', function () {
			setTheme(btn.dataset.themeValue);
		});
	});

	highlightSelected(currentTheme());
})();
