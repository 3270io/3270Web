// Screen-accurate and rectangular copy.
//
// Browser selection over the terminal is unusable for copying: the screen is
// a <pre> with <input> elements spliced into it, so dragging across it
// silently drops every input's value and mangles column alignment. That
// matters more than it sounds — getting a column of account numbers out of a
// screen and into a spreadsheet is routine work in a 3270 shop, and every
// desktop emulator ships rectangular block copy for exactly this.
//
// The approach: the renderer emits the host's character grid on the form as
// data-screen-text. We overlay the live (possibly not-yet-submitted) input
// values on top of it to get what is actually on screen right now, then copy
// either the whole grid or a marked rectangle out of it.
(function () {
  "use strict";

  var MARK_HINT_ID = "screen-copy-hint";

  function getForm() {
    return document.querySelector("form.renderer-form");
  }

  function gridDimensions(form) {
    var rows = parseInt(form.getAttribute("data-rows"), 10);
    var cols = parseInt(form.getAttribute("data-cols"), 10);
    return {
      rows: isNaN(rows) || rows <= 0 ? 24 : rows,
      cols: isNaN(cols) || cols <= 0 ? 80 : cols
    };
  }

  // buildGrid returns the screen as an array of equal-length row strings.
  function buildGrid() {
    var form = getForm();
    if (!form) {
      return null;
    }
    var dims = gridDimensions(form);
    var raw = form.getAttribute("data-screen-text") || "";
    var lines = raw.split("\n");
    var grid = [];
    var r;
    for (r = 0; r < dims.rows; r++) {
      var line = lines[r] || "";
      if (line.length < dims.cols) {
        line += new Array(dims.cols - line.length + 1).join(" ");
      }
      grid.push(line.slice(0, dims.cols).split(""));
    }

    // Overlay what the operator has typed but not yet submitted. Without
    // this, copying a screen mid-entry returns the host's stale values and
    // quietly disagrees with what is on the display.
    var inputs = form.querySelectorAll("input[data-x][data-y]");
    for (var i = 0; i < inputs.length; i++) {
      var input = inputs[i];
      var x = parseInt(input.dataset.x, 10);
      var y = parseInt(input.dataset.y, 10);
      var w = parseInt(input.dataset.w, 10);
      if (isNaN(x) || isNaN(y) || y < 0 || y >= dims.rows) {
        continue;
      }
      if (isNaN(w) || w <= 0) {
        w = input.maxLength > 0 ? input.maxLength : 0;
      }
      // A hidden (password) field shows nothing on screen, so copying its
      // value would leak a secret that was never displayed.
      var hidden = input.classList.contains("color-input-hidden");
      var value = hidden ? "" : input.value || "";
      for (var c = 0; c < w; c++) {
        var col = x + c;
        if (col < 0 || col >= dims.cols) {
          continue;
        }
        grid[y][col] = c < value.length ? value.charAt(c) : " ";
      }
    }

    return { rows: dims.rows, cols: dims.cols, cells: grid };
  }

  function gridToText(grid, rect) {
    if (!grid) {
      return "";
    }
    var top = rect ? rect.top : 0;
    var left = rect ? rect.left : 0;
    var bottom = rect ? rect.bottom : grid.rows - 1;
    var right = rect ? rect.right : grid.cols - 1;
    var out = [];
    for (var r = top; r <= bottom && r < grid.rows; r++) {
      var line = grid.cells[r].slice(left, right + 1).join("");
      // Trailing padding is an artefact of a fixed-width grid, not content.
      out.push(line.replace(/\s+$/, ""));
    }
    return out.join("\n");
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
    copyText(gridToText(buildGrid(), null), "Screen");
  }

  /* ------------------------------------------------------------------ */
  /* Rectangular block marking                                           */
  /* ------------------------------------------------------------------ */

  var marking = false;
  var markAnchor = null;
  var markRect = null;
  var overlay = null;

  function getScreenContainer() {
    return document.querySelector(".screen-container");
  }

  // cellMetrics derives the character cell size from the rendered <pre>. The
  // grid is uniform monospace, so dividing the box by the row/column count is
  // exact and survives the zoom slider without needing to be told about it.
  function cellMetrics() {
    var form = getForm();
    var pre = form && form.querySelector("pre");
    if (!pre) {
      return null;
    }
    var dims = gridDimensions(form);
    var rect = pre.getBoundingClientRect();
    if (!rect.width || !rect.height) {
      return null;
    }
    return {
      rect: rect,
      cellW: rect.width / dims.cols,
      cellH: rect.height / dims.rows,
      rows: dims.rows,
      cols: dims.cols
    };
  }

  function pointToCell(metrics, clientX, clientY) {
    var col = Math.floor((clientX - metrics.rect.left) / metrics.cellW);
    var row = Math.floor((clientY - metrics.rect.top) / metrics.cellH);
    return {
      row: Math.max(0, Math.min(metrics.rows - 1, row)),
      col: Math.max(0, Math.min(metrics.cols - 1, col))
    };
  }

  function ensureOverlay(container) {
    if (overlay && overlay.parentNode) {
      return overlay;
    }
    overlay = document.createElement("div");
    overlay.className = "screen-block-selection";
    overlay.setAttribute("aria-hidden", "true");
    container.appendChild(overlay);
    return overlay;
  }

  function paintOverlay(metrics, rect) {
    var container = getScreenContainer();
    if (!container) {
      return;
    }
    var el = ensureOverlay(container);
    var containerRect = container.getBoundingClientRect();
    el.style.left = metrics.rect.left - containerRect.left + rect.left * metrics.cellW + "px";
    el.style.top = metrics.rect.top - containerRect.top + rect.top * metrics.cellH + "px";
    el.style.width = (rect.right - rect.left + 1) * metrics.cellW + "px";
    el.style.height = (rect.bottom - rect.top + 1) * metrics.cellH + "px";
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
    var container = getScreenContainer();
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
      var metrics = cellMetrics();
      if (!metrics) {
        return;
      }
      event.preventDefault();
      event.stopPropagation();
      marking = true;
      markAnchor = pointToCell(metrics, event.clientX, event.clientY);
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
      var metrics = cellMetrics();
      if (!metrics) {
        return;
      }
      event.preventDefault();
      markRect = rectFrom(markAnchor, pointToCell(metrics, event.clientX, event.clientY));
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
        copyText(gridToText(buildGrid(), markRect), "Block");
        clearMark();
      }
    }, true);
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
      copyText(gridToText(buildGrid(), markRect), "Block");
      clearMark();
    },
    screenText: function () {
      return gridToText(buildGrid(), null);
    },
    clearMark: clearMark
  };
})();
