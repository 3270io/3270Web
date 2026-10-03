(function () {
  "use strict";
  const account = document.body.dataset.account || "local";
  const authenticated = account !== "local";
  const key = "3270Web.uiPreferences.v1." + account;
  let prefs = {
    workspaceMode: document.body.dataset.savedWorkspace || "",
    quickStartDismissed: document.body.dataset.quickstartDismissed === "true",
    terminalView: document.body.dataset.terminalView || ""
  };
  if (!authenticated) {
    try {
      prefs = Object.assign(prefs, JSON.parse(localStorage.getItem(key) || "{}"));
      if (!prefs.workspaceMode) prefs.workspaceMode = localStorage.getItem("3270Web.workspaceMode.v1") || "";
    } catch (_) {}
  }
  let pending = Promise.resolve();
  window.ThreeSeventyWeb = window.ThreeSeventyWeb || {};
  window.ThreeSeventyWeb.preferences = {
    get(name) { return prefs[name]; },
    save(patch) {
      prefs = Object.assign(prefs, patch);
      if (!authenticated) {
        try { localStorage.setItem(key, JSON.stringify(prefs)); }
        catch (_) { window.ThreeSeventyWeb.notify?.("Your choice works now, but this browser could not save it.", "warning"); }
        return Promise.resolve();
      }
      // Serialize writes so a quick goal + dismissal cannot overwrite each other.
      pending = pending.catch(() => {}).then(() => fetch("/api/preferences", {
        method: "PATCH", headers: { "Content-Type": "application/json", Accept: "application/json" },
        body: JSON.stringify(patch)
      })).then(response => { if (!response.ok) throw new Error("save failed"); }).catch(() => {
        window.ThreeSeventyWeb.notify?.("Your choice works now, but could not be saved to your account. Try again when connected.", "warning");
      });
      return pending;
    }
  };
})();
