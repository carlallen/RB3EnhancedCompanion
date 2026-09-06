// Applies the saved theme before first paint, to avoid a flash of the
// wrong theme. Must be loaded synchronously in <head>, before app.css.
(function () {
	'use strict';
	try {
		var theme = localStorage.getItem('rb3ec-theme');
		if (theme === 'light' || theme === 'dark') {
			document.documentElement.setAttribute('data-theme', theme);
		}
	} catch (e) {
		// localStorage unavailable (private mode, etc.) - fall back to system theme.
	}
})();
