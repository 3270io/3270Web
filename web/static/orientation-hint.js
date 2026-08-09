// Suggest landscape when a phone is holding a 3270 screen sideways-on.
//
// A 3270 screen is 80 columns wide and that is not negotiable — it is what the
// host sends. On a phone held upright those 80 columns are fitted into about
// 320 CSS pixels, which works out at a four-pixel character cell: legible if
// you hold it close, and not something anybody would choose. Turned sideways
// the same screen gets half again as much width.
//
// So this suggests it. What it does not do matters more than what it does:
//
//   - It never blocks. A "rotate your device" wall over the terminal would
//     lock out everybody whose rotation is locked, whose device is mounted, or
//     who is holding a phone one-handed on purpose — and the terminal behind
//     it works perfectly well.
//   - It is not a dialog. Nothing here is worth taking focus from a field
//     somebody is typing into, so it is a polite status message that a screen
//     reader mentions and a keyboard ignores until Tab reaches it.
//   - It sits in the empty space below the terminal, so it displaces nothing.
//     In portrait that space is there whether this uses it or not: the screen
//     is width-constrained, and the room left underneath is room the terminal
//     cannot grow into.
//   - It appears because the screen is actually cramped, measured, rather than
//     because the device is a phone. A tablet held upright has room for a
//     nine-pixel cell and is not asked to rotate.
//
// Dismissing it is permanent — the operator has answered the question, and
// asking again on the next screen is how a hint becomes a nag. Rotating to
// landscape retires it for the life of the page without recording anything:
// they took the suggestion, and if they turn back it was deliberate.
(function () {
  "use strict";

  var DISMISSED_KEY = "3270Web.orientationHintDismissed";
  var HINT_ID = "orientation-hint";

  // Below this, a character cell is too small to read comfortably. A phone
  // held upright renders about 4px; the same phone sideways, and any tablet,
  // renders more than this and is left alone.
  var CRAMPED_CELL_PX = 6;

  var hint = null;
  var retiredForThisPage = false;

  function dismissedForGood() {
    try {
      return window.localStorage.getItem(DISMISSED_KEY) === "1";
    } catch (_) {
      // Private browsing. The hint is still dismissible, it just does not
      // remember — which is better than not offering it at all.
      return false;
    }
  }

  function rememberDismissal() {
    try {
      window.localStorage.setItem(DISMISSED_KEY, "1");
    } catch (_) {
      /* as above */
    }
  }

  function isPortrait() {
    return window.innerHeight > window.innerWidth;
  }

  // cellWidth asks the grid rather than measuring anything itself, so there is
  // one definition of a character cell in the codebase.
  function cellWidth() {
    var grid = window.ThreeSeventyWeb && window.ThreeSeventyWeb.screenGrid;
    if (!grid || typeof grid.metrics !== "function") {
      return null;
    }
    var metrics = grid.metrics();
    return metrics && metrics.cellW ? metrics.cellW : null;
  }

  function shouldOffer() {
    if (retiredForThisPage || dismissedForGood()) {
      return false;
    }
    if (!isPortrait()) {
      return false;
    }
    // A dialog is open. It has the operator's attention and, being a modal,
    // has taken the keyboard as well; a suggestion underneath it is noise.
    // Asked by layout rather than by the hidden attribute, because dialogs
    // here are dismissed several different ways.
    var dialogs = document.querySelectorAll('[role="dialog"][aria-modal="true"]');
    for (var i = 0; i < dialogs.length; i++) {
      var rect = dialogs[i].getBoundingClientRect();
      if (rect.width || rect.height) {
        return false;
      }
    }
    var width = cellWidth();
    // No measurement yet — the screen has not rendered. Say nothing rather
    // than guess; this is re-evaluated when it has.
    if (width === null) {
      return false;
    }
    return width < CRAMPED_CELL_PX;
  }

  function build() {
    var el = document.createElement("aside");
    el.id = HINT_ID;
    el.className = "orientation-hint";
    // status, not dialog: announced by a screen reader in its turn, and never
    // taking focus from whatever field is being typed into.
    el.setAttribute("role", "status");
    el.setAttribute("aria-live", "polite");

    var icon = document.createElement("span");
    icon.className = "orientation-hint-icon";
    icon.setAttribute("aria-hidden", "true");
    icon.innerHTML =
      '<svg viewBox="0 0 24 24" focusable="false">' +
      '<rect x="7" y="2" width="10" height="20" rx="2" fill="none" stroke="currentColor" stroke-width="1.6"/>' +
      '<path d="M3.5 15.5a9 9 0 0 0 3.2 4.6" fill="none" stroke="currentColor" stroke-width="1.6" ' +
      'stroke-linecap="round"/>' +
      '<path d="M2.2 12.4 3.6 16l3.6-1.3" fill="none" stroke="currentColor" stroke-width="1.6" ' +
      'stroke-linecap="round" stroke-linejoin="round"/>' +
      "</svg>";

    var body = document.createElement("div");
    body.className = "orientation-hint-body";

    var title = document.createElement("p");
    title.className = "orientation-hint-title";
    title.textContent = "Turn your phone sideways";

    var detail = document.createElement("p");
    detail.className = "orientation-hint-detail";
    detail.textContent =
      "This screen is 80 characters wide. Landscape gives it half again as much room.";

    body.appendChild(title);
    body.appendChild(detail);

    var dismiss = document.createElement("button");
    dismiss.type = "button";
    dismiss.className = "orientation-hint-dismiss";
    dismiss.setAttribute("aria-label", "Dismiss the landscape suggestion");
    dismiss.innerHTML =
      '<svg viewBox="0 0 24 24" aria-hidden="true" focusable="false">' +
      '<path d="M18 6L6 18M6 6l12 12" stroke="currentColor" stroke-width="2" stroke-linecap="round"/>' +
      "</svg>";
    dismiss.addEventListener("click", function () {
      rememberDismissal();
      retiredForThisPage = true;
      hide();
    });

    el.appendChild(icon);
    el.appendChild(body);
    el.appendChild(dismiss);
    return el;
  }

  function mount() {
    if (hint) {
      return hint;
    }
    var shell = document.querySelector(".terminal-shell");
    if (!shell || !shell.parentNode) {
      return null;
    }
    hint = build();
    hint.hidden = true;
    // After the terminal, before the keypad: the empty space in portrait,
    // and nothing is pushed anywhere.
    shell.parentNode.insertBefore(hint, shell.nextSibling);
    return hint;
  }

  function show() {
    var el = mount();
    if (!el || !el.hidden) {
      return;
    }
    el.hidden = false;
    // Next frame, so the entrance transition has a state to start from.
    window.requestAnimationFrame(function () {
      el.classList.add("is-shown");
    });
  }

  function hide() {
    if (!hint || hint.hidden) {
      return;
    }
    hint.classList.remove("is-shown");
    hint.hidden = true;
  }

  function evaluate() {
    if (shouldOffer()) {
      show();
    } else {
      hide();
    }
  }

  // Turning the phone is the suggestion being taken. Retire it for the life of
  // the page: turning back afterwards is a choice, not a mistake to correct.
  function noteOrientation() {
    if (!isPortrait()) {
      retiredForThisPage = true;
    }
    evaluate();
  }

  document.addEventListener("DOMContentLoaded", function () {
    // Coarse pointer, matching touch.js: a narrow desktop window is still a
    // desktop, and its owner can widen it without being told to.
    if (!window.matchMedia || !window.matchMedia("(pointer: coarse)").matches) {
      return;
    }
    if (!document.querySelector(".terminal-shell")) {
      return;
    }

    noteOrientation();

    window.addEventListener("resize", evaluate);
    if (window.screen && window.screen.orientation) {
      window.screen.orientation.addEventListener("change", noteOrientation);
    } else {
      // Safari before 16.4 has no ScreenOrientation events.
      window.addEventListener("orientationchange", noteOrientation);
    }

    // A screen refresh replaces the terminal's contents, and a host that
    // switches to a wider model changes the cell width with it.
    var grid = window.ThreeSeventyWeb && window.ThreeSeventyWeb.screenGrid;
    if (grid && typeof grid.onScreenReplaced === "function") {
      grid.onScreenReplaced(evaluate);
    }

    // The first evaluation can land before the terminal has laid out, which
    // reads as no measurement rather than a comfortable one. Ask again once.
    window.setTimeout(evaluate, 400);
  });

  window.ThreeSeventyWeb = window.ThreeSeventyWeb || {};
  window.ThreeSeventyWeb.orientationHint = {
    evaluate: evaluate,
    // For the browser check: it needs to undo a dismissal it just made.
    reset: function () {
      try {
        window.localStorage.removeItem(DISMISSED_KEY);
      } catch (_) {
        /* nothing to undo */
      }
      retiredForThisPage = false;
      evaluate();
    }
  };
})();
