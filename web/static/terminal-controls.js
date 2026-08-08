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
      slider: controls.querySelector("[data-terminal-size-slider]"),
      stepDown: controls.querySelector("[data-terminal-size-down]"),
      stepUp: controls.querySelector("[data-terminal-size-up]"),
      label: controls.querySelector("[data-terminal-size-label]"),
      fit: controls.querySelector("[data-terminal-fit]"),
      zoomReset: controls.querySelector("[data-terminal-zoom-reset]")
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

  function shellFullyVisible(shell) {
    if (!shell) {
      return false;
    }
    var rect = shell.getBoundingClientRect();
    return rect.left >= 0 && rect.top >= 0 && rect.right <= window.innerWidth + 1 && rect.bottom <= window.innerHeight + 1;
  }

  function fitsViewport(shell) {
    // Terminal sizing should be constrained by the terminal shell itself, not
    // by unrelated page chrome (e.g. an on-screen keyboard shown below it).
    return shellFullyVisible(shell);
  }

  // The width to fit against. It must be the *layout* viewport: when content
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

  // Width-only variant, used for automatic fitting. Vertical overflow just
  // means the page scrolls, which is normal; horizontal overflow clips columns
  // off the right edge of a 3270 screen, which is not. Requiring both would
  // force the text smaller than it needs to be on a phone held in portrait.
  function fitsViewportWidth(shell) {
    if (!shell) {
      return false;
    }
    var rect = shell.getBoundingClientRect();
    return rect.left >= -1 && rect.right <= layoutViewportWidth() + 1;
  }

  function fitToLargestSize(elements, predicate, floorPx) {
    var fits = predicate || fitsViewport;
    var floor = Number.isFinite(floorPx) ? floorPx : minCellSizePx;
    var low = floor;
    var high = maxCellSizePx;
    var best = floor;

    while (low <= high) {
      var mid = Math.floor((low + high) / 2);
      writeCellSize(mid);
      if (fits(elements.shell)) {
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
        if (!fitsViewportWidth(elements.shell)) {
          current = fitToLargestSize(elements, fitsViewportWidth);
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
      if (enforcingWidthFit || fitsViewportWidth(elements.shell)) {
        return;
      }
      // writeCellSize re-runs the container sizing, which can mutate the very
      // subtree the MutationObserver below is watching. Guard against
      // re-entering the binary search from our own writes.
      enforcingWidthFit = true;
      try {
        current = fitToLargestSize(elements, fitsViewportWidth);
        sizeIsDerived = true;
        persistSize(current);
        updateSlider(elements.slider, current);
        updateSizeLabel(elements.label, current, baseline);
      } finally {
        enforcingWidthFit = false;
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
