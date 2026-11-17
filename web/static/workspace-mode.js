// Automation tools: whether the toolbar carries the Automation menu.
//
// The toolbar grew around the automation audience — of roughly 27 controls,
// three (Disconnect, Logs, Print) serve someone whose job is to use the
// mainframe application rather than to explore it. Exploration alone had
// twelve. A clerk opening 3270Web for the first time met a control panel for
// a testing tool.
//
// Nothing is removed. Off is the default and collapses the recording and
// exploration groups; On restores the full toolbar and is one click away.
// The choice persists per browser. Users see On and Off and nothing else: this
// used to be presented as a Business/Engineering "workspace mode", and the
// stored values below keep those names only because accounts and browsers
// already hold them.
(function () {
  "use strict";

  var STORAGE_KEY = "3270Web.workspaceMode.v1";
  function preferences() { return window.ThreeSeventyWeb && window.ThreeSeventyWeb.preferences; }
  // Stored values, not wording. "business" is Automation tools off and
  // "engineering" is on; renaming them would drop everyone's saved choice.
  var BUSINESS = "business";
  var ENGINEERING = "engineering";

  // What belongs to the automation surface: the Automation menu (recording,
  // playback and exploration, all of it) and the sample-app chip. The run
  // status widget is deliberately NOT here — workflow.js shows it whenever a
  // run is live and hides it otherwise, which is the right rule either way: an
  // operator with Automation tools off still needs to see what is driving
  // their terminal when Copilot starts a run.
  // The sample-app chip is here for the same reason as the menu: which local
  // test server is running and on which port is a development detail, not
  // something an application user reads.
  var AUTOMATION_SELECTORS = [
    "[data-menu-automation]",
    "[data-sample-status]"
  ];

  function readMode() {
    try {
      var stored = preferences() ? preferences().get("workspaceMode") : localStorage.getItem(STORAGE_KEY);
      if (stored === BUSINESS || stored === ENGINEERING) {
        return stored;
      }
    } catch (_) { /* private browsing, fall through */ }
    return BUSINESS;
  }

  function writeMode(mode) {
    try {
      if (preferences()) preferences().save({ workspaceMode: mode });
      else localStorage.setItem(STORAGE_KEY, mode);
    } catch (_) { /* nothing we can do, the session still works */ }
  }

  function applyMode(mode) {
    var automationVisible = mode === ENGINEERING;
    document.body.setAttribute("data-workspace-mode", mode);

    for (var i = 0; i < AUTOMATION_SELECTORS.length; i++) {
      var nodes = document.querySelectorAll(AUTOMATION_SELECTORS[i]);
      for (var j = 0; j < nodes.length; j++) {
        nodes[j].hidden = !automationVisible;
      }
    }

    var toggle = document.querySelector("[data-workspace-toggle]");
    if (toggle) {
      toggle.setAttribute("aria-pressed", String(automationVisible));
      // The sub-label shows the current state, so it reads as a switch:
      // "Automation tools — On".
      var label = automationVisible ? "On" : "Off";
      var labelEl = toggle.querySelector("[data-workspace-label]");
      if (labelEl) {
        labelEl.textContent = label;
      }
      // The accessible name starts with the visible words (WCAG 2.5.3) and
      // then says what the state means and what selecting it does.
      var hint = automationVisible
        ? "Automation tools: On — the Automation menu is shown. Select to turn off."
        : "Automation tools: Off — the Automation menu is hidden. Select to turn on.";
      toggle.setAttribute("aria-label", hint);
      toggle.setAttribute("title", hint);
      if (toggle.hasAttribute("data-tippy-content")) {
        toggle.setAttribute("data-tippy-content", hint);
      }
    }
  }

  function currentMode() {
    return document.body.getAttribute("data-workspace-mode") || readMode();
  }

  function setMode(mode) {
    var next = mode === ENGINEERING ? ENGINEERING : BUSINESS;
    writeMode(next);
    applyMode(next);
    if (window.ThreeSeventyWeb && typeof window.ThreeSeventyWeb.notify === "function") {
      window.ThreeSeventyWeb.notify(
        next === ENGINEERING
          ? "Automation tools on — the Automation menu is available."
          : "Automation tools off — the Automation menu is hidden.",
        "info",
        { duration: 2500 }
      );
    }
    // The palette hides commands whose control is hidden, so its contents
    // change with the mode; let it know.
    document.dispatchEvent(new CustomEvent("workspacemodechange", { detail: { mode: next } }));
  }

  document.addEventListener("DOMContentLoaded", function () {
    applyMode(readMode());

    var toggle = document.querySelector("[data-workspace-toggle]");
    if (toggle) {
      toggle.addEventListener("click", function () {
        setMode(currentMode() === ENGINEERING ? BUSINESS : ENGINEERING);
      });
    }
  });

  window.ThreeSeventyWeb = window.ThreeSeventyWeb || {};
  window.ThreeSeventyWeb.workspace = {
    mode: currentMode,
    setMode: setMode,
    toggle: function () {
      setMode(currentMode() === ENGINEERING ? BUSINESS : ENGINEERING);
    }
  };
})();
