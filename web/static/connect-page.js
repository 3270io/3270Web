document.addEventListener("DOMContentLoaded", () => {
  const form = document.getElementById("connect-form");
  const host = form && form.elements.hostname;
  const connect = document.getElementById("connect-btn");
  const notify = (message, type) => window.ThreeSeventyWeb?.notify?.(message, type || "info");
  const loading = () => { connect.disabled = true; connect.textContent = "Connecting…"; connect.setAttribute("aria-busy", "true"); };
  form?.addEventListener("submit", loading);
  window.addEventListener("pageshow", () => { if (connect) { connect.disabled = false; connect.textContent = "Connect"; connect.removeAttribute("aria-busy"); } });
  document.querySelector("[data-connect-error]")?.focus();
  document.querySelector("[data-copy-diagnostic]")?.addEventListener("click", async () => {
    const details = document.querySelector("[data-connection-diagnostic]");
    try { await navigator.clipboard.writeText(details.textContent); notify("Connection details copied.", "success"); }
    catch (_) { const range = document.createRange(); range.selectNodeContents(details); const selection = getSelection(); selection.removeAllRanges(); selection.addRange(range); notify("Select and copy the highlighted details.", "info"); }
  });
  const advanced = document.querySelector("[data-connection-options]");
  advanced?.addEventListener("change", event => { if (event.target.name !== "advanced") form.elements.advanced.checked = true; });
  document.querySelector("[data-save-connection]")?.addEventListener("click", () => {
    if (!host.value.trim()) { host.focus(); notify("Enter a mainframe address first.", "warning"); return; }
    window.ThreeSeventyWeb?.connectionProfiles?.saveCurrent?.(form);
  });
  document.querySelector("[data-try-sample]")?.addEventListener("click", () => {
    if (host.readOnly) return;
    host.value = "sampleapp:petstore";
    if (form.elements.profile) form.elements.profile.value = "";
    if (form.elements.advanced) form.elements.advanced.checked = false;
    form.requestSubmit();
  });
  const modal = document.querySelector("[data-sample-modal]");
  const close = () => { if (!modal) return; modal.hidden = true; window.ThreeSeventyWeb?.popModal?.(modal); };
  document.querySelector("[data-open-sample-modal]")?.addEventListener("click", () => {
    modal.hidden = false;
    window.ThreeSeventyWeb?.pushModal?.(modal, close, { initialFocus: modal.querySelector("select") });
  });
  document.querySelector("[data-sample-close]")?.addEventListener("click", close);
  modal?.addEventListener("click", event => { if (event.target === modal) close(); });
  document.addEventListener("keydown", event => { if (event.key === "Escape" && modal && !modal.hidden) close(); });
  document.querySelector("[data-sample-start]")?.addEventListener("click", () => {
    if (host.readOnly) return;
    const app = document.getElementById("sample-app-select");
    const port = document.getElementById("sample-app-port");
    if (!app.value || !port.value) return;
    host.value = `sampleapp:${app.value}:${port.value}`;
    if (form.elements.profile) form.elements.profile.value = "";
    if (form.elements.advanced) form.elements.advanced.checked = false;
    close(); form.requestSubmit();
  });
});
