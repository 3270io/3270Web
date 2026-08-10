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

  // Some of the size controls sit inside [data-terminal-controls] and some
  // alongside it: "Fit to window" and "Reset" are their own row in the View
  // menu, one level up from the stepper group. Look in the group first, then
  // in the document. Scoping every lookup to the group found neither button,
  // init() bailed on the null, and the whole module went with it — no
  // automatic shrink-to-fit, which on a phone means the 80-column grid keeps
  // its desktop width, overflows the viewport, and the browser answers by
  // zooming the entire page out until the menu bar is too small to tap.
  function findControl(controls, selector) {
    return controls.querySelector(selector) || document.querySelector(selector);
  }

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
      slider: findControl(controls, "[data-terminal-size-slider]"),
      stepDown: findControl(controls, "[data-terminal-size-down]"),
      stepUp: findControl(controls, "[data-terminal-size-up]"),
      label: findControl(controls, "[data-terminal-size-label]"),
      fit: findControl(controls, "[data-terminal-fit]"),
      zoomReset: findControl(controls, "[data-terminal-zoom-reset]")
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
    return rect.top >= 0 && rect.bottom <= window.innerHeight - reservedBelowTerminal() + 1;
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

  function persistSize(current) {
    localStorage.setItem(storageSizeKey, String(current));
  }

  function init() {
    var elements = getElements();
    if (!elements || !elements.slider || !elements.zoomReset || !elements.fit || !elements.stepDown || !elements.stepUp) {
      return;
    }
    var animationFrameId = 0;
    var layoutRefitTimer = 0;

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

    function setManualSize(next, durationMs) {
      current = Math.round(clampCellSize(next));
      sizeIsDerived = false;
      updateSlider(elements.slider, current);
      updateSizeLabel(elements.label, current, baseline);
      animateToCellSize(current, durationMs, function () {
        // Snap back only for horizontal overflow. Testing full visibility here
        // meant that on a phone — where the 24 rows never fit the height —
        // every nudge of the slider was immediately overridden, so the control
        // fought the person using it. Vertical overflow just scrolls.
        if (!fitsWidth(elements)) {
          current = fitToLargestSize(elements, fitsWidth, minCellSizePx, current);
          updateSlider(elements.slider, current);
          updateSizeLabel(elements.label, current, baseline);
        }
        persistSize(current);
      });
    }

    function fitForCurrentLayout(allowGrow) {
      stopAnimation();
      var canGrow = allowGrow === true;
      var previous = current;
      // Height-aware fit, floored at the comfortable size: chasing 24 rows onto
      // a landscape phone would otherwise drive the text to the absolute floor
      // and leave a postage-stamp terminal on a screen with width to spare.
      var fitted = fitToLargestSize(elements, fitsViewport, comfortableMinCellSizePx);
      if (!canGrow && Number.isFinite(previous) && fitted > previous) {
        current = writeCellSize(previous);
      } else {
        current = fitted;
      }
      // The comfortable floor is a preference, not a licence to clip columns —
      // if the result is too wide for the viewport, width wins.
      enforceWidthFit();
      // Nor a licence to clip rows. In the normal layout a terminal taller
      // than the viewport just scrolls, which is why the floor holds there.
      // Focus mode has nowhere to scroll to, so a floor it cannot honour puts
      // the first rows above the top edge instead.
      enforceViewportFit();
      persistSize(current);
      updateSlider(elements.slider, current);
      updateSizeLabel(elements.label, current, baseline);
    }

    function scheduleLayoutRefit() {
      stopLayoutRefitTimer();
      window.requestAnimationFrame(function () {
        fitForCurrentLayout(false);
      });
      layoutRefitTimer = window.setTimeout(function () {
        layoutRefitTimer = 0;
        fitForCurrentLayout(false);
      }, 180);
    }

    elements.slider.min = String(minCellSizePx);
    elements.slider.max = String(maxCellSizePx);

    var baseline = readCellSizeFromRoot();
    var stored = Number.parseFloat(localStorage.getItem(storageSizeKey) || "");
    var current = Number.isFinite(stored) && stored > 0 ? writeCellSize(stored) : writeCellSize(baseline);
    var enforcingWidthFit = false;
    var enforcingViewportFit = false;
    // Whether the size on screen was derived by shrinking to fit rather than
    // asked for. A derived size may be recomputed freely when the viewport
    // changes shape; one the user chose is left alone.
    var sizeIsDerived = false;

    // Neither the stored size nor the stylesheet baseline knows how wide this
    // screen is, so on a phone both open with the right-hand columns off the
    // edge. Shrink until the terminal fits across; never grow here, so a size
    // chosen deliberately on a roomy screen survives.
    //
    // This has to be re-checked whenever the screen content changes, not just
    // once at startup: at DOMContentLoaded the terminal is still empty and so
    // trivially "fits", and it only reaches its true width once the first
    // screen is painted into it.
    function enforceWidthFit() {
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
        sizeIsDerived = true;
        persistSize(current);
        updateSlider(elements.slider, current);
        updateSizeLabel(elements.label, current, baseline);
      } finally {
        enforcingWidthFit = false;
      }
    }

    // The vertical twin of enforceWidthFit, and deliberately narrower: it only
    // applies in focus mode, because that is the only layout with nowhere to
    // scroll. Everywhere else a terminal taller than the viewport is normal
    // and the comfortable floor is the right answer.
    function enforceViewportFit() {
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
        sizeIsDerived = true;
        persistSize(current);
        updateSlider(elements.slider, current);
        updateSizeLabel(elements.label, current, baseline);
      } finally {
        enforcingViewportFit = false;
      }
    }

    enforceWidthFit();
    persistSize(current);
    document.body.classList.remove("terminal-fit-active");
    updateSlider(elements.slider, current);
    updateSizeLabel(elements.label, current, baseline);

    elements.slider.addEventListener("input", function () {
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

    elements.zoomReset.addEventListener("click", function () {
      setManualSize(baseline, 120);
    });

    elements.fit.addEventListener("click", function (event) {
      stopAnimation();
      fitForCurrentLayout(true);
      // Pressing Fit is a deliberate choice of size, even though the number
      // came from a measurement -- do not silently re-derive it later.
      //
      // Only a real press, though: focus-mode.js refits by calling .click() on
      // this control, and a synthetic click carries isTrusted === false. Taking
      // that for a user's choice would pin whatever size focus mode happened to
      // land on and stop the terminal re-deriving on rotation.
      if (event && event.isTrusted) {
        sizeIsDerived = false;
      }
    });

    var lastViewportWidth = layoutViewportWidth();

    window.addEventListener("resize", function () {
      stopAnimation();
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
      // Turning a phone to landscape triples the available width. A size that
      // was only ever a shrink-to-fit should be re-derived from scratch so the
      // terminal grows back into the new space instead of staying postage-stamp
      // sized; a size the user picked is left where they put it.
      if (sizeIsDerived) {
        current = writeCellSize(baseline);
      }
      enforceWidthFit();
      updateSlider(elements.slider, current);
      updateSizeLabel(elements.label, current, baseline);
    });

    window.addEventListener("h3270:layout-changed", function () {
      scheduleLayoutRefit();
    });

    if (typeof MutationObserver !== "undefined") {
      var observer = new MutationObserver(function () {
        if (enforcingWidthFit) {
          return;
        }
        stopAnimation();
        if (typeof window.sizeScreenContainer === "function") {
          window.sizeScreenContainer();
        }
        // The first screen painted into the container is what gives the
        // terminal its real width, so this is where a too-large stored size
        // actually becomes visible as overflow.
        enforceWidthFit();
        updateSlider(elements.slider, current);
        updateSizeLabel(elements.label, current, baseline);
      });
      observer.observe(elements.container, { childList: true, subtree: true });
    }
  }

  document.addEventListener("DOMContentLoaded", init);
})();
