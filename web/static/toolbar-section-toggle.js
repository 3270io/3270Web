(function () {
  "use strict";

  var recordingStorageKey = "recordingControlsCollapsed";
  var chaosStorageKey = "chaosControlsCollapsed";

  function setSectionState(container, toggle, collapsed, openLabel, collapseLabel) {
    if (!container || !toggle) {
      return;
    }
    container.classList.toggle("is-collapsed", collapsed);
    toggle.setAttribute("aria-expanded", collapsed ? "false" : "true");
    var label = collapsed ? openLabel : collapseLabel;
    toggle.setAttribute("aria-label", label);
    toggle.setAttribute("title", label);
    toggle.setAttribute("data-tippy-content", label);
    if (toggle._tippy && typeof toggle._tippy.setContent === "function") {
      toggle._tippy.setContent(label);
    }
  }

  function initSection(containerSelector, toggleSelector, storageKey, openLabel, collapseLabel) {
    var container = document.querySelector(containerSelector);
    var toggle = document.querySelector(toggleSelector);
    if (!container || !toggle) {
      return;
    }

    var collapsed = false;
    try {
      collapsed = localStorage.getItem(storageKey) === "1";
    } catch (err) {
      collapsed = false;
    }
    setSectionState(container, toggle, collapsed, openLabel, collapseLabel);

    toggle.addEventListener("click", function () {
      var nextCollapsed = !container.classList.contains("is-collapsed");
      setSectionState(container, toggle, nextCollapsed, openLabel, collapseLabel);
      try {
        localStorage.setItem(storageKey, nextCollapsed ? "1" : "0");
      } catch (err) {
        // ignore storage failures
      }
    });
  }

  function init() {
    initSection(
      "[data-recording-controls]",
      "[data-recording-toggle]",
      recordingStorageKey,
      "Expand recording controls",
      "Collapse recording controls"
    );
    initSection(
      "[data-chaos-controls]",
      "[data-chaos-toggle]",
      chaosStorageKey,
      "Expand chaos controls",
      "Collapse chaos controls"
    );
  }

  document.addEventListener("DOMContentLoaded", init);
})();
