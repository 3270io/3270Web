// Focus mode — the terminal, filling the screen, and almost nothing else.
//
// The default layout spends a lot of vertical space on things a business user
// never reads: a page header, a card border, an application banner. An
// operator working a green screen all day wants the screen.
//
// What makes this better than a desktop emulator's full-screen: Quick3270 and
// PCOMM can only fill their own window, and their toolbars either stay or
// vanish entirely. Here the real toolbar is *relocated* by CSS into a rail
// that hides until you reach for it — no duplicate buttons, so every control,
// tooltip, keyboard handler and command-palette entry keeps working exactly
// as it does in the normal layout. And the browser's Fullscreen API means it
// fills the display, not a window.
//
// Alt+Enter is the toggle, which is what PCOMM and Quick3270 have used for
// full screen for decades.
(function () {
  "use strict";

  var STORAGE_KEY = "3270Web.focusMode.v1";
  var HINT_SHOWN_KEY = "3270Web.focusModeHintSeen.v1";
  // How close to the top edge the pointer must come before the rail appears.
  // Wide enough to reach for without aiming, narrow enough not to trigger
  // while working the top rows of the screen.
  var REVEAL_BAND_PX = 64;

  var active = false;
  var railPinned = false;
  var hideTimer = 0;

  function read() {
    try {
      return localStorage.getItem(STORAGE_KEY) === "1";
    } catch (_) {
      return false;
    }
  }

  function write(on) {
    try {
      localStorage.setItem(STORAGE_KEY, on ? "1" : "0");
    } catch (_) { /* private browsing; the session still works */ }
  }

  function notify(message, type, options) {
    if (window.ThreeSeventyWeb && typeof window.ThreeSeventyWeb.notify === "function") {
      window.ThreeSeventyWeb.notify(message, type, options);
    }
  }

  // refit asks the terminal to grow into whatever space it now has. Clicking
  // the existing fit control rather than reimplementing the maths keeps one
  // definition of "fit" in the codebase.
  function refit() {
    window.dispatchEvent(new CustomEvent("h3270:layout-changed", { detail: { source: "focus" } }));
    window.requestAnimationFrame(function () {
      var fit = document.querySelector("[data-terminal-fit]");
      if (fit) {
        fit.click();
      }
      if (typeof window.sizeScreenContainer === "function") {
        window.sizeScreenContainer();
      }
    });
  }

  function showRail(sticky) {
    document.body.setAttribute("data-focus-rail", "shown");
    if (hideTimer) {
      window.clearTimeout(hideTimer);
      hideTimer = 0;
    }
    if (sticky) {
      // Give a newcomer time to read the rail before it slides away.
      hideTimer = window.setTimeout(hideRail, 2600);
    }
  }

  function hideRail() {
    if (railPinned) {
      return;
    }
    // Never yank the rail away from someone who is using it.
    var focused = document.activeElement;
    if (focused && typeof focused.closest === "function" && focused.closest("[data-focus-rail-zone]")) {
      return;
    }
    document.body.setAttribute("data-focus-rail", "hidden");
    if (hideTimer) {
      window.clearTimeout(hideTimer);
      hideTimer = 0;
    }
  }

  function syncToggleButtons() {
    var nodes = document.querySelectorAll("[data-focus-toggle]");
    for (var i = 0; i < nodes.length; i++) {
      var el = nodes[i];
      el.setAttribute("aria-pressed", String(active));
      var hint = active ? "Exit focus mode (Alt+Enter)" : "Focus mode — fill the screen (Alt+Enter)";
      el.setAttribute("aria-label", hint);
      el.setAttribute("title", hint);
      if (el.hasAttribute("data-tippy-content")) {
        el.setAttribute("data-tippy-content", hint);
      }
      if (el._tippy && typeof el._tippy.setContent === "function") {
        el._tippy.setContent(hint);
      }
    }
  }

  function requestFullscreen() {
    var el = document.documentElement;
    var fn = el.requestFullscreen || el.webkitRequestFullscreen;
    if (!fn) {
      return;
    }
    try {
      var result = fn.call(el);
      if (result && typeof result.catch === "function") {
        // Denied (no user gesture, or a policy blocks it). Focus mode still
        // fills the viewport, so this is a downgrade, not a failure.
        result.catch(function () {});
      }
    } catch (_) { /* same */ }
  }

  function exitFullscreen() {
    if (!document.fullscreenElement && !document.webkitFullscreenElement) {
      return;
    }
    var fn = document.exitFullscreen || document.webkitExitFullscreen;
    if (!fn) {
      return;
    }
    try {
      var result = fn.call(document);
      if (result && typeof result.catch === "function") {
        result.catch(function () {});
      }
    } catch (_) { /* nothing useful to do */ }
  }

  function apply(on, options) {
    var opts = options || {};
    active = !!on;
    document.body.setAttribute("data-focus", active ? "on" : "off");
    syncToggleButtons();

    if (active) {
      showRail(true);
      if (opts.userGesture) {
        requestFullscreen();
      }
      if (opts.announce) {
        announceOnce();
      }
    } else {
      document.body.removeAttribute("data-focus-rail");
      railPinned = false;
      if (opts.userGesture) {
        exitFullscreen();
      }
    }
    refit();
  }

  // announceOnce tells a first-time user how to get back out. A mode you
  // cannot see your way out of is a trap, and the rail is hidden by design.
  function announceOnce() {
    var seen = false;
    try {
      seen = localStorage.getItem(HINT_SHOWN_KEY) === "1";
    } catch (_) { /* treat as unseen */ }
    if (seen) {
      return;
    }
    notify(
      "Focus mode. Move the pointer to the top of the screen for the toolbar, or press Alt+Enter to leave.",
      "info",
      { duration: 6000 }
    );
    try {
      localStorage.setItem(HINT_SHOWN_KEY, "1");
    } catch (_) { /* nothing to do */ }
  }

  function toggle(userGesture) {
    var next = !active;
    write(next);
    apply(next, { userGesture: userGesture !== false, announce: next });
  }

  document.addEventListener("DOMContentLoaded", function () {
    if (!document.querySelector(".terminal-shell")) {
      return;
    }

    // Restoring focus mode on load cannot request fullscreen — that needs a
    // user gesture — so the layout applies and fullscreen waits for the next
    // deliberate toggle.
    apply(read(), { userGesture: false, announce: false });

    var toggles = document.querySelectorAll("[data-focus-toggle]");
    for (var i = 0; i < toggles.length; i++) {
      toggles[i].addEventListener("click", function () {
        toggle(true);
      });
    }

    // Reveal the rail when the pointer reaches for the top edge.
    document.addEventListener("pointermove", function (event) {
      if (!active) {
        return;
      }
      if (event.clientY <= REVEAL_BAND_PX) {
        showRail(false);
      } else if (document.body.getAttribute("data-focus-rail") === "shown") {
        hideRail();
      }
    });

    // Keyboard users get the rail by tabbing into it.
    document.addEventListener("focusin", function (event) {
      if (!active) {
        return;
      }
      if (event.target && typeof event.target.closest === "function" && event.target.closest("[data-focus-rail-zone]")) {
        showRail(false);
      }
    });

    // Keep it up while a menu or dialog launched from it is open, otherwise
    // it slides away the moment you move the pointer down to the thing you
    // just opened.
    document.addEventListener("pointerdown", function (event) {
      if (!active) {
        return;
      }
      if (event.target && typeof event.target.closest === "function" && event.target.closest("[data-focus-rail-zone]")) {
        railPinned = true;
        showRail(false);
        window.setTimeout(function () {
          railPinned = false;
        }, 1200);
      }
    });

    // Alt+Enter, the full-screen shortcut PCOMM and Quick3270 have used for
    // decades. Claimed before the terminal's own Enter handling, which would
    // otherwise send an AID key to the host.
    document.addEventListener(
      "keydown",
      function (event) {
        if (!event.altKey || event.ctrlKey || event.metaKey || event.shiftKey) {
          return;
        }
        if (event.key !== "Enter") {
          return;
        }
        event.preventDefault();
        event.stopPropagation();
        toggle(true);
      },
      true
    );

    // Leaving browser fullscreen (Esc, F11) should leave focus mode too —
    // otherwise the operator escapes the fullscreen and is still staring at a
    // chromeless page wondering where the toolbar went.
    var onFullscreenChange = function () {
      var isFull = !!(document.fullscreenElement || document.webkitFullscreenElement);
      if (!isFull && active) {
        write(false);
        apply(false, { userGesture: false, announce: false });
      }
    };
    document.addEventListener("fullscreenchange", onFullscreenChange);
    document.addEventListener("webkitfullscreenchange", onFullscreenChange);

    window.addEventListener("resize", function () {
      if (active) {
        refit();
      }
    });
  });

  window.ThreeSeventyWeb = window.ThreeSeventyWeb || {};
  window.ThreeSeventyWeb.focusMode = {
    isActive: function () {
      return active;
    },
    toggle: function () {
      toggle(true);
    },
    enter: function () {
      if (!active) {
        toggle(true);
      }
    },
    exit: function () {
      if (active) {
        toggle(true);
      }
    }
  };
})();
