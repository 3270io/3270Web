(function () {
  "use strict";

  var minCellSizePx = 12;
  var maxCellSizePx = 36;
  var storageSizeKey = "h3270TerminalCellSizePx";
  var storageMaximizedKey = "h3270TerminalMaximized";
  var manualSizeBeforeMaximize = null;

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
      zoomReset: controls.querySelector("[data-terminal-zoom-reset]"),
      maximize: controls.querySelector("[data-terminal-maximize]")
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

  function setMaximizedState(button, enabled) {
    if (!button) {
      return;
    }
    var label = enabled ? "Restore standard size" : "Maximize terminal";
    button.setAttribute("aria-label", label);
    button.setAttribute("title", label);
    button.setAttribute("data-tippy-content", label);
    button.setAttribute("aria-pressed", enabled ? "true" : "false");
    button.classList.toggle("is-active", enabled);
    if (button._tippy && typeof button._tippy.setContent === "function") {
      button._tippy.setContent(label);
    }
    document.body.classList.toggle("terminal-fit-active", enabled);
  }

  function viewportHasNoScrollbars() {
    var de = document.documentElement;
    return de.scrollWidth <= de.clientWidth + 1 && de.scrollHeight <= de.clientHeight + 1;
  }

  function shellFullyVisible(shell) {
    if (!shell) {
      return false;
    }
    var rect = shell.getBoundingClientRect();
    return rect.left >= 0 && rect.top >= 0 && rect.right <= window.innerWidth + 1 && rect.bottom <= window.innerHeight + 1;
  }

  function fitsViewport(shell) {
    return viewportHasNoScrollbars() && shellFullyVisible(shell);
  }

  function fitToLargestSize(elements) {
    var low = minCellSizePx;
    var high = maxCellSizePx;
    var best = minCellSizePx;

    while (low <= high) {
      var mid = Math.floor((low + high) / 2);
      writeCellSize(mid);
      if (fitsViewport(elements.shell)) {
        best = mid;
        low = mid + 1;
      } else {
        high = mid - 1;
      }
    }

    return writeCellSize(best);
  }

  function persistSize(current, maximized) {
    localStorage.setItem(storageSizeKey, String(current));
    localStorage.setItem(storageMaximizedKey, maximized ? "1" : "0");
  }

  function init() {
    var elements = getElements();
    if (!elements || !elements.slider || !elements.zoomReset || !elements.maximize || !elements.fit || !elements.stepDown || !elements.stepUp) {
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
      maximized = false;
      setMaximizedState(elements.maximize, false);
      current = Math.round(clampCellSize(next));
      updateSlider(elements.slider, current);
      updateSizeLabel(elements.label, current, baseline);
      animateToCellSize(current, durationMs, function () {
        if (!fitsViewport(elements.shell)) {
          current = fitToLargestSize(elements);
          updateSlider(elements.slider, current);
          updateSizeLabel(elements.label, current, baseline);
        }
        persistSize(current, false);
      });
    }

    function fitForCurrentLayout(allowGrow) {
      stopAnimation();
      var canGrow = allowGrow === true;
      var previous = current;
      var fitted = fitToLargestSize(elements);
      if (!canGrow && !maximized && Number.isFinite(previous) && fitted > previous) {
        current = writeCellSize(previous);
      } else {
        current = fitted;
      }
      persistSize(current, maximized);
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
    var maximized = localStorage.getItem(storageMaximizedKey) === "1";

    if (maximized) {
      manualSizeBeforeMaximize = current;
      current = fitToLargestSize(elements);
      persistSize(current, true);
    } else {
      persistSize(current, false);
    }

    setMaximizedState(elements.maximize, maximized);
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

    elements.fit.addEventListener("click", function () {
      stopAnimation();
      maximized = false;
      setMaximizedState(elements.maximize, false);
      fitForCurrentLayout(true);
    });

    elements.maximize.addEventListener("click", function () {
      stopAnimation();
      maximized = !maximized;
      if (maximized) {
        manualSizeBeforeMaximize = current;
        current = fitToLargestSize(elements);
      } else {
        var restore = Number.isFinite(manualSizeBeforeMaximize) ? manualSizeBeforeMaximize : baseline;
        current = writeCellSize(restore);
      }
      persistSize(current, maximized);
      setMaximizedState(elements.maximize, maximized);
      updateSlider(elements.slider, current);
      updateSizeLabel(elements.label, current, baseline);
    });

    window.addEventListener("resize", function () {
      stopAnimation();
      if (!maximized) {
        return;
      }
      fitForCurrentLayout();
    });

    window.addEventListener("h3270:layout-changed", function () {
      scheduleLayoutRefit();
    });

    if (typeof MutationObserver !== "undefined") {
      var observer = new MutationObserver(function () {
        stopAnimation();
        if (maximized) {
          current = fitToLargestSize(elements);
          persistSize(current, true);
        } else if (typeof window.sizeScreenContainer === "function") {
          window.sizeScreenContainer();
        }
        updateSlider(elements.slider, current);
        updateSizeLabel(elements.label, current, baseline);
      });
      observer.observe(elements.container, { childList: true, subtree: true });
    }
  }

  document.addEventListener("DOMContentLoaded", init);
})();
