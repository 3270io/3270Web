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

    function collapse(persist) {
      if (widget.classList.contains("is-collapsed")) {
        return;
      }
      setWidgetState(widget, toggle, true);
      if (!persist) {
        return;
      }
      try {
        localStorage.setItem(storageKey, "1");
      } catch (err) {
        // ignore storage failures
      }
    }

    toggle.addEventListener("click", function () {
      var nextCollapsed = !widget.classList.contains("is-collapsed");
      setWidgetState(widget, toggle, nextCollapsed);
      try {
        localStorage.setItem(storageKey, nextCollapsed ? "1" : "0");
      } catch (err) {
        // ignore storage failures
      }
    });

    // Expanded, the panel is a floating box over the page. On a wide screen it
    // sits in the corner beside everything else; on a phone it spans the width
    // and covers the header underneath it, so the controls it covers stop
    // responding with nothing on screen to say a panel is in the way.
    //
    // Treating it as the popover it looks like fixes that without moving it:
    // a tap anywhere else, or Escape, puts it away. Not persisted — dismissing
    // a panel to get at what is behind it is not a decision to keep it shut
    // next time.
    document.addEventListener("pointerdown", function (event) {
      if (widget.classList.contains("is-collapsed")) {
        return;
      }
      if (event.target && widget.contains(event.target)) {
        return;
      }
      collapse(false);
    });

    document.addEventListener("keydown", function (event) {
      if (event.key !== "Escape" || event.defaultPrevented) {
        return;
      }
      if (widget.classList.contains("is-collapsed")) {
        return;
      }
      collapse(false);
    });
  }

  document.addEventListener("DOMContentLoaded", init);
})();
