document.addEventListener("DOMContentLoaded", () => {
  const form = document.getElementById("connect-form");
  const host = form && form.elements.hostname;
  const connect = document.getElementById("connect-btn");
  const notify = (message, type) => window.ThreeSeventyWeb?.notify?.(message, type || "info");
  // A connect is a full-page POST that can take as long as the host takes to
  // answer — a bad name failed after two seconds, a slow one can take far
  // longer — and the page gave no sign that the click had landed beyond the
  // button itself. So while it is pending: the button is off and says what it
  // is doing, the form is marked busy, a polite live region names the host
  // (screen-reader users get no other cue), and Cancel is offered.
  //
  // The live region sits outside the form, deliberately: aria-busy on a form
  // asks assistive technology to hold back changes inside it, which would
  // silence the very announcement made while the form is busy.
  const status = document.querySelector("[data-connect-status]");
  const cancel = document.querySelector("[data-connect-cancel]");
  let connecting = false;
  const loading = () => {
    connecting = true;
    connect.disabled = true;
    connect.textContent = "Connecting…";
    connect.setAttribute("aria-busy", "true");
    form.setAttribute("aria-busy", "true");
    const target = host && host.value.trim();
    if (status) status.textContent = target ? `Connecting to ${target}…` : "Connecting…";
    if (cancel) cancel.hidden = false;
  };
  const idle = (message) => {
    connecting = false;
    if (!connect) return;
    connect.disabled = false;
    connect.textContent = "Connect";
    connect.removeAttribute("aria-busy");
    form?.removeAttribute("aria-busy");
    if (status) status.textContent = message || "";
    if (cancel) cancel.hidden = true;
  };
  // Capture phase, so a second submit while one is in flight — Enter in the
  // address field, or one of the sample buttons calling requestSubmit() — is
  // stopped before the browser starts a second connect behind the first.
  form?.addEventListener("submit", event => {
    if (connecting) { event.preventDefault(); event.stopImmediatePropagation(); return; }
    loading();
  }, true);
  // The only thing that can be aborted from here is the browser's wait: stop()
  // ends the pending navigation, so the page is usable again at once. The server
  // has no way to be told, so a host that was merely slow may still finish
  // connecting in the background; that session is never used and is reaped when
  // idle. This is what the browser's own Stop button does, with a label.
  cancel?.addEventListener("click", () => {
    window.stop();
    idle("Connection cancelled.");
    host?.focus();
  });
  // A page restored from the back/forward cache comes back with whatever state
  // it was left in — which, after a connect, is the busy one.
  window.addEventListener("pageshow", () => idle());
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
