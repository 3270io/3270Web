// Hotspots: turn the key legends a 3270 application already prints into
// things you can click.
//
// Almost every mainframe screen ends with a line like
// "F1=Help  F3=Exit  F7=Bkwd  F8=Fwd", and on a real emulator those are
// clickable. It is a small feature that gets used constantly — it is how
// someone who does not know the keyboard drives the application at all — and
// Quick3270, PCOMM and Rumba+ all ship it.
//
// Detection runs on the character grid rather than the DOM, because a legend
// is just text: it has no markup of its own to hook, and the same label can
// be split across protected fields in ways that make DOM walking unreliable.
//
// Two rules keep this from doing damage:
//   - A hotspot is never placed over an input field. "PF3" appearing inside a
//     value the operator typed is data, not a control.
//   - Only recognised key names become key hotspots, and each one maps to a
//     key the server already accepts, so a click cannot invent an action.
(function () {
  "use strict";

  var STORAGE_KEY = "3270Web.hotspots.v1";
  var enabled = true;
  var nodes = [];

  function grid() {
    return window.ThreeSeventyWeb.screenGrid;
  }

  function readEnabled() {
    try {
      var stored = localStorage.getItem(STORAGE_KEY);
      if (stored === "0") {
        return false;
      }
    } catch (_) { /* private browsing */ }
    return true;
  }

  function writeEnabled(value) {
    try {
      localStorage.setItem(STORAGE_KEY, value ? "1" : "0");
    } catch (_) { /* nothing to do */ }
  }

  // A key legend reference. Matches PF3, F3, PA1 and the bare "3=" form some
  // applications use, each optionally followed by a separator and a label.
  // The key token is what becomes clickable — not the label, which can be
  // long and would swallow neighbouring text.
  var KEY_PATTERN = /\b(PF|PA|F)(\d{1,2})\b/g;
  var URL_PATTERN = /\b(https?:\/\/[^\s<>"']+|www\.[^\s<>"']+)/g;

  // resolveKey maps a detected token to the key name the server accepts, or
  // "" if the number is out of range for that family. Rejecting out-of-range
  // numbers matters: "F30" is not a key, and a screen full of numbers should
  // not sprout dozens of dead hotspots.
  function resolveKey(family, digits) {
    var n = parseInt(digits, 10);
    if (isNaN(n)) {
      return "";
    }
    if (family === "PA") {
      return n >= 1 && n <= 3 ? "PA" + n : "";
    }
    // PF and the bare F form are the same key family.
    return n >= 1 && n <= 24 ? "PF" + n : "";
  }

  function regionIsInput(g, row, from, to) {
    for (var c = from; c <= to; c++) {
      if (g.inputMask[row] && g.inputMask[row][c]) {
        return true;
      }
    }
    return false;
  }

  // labelAfter reads the short description that follows a key token, so the
  // hotspot's tooltip can say "PF3 — Exit" rather than just "PF3". Stops at
  // two spaces, which is how legends separate entries.
  function labelAfter(line, index) {
    var rest = line.slice(index);
    var m = rest.match(/^[=:. ]\s*([A-Za-z][A-Za-z0-9/ -]{0,20}?)(?:\s{2,}|$)/);
    if (!m) {
      return "";
    }
    return m[1].trim();
  }

  function detect(g) {
    var found = [];
    if (!g) {
      return found;
    }
    for (var row = 0; row < g.rows; row++) {
      var line = g.cells[row].join("");
      var match;

      KEY_PATTERN.lastIndex = 0;
      while ((match = KEY_PATTERN.exec(line)) !== null) {
        var key = resolveKey(match[1] === "F" ? "PF" : match[1], match[2]);
        if (!key) {
          continue;
        }
        var from = match.index;
        var to = match.index + match[0].length - 1;
        if (regionIsInput(g, row, from, to)) {
          continue;
        }
        found.push({
          kind: "key",
          row: row,
          from: from,
          to: to,
          key: key,
          label: labelAfter(line, to + 1),
          text: match[0]
        });
      }

      URL_PATTERN.lastIndex = 0;
      while ((match = URL_PATTERN.exec(line)) !== null) {
        var uFrom = match.index;
        var uTo = match.index + match[0].length - 1;
        if (regionIsInput(g, row, uFrom, uTo)) {
          continue;
        }
        // Trailing punctuation is almost always sentence punctuation rather
        // than part of the address.
        var raw = match[0].replace(/[.,;:)\]]+$/, "");
        uTo = uFrom + raw.length - 1;
        found.push({
          kind: "url",
          row: row,
          from: uFrom,
          to: uTo,
          url: /^www\./i.test(raw) ? "https://" + raw : raw,
          text: raw
        });
      }
    }
    return found;
  }

  function clear() {
    for (var i = 0; i < nodes.length; i++) {
      if (nodes[i].parentNode) {
        nodes[i].parentNode.removeChild(nodes[i]);
      }
    }
    nodes = [];
  }

  function activate(spot) {
    if (spot.kind === "url") {
      window.open(spot.url, "_blank", "noopener,noreferrer");
      return;
    }
    if (typeof window.sendKey === "function") {
      window.sendKey(spot.key, null);
    }
  }

  function render() {
    clear();
    if (!enabled) {
      return;
    }
    var g = grid();
    var layer = g.overlayLayer();
    var metrics = g.metrics();
    if (!layer || !metrics) {
      return;
    }
    var spots = detect(g.build());
    for (var i = 0; i < spots.length; i++) {
      var spot = spots[i];
      var el = document.createElement("button");
      el.type = "button";
      el.className = "screen-hotspot screen-hotspot-" + spot.kind;
      el.dataset.hotspotKind = spot.kind;
      if (spot.kind === "key") {
        var described = spot.label ? spot.key + " — " + spot.label : spot.key;
        el.setAttribute("aria-label", "Send " + described);
        el.setAttribute("title", "Send " + described);
      } else {
        el.setAttribute("aria-label", "Open " + spot.text);
        el.setAttribute("title", spot.url);
      }
      g.positionOverCells(el, metrics, {
        top: spot.row,
        bottom: spot.row,
        left: spot.from,
        right: spot.to
      });
      (function (s) {
        el.addEventListener("click", function (event) {
          event.preventDefault();
          event.stopPropagation();
          activate(s);
        });
      })(spot);
      layer.appendChild(el);
      nodes.push(el);
    }
  }

  function setEnabled(next) {
    enabled = !!next;
    writeEnabled(enabled);
    document.body.setAttribute("data-hotspots", enabled ? "on" : "off");
    var toggle = document.querySelector("[data-hotspots-toggle]");
    if (toggle) {
      toggle.setAttribute("aria-pressed", String(enabled));
      var hint = enabled
        ? "Hotspots on — PF labels and URLs on screen are clickable"
        : "Hotspots off";
      toggle.setAttribute("aria-label", hint);
      toggle.setAttribute("title", hint);
      if (toggle.hasAttribute("data-tippy-content")) {
        toggle.setAttribute("data-tippy-content", hint);
      }
    }
    render();
    if (window.ThreeSeventyWeb && typeof window.ThreeSeventyWeb.notify === "function") {
      window.ThreeSeventyWeb.notify(
        enabled ? "Hotspots enabled." : "Hotspots disabled.",
        "info",
        { duration: 2000 }
      );
    }
  }

  document.addEventListener("DOMContentLoaded", function () {
    enabled = readEnabled();
    document.body.setAttribute("data-hotspots", enabled ? "on" : "off");

    var toggle = document.querySelector("[data-hotspots-toggle]");
    if (toggle) {
      toggle.setAttribute("aria-pressed", String(enabled));
      toggle.addEventListener("click", function () {
        setEnabled(!enabled);
      });
    }

    render();
    grid().onScreenReplaced(render);

    // Overlays are positioned in pixels, so they have to be recomputed
    // whenever the cell size changes — the zoom slider, the fit button, a
    // window resize, or the Copilot panel reflowing the page.
    var reflow = 0;
    var scheduleRender = function () {
      if (reflow) {
        window.clearTimeout(reflow);
      }
      reflow = window.setTimeout(function () {
        reflow = 0;
        render();
      }, 80);
    };
    window.addEventListener("resize", scheduleRender);
    var slider = document.querySelector("[data-terminal-size-slider]");
    if (slider) {
      slider.addEventListener("input", scheduleRender);
    }
    document.addEventListener("terminalresize", scheduleRender);
  });

  window.ThreeSeventyWeb = window.ThreeSeventyWeb || {};
  window.ThreeSeventyWeb.hotspots = {
    isEnabled: function () {
      return enabled;
    },
    setEnabled: setEnabled,
    toggle: function () {
      setEnabled(!enabled);
    },
    refresh: render,
    // Exposed for tests: the detected spots for the current screen.
    detect: function () {
      return detect(grid().build());
    }
  };
})();
