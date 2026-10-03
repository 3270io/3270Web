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
        document.querySelector("[data-menu-automation] [data-app-menu-trigger]")?.click();
      } else document.querySelector(".renderer-form input, .renderer-form textarea")?.focus();
    }));
    const show = document.querySelector("[data-show-automation]");
    const sync = () => { if (show) show.hidden = ui.workspace?.mode() === "engineering"; };
    show?.addEventListener("click", () => { ui.workspace?.setMode("engineering"); sync(); });
    document.addEventListener("workspacemodechange", sync); sync();
  });
})();
