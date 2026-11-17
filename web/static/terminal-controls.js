(function () {
  "use strict";

  // Width and height are not the same kind of constraint, so they get
  // different floors.
  //
  // Width is hard: a 24x80 grid is about 48 CSS pixels wide per point of cell
  // size, so fitting a 412px phone needs 8px or less. The old single floor of
  // 12px (~600px wide) was unreachable there, which is why "fit" bottomed out
  // still overflowing and the right-hand columns were simply clipped. Cells
  // may shrink to minCellSizePx to keep every column on screen; below 6px the
  // glyphs stop resolving at all.
  //
  // Height is soft: the page can scroll. Fitting the full 24 rows on a phone
  // held in landscape would drive the text down to the absolute floor for no
  // good reason, so height-aware fitting stops at the comfortable size and
  // lets the page scroll instead.
  var minCellSizePx = 6;
  var comfortableMinCellSizePx = 12;
  var maxCellSizePx = 36;
  var storageSizeKey = "h3270TerminalCellSizePx";

  // The terminal's size is one control above the terminal: a Fit / Readable /
  // Custom radio group and, for Custom only, a stepper and slider. It used to
  // be five — two toggles there, a stepper row and two chips in the View menu,
  // and Focus mode refitting behind them — which disagreed with one another.
  //
  //   Fit       the largest size at which the whole screen is in view
  //   Readable  100%, the size the text was designed at; a screen wider than
  //             the window pans inside its box
  //   Custom    the size the person sets; pans the same way when it is wider
  //             than the room

  function getElements() {
    var controls = document.querySelector("[data-terminal-controls]");
    var shell = document.querySelector(".terminal-shell");
    var container = document.querySelector(".screen-container");
    if (!controls || !shell || !container) {
      return null;
    }
    return {
      controls: controls,
      shell: shell,
      container: container,
      group: controls.querySelector('[role="radiogroup"]') || controls,
      choices: controls.querySelectorAll("[data-terminal-view-choice]"),
      custom: controls.querySelector("[data-terminal-custom]"),
      slider: controls.querySelector("[data-terminal-size-slider]"),
      stepDown: controls.querySelector("[data-terminal-size-down]"),
      stepUp: controls.querySelector("[data-terminal-size-up]"),
      label: controls.querySelector("[data-terminal-size-label]")
    };
  }

  function readCellSizeFromRoot() {
    var pre = document.querySelector(".screen-container pre, .screen-container textarea, .screen-container input");
    if (!pre) {
      return 16;
    }
    var size = Number.parseFloat(window.getComputedStyle(pre).fontSize);
    if (!Number.isFinite(size) || size <= 0) {
      return 16;
    }
    return size;
  }

  function clampCellSize(px) {
    return Math.max(minCellSizePx, Math.min(maxCellSizePx, px));
  }

  function applyCellSize(px) {
    var clamped = clampCellSize(px);
    document.documentElement.style.setProperty("--terminal-cell-size", clamped.toFixed(3) + "px");
    if (typeof window.sizeScreenContainer === "function") {
      window.sizeScreenContainer();
    }
    // Hotspots are positioned in pixels and only redraw when told the cells
    // changed size. They heard it from the slider alone, so every other way
    // of resizing — a choice, a stepper, a refit — left them where the old
    // size had put them, a screen's height away from the text they sit on.
    document.dispatchEvent(new Event("terminalresize"));
    return clamped;
  }

  function writeCellSize(px) {
    var clamped = Math.round(clampCellSize(px));
    applyCellSize(clamped);
    return clamped;
  }

  function updateSizeLabel(label, current, baseline) {
    if (!label || !baseline || baseline <= 0) {
      return;
    }
    var pct = Math.round((current / baseline) * 100);
    label.textContent = pct + "%";
    // The slider's own value is a size in pixels, which means nothing to
    // whoever hears it read out. Say the percentage instead.
    var slider = label.parentElement && label.parentElement.querySelector('input[type="range"]');
    if (slider) {
      slider.setAttribute("aria-valuetext", pct + "%");
    }
  }

  function updateSlider(slider, current) {
    if (!slider) {
      return;
    }
    slider.value = String(current);
  }

  // The width to fall back on. It must be the *layout* viewport: when content
  // overflows, a mobile browser widens the visual viewport to contain it, so
  // window.innerWidth reports the overflowing width (1607 on a 412px phone)
  // and every candidate size looks like it already fits. documentElement
  // .clientWidth stays at the layout width and is the honest measure.
  function layoutViewportWidth() {
    var docWidth = document.documentElement ? document.documentElement.clientWidth : 0;
    if (Number.isFinite(docWidth) && docWidth > 0) {
      return docWidth;
    }
    return window.innerWidth;
  }

  function edgeSize(styles, names) {
    var total = 0;
    for (var i = 0; i < names.length; i += 1) {
      var value = Number.parseFloat(styles[names[i]]);
      total += Number.isFinite(value) ? value : 0;
    }
    return total;
  }

  // How much room the terminal actually has.
  //
  // Not the viewport. The terminal sits in a column inside the page card, and
  // that column is what it has to fit into — measuring the window instead is
  // how a tablet ended up with a grid wider than the bar above it, hanging
  // over the card's right-hand edge and no longer centred on anything: every
  // candidate size "fitted" because the window was wider than the card.
  function availableWidth(shell) {
    var column = shell && shell.closest ? shell.closest(".terminal-width-column") : null;
    var host = (column && column.parentElement) || (shell && shell.parentElement);
    if (!host) {
      return layoutViewportWidth();
    }
    var styles = window.getComputedStyle(host);
    var width = host.clientWidth - edgeSize(styles, ["paddingLeft", "paddingRight"]);
    if (!Number.isFinite(width) || width <= 0) {
      return layoutViewportWidth();
    }
    return width;
  }

  // What the terminal would need to show every column.
  //
  // The grid's own box plus the bezel around it, rather than the bezel's
  // measured width: below the tablet breakpoint the shell scrolls, and a
  // scroll container is always exactly as wide as the room it was given, so
  // measuring it answers "it fits" however far the grid inside it overflows.
  function requiredWidth(elements) {
    var styles = window.getComputedStyle(elements.shell);
    var chrome = edgeSize(styles, [
      "paddingLeft",
      "paddingRight",
      "borderLeftWidth",
      "borderRightWidth"
    ]);
    return elements.container.getBoundingClientRect().width + chrome;
  }

  // Width-only, used for automatic fitting. Vertical overflow just means the
  // page scrolls, which is normal; horizontal overflow clips columns off the
  // right edge of a 3270 screen, which is not. Requiring both would force the
  // text smaller than it needs to be on a phone held in portrait.
  function fitsWidth(elements) {
    if (!elements || !elements.shell || !elements.container) {
      return false;
    }
    return requiredWidth(elements) <= availableWidth(elements.shell) + 1;
  }

  // How much of the viewport below the terminal is already spoken for. Zero
  // everywhere except focus mode: in the normal layout a terminal taller than
  // the viewport simply scrolls, and letting page chrome shrink it there is
  // what drove the text smaller than it needed to be on a phone.
  //
  // Focus mode has nowhere to scroll, so the on-screen keypad is not
  // "unrelated chrome" there — it is the other occupant of a fixed budget.
  // Reserving its floor here is what lets the terminal be sized once and take
  // what it wants, with the keypad scaling into the remainder afterwards.
  function reservedBelowTerminal() {
    if (document.body.getAttribute("data-focus") !== "on") {
      return 0;
    }
    var kp = window.ThreeSeventyWeb && window.ThreeSeventyWeb.keypad;
    if (!kp || typeof kp.reservedHeight !== "function") {
      return 0;
    }
    var reserved = kp.reservedHeight();
    return Number.isFinite(reserved) && reserved > 0 ? reserved : 0;
  }

  function fitsViewport(elements) {
    if (!fitsWidth(elements)) {
      return false;
    }
    var rect = elements.shell.getBoundingClientRect();
    // On the page, not on the screen: the question is whether the terminal
    // would be entirely in view with the page at the top. Asked of the screen
    // as it happens to be scrolled, a person who had scrolled down a little
    // and then pressed Fit got the floor size, because the top edge was
    // already above the window and no size could put it back. Focus mode never
    // scrolls, so there this is the same measurement as before.
    var scrolled = window.pageYOffset || 0;
    return rect.top + scrolled >= 0 && rect.bottom + scrolled <= window.innerHeight - reservedBelowTerminal() + 1;
  }

  // ceilingPx bounds the search from above, which is what makes a shrink a
  // shrink: without it the binary search happily returns a size larger than
  // the one it was asked to correct, and a terminal that merely overflowed
  // came back bigger than the operator left it.
  function fitToLargestSize(elements, predicate, floorPx, ceilingPx) {
    var fits = predicate || fitsViewport;
    var floor = Number.isFinite(floorPx) ? floorPx : minCellSizePx;
    var ceiling = Number.isFinite(ceilingPx) ? Math.min(maxCellSizePx, Math.round(ceilingPx)) : maxCellSizePx;
    if (ceiling < floor) {
      ceiling = floor;
    }
    var low = floor;
    var high = ceiling;
    var best = floor;

    while (low <= high) {
      var mid = Math.floor((low + high) / 2);
      writeCellSize(mid);
      if (fits(elements)) {
        best = mid;
        low = mid + 1;
      } else {
        high = mid - 1;
      }
    }

    return writeCellSize(best);
  }

  // The size the Custom choice was last left at. Fit and Readable are computed
  // every time, so this is the one size worth remembering: it is read back
  // only when Custom is the saved choice. Browser-local on purpose — a size in
  // pixels means something on this display and nothing on another.
  function persistSize(current) {
    try { localStorage.setItem(storageSizeKey, String(current)); } catch (_) {}
  }

  function readStoredSize() {
    var stored = NaN;
    try { stored = Number.parseFloat(localStorage.getItem(storageSizeKey) || ""); } catch (_) {}
    return Number.isFinite(stored) && stored > 0 ? stored : NaN;
  }

  // The three choices of the one size control, as stored. Fit has always been
  // saved as "overview", and accounts already carry that, so the stored word
  // stays and only the label people read changed.
  var MODE_FIT = "overview";
  var MODE_READABLE = "readable";
  var MODE_CUSTOM = "custom";

  function isMode(value) {
    return value === MODE_FIT || value === MODE_READABLE || value === MODE_CUSTOM;
  }

  function init() {
    var elements = getElements();
    if (!elements || !elements.slider || !elements.stepDown || !elements.stepUp || !elements.choices.length) {
      return;
    }
    var animationFrameId = 0;
    var layoutRefitTimer = 0;
    var preferences = window.ThreeSeventyWeb && window.ThreeSeventyWeb.preferences;

    // 100% is the stylesheet's own size, read before anything here changes it.
    var baseline = readCellSizeFromRoot();
    var mode = MODE_FIT;
    var current = baseline;
    var enforcingWidthFit = false;
    var enforcingViewportFit = false;
    // The screen shape (rows x columns) Fit last sized for. A different shape
    // is a different fit: connecting a Model 4 after a Model 2 changes how
    // tall the grid is at any given cell size.
    var fittedShape = "";

    function stopAnimation() {
      if (animationFrameId) {
        window.cancelAnimationFrame(animationFrameId);
        animationFrameId = 0;
      }
    }

    function stopLayoutRefitTimer() {
      if (layoutRefitTimer) {
        window.clearTimeout(layoutRefitTimer);
        layoutRefitTimer = 0;
      }
    }

    function animateToCellSize(targetPx, durationMs, done) {
      stopAnimation();
      var start = readCellSizeFromRoot();
      var target = Math.round(clampCellSize(targetPx));
      if (Math.abs(start - target) < 0.01 || durationMs <= 0) {
        writeCellSize(target);
        if (done) {
          done();
        }
        return;
      }
      var startedAt = performance.now();
      var duration = Math.max(40, durationMs);
      var step = function (now) {
        var t = (now - startedAt) / duration;
        if (t < 0) {
          t = 0;
        }
        if (t > 1) {
          t = 1;
        }
        var eased = t < 0.5 ? 2 * t * t : -1 + (4 - 2 * t) * t;
        var value = start + (target - start) * eased;
        applyCellSize(value);
        if (t < 1) {
          animationFrameId = window.requestAnimationFrame(step);
          return;
        }
        animationFrameId = 0;
        writeCellSize(target);
        if (done) {
          done();
        }
      };
      animationFrameId = window.requestAnimationFrame(step);
    }

    function screenShape() {
      var form = elements.container.querySelector(".renderer-form");
      return form ? (form.dataset.rows || "") + "x" + (form.dataset.cols || "") : "";
    }

    // Readable and Custom are the two choices where the person, not the
    // window, decides how big the text is. Where that is wider than the room,
    // the terminal pans inside its own box instead of being shrunk.
    function panning() {
      return mode !== MODE_FIT;
    }

    function updatePosition() {
      var output = document.querySelector("[data-terminal-position]");
      if (!output) return;
      var form = elements.container.querySelector(".renderer-form");
      var cols = form ? Number(form.dataset.cols || 80) : 80;
      var width = elements.container.getBoundingClientRect().width;
      var visible = Math.min(cols, Math.max(1, Math.floor(elements.shell.clientWidth / (width / cols))));
      var first = Math.min(cols, Math.floor(elements.shell.scrollLeft / (width / cols)) + 1);
      output.textContent = panning() && width > elements.shell.clientWidth ? "Columns " + first + "–" + Math.min(cols, first + visible - 1) + " of " + cols + " · swipe to pan" : "";
    }

    // Everything the person can see of the current size and choice, brought in
    // line with `mode` and `current`. The slider and its percentage exist for
    // Custom alone; Fit and Readable have nothing to adjust.
    function syncControls() {
      elements.choices.forEach(function (button) {
        var selected = button.dataset.terminalViewChoice === mode;
        button.setAttribute("aria-checked", String(selected));
        // One tab stop for the group, on the selected choice; the arrow keys
        // move within it.
        button.tabIndex = selected ? 0 : -1;
      });
      if (elements.custom) {
        elements.custom.hidden = mode !== MODE_CUSTOM;
      }
      updateSlider(elements.slider, current);
      updateSizeLabel(elements.label, current, baseline);
      updatePosition();
    }

    // Neither the stored size nor the stylesheet baseline knows how wide this
    // screen is, so on a phone both open with the right-hand columns off the
    // edge. Shrink until the terminal fits across; never grow here.
    //
    // This has to be re-checked whenever the screen content changes, not just
    // once at startup: at DOMContentLoaded the terminal can still be empty and
    // so trivially "fits", and it only reaches its true width once the first
    // screen is painted into it.
    function enforceWidthFit() {
      if (mode !== MODE_FIT) { updatePosition(); return; }
      if (enforcingWidthFit || fitsWidth(elements)) {
        return;
      }
      // writeCellSize re-runs the container sizing, which can mutate the very
      // subtree the MutationObserver below is watching. Guard against
      // re-entering the binary search from our own writes.
      enforcingWidthFit = true;
      try {
        // Ceilinged at the size already on screen: this is the correction for
        // a terminal that is too wide, and it has no business making one
        // bigger than it was asked to be.
        current = fitToLargestSize(elements, fitsWidth, minCellSizePx, current);
        syncControls();
      } finally {
        enforcingWidthFit = false;
      }
    }

    // The vertical twin of enforceWidthFit, and deliberately narrower: it only
    // applies in focus mode, because that is the only layout with nowhere to
    // scroll. Everywhere else a terminal taller than the viewport is normal
    // and the comfortable floor is the right answer.
    function enforceViewportFit() {
      if (mode !== MODE_FIT) return;
      if (enforcingWidthFit || enforcingViewportFit) {
        return;
      }
      if (document.body.getAttribute("data-focus") !== "on") {
        return;
      }
      if (fitsViewport(elements)) {
        return;
      }
      enforcingViewportFit = true;
      try {
        // Ceilinged at the size already on screen, for the same reason the
        // width correction is: this exists to bring an overflowing terminal
        // back, never to make one bigger than it was asked to be.
        current = fitToLargestSize(elements, fitsViewport, minCellSizePx, current);
        syncControls();
      } finally {
        enforcingViewportFit = false;
      }
    }

    // Fit: the largest size at which the whole screen is in view. Only means
    // anything in Fit; in the other two choices the size is the person's.
    function fit() {
      if (mode !== MODE_FIT) { updatePosition(); return; }
      stopAnimation();
      fittedShape = screenShape();
      if (!fittedShape) {
        // Nothing painted yet, so there is nothing to measure and every size
        // "fits". Sizing an empty container to the ceiling would only make the
        // first screen arrive huge; wait for it (the observer below refits).
        current = writeCellSize(baseline);
        syncControls();
        return;
      }
      // Height-aware, floored at the comfortable size: chasing 24 rows onto a
      // landscape phone would otherwise drive the text to the absolute floor
      // and leave a postage-stamp terminal on a screen with width to spare.
      current = fitToLargestSize(elements, fitsViewport, comfortableMinCellSizePx);
      // The comfortable floor is a preference, not a licence to clip columns —
      // if the result is too wide for the viewport, width wins.
      enforceWidthFit();
      // Nor a licence to clip rows. In the normal layout a terminal taller
      // than the viewport just scrolls, which is why the floor holds there.
      // Focus mode has nowhere to scroll to, so a floor it cannot honour puts
      // the first rows above the top edge instead.
      enforceViewportFit();
      syncControls();
    }

    function scheduleLayoutRefit() {
      if (mode !== MODE_FIT) {
        updatePosition();
        return;
      }
      stopLayoutRefitTimer();
      window.requestAnimationFrame(function () {
        fit();
      });
      layoutRefitTimer = window.setTimeout(function () {
        layoutRefitTimer = 0;
        fit();
      }, 180);
    }

    // Switch to a choice. `remember` is for a person's own pick: it is saved
    // so it follows them, where applying the saved choice at load is not a
    // new decision and saves nothing.
    function applyMode(next, remember) {
      stopAnimation();
      stopLayoutRefitTimer();
      var previous = mode;
      mode = next;
      document.body.dataset.terminalView = mode;
      if (mode === MODE_READABLE) {
        // Readable is 100%: the size the stylesheet was designed at.
        current = writeCellSize(baseline);
      } else if (mode === MODE_CUSTOM) {
        // Entering Custom must not move the text: it starts from whatever the
        // person is looking at, and they adjust from there.
        if (previous !== MODE_CUSTOM) {
          current = writeCellSize(current);
        }
        persistSize(current);
      }
      syncControls();
      if (mode === MODE_FIT) {
        fit();
      }
      if (remember && preferences && previous !== mode) {
        preferences.save({ terminalView: mode });
      }
    }

    function choose(button) {
      var choice = button.dataset.terminalViewChoice;
      if (isMode(choice)) {
        applyMode(choice, true);
      }
    }

    function setManualSize(next, durationMs) {
      if (mode !== MODE_CUSTOM) {
        // Reaching for the slider or a stepper is choosing Custom.
        applyMode(MODE_CUSTOM, true);
      }
      current = Math.round(clampCellSize(next));
      updateSlider(elements.slider, current);
      updateSizeLabel(elements.label, current, baseline);
      animateToCellSize(current, durationMs, function () {
        persistSize(current);
        updatePosition();
      });
    }

    elements.slider.min = String(minCellSizePx);
    elements.slider.max = String(maxCellSizePx);

    // Which choice to open in. A saved one wins; otherwise a phone gets
    // Readable (the whole grid at a size it can be read at, panning) and
    // everything wider gets Fit.
    var savedMode = preferences && preferences.get("terminalView");
    if (!isMode(savedMode)) {
      savedMode = layoutViewportWidth() <= 640 ? MODE_READABLE : MODE_FIT;
    }
    mode = savedMode;
    document.body.dataset.terminalView = mode;
    if (mode === MODE_CUSTOM) {
      var storedSize = readStoredSize();
      current = writeCellSize(Number.isFinite(storedSize) ? storedSize : baseline);
    } else {
      current = writeCellSize(baseline);
    }
    document.body.classList.remove("terminal-fit-active");
    syncControls();
    if (mode === MODE_FIT) {
      fit();
    }

    elements.choices.forEach(function (button) {
      button.addEventListener("click", function (event) {
        // focus-mode.js used to refit by calling .click() on the Fit control,
        // and a synthetic click carries isTrusted === false. That is a request
        // to re-fit within the current choice, not a person choosing Fit, and
        // taking it for one would pull someone out of Readable or Custom
        // whenever they entered Focus mode.
        if (button.dataset.terminalViewChoice === MODE_FIT && event.isTrusted === false) {
          fit();
          return;
        }
        choose(button);
      });
    });

    // The choices are a radio group, so the arrow keys move between them and
    // select as they go, as native radios do.
    elements.group.addEventListener("keydown", function (event) {
      if (event.altKey || event.ctrlKey || event.metaKey) return;
      var buttons = Array.prototype.slice.call(elements.choices);
      var at = buttons.indexOf(document.activeElement);
      if (at < 0) return;
      var to = -1;
      switch (event.key) {
        case "ArrowRight":
        case "ArrowDown":
          to = (at + 1) % buttons.length;
          break;
        case "ArrowLeft":
        case "ArrowUp":
          to = (at - 1 + buttons.length) % buttons.length;
          break;
        case "Home":
          to = 0;
          break;
        case "End":
          to = buttons.length - 1;
          break;
        default:
          return;
      }
      event.preventDefault();
      buttons[to].focus();
      choose(buttons[to]);
    });

    elements.shell.addEventListener("scroll", updatePosition, { passive: true });
    elements.container.addEventListener("focusin", function (event) {
      if (!panning() || !event.target.matches("input, textarea")) return;
      window.requestAnimationFrame(function () {
        var field = event.target.getBoundingClientRect(), shell = elements.shell.getBoundingClientRect();
        if (field.left < shell.left + 12) elements.shell.scrollLeft += field.left - shell.left - 12;
        else if (field.right > shell.right - 12) elements.shell.scrollLeft += field.right - shell.right + 12;
        updatePosition();
      });
    });

    elements.slider.addEventListener("input", function () {
      if (mode !== MODE_CUSTOM) {
        applyMode(MODE_CUSTOM, true);
      }
      current = Math.round(clampCellSize(Number.parseFloat(elements.slider.value)));
      updateSizeLabel(elements.label, current, baseline);
      animateToCellSize(current, 120);
    });

    elements.slider.addEventListener("change", function () {
      setManualSize(Number.parseFloat(elements.slider.value), 90);
    });

    elements.stepDown.addEventListener("click", function () {
      setManualSize(current - 1, 90);
    });

    elements.stepUp.addEventListener("click", function () {
      setManualSize(current + 1, 90);
    });

    var lastViewportWidth = layoutViewportWidth();

    window.addEventListener("resize", function () {
      if (typeof window.sizeScreenContainer === "function") {
        window.sizeScreenContainer();
      }
      // An on-screen keyboard resizes the viewport vertically, and it does so
      // exactly when the user is about to type. Refitting on that would shrink
      // the terminal every time it opens, so react only to a real width change
      // (rotation, or a resized desktop window).
      if (layoutViewportWidth() === lastViewportWidth) {
        return;
      }
      lastViewportWidth = layoutViewportWidth();
      // Turning a phone to landscape triples the available width. Fit is
      // re-derived from scratch so the terminal grows back into the new space
      // instead of staying postage-stamp sized; Readable and Custom are
      // sizes the person picked and are left where they put them.
      fit();
      updatePosition();
    });

    window.addEventListener("h3270:layout-changed", function () {
      scheduleLayoutRefit();
    });

    // Fonts and late layout move the terminal after the first measurement.
    window.addEventListener("load", scheduleLayoutRefit);
    if (document.fonts && document.fonts.ready && typeof document.fonts.ready.then === "function") {
      document.fonts.ready.then(scheduleLayoutRefit, function () {});
    }

    if (typeof MutationObserver !== "undefined") {
      var observer = new MutationObserver(function () {
        if (enforcingWidthFit) {
          return;
        }
        // No stopAnimation() here: a zoom in flight is Custom's, nothing in
        // Fit animates, and cutting one short on any repaint of the screen
        // left the text at a size between two steps.
        if (typeof window.sizeScreenContainer === "function") {
          window.sizeScreenContainer();
        }
        // The first screen painted into the container is what gives the
        // terminal its real width, so this is where a too-large size actually
        // becomes visible as overflow. A screen of a different shape (another
        // session, another model) is a different fit altogether.
        if (mode === MODE_FIT && screenShape() && screenShape() !== fittedShape) {
          fit();
        } else {
          enforceWidthFit();
        }
        updateSlider(elements.slider, current);
        updateSizeLabel(elements.label, current, baseline);
        updatePosition();
      });
      observer.observe(elements.container, { childList: true, subtree: true });
    }

    window.ThreeSeventyWeb = window.ThreeSeventyWeb || {};
    window.ThreeSeventyWeb.terminalSize = {
      // "overview" (Fit), "readable" or "custom": the stored words.
      mode: function () { return mode; },
      // Re-fit after the room around the terminal changed. Does nothing unless
      // Fit is the choice, so a caller cannot override a size someone picked.
      refit: function () { fit(); }
    };
  }

  document.addEventListener("DOMContentLoaded", init);
})();
