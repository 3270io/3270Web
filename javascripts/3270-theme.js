/* ==========================================================================
   3270.io documentation theme — palette switch and tab strip

   Injects the GRN / AMB / ICE / DAY segmented control into the Material
   header and keeps the choice in sync with 3270.io and the sibling docs site.
   Also makes the product tab strip reachable on a desktop, where Material
   leaves the overflowing tabs scrollable but with nothing to scroll them by
   — see the tab strip section below.

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
    setupTabs();
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

  /* --- Tab strip ----------------------------------------------------------
     Material's tab strip is `overflow-x: auto` with `scrollbar-width: none`.
     With a nav this long that puts most of the pages past the right edge with
     no way to get at them on a desktop: no scrollbar to drag, no arrows, and
     a mouse wheel that reports only a vertical delta. The three things below
     are what was missing — a stepper at each end, the wheel mapped onto the
     strip, and the current page scrolled into view on arrival. The stylesheet
     shows the steppers and fades the cut edges off `data-t3270-overflow`,
     which is set here and left off entirely when the whole nav fits. */

  var SVG_NS = 'http://www.w3.org/2000/svg';

  var CHEVRON = {
    prev: 'M15.41 16.58 10.83 12l4.58-4.58L14 6l-6 6 6 6z',
    next: 'M8.59 16.58 13.17 12 8.59 7.42 10 6l6 6-6 6z'
  };

  function chevron(direction) {
    var svg = document.createElementNS(SVG_NS, 'svg');
    svg.setAttribute('viewBox', '0 0 24 24');
    svg.setAttribute('aria-hidden', 'true');
    svg.setAttribute('focusable', 'false');
    var path = document.createElementNS(SVG_NS, 'path');
    path.setAttribute('d', CHEVRON[direction]);
    svg.appendChild(path);
    return svg;
  }

  function reducedMotion() {
    return !!(window.matchMedia &&
      window.matchMedia('(prefers-reduced-motion: reduce)').matches);
  }

  /** Publish how much strip is left at each end. `end` means the tabs run off
   *  to the right and we are at the left of them; `start` is the mirror; and
   *  no attribute at all means the nav fits and none of this applies — on a
   *  phone the strip is `display: none`, which lands here as a fit. */
  function measureTabs(tabs, list) {
    // Sub-pixel layout leaves a stray pixel of slack on a strip that fits.
    var slack = Math.max(0, list.scrollWidth - list.clientWidth);
    var atStart = slack <= 2 || list.scrollLeft <= 1;
    var atEnd = slack <= 2 || list.scrollLeft >= slack - 1;

    if (slack <= 2) tabs.removeAttribute('data-t3270-overflow');
    else tabs.setAttribute('data-t3270-overflow', atStart ? 'end' : atEnd ? 'start' : 'both');

    var steppers = tabs.querySelectorAll('.t3270-tabscroll');
    for (var i = 0; i < steppers.length; i++) {
      steppers[i].disabled =
        steppers[i].classList.contains('t3270-tabscroll--prev') ? atStart : atEnd;
    }
  }

  /** Put the page you are on back on screen. Opening a page from halfway down
   *  the nav otherwise lands on a strip scrolled hard left with no tab marked
   *  — the reader's own position being one more thing hidden past the edge.
   *  Jumps rather than glides: this runs on arrival, and a strip that slides
   *  sideways as the page appears reads as a fault rather than as a hint. */
  function revealActiveTab(list) {
    var active = list.querySelector('.md-tabs__item--active');
    if (!active || list.scrollWidth <= list.clientWidth) return;
    var item = active.getBoundingClientRect();
    var strip = list.getBoundingClientRect();
    // Centred, so the tabs either side show where in the nav this page sits.
    list.scrollLeft += (item.left - strip.left) - (strip.width - item.width) / 2;
  }

  function setupTabs() {
    var tabs = document.querySelector('[data-md-component="tabs"]');
    var list = tabs && tabs.querySelector('.md-tabs__list');
    if (!list) return;

    // Material replaces the whole tabs element on an instant navigation, so
    // the already-done guard is a property of the element rather than a flag
    // up here: a replacement strip arrives without it and is fitted out again.
    if (!list.t3270Stepped) {
      list.t3270Stepped = true;

      ['prev', 'next'].forEach(function (direction) {
        var button = document.createElement('button');
        button.type = 'button';
        button.className = 't3270-tabscroll t3270-tabscroll--' + direction;
        // Every tab is a Tab stop already and browsers scroll a focused link
        // into view, so the far end of the strip is reachable from the
        // keyboard without these. They are a pointer affordance: two extra
        // stops duplicating a route that already works would cost a keyboard
        // reader more than they give, so they stay out of the focus order and
        // out of the accessibility tree.
        button.tabIndex = -1;
        button.setAttribute('aria-hidden', 'true');
        button.appendChild(chevron(direction));
        button.addEventListener('click', function () {
          // Most of a screenful, with an overlap so whichever tab straddles
          // the seam is still whole after the step.
          var step = Math.max(160, list.clientWidth * 0.8);
          list.scrollBy({
            left: direction === 'prev' ? -step : step,
            behavior: reducedMotion() ? 'auto' : 'smooth'
          });
        });
        list.parentNode.insertBefore(button, direction === 'prev' ? list : list.nextSibling);
      });

      list.addEventListener('scroll', function () { measureTabs(tabs, list); }, { passive: true });

      // A mouse has one axis, so without this the strip ignores the only
      // input most desktop readers have for it. The page keeps the wheel once
      // the strip has run out of travel that way, so hovering the tabs on the
      // way down the page never swallows a scroll.
      list.addEventListener('wheel', function (event) {
        if (!event.deltaY || event.shiftKey) return;
        var slack = list.scrollWidth - list.clientWidth;
        if (event.deltaY < 0 && list.scrollLeft <= 0) return;
        if (event.deltaY > 0 && list.scrollLeft >= slack) return;
        event.preventDefault();
        list.scrollLeft += event.deltaY;
      }, { passive: false });

      if (window.ResizeObserver) {
        new ResizeObserver(function () { measureTabs(tabs, list); }).observe(list);
      } else {
        window.addEventListener('resize', function () { measureTabs(tabs, list); });
      }

      // Tab widths are not final until the web fonts land, and the swap can
      // take a strip that fitted and push it over the edge.
      if (document.fonts && document.fonts.ready) {
        document.fonts.ready.then(function () { measureTabs(tabs, list); });
      }
    }

    // Measure first: showing the steppers narrows the strip, and the reveal
    // below has to centre against the width the reader will actually see.
    measureTabs(tabs, list);
    revealActiveTab(list);
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
