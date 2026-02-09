(function () {
  "use strict";

  var storageKey = "terminalToolsWidgetCollapsed";

  function setWidgetState(widget, toggle, collapsed) {
    if (!widget || !toggle) {
      return;
    }
    widget.classList.toggle("is-collapsed", collapsed);
    toggle.setAttribute("aria-expanded", collapsed ? "false" : "true");
    var label = collapsed ? "Open terminal tools" : "Collapse terminal tools";
    toggle.setAttribute("aria-label", label);
    toggle.setAttribute("title", label);
    toggle.setAttribute("data-tippy-content", label);
    if (toggle._tippy && typeof toggle._tippy.setContent === "function") {
      toggle._tippy.setContent(label);
    }
  }

  function init() {
    var widget = document.querySelector("[data-terminal-tools-widget]");
    var toggle = document.querySelector("[data-terminal-tools-toggle]");
    if (!widget || !toggle) {
      return;
    }

    var collapsed = true;
    try {
      collapsed = localStorage.getItem(storageKey) !== "0";
    } catch (err) {
      collapsed = true;
    }
    setWidgetState(widget, toggle, collapsed);

    toggle.addEventListener("click", function () {
      var nextCollapsed = !widget.classList.contains("is-collapsed");
      setWidgetState(widget, toggle, nextCollapsed);
      try {
        localStorage.setItem(storageKey, nextCollapsed ? "1" : "0");
      } catch (err) {
        // ignore storage failures
      }
    });
  }

  document.addEventListener("DOMContentLoaded", init);
})();
