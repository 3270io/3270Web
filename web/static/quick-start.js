(function () {
  "use strict";
  document.addEventListener("DOMContentLoaded", () => {
    const ui = window.ThreeSeventyWeb;
    const section = document.querySelector("[data-quick-start]");
    if (!section || !ui?.preferences || document.body.dataset.embedded === "true") return;
    section.hidden = !!ui.preferences.get("quickStartDismissed");
    const exercise = section.querySelector("[data-sample-exercise]");
    if (exercise) exercise.hidden = !document.body.dataset.targetHost?.startsWith("sampleapp:petstore");
    const dismiss = () => { section.hidden = true; return ui.preferences.save({ quickStartDismissed: true }); };
    section.querySelector("[data-quick-start-dismiss]").addEventListener("click", dismiss);
    section.querySelectorAll("[data-start-goal]").forEach(button => button.addEventListener("click", async () => {
      const goal = button.dataset.startGoal;
      ui.workspace?.setMode(goal === "terminal" ? "business" : "engineering");
      await dismiss();
      if (goal === "record") {
        // Open the menu, rather than recording unexpectedly before the user confirms.
        document.querySelector("[data-menu-automation] [data-app-menu-trigger]")?.click();
        document.querySelector("[data-recording-start] button")?.focus();
      } else if (goal === "explore") {
        // Straight to the start dialog, which says what an exploration does
        // and waits for a yes. The menu on its own left the user to guess
        // which item they had just been promised.
        if (typeof window.ChaosUI?.openStartDialog === "function") window.ChaosUI.openStartDialog("start");
        else document.querySelector("[data-menu-automation] [data-app-menu-trigger]")?.click();
      } else document.querySelector(".renderer-form input, .renderer-form textarea")?.focus();
    }));
    const show = document.querySelector("[data-show-automation]");
    const sync = () => { if (show) show.hidden = ui.workspace?.mode() === "engineering"; };
    show?.addEventListener("click", () => { ui.workspace?.setMode("engineering"); sync(); });
    document.addEventListener("workspacemodechange", sync); sync();

    // The banner mentions the PF keys, and where they are depends on whether the
    // on-screen keypad is showing — which it is not by default on a desktop.
    // "PF keys use the on-screen keypad" sent people looking for a keypad that
    // was not there. The wording follows the keypad's real state, and while it
    // is hidden the banner can show it. That goes through the View menu's own
    // toggle, so there is one piece of logic for showing the keypad, not two.
    const keypad = document.getElementById("keypad");
    const keypadToggle = document.querySelector("[data-keypad-toggle]");
    const keysHint = section.querySelector("[data-quick-start-keys]");
    const showKeypad = section.querySelector("[data-quick-start-show-keypad]");
    const syncKeypad = () => {
      const hidden = !keypad || keypad.hidden;
      if (keysHint) {
        keysHint.textContent = hidden
          ? "Use F1–F12 for PF keys, or show the on-screen keypad."
          : "Use F1–F12 for PF keys, or the on-screen keypad.";
      }
      if (showKeypad) showKeypad.hidden = !hidden || !keypadToggle;
    };
    if (keypad) new MutationObserver(syncKeypad).observe(keypad, { attributes: true, attributeFilter: ["hidden"] });
    showKeypad?.addEventListener("click", () => {
      keypadToggle?.click();
      // The button hides itself now that the keypad is up; put the keyboard back
      // where it was working rather than leaving focus on nothing.
      ui.terminal?.focusTerminal?.();
    });
    syncKeypad();

    // The banner is a first-run aid; once the terminal is in use it is only
    // chrome, and during an automation run it sits over the thing being
    // watched. So it goes away by itself at the first terminal action — a key
    // sent to the host, or an automation run starting — and Dismiss stays for
    // anyone who wants it gone sooner. Dismissal is saved like Dismiss's, so a
    // page reload (starting a recording is one) does not bring it back.
    const settle = () => { if (!section.hidden) dismiss(); };
    // A key sent, and the fetch-started runs, are seen as the requests they
    // make rather than by hooking the code that makes them: keyboard.js owns
    // sending, and every route to it (typing, PF keys, the keypad, hotspots,
    // key bindings) ends in the same POST.
    const ACTION_REQUEST = /\/(submit\/async|chaos\/start|tasks\/run)(\?|$)/;
    try {
      new PerformanceObserver(list => {
        if (list.getEntries().some(entry => entry.initiatorType === "fetch" && ACTION_REQUEST.test(entry.name))) settle();
      }).observe({ type: "resource" });
    } catch (_) { /* no resource timing: Dismiss still works */ }
    // Playback and recording start with a form POST that reloads the page, so
    // the request is gone before anything could observe it.
    document.addEventListener("submit", event => {
      const action = event.target?.getAttribute?.("action");
      if (action === "/record/start" || action === "/workflow/play" || action === "/workflow/debug") settle();
    }, true);
    // And a run that is already going when the page loads (or that another
    // surface starts): the active-run row and the recording indicator are shown
    // by workflow.js whenever there is one.
    const runSurfaces = document.querySelectorAll("[data-active-run-container], [data-recording-indicator]");
    const runActive = () => Array.from(runSurfaces).some(el => !el.hidden);
    if (runSurfaces.length) {
      const watch = new MutationObserver(() => { if (runActive()) settle(); });
      runSurfaces.forEach(el => watch.observe(el, { attributes: true, attributeFilter: ["hidden"] }));
      if (runActive()) settle();
    }
  });
})();
