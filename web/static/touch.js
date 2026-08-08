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
  var EXPANDED_KEY = "3270Web.touchBarExpanded";

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

  function buildBar() {
    var bar = document.createElement("div");
    bar.id = BAR_ID;
    bar.className = "touch-action-bar";
    bar.setAttribute("role", "toolbar");
    bar.setAttribute("aria-label", "Terminal keys");

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
    bar.appendChild(row);

    var drawer = document.createElement("div");
    drawer.className = "touch-key-row touch-key-row--drawer";
    drawer.hidden = true;
    var specs = drawerKeys();
    for (var j = 0; j < specs.length; j++) {
      drawer.appendChild(makeKeyButton(specs[j]));
    }
    bar.appendChild(drawer);

    more.addEventListener("pointerdown", function (event) {
      event.preventDefault();
    });
    more.addEventListener("click", function () {
      var nowOpen = drawer.hidden;
      drawer.hidden = !nowOpen;
      more.setAttribute("aria-expanded", nowOpen ? "true" : "false");
      try {
        window.localStorage.setItem(EXPANDED_KEY, nowOpen ? "1" : "0");
      } catch (_) {
        // Private browsing; the drawer simply does not persist.
      }
    });

    try {
      if (window.localStorage.getItem(EXPANDED_KEY) === "1") {
        drawer.hidden = false;
        more.setAttribute("aria-expanded", "true");
      }
    } catch (_) {
      // As above.
    }

    return bar;
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
      var occluded = Math.max(0, window.innerHeight - vv.height - vv.offsetTop);
      bar.style.transform = occluded > 0 ? "translateY(-" + occluded + "px)" : "";
    }
    vv.addEventListener("resize", reposition);
    vv.addEventListener("scroll", reposition);
    reposition();
  }

  // Tap-to-place-cursor. A tap that lands on an input is the browser's to
  // handle — that is how the software keyboard opens — so only taps on the
  // protected parts of the screen come here, which is precisely the case a
  // touch device had no answer for.
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
      return;
    }

    var bar = buildBar();
    document.body.appendChild(bar);
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
