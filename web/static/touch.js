// Touch support: what a 3270 terminal needs on a device with no keyboard.
//
// Three things are missing on a tablet or a phone, and they are not styling
// problems.
//
// There is no Enter key and no PF3. Every 3270 screen ends with an AID key,
// so without one the terminal is read-only. The virtual keypad has always
// existed, but it is a desktop panel: it takes a third of a tablet's display
// and none of it is within a thumb's reach.
//
// There is no way to put the cursor on a protected cell. "Position the cursor
// beside your choice and press Enter" is a whole genre of mainframe screen,
// and with a keyboard it is an arrow key. With a finger it is a tap, and a tap
// on protected text did nothing at all.
//
// And every tap paid a browser's double-tap-to-zoom delay, which on a keypad
// is the difference between a terminal and a slideshow.
//
// The bar below is deliberately not a second keypad. It is the keys a screen
// actually ends with, sized for a thumb, docked where a thumb is — and it
// defers to window.sendKey for every one of them, so a key pressed here goes
// through exactly the path a key pressed on the physical keyboard does.
(function () {
  "use strict";

  var BAR_ID = "touch-action-bar";
  var KEYS_ID = "touch-action-bar-keys";
  var EXPANDED_KEY = "3270Web.touchBarExpanded";
  var COLLAPSED_KEY = "3270Web.touchBarCollapsed";
  var HEIGHT_VAR = "--touch-bar-height";

  // PRIMARY is what ends a screen: submit it, move between fields, clear it,
  // recover from an operator error. PF3 is here rather than in the drawer
  // because "go back" is the second thing anybody does on a green screen and
  // reaching it should not cost a tap.
  var PRIMARY = [
    { key: "Enter", label: "Enter", primary: true },
    { key: "Tab", label: "Tab" },
    { key: "BackTab", label: "⇤ Tab" },
    { key: "PF3", label: "PF3" },
    { key: "Clear", label: "Clear" },
    { key: "Reset", label: "Reset" },
  ];

  // The drawer: everything else, in one horizontally scrollable row. A phone
  // cannot show 24 function keys at a usable size, and shrinking them until it
  // can is how a keypad becomes untappable.
  function drawerKeys() {
    var keys = [];
    for (var i = 1; i <= 24; i++) {
      keys.push({ key: "PF" + i, label: "PF" + i });
    }
    keys.push({ key: "PA1", label: "PA1" });
    keys.push({ key: "PA2", label: "PA2" });
    keys.push({ key: "PA3", label: "PA3" });
    keys.push({ key: "Home", label: "Home" });
    keys.push({ key: "EraseEOF", label: "ErEOF" });
    keys.push({ key: "Insert", label: "Insert" });
    return keys;
  }

  function isTouchDevice() {
    if (!window.matchMedia) {
      return false;
    }
    // Coarse pointer, not "small screen" and not "has a touchscreen": a
    // laptop with a touchscreen still has a keyboard and does not want a
    // permanent bar across the bottom, and a narrow desktop window is still
    // a desktop.
    return window.matchMedia("(pointer: coarse)").matches;
  }

  function makeKeyButton(spec) {
    var btn = document.createElement("button");
    btn.type = "button";
    btn.className = "touch-key" + (spec.primary ? " touch-key--primary" : "");
    btn.dataset.key = spec.key;
    btn.textContent = spec.label;
    btn.setAttribute("aria-label", spec.key);

    // Without this the button takes focus, the field loses it, and two things
    // break at once: the software keyboard closes, and the key is sent with
    // the button as the active element rather than the field the operator was
    // typing into.
    btn.addEventListener("pointerdown", function (event) {
      event.preventDefault();
    });
    btn.addEventListener("click", function () {
      if (typeof window.sendKey === "function") {
        window.sendKey(spec.key);
      }
    });
    return btn;
  }

  function readFlag(key) {
    try {
      return window.localStorage.getItem(key);
    } catch (_) {
      // Private browsing; the caller falls back to its default.
      return null;
    }
  }

  function writeFlag(key, value) {
    try {
      window.localStorage.setItem(key, value);
    } catch (_) {
      // As above: the preference simply does not persist.
    }
  }

  // The collapse handle. The bar is the only permanently docked furniture on a
  // touch device, and a 3270 screen it covers is a screen an operator cannot
  // read — most visibly the AI chat panel, whose input sits at the bottom of
  // its own column and so lands underneath the keys. Collapsing is the same
  // gesture the terminal tools widget offers at the top of the screen: the
  // panel folds away and a labelled pill stays behind to bring it back.
  function makeHandle() {
    var handle = document.createElement("button");
    handle.type = "button";
    handle.className = "touch-bar-handle";
    handle.setAttribute("aria-controls", KEYS_ID);

    var icon = document.createElement("span");
    icon.className = "touch-bar-handle-icon";
    icon.setAttribute("aria-hidden", "true");

    var label = document.createElement("span");
    label.className = "touch-bar-handle-label";
    label.textContent = "Keys";

    handle.appendChild(icon);
    handle.appendChild(label);

    // Same reason as the keys: taking focus would close the software keyboard
    // and move the active element off the field being typed into.
    handle.addEventListener("pointerdown", function (event) {
      event.preventDefault();
    });
    return handle;
  }

  function setCollapsed(bar, handle, collapsed) {
    bar.classList.toggle("is-collapsed", collapsed);
    handle.setAttribute("aria-expanded", collapsed ? "false" : "true");
    var label = collapsed ? "Show terminal keys" : "Hide terminal keys";
    handle.setAttribute("aria-label", label);
    handle.setAttribute("title", label);
  }

  function buildBar() {
    var bar = document.createElement("div");
    bar.id = BAR_ID;
    bar.className = "touch-action-bar";
    bar.setAttribute("role", "toolbar");
    bar.setAttribute("aria-label", "Terminal keys");

    var handleRow = document.createElement("div");
    handleRow.className = "touch-bar-handle-row";
    var handle = makeHandle();
    handleRow.appendChild(handle);
    bar.appendChild(handleRow);

    var keys = document.createElement("div");
    keys.id = KEYS_ID;
    keys.className = "touch-bar-keys";
    bar.appendChild(keys);

    var row = document.createElement("div");
    row.className = "touch-key-row touch-key-row--primary";
    for (var i = 0; i < PRIMARY.length; i++) {
      row.appendChild(makeKeyButton(PRIMARY[i]));
    }

    var more = document.createElement("button");
    more.type = "button";
    more.className = "touch-key touch-key--more";
    more.textContent = "PF…";
    more.setAttribute("aria-expanded", "false");
    more.setAttribute("aria-label", "More keys");
    row.appendChild(more);
    keys.appendChild(row);

    var drawer = document.createElement("div");
    drawer.className = "touch-key-row touch-key-row--drawer";
    drawer.hidden = true;
    var specs = drawerKeys();
    for (var j = 0; j < specs.length; j++) {
      drawer.appendChild(makeKeyButton(specs[j]));
    }
    keys.appendChild(drawer);

    more.addEventListener("pointerdown", function (event) {
      event.preventDefault();
    });
    more.addEventListener("click", function () {
      var nowOpen = drawer.hidden;
      drawer.hidden = !nowOpen;
      more.setAttribute("aria-expanded", nowOpen ? "true" : "false");
      writeFlag(EXPANDED_KEY, nowOpen ? "1" : "0");
    });

    if (readFlag(EXPANDED_KEY) === "1") {
      drawer.hidden = false;
      more.setAttribute("aria-expanded", "true");
    }

    // Expanded by default: the keys are the reason the bar exists, and an
    // operator who has never collapsed it should not have to find the handle
    // before they can press Enter.
    setCollapsed(bar, handle, readFlag(COLLAPSED_KEY) === "1");
    handle.addEventListener("click", function () {
      var nextCollapsed = !bar.classList.contains("is-collapsed");
      setCollapsed(bar, handle, nextCollapsed);
      writeFlag(COLLAPSED_KEY, nextCollapsed ? "1" : "0");
    });

    return bar;
  }

  // How much of the bottom of the viewport the bar is standing on, published
  // as a custom property so anything else docked down there can get out of the
  // way. A fixed number would not do: the height changes when the PF drawer
  // opens, when the bar is collapsed, when the device rotates, and it has to
  // include the software keyboard the bar is riding on top of — the AI chat
  // panel's input is exactly the thing that ends up underneath otherwise.
  var occludedPx = 0;

  function publishHeight(bar) {
    var height = bar.offsetHeight + occludedPx;
    document.documentElement.style.setProperty(HEIGHT_VAR, height + "px");
  }

  function trackHeight(bar) {
    var republish = function () {
      publishHeight(bar);
    };
    if (window.ResizeObserver) {
      new window.ResizeObserver(republish).observe(bar);
    } else {
      // No ResizeObserver: the drawer and collapse toggles are the only things
      // that change the height under their own steam, so catch them on the way
      // back out of the click handlers.
      bar.addEventListener("click", function () {
        window.setTimeout(republish, 0);
      });
    }
    window.addEventListener("resize", republish);
    window.addEventListener("orientationchange", republish);
    republish();
  }

  // keepAboveSoftwareKeyboard tracks the visual viewport so the bar sits on
  // top of the software keyboard rather than under it. Without this, the
  // moment a field is focused the keys are behind the keyboard that focusing
  // it opened — which is exactly when they are needed.
  function keepAboveSoftwareKeyboard(bar) {
    var vv = window.visualViewport;
    if (!vv) {
      return;
    }
    function reposition() {
      occludedPx = Math.max(0, window.innerHeight - vv.height - vv.offsetTop);
      bar.style.transform = occludedPx > 0 ? "translateY(-" + occludedPx + "px)" : "";
      publishHeight(bar);
    }
    vv.addEventListener("resize", reposition);
    vv.addEventListener("scroll", reposition);
    reposition();
  }

  // Tap-to-place-cursor. A tap that lands on an input is the browser's to
  // handle — that is how the software keyboard opens — so only taps on the
  // protected parts of the screen come here, which is precisely the case a
  // touch device had no answer for.
  //
  // This is the fallback path. keyboard.js answers the same tap with a free
  // cursor: it paints a caret on the cell, keeps DOM focus where it was, and
  // reports the position to the host with the next AID key. That is one host
  // round trip, at the moment the operator presses Enter.
  //
  // When both ran, one tap became two host mutations. The POST below moves the
  // real cursor immediately and the AID key that follows carries the free
  // cursor's position as well, so a quick tap-then-Enter is a race: which of
  // the two the host acts on depends on whether the POST landed first, and the
  // screen that comes back can have the cursor somewhere neither of them meant.
  // The refresh is its own cost — it replaces the screen's markup and takes
  // focus with it, closing the software keyboard mid-transaction.
  //
  // So this defers when the free cursor is present, and stands in when it is
  // not.
  function hasFreeCursor() {
    return !!(window.ThreeSeventyWeb && window.ThreeSeventyWeb.freeCursorInstalled);
  }

  function installCursorTap() {
    var api = window.ThreeSeventyWeb && window.ThreeSeventyWeb.screenGrid;
    if (!api) {
      return;
    }
    var container = api.container();
    if (!container) {
      return;
    }
    // A screen refresh replaces the container's innerHTML but not the
    // container, so the listener outlives the screen it was installed over —
    // and installing a second one would send two cursor moves per tap.
    if (container.dataset.touchCursorTap === "1") {
      return;
    }
    container.dataset.touchCursorTap = "1";

    container.addEventListener("click", function (event) {
      // Checked per tap rather than once at install time: which script has
      // finished running by DOMContentLoaded is not something to depend on,
      // and by the first tap both have.
      if (hasFreeCursor()) {
        return;
      }
      var target = event.target;
      if (!target || target.tagName === "INPUT" || target.closest("input")) {
        return;
      }
      var metrics = api.metrics();
      if (!metrics) {
        return;
      }
      var cell = api.pointToCell(metrics, event.clientX, event.clientY);
      fetch("/screen/cursor", {
        method: "POST",
        credentials: "same-origin",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ row: cell.row, col: cell.col }),
      })
        .then(function () {
          if (typeof window.refreshScreenContent === "function") {
            window.refreshScreenContent();
          }
        })
        .catch(function () {
          // A tap that could not be delivered is not worth a dialog; the
          // cursor simply stays where it was, which is what the operator can
          // already see.
        });
    });
  }

  document.addEventListener("DOMContentLoaded", function () {
    if (!isTouchDevice()) {
      return;
    }
    document.body.classList.add("touch-device");

    // The connect page has no terminal to send keys to.
    if (!document.querySelector(".screen-container")) {
      // Say so, rather than leaving --touch-bar-height unset. The page
      // clearance rule falls back to a bar-sized number when the property is
      // missing, which on a page with no bar is a strip of empty space below
      // the connect form — most of a phone's remaining height on a short
      // screen, reserved for furniture that was never added.
      document.documentElement.style.setProperty(HEIGHT_VAR, "0px");
      return;
    }

    var bar = buildBar();
    document.body.appendChild(bar);
    trackHeight(bar);
    keepAboveSoftwareKeyboard(bar);
    installCursorTap();

    // A screen refresh replaces the container's contents, taking the click
    // handler with it.
    var api = window.ThreeSeventyWeb && window.ThreeSeventyWeb.screenGrid;
    if (api && typeof api.onScreenReplaced === "function") {
      api.onScreenReplaced(installCursorTap);
    }
  });
})();
