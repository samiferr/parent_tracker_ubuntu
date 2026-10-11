// Sets the theme before the page paints, so there is no flash. Lumen has two
// themes, light and dark; the default follows the operating system and a manual
// choice is kept in this browser only (docs/12, DS-4).
(function () {
  var choice = 'system';
  try { choice = localStorage.getItem('pt-theme') || 'system'; } catch (e) { /* storage may be unavailable */ }
  var dark = window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches;
  var theme = choice === 'light' || choice === 'dark' ? choice : (dark ? 'dark' : 'light');
  document.documentElement.setAttribute('data-theme', theme);
  document.documentElement.style.colorScheme = theme;
})();
