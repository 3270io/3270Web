// Workspace mode: who the toolbar is for.
//
// The toolbar grew around the automation audience — of roughly 27 controls,
// three (Disconnect, Logs, Print) serve someone whose job is to use the
// mainframe application rather than to explore it. Chaos alone had twelve.
// A clerk opening 3270Web for the first time met a control panel for a
// testing tool.
//
// Nothing is removed. Business mode is the default surface and collapses the
// recording and chaos groups; Engineering mode restores the full toolbar and
// is one click away. The choice persists per browser.
(function () {
  "use strict";

  var STORAGE_KEY = "3270Web.workspaceMode.v1";
  var BUSINESS = "business";
  var ENGINEERING = "engineering";

  // What belongs to the automation surface: the Automation menu (recording,
  // playback and chaos, all of it) and the sample-app chip. The workflow
  // status widget is deliberately NOT here — workflow.js shows it whenever a
  // run is live and hides it otherwise, which is the right rule in both modes:
  // an operator in Business mode still needs to see what is driving their
  // terminal when Copilot starts a run.
  // The sample-app chip is here for the same reason as the menu: which local
  // test server is running and on which port is a development detail, not
  // something a business user reads.
  var AUTOMATION_SELECTORS = [
    "[data-menu-automation]",
    "[data-sample-status]"
  ];

  function readMode() {
    try {
      var stored = localStorage.getItem(STORAGE_KEY);
      if (stored === BUSINESS || stored === ENGINEERING) {
        return stored;
      }
    } catch (_) { /* private browsing, fall through */ }
    return BUSINESS;
  }

  function writeMode(mode) {
    try {
      localStorage.setItem(STORAGE_KEY, mode);
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
      var label = automationVisible
        ? "Engineering — recording and chaos shown"
        : "Business — terminal only";
      var labelEl = toggle.querySelector("[data-workspace-label]");
      if (labelEl) {
        labelEl.textContent = label;
      }
      var hint = automationVisible
        ? "Engineering mode — the Automation menu is shown. Switch off for Business mode."
        : "Business mode — terminal only. Switch on for the Automation menu (recording, chaos).";
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
          ? "Engineering mode — the Automation menu is available."
          : "Business mode — the Automation menu is hidden.",
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
