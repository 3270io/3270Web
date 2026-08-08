/* ==========================================================================
   3270.io documentation theme — palette switch

   Injects the GRN / AMB / ICE / DAY segmented control into the Material
   header and keeps the choice in sync with 3270.io and the sibling docs site.

   Persistence is deliberately two-layered:
     • a cookie on `.3270.io`  — shared across 3270.io, 3270connect.3270.io
                                 and 3270web.3270.io, so the palette follows
                                 the reader between the three sites;
     • localStorage            — the fallback when the page is served from
                                 somewhere else (a local `mkdocs serve`, a
                                 preview deploy, or a file:// build).

   Keep this file byte-identical between the 3270Connect and 3270Web repos.
   ========================================================================== */

(function () {
  'use strict';

  var THEMES = [
    { id: 'phosphor', short: 'GRN', label: 'Phosphor green' },
    { id: 'amber', short: 'AMB', label: 'Amber CRT' },
    { id: 'ice', short: 'ICE', label: 'Ice' },
    { id: 'daylight', short: 'DAY', label: 'Daylight' }
  ];

  var KEY = '3270io:theme';
  var DEFAULT = 'phosphor';

  function isValid(id) {
    for (var i = 0; i < THEMES.length; i++) if (THEMES[i].id === id) return true;
    return false;
  }

  /* --- Storage ----------------------------------------------------------- */

  function readCookie() {
    var match = document.cookie.match(/(?:^|;\s*)3270io_theme=([^;]+)/);
    return match ? decodeURIComponent(match[1]) : null;
  }

  /** Widest domain we may legitimately write. On *.3270.io that is `.3270.io`
   *  so all three sites share one choice; anywhere else, the host itself. */
  function cookieDomain() {
    return /(^|\.)3270\.io$/i.test(location.hostname) ? '; domain=.3270.io' : '';
  }

  function read() {
    var fromCookie = readCookie();
    if (isValid(fromCookie)) return fromCookie;
    try {
      var stored = localStorage.getItem(KEY);
      if (isValid(stored)) return stored;
    } catch (e) { /* storage blocked — the default palette is fine */ }
    return DEFAULT;
  }

  function write(id) {
    try {
      document.cookie =
        '3270io_theme=' + encodeURIComponent(id) +
        '; path=/; max-age=31536000; samesite=lax' + cookieDomain() +
        (location.protocol === 'https:' ? '; secure' : '');
    } catch (e) { /* cookies blocked */ }
    try {
      localStorage.setItem(KEY, id);
    } catch (e) { /* storage blocked — the choice still applies this visit */ }
  }

  /* --- Apply -------------------------------------------------------------- */

  function apply(id) {
    // `data-theme` is the only switch: the stylesheet hangs all four palettes
    // off it, and `color-scheme` inside each block tells the browser how to
    // paint form controls and scrollbars. Material's own scheme attribute is
    // pinned to the inert value `3270` by mkdocs.yml and left alone here.
    document.documentElement.setAttribute('data-theme', id);
    sync(id);
  }

  function sync(id) {
    var buttons = document.querySelectorAll('.t3270-palette button');
    for (var i = 0; i < buttons.length; i++) {
      buttons[i].setAttribute(
        'aria-pressed',
        buttons[i].getAttribute('data-theme-id') === id ? 'true' : 'false'
      );
    }
  }

  /* --- Control ------------------------------------------------------------ */

  function makeGroup(variant) {
    var group = document.createElement('div');
    group.className = 't3270-palette' + (variant ? ' t3270-palette--' + variant : '');
    group.setAttribute('role', 'group');
    group.setAttribute('aria-label', 'Colour palette');

    THEMES.forEach(function (entry) {
      var button = document.createElement('button');
      button.type = 'button';
      button.textContent = entry.short;
      button.title = entry.label;
      button.setAttribute('data-theme-id', entry.id);
      button.setAttribute('aria-pressed', 'false');
      button.addEventListener('click', function () {
        write(entry.id);
        apply(entry.id);
      });
      group.appendChild(button);
    });

    return group;
  }

  /** Two instances, each shown at the width where it has room: the header
   *  control on desktop, and one inside the navigation drawer on phones,
   *  where the header has no space for four more targets. `sync()` addresses
   *  every button on the page, so the pair never drifts apart. */
  function build() {
    var header = document.querySelector('.md-header__inner');
    if (header && !header.querySelector('.t3270-palette')) {
      // Sit just before the search box so the control lands to the right of
      // the title and left of search/repo, matching the landing page's nav.
      var search = header.querySelector('.md-header__option, .md-search');
      var group = makeGroup('header');
      if (search) header.insertBefore(group, search);
      else header.appendChild(group);
    }

    var drawer = document.querySelector('.md-nav--primary');
    if (drawer && !drawer.querySelector('.t3270-palette')) {
      var title = drawer.querySelector(':scope > .md-nav__title');
      var nested = makeGroup('drawer');
      if (title && title.nextSibling) drawer.insertBefore(nested, title.nextSibling);
      else drawer.appendChild(nested);
    }

    linkTitle();
    sync(read());
  }

  /** Material renders the site name in the header as a bare <span>, so the only
   *  way home is the small logo icon beside it — not where anyone aims. Wrap
   *  the name in a link pointing wherever the logo already points, rather than
   *  hardcoding a root, so it stays correct under any base URL. Idempotent:
   *  build() re-runs on every instant navigation. */
  function linkTitle() {
    var topic = document.querySelector(
      '.md-header__title .md-header__topic:first-child .md-ellipsis'
    );
    if (!topic || topic.querySelector('a')) return;

    var logo = document.querySelector('.md-header__button.md-logo');
    if (!logo || !logo.getAttribute('href')) return;

    var link = document.createElement('a');
    link.className = 't3270-home-link';
    link.href = logo.getAttribute('href');
    link.textContent = topic.textContent.trim();
    topic.textContent = '';
    topic.appendChild(link);
  }

  /* --- Boot --------------------------------------------------------------- */

  apply(read());

  if (document.readyState !== 'loading') build();
  else document.addEventListener('DOMContentLoaded', build);

  // With navigation.instant, Material swaps the document without a reload.
  // `document$` fires on every such navigation; re-assert both the palette
  // and the control (build() is idempotent).
  if (typeof window.document$ !== 'undefined' && window.document$.subscribe) {
    window.document$.subscribe(function () {
      apply(read());
      build();
    });
  }
})();
