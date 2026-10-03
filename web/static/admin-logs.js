document.addEventListener("DOMContentLoaded", () => {
  const content = document.querySelector("[data-admin-logs-content]");
  const status = document.querySelector("[data-admin-logs-status]");
  const button = document.querySelector("[data-admin-logs-refresh]");
  if (!content) return;
  const refresh = async () => {
    button.disabled = true; status.textContent = "Loading logs…";
    try {
      const response = await fetch("/logs", { headers: { Accept: "application/json" } });
      const payload = await response.json();
      if (!response.ok) throw new Error(payload.error || "Could not load logs.");
      content.textContent = payload.content || "No log entries yet.";
      status.textContent = "Logs refreshed.";
    } catch (error) { status.textContent = error.message; }
    finally { button.disabled = false; }
  };
  button.addEventListener("click", refresh); refresh();
});
