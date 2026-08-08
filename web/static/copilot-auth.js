// Copilot login flow — GitHub OAuth device flow.
//
// Exposes window.CopilotAuth with:
//   status()                        -> Promise<{logged_in, enterprise, model}>
//   ensureLogin()                   -> Promise<true|false>   (opens modal if needed)
//   logout()                        -> Promise<void>
//   setEnterprise(host)             -> Promise<void>
//
// All requests are same-origin POSTs, so the OriginRefererCheckMiddleware
// in the Go backend accepts them without a CSRF token.

(function () {
    "use strict";

    const ENDPOINTS = {
        status: "/api/copilot/status",
        loginStart: "/api/copilot/login/start",
        loginPoll: "/api/copilot/login/poll",
        logout: "/api/copilot/logout",
        enterprise: "/api/copilot/enterprise",
    };

    async function jsonPost(url, body) {
        const resp = await fetch(url, {
            method: "POST",
            credentials: "same-origin",
            headers: { "Content-Type": "application/json" },
            body: body == null ? "" : JSON.stringify(body),
        });
        const text = await resp.text();
        let data = null;
        try { data = text ? JSON.parse(text) : null; } catch (_) {}
        if (!resp.ok) {
            const msg = (data && data.error) || resp.statusText || ("HTTP " + resp.status);
            throw new Error(msg);
        }
        return data;
    }

    async function status() {
        const resp = await fetch(ENDPOINTS.status, { credentials: "same-origin" });
        if (!resp.ok) throw new Error("status: HTTP " + resp.status);
        return resp.json();
    }

    async function logout() { await jsonPost(ENDPOINTS.logout); }
    async function setEnterprise(url) { await jsonPost(ENDPOINTS.enterprise, { url: url || "" }); }

    // -- Modal -------------------------------------------------------------

    let modalEl = null;
    let modalPromise = null;
    let modalResolve = null;
    let pollTimer = 0;
    let currentLoginId = "";
    let focusTrap = { activate() {}, deactivate() {} };

    function buildModal() {
        if (modalEl) return modalEl;
        const wrap = document.createElement("div");
        wrap.className = "copilot-modal-backdrop";
        wrap.setAttribute("data-copilot-modal", "");
        wrap.hidden = true;
        wrap.innerHTML = `
            <div class="copilot-modal" role="dialog" aria-modal="true" aria-labelledby="copilot-login-title">
                <div class="copilot-modal-header">
                    <h3 id="copilot-login-title">Sign in to GitHub Copilot</h3>
                    <button type="button" class="copilot-modal-close" data-copilot-modal-close aria-label="Close">&times;</button>
                </div>
                <div class="copilot-modal-body">
                    <p class="copilot-modal-intro">
                        Open the GitHub verification page and enter the code below.
                        Leave this dialog open while you sign in; it will detect when you're done.
                    </p>
                    <ol class="copilot-modal-steps">
                        <li>
                            Open
                            <a data-copilot-verify-link href="#" target="_blank" rel="noopener noreferrer"></a>
                        </li>
                        <li>
                            Enter this code:
                            <code class="copilot-user-code" data-copilot-user-code></code>
                            <button type="button" class="copilot-copy-button" data-copilot-copy-code>Copy</button>
                        </li>
                    </ol>
                    <p class="copilot-modal-status" data-copilot-modal-status>Waiting for sign-in...</p>
                </div>
                <div class="copilot-modal-footer">
                    <button type="button" data-copilot-modal-cancel>Cancel</button>
                </div>
            </div>
        `;
        document.body.appendChild(wrap);
        modalEl = wrap;

        wrap.addEventListener("click", function (ev) {
            if (ev.target === wrap) closeModal(false);
        });
        if (window.ThreeSeventyWeb && window.ThreeSeventyWeb.createFocusTrap) {
            focusTrap = window.ThreeSeventyWeb.createFocusTrap(wrap);
        }
        wrap.querySelector("[data-copilot-modal-close]").addEventListener("click", () => closeModal(false));
        wrap.querySelector("[data-copilot-modal-cancel]").addEventListener("click", () => closeModal(false));
        wrap.querySelector("[data-copilot-copy-code]").addEventListener("click", function () {
            const btn = this;
            const code = wrap.querySelector("[data-copilot-user-code]").textContent || "";
            if (!code) return;
            const writePromise =
                navigator.clipboard && navigator.clipboard.writeText
                    ? navigator.clipboard.writeText(code)
                    : Promise.reject(new Error("clipboard unavailable"));
            writePromise.then(function () {
                btn.textContent = "Copied";
            }).catch(function () {
                // writeText rejects on insecure origins (plain http) or when
                // permission is denied — many 3270 hosts are reached over http.
                btn.textContent = "Copy failed";
            }).finally(function () {
                setTimeout(() => { btn.textContent = "Copy"; }, 1500);
            });
        });
        return wrap;
    }

    function setStatus(text) {
        const el = modalEl && modalEl.querySelector("[data-copilot-modal-status]");
        if (el) el.textContent = text;
    }

    function closeModal(success) {
        if (pollTimer) { clearTimeout(pollTimer); pollTimer = 0; }
        focusTrap.deactivate();
        if (modalEl) {
            modalEl.hidden = true;
            if (window.ThreeSeventyWeb && window.ThreeSeventyWeb.popModal) {
                window.ThreeSeventyWeb.popModal(modalEl);
            }
        }
        const resolve = modalResolve;
        modalResolve = null;
        modalPromise = null;
        if (resolve) resolve(!!success);
    }

    function openLoginModal() {
        if (modalPromise) return modalPromise;
        const wrap = buildModal();
        wrap.hidden = false;
        focusTrap.activate();
        if (window.ThreeSeventyWeb && window.ThreeSeventyWeb.pushModal) {
            window.ThreeSeventyWeb.pushModal(wrap, function () { closeModal(false); });
        }
        const cancelBtn = wrap.querySelector("[data-copilot-modal-cancel]");
        if (cancelBtn) cancelBtn.focus();
        modalPromise = new Promise((resolve) => { modalResolve = resolve; });
        setStatus("Requesting device code from GitHub...");
        wrap.querySelector("[data-copilot-user-code]").textContent = "----";
        const link = wrap.querySelector("[data-copilot-verify-link]");
        link.textContent = "github.com/login/device";
        link.href = "https://github.com/login/device";

        jsonPost(ENDPOINTS.loginStart).then(function (data) {
            currentLoginId = data.login_id || "";
            wrap.querySelector("[data-copilot-user-code]").textContent = data.user_code || "";
            const verify = data.verification_uri || "https://github.com/login/device";
            link.textContent = verify;
            link.href = verify;
            setStatus("Waiting for you to approve on github.com...");
            schedulePoll(Math.max(2, data.interval || 5) * 1000);
        }).catch(function (err) {
            setStatus("Failed to start login: " + (err && err.message || err));
        });

        return modalPromise;
    }

    function schedulePoll(intervalMs) {
        pollTimer = window.setTimeout(async function () {
            try {
                const res = await jsonPost(ENDPOINTS.loginPoll, { login_id: currentLoginId });
                if (!res || !res.status) {
                    schedulePoll(intervalMs);
                    return;
                }
                if (res.status === "success") { setStatus("Signed in."); closeModal(true); return; }
                if (res.status === "pending") { schedulePoll(intervalMs); return; }
                if (res.status === "slow_down") { schedulePoll(intervalMs + 5000); return; }
                if (res.status === "expired") { setStatus("Code expired. Close and try again."); return; }
                if (res.status === "denied") { setStatus("Sign-in denied."); return; }
                if (res.status === "superseded") { setStatus("This sign-in attempt was replaced by a newer one. Close and try again."); return; }
                setStatus(res.error || ("Sign-in failed: " + res.status));
            } catch (err) {
                setStatus("Polling failed: " + (err && err.message || err));
                schedulePoll(intervalMs);
            }
        }, intervalMs);
    }

    // ensureLogin is kept for callers that predate multi-provider support.
    // GitHub Copilot is now one backend among several, and the others are
    // unblocked by an API key rather than a device-flow login, so delegate to
    // the provider layer when it is present.
    async function ensureLogin() {
        if (window.AIProvider && typeof window.AIProvider.ensureReady === "function") {
            return window.AIProvider.ensureReady();
        }
        const s = await status();
        if (s && s.logged_in) return true;
        return openLoginModal();
    }

    window.CopilotAuth = {
        status,
        ensureLogin,
        logout,
        setEnterprise,
        openLoginModal,
    };
})();
