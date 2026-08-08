// Screen-accurate and rectangular copy.
//
// Browser selection over the terminal is unusable for copying: the screen is
// a <pre> with <input> elements spliced into it, so dragging across it
// silently drops every input's value and mangles column alignment. That
// matters more than it sounds — getting a column of account numbers out of a
// screen and into a spreadsheet is routine work in a 3270 shop, and every
// desktop emulator ships rectangular block copy for exactly this.
//
// The grid and geometry come from screen-grid.js.
(function () {
  "use strict";

  var MARK_HINT_ID = "screen-copy-hint";

  function grid() {
    return window.ThreeSeventyWeb.screenGrid;
  }

  function toast(message, type) {
    if (window.ThreeSeventyWeb && typeof window.ThreeSeventyWeb.notify === "function") {
      window.ThreeSeventyWeb.notify(message, type);
    }
  }

  function copyText(text, label) {
    var done = function (ok) {
      toast(
        ok ? label + " copied to clipboard" : "Copy failed — clipboard unavailable",
        ok ? "success" : "error"
      );
    };
    if (!text) {
      done(false);
      return;
    }
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(
        function () {
          done(true);
        },
        function () {
          done(fallbackCopy(text));
        }
      );
      return;
    }
    done(fallbackCopy(text));
  }

  // execCommand is deprecated but remains the only path when the page is not
  // a secure context, which is common for an internally-hosted terminal
  // reached over plain HTTP.
  function fallbackCopy(text) {
    try {
      var area = document.createElement("textarea");
      area.value = text;
      area.setAttribute("readonly", "");
      area.style.position = "fixed";
      area.style.opacity = "0";
      document.body.appendChild(area);
      area.select();
      var ok = document.execCommand("copy");
      document.body.removeChild(area);
      return ok;
    } catch (_) {
      return false;
    }
  }

  function copyWholeScreen() {
    var g = grid();
    copyText(g.toText(g.build(), null), "Screen");
  }

  /* ------------------------------------------------------------------ */
  /* Rectangular block marking                                           */
  /* ------------------------------------------------------------------ */

  var marking = false;
  var markAnchor = null;
  var markRect = null;
  var overlay = null;

  function ensureOverlay() {
    if (overlay && overlay.parentNode) {
      return overlay;
    }
    var layer = grid().overlayLayer();
    if (!layer) {
      return null;
    }
    overlay = document.createElement("div");
    overlay.className = "screen-block-selection";
    overlay.setAttribute("aria-hidden", "true");
    layer.appendChild(overlay);
    return overlay;
  }

  function paintOverlay(metrics, rect) {
    var el = ensureOverlay();
    if (!el) {
      return;
    }
    grid().positionOverCells(el, metrics, rect);
    el.hidden = false;
  }

  function clearMark() {
    markRect = null;
    markAnchor = null;
    marking = false;
    if (overlay) {
      overlay.hidden = true;
    }
    setHint("");
  }

  function setHint(text) {
    var el = document.getElementById(MARK_HINT_ID);
    if (!el) {
      return;
    }
    el.textContent = text;
    el.hidden = !text;
  }

  function rectFrom(a, b) {
    return {
      top: Math.min(a.row, b.row),
      bottom: Math.max(a.row, b.row),
      left: Math.min(a.col, b.col),
      right: Math.max(a.col, b.col)
    };
  }

  function initBlockSelection() {
    var container = grid().container();
    if (!container) {
      return;
    }

    // Alt+drag marks a block. Alt is the modifier every desktop emulator and
    // terminal uses for column selection, and it leaves an unmodified drag
    // free for ordinary text selection and for clicking into a field.
    container.addEventListener("pointerdown", function (event) {
      if (!event.altKey || event.button !== 0) {
        return;
      }
      var metrics = grid().metrics();
      if (!metrics) {
        return;
      }
      event.preventDefault();
      event.stopPropagation();
      marking = true;
      markAnchor = grid().pointToCell(metrics, event.clientX, event.clientY);
      markRect = rectFrom(markAnchor, markAnchor);
      paintOverlay(metrics, markRect);
      if (container.setPointerCapture && event.pointerId != null) {
        try {
          container.setPointerCapture(event.pointerId);
        } catch (_) { /* not fatal */ }
      }
    }, true);

    container.addEventListener("pointermove", function (event) {
      if (!marking || !markAnchor) {
        return;
      }
      var metrics = grid().metrics();
      if (!metrics) {
        return;
      }
      event.preventDefault();
      markRect = rectFrom(markAnchor, grid().pointToCell(metrics, event.clientX, event.clientY));
      paintOverlay(metrics, markRect);
    }, true);

    var finish = function () {
      if (!marking) {
        return;
      }
      marking = false;
      if (!markRect) {
        return;
      }
      var width = markRect.right - markRect.left + 1;
      var height = markRect.bottom - markRect.top + 1;
      setHint(
        "Block marked " + height + "×" + width + " — Ctrl+C to copy, Esc to clear"
      );
    };

    container.addEventListener("pointerup", finish, true);
    container.addEventListener("pointercancel", finish, true);

    // Ctrl/Cmd+C copies the marked block. Without a mark the browser's own
    // copy is left alone, so selecting a label and copying it still works.
    document.addEventListener("keydown", function (event) {
      if (!markRect) {
        return;
      }
      if (event.key === "Escape") {
        clearMark();
        return;
      }
      if ((event.ctrlKey || event.metaKey) && (event.key === "c" || event.key === "C")) {
        event.preventDefault();
        event.stopPropagation();
        var g = grid();
        copyText(g.toText(g.build(), markRect), "Block");
        clearMark();
      }
    }, true);

    // A screen refresh replaces the container's contents, so a mark drawn
    // against the old screen no longer means anything.
    grid().onScreenReplaced(clearMark);
  }

  document.addEventListener("DOMContentLoaded", function () {
    initBlockSelection();

    var copyButton = document.querySelector("[data-copy-screen]");
    if (copyButton) {
      copyButton.addEventListener("click", function () {
        copyWholeScreen();
      });
    }
  });

  window.ThreeSeventyWeb = window.ThreeSeventyWeb || {};
  window.ThreeSeventyWeb.screenCopy = {
    copyScreen: copyWholeScreen,
    copyBlock: function () {
      if (!markRect) {
        toast("Alt+drag over the screen to mark a block first", "info");
        return;
      }
      var g = grid();
      copyText(g.toText(g.build(), markRect), "Block");
      clearMark();
    },
    screenText: function () {
      var g = grid();
      return g.toText(g.build(), null);
    },
    clearMark: clearMark
  };
})();
