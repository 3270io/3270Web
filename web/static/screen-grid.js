// Shared access to the terminal screen as a character grid.
//
// Three features need the same two things — the screen as a rectangular grid
// of characters, and the geometry to map a grid cell to a pixel box:
//
//   screen-copy.js  whole-screen and rectangular block copy
//   hotspots.js     clickable PF labels and URLs
//   screen-find.js  find-on-screen with highlighted matches
//
// The grid comes from data-screen-text, which the Go renderer emits on the
// form, with live input values overlaid on top so it reflects what is on the
// display right now rather than what the host last sent. The geometry comes
// from measuring the rendered <pre>: the font is uniform monospace, so
// dividing its box by the row and column count is exact, and it stays correct
// through the zoom slider without anything having to tell it.
//
// Must load before its three consumers.
(function () {
  "use strict";

  function getForm() {
    return document.querySelector("form.renderer-form");
  }

  function getContainer() {
    return document.querySelector(".screen-container");
  }

  function dimensions(form) {
    var rows = parseInt(form.getAttribute("data-rows"), 10);
    var cols = parseInt(form.getAttribute("data-cols"), 10);
    return {
      rows: isNaN(rows) || rows <= 0 ? 24 : rows,
      cols: isNaN(cols) || cols <= 0 ? 80 : cols
    };
  }

  // buildGrid returns { rows, cols, cells } where cells is an array of
  // per-row character arrays, every row exactly cols long. Also records which
  // cells belong to an input field, which hotspot detection needs so it never
  // covers something the operator has to type into.
  function buildGrid() {
    var form = getForm();
    if (!form) {
      return null;
    }
    var dims = dimensions(form);
    var lines = (form.getAttribute("data-screen-text") || "").split("\n");
    var cells = [];
    var inputMask = [];
    var r;
    var c;
    for (r = 0; r < dims.rows; r++) {
      var line = lines[r] || "";
      if (line.length < dims.cols) {
        line += new Array(dims.cols - line.length + 1).join(" ");
      }
      cells.push(line.slice(0, dims.cols).split(""));
      var maskRow = new Array(dims.cols);
      for (c = 0; c < dims.cols; c++) {
        maskRow[c] = false;
      }
      inputMask.push(maskRow);
    }

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
      // A hidden (password) field displays nothing, so the grid must not
      // carry its value — copying or searching it would surface a secret
      // that was never on screen.
      var value = input.classList.contains("color-input-hidden") ? "" : input.value || "";
      for (c = 0; c < w; c++) {
        var col = x + c;
        if (col < 0 || col >= dims.cols) {
          continue;
        }
        cells[y][col] = c < value.length ? value.charAt(c) : " ";
        inputMask[y][col] = true;
      }
    }

    return { rows: dims.rows, cols: dims.cols, cells: cells, inputMask: inputMask };
  }

  // gridToText joins the grid, optionally clipped to a rectangle, stripping
  // the trailing padding that is an artefact of a fixed-width grid.
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
      out.push(grid.cells[r].slice(left, right + 1).join("").replace(/\s+$/, ""));
    }
    return out.join("\n");
  }

  function rowText(grid, row) {
    if (!grid || row < 0 || row >= grid.rows) {
      return "";
    }
    return grid.cells[row].join("");
  }

  function cellMetrics() {
    var form = getForm();
    var pre = form && form.querySelector("pre");
    if (!pre) {
      return null;
    }
    var dims = dimensions(form);
    return metricsFor(pre, dims.rows, dims.cols);
  }

  // metricsFor computes cell geometry for any monospaced block of a known
  // size, not only the live terminal. The task authoring wizard needs exactly
  // the same "which cell is under the pointer" arithmetic over a *static*
  // screen — the one a recording ended on — and reusing this is what keeps
  // one definition of a cell in the codebase instead of two that drift.
  function metricsFor(pre, rows, cols) {
    if (!pre || rows <= 0 || cols <= 0) {
      return null;
    }
    var rect = pre.getBoundingClientRect();
    if (!rect.width || !rect.height) {
      return null;
    }
    return {
      rect: rect,
      cellW: rect.width / cols,
      cellH: rect.height / rows,
      rows: rows,
      cols: cols
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

  // overlayLayer is where every absolutely-positioned decoration lives —
  // hotspots, find highlights, the block-selection marquee.
  //
  // They need their own layer for a specific reason: onScreenReplaced watches
  // .screen-container for mutations, and appending an overlay to the container
  // is itself a mutation. Overlays rendered directly into the container
  // therefore retrigger the very observer that rendered them, and the feature
  // spins forever, tearing its own nodes down mid-click.
  //
  // The layer is recreated on demand because a screen refresh replaces the
  // container's innerHTML wholesale, taking the layer with it.
  function overlayLayer() {
    var container = getContainer();
    if (!container) {
      return null;
    }
    var layer = container.querySelector(":scope > .screen-overlay-layer");
    if (!layer) {
      layer = document.createElement("div");
      layer.className = "screen-overlay-layer";
      container.appendChild(layer);
    }
    return layer;
  }

  function isInsideOverlayLayer(node) {
    for (var n = node; n; n = n.parentNode) {
      if (n.classList && n.classList.contains("screen-overlay-layer")) {
        return true;
      }
    }
    return false;
  }

  // positionOverElement places an absolutely-positioned element over a grid
  // rectangle. Offsets are relative to .screen-container, which is the
  // positioned ancestor these overlays live in.
  function positionOverCells(el, metrics, rect) {
    var container = getContainer();
    if (!container || !el) {
      return false;
    }
    var containerRect = container.getBoundingClientRect();
    el.style.left = metrics.rect.left - containerRect.left + rect.left * metrics.cellW + "px";
    el.style.top = metrics.rect.top - containerRect.top + rect.top * metrics.cellH + "px";
    el.style.width = (rect.right - rect.left + 1) * metrics.cellW + "px";
    el.style.height = (rect.bottom - rect.top + 1) * metrics.cellH + "px";
    return true;
  }

  // onScreenReplaced runs cb whenever .screen-container's contents are swapped
  // out. Every overlay-based feature has to rebuild after a screen refresh,
  // and there are three independent code paths that do the swapping (a manual
  // submit, the SSE idle push, and the chaos/playback poller), so watching the
  // DOM is more reliable than trying to hook all of them.
  function onScreenReplaced(cb) {
    var container = getContainer();
    if (!container || typeof MutationObserver !== "function") {
      return;
    }
    var pending = 0;
    var observer = new MutationObserver(function (records) {
      // Ignore our own overlays. Without this the callback fires on the nodes
      // it just caused to be added and the feature loops.
      var real = false;
      for (var i = 0; i < records.length && !real; i++) {
        var rec = records[i];
        if (isInsideOverlayLayer(rec.target)) {
          continue;
        }
        var j;
        for (j = 0; j < rec.addedNodes.length; j++) {
          if (!isInsideOverlayLayer(rec.addedNodes[j])) {
            real = true;
            break;
          }
        }
        for (j = 0; j < rec.removedNodes.length && !real; j++) {
          if (!isInsideOverlayLayer(rec.removedNodes[j])) {
            real = true;
          }
        }
      }
      if (!real || pending) {
        return;
      }
      pending = window.setTimeout(function () {
        pending = 0;
        cb();
      }, 30);
    });
    observer.observe(container, { childList: true, subtree: true });
  }

  window.ThreeSeventyWeb = window.ThreeSeventyWeb || {};
  window.ThreeSeventyWeb.screenGrid = {
    form: getForm,
    container: getContainer,
    build: buildGrid,
    toText: gridToText,
    rowText: rowText,
    metrics: cellMetrics,
    metricsFor: metricsFor,
    pointToCell: pointToCell,
    positionOverCells: positionOverCells,
    overlayLayer: overlayLayer,
    onScreenReplaced: onScreenReplaced
  };
})();
