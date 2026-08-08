// AI provider selection for the chat panel.
//
// The panel can talk to GitHub Copilot, Claude, OpenAI, Google AI, Ollama
// (local or cloud), or any OpenAI-compatible endpoint. This module owns the
// settings dialog and the "is the selected provider usable yet?" gate; the
// panel itself stays provider-agnostic because the backend normalizes every
// provider onto the same streaming protocol.
//
// Exposes window.AIProvider with:
//   status()                 -> Promise<{provider, providerLabel, ready, needs, model, ...}>
//   catalogue()              -> Promise<{providers, settings}>
//   save(patch)              -> Promise<settings>   (partial update)
//   forget()                 -> Promise<void>       (clear current credentials)
//   openSettings()           -> Promise<boolean>    (true if the user saved)
//   ensureReady()            -> Promise<boolean>    (prompts for whatever is missing)
//   onChange(fn)             -> subscribe to provider/model changes
//
// All requests are same-origin, so OriginRefererCheckMiddleware accepts them
// without a CSRF token.

(function () {
    "use strict";

    const ENDPOINTS = {
        providers: "/api/ai/providers",
        status: "/api/ai/status",
        config: "/api/ai/config",
        forget: "/api/ai/forget",
    };

    let catalogueCache = null;   // {providers: [...], settings: {...}}
    const listeners = [];

    function notifyChanged(status) {
        for (const fn of listeners) {
            try { fn(status); } catch (_) {}
        }
    }

    async function readJSON(resp) {
        const text = await resp.text();
        let data = null;
        try { data = text ? JSON.parse(text) : null; } catch (_) {}
        if (!resp.ok) {
            throw new Error((data && data.error) || resp.statusText || ("HTTP " + resp.status));
        }
        return data;
    }

    async function getJSON(url) {
        return readJSON(await fetch(url, { credentials: "same-origin" }));
    }

    async function postJSON(url, body) {
        return readJSON(await fetch(url, {
            method: "POST",
            credentials: "same-origin",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify(body || {}),
        }));
    }

    function status() { return getJSON(ENDPOINTS.status); }

    async function catalogue(force) {
        if (!catalogueCache || force) catalogueCache = await getJSON(ENDPOINTS.providers);
        return catalogueCache;
    }

    async function save(patch) {
        const settings = await postJSON(ENDPOINTS.config, patch);
        if (catalogueCache) catalogueCache.settings = settings;
        return settings;
    }

    async function forget(providerId) {
        await postJSON(ENDPOINTS.forget, providerId ? { provider: providerId } : {});
        catalogueCache = null;
        notifyChanged(await status());
    }

    // -- Dialog ------------------------------------------------------------

    let modalEl = null;
    let modalPromise = null;
    let modalResolve = null;
    let focusTrap = { activate() {}, deactivate() {} };

    function buildModal() {
        if (modalEl) return modalEl;
        const wrap = document.createElement("div");
        wrap.className = "copilot-modal-backdrop";
        wrap.setAttribute("data-ai-modal", "");
        wrap.hidden = true;
        wrap.innerHTML = `
            <div class="copilot-modal ai-provider-modal" role="dialog" aria-modal="true" aria-labelledby="ai-provider-title">
                <div class="copilot-modal-header">
                    <h3 id="ai-provider-title">AI provider</h3>
                    <button type="button" class="copilot-modal-close" data-ai-close aria-label="Close">&times;</button>
                </div>
                <div class="copilot-modal-body">
                    <label class="ai-field">
                        <span class="ai-field-label" for="ai-provider-select">Provider</span>
                        <select id="ai-provider-select" data-ai-provider></select>
                    </label>
                    <p class="ai-field-help" data-ai-blurb></p>

                    <div class="ai-field ai-field-inline" data-ai-signin-row hidden>
                        <button type="button" class="ai-secondary-btn" data-ai-signin>Sign in with GitHub</button>
                        <span class="ai-field-help" data-ai-signin-state></span>
                    </div>

                    <label class="ai-field" data-ai-baseurl-row hidden>
                        <span class="ai-field-label" for="ai-baseurl">Endpoint URL</span>
                        <input id="ai-baseurl" type="url" data-ai-baseurl spellcheck="false" autocomplete="off">
                    </label>

                    <label class="ai-field" data-ai-key-row hidden>
                        <span class="ai-field-label" for="ai-key" data-ai-key-label>API key</span>
                        <input id="ai-key" type="password" data-ai-key spellcheck="false" autocomplete="off">
                    </label>
                    <p class="ai-field-help" data-ai-key-help hidden></p>

                    <label class="ai-field" data-ai-model-row>
                        <span class="ai-field-label" for="ai-model">Model</span>
                        <input id="ai-model" type="text" list="ai-model-suggestions" data-ai-model spellcheck="false" autocomplete="off">
                        <datalist id="ai-model-suggestions" data-ai-model-list></datalist>
                    </label>

                    <p class="copilot-modal-status" data-ai-status hidden></p>
                </div>
                <div class="copilot-modal-footer">
                    <button type="button" data-ai-cancel>Cancel</button>
                    <button type="button" class="ai-primary-btn" data-ai-save>Save</button>
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
        wrap.querySelector("[data-ai-close]").addEventListener("click", () => closeModal(false));
        wrap.querySelector("[data-ai-cancel]").addEventListener("click", () => closeModal(false));
        wrap.querySelector("[data-ai-provider]").addEventListener("change", function () {
            renderFields(this.value);
        });
        wrap.querySelector("[data-ai-signin]").addEventListener("click", onSignInClicked);
        wrap.querySelector("[data-ai-save]").addEventListener("click", onSaveClicked);
        return wrap;
    }

    function q(sel) { return modalEl ? modalEl.querySelector(sel) : null; }

    function setStatusText(text, isError) {
        const el = q("[data-ai-status]");
        if (!el) return;
        el.textContent = text || "";
        // The status strip is a bordered callout; leave it out of the layout
        // entirely rather than rendering an empty bar under the form.
        el.hidden = !text;
        el.classList.toggle("ai-status-error", !!isError);
    }

    function providerById(id) {
        const list = (catalogueCache && catalogueCache.providers) || [];
        return list.find((p) => p.id === id) || null;
    }

    function savedFor(id) {
        const all = (catalogueCache && catalogueCache.settings && catalogueCache.settings.providers) || {};
        return all[id] || {};
    }

    // renderFields reshapes the dialog for the selected provider: which
    // credential it needs, whether its endpoint is editable, and what its
    // models are called.
    function renderFields(providerId) {
        const prov = providerById(providerId);
        if (!prov) return;
        const saved = savedFor(providerId);

        q("[data-ai-blurb]").textContent = prov.blurb || "";

        const isOAuth = prov.auth === "oauth-device";
        q("[data-ai-signin-row]").hidden = !isOAuth;
        if (isOAuth) refreshSignInState();

        const baseRow = q("[data-ai-baseurl-row]");
        baseRow.hidden = !prov.baseUrlEditable;
        const baseInput = q("[data-ai-baseurl]");
        baseInput.value = saved.baseUrl || "";
        baseInput.placeholder = prov.defaultBaseUrl || "https://your-endpoint.example.com/v1";
        baseInput.required = !!prov.baseUrlRequired;

        const keyRow = q("[data-ai-key-row]");
        keyRow.hidden = isOAuth;
        q("[data-ai-key-label]").textContent = prov.keyLabel || "API key";
        const keyInput = q("[data-ai-key]");
        keyInput.value = "";
        // The server never sends a stored key back, so an empty box means
        // "leave whatever is saved alone" rather than "clear it".
        keyInput.placeholder = saved.hasKey ? "•••••••• (saved — leave blank to keep)" : "";
        const keyHelp = q("[data-ai-key-help]");
        if (!isOAuth && prov.keyUrl) {
            keyHelp.hidden = false;
            keyHelp.innerHTML = "";
            const a = document.createElement("a");
            a.href = prov.keyUrl;
            a.target = "_blank";
            a.rel = "noopener noreferrer";
            a.textContent = "Get a key";
            keyHelp.append(a, document.createTextNode(" — stored on this server for this browser only."));
        } else {
            keyHelp.hidden = true;
        }

        const modelInput = q("[data-ai-model]");
        modelInput.value = saved.model || prov.defaultModel || "";
        modelInput.placeholder = prov.defaultModel || "model name";
        const list = q("[data-ai-model-list]");
        list.innerHTML = "";
        for (const id of prov.models || []) {
            const opt = document.createElement("option");
            opt.value = id;
            list.appendChild(opt);
        }

        setStatusText("");
    }

    async function refreshSignInState() {
        const el = q("[data-ai-signin-state]");
        const btn = q("[data-ai-signin]");
        if (!el || !btn) return;
        el.textContent = "Checking…";
        try {
            const s = await window.CopilotAuth.status();
            const in_ = !!(s && s.logged_in);
            el.textContent = in_ ? "Signed in." : "Not signed in.";
            btn.textContent = in_ ? "Sign in again" : "Sign in with GitHub";
        } catch (_) {
            el.textContent = "";
        }
    }

    async function onSignInClicked() {
        if (!window.CopilotAuth) return;
        const ok = await window.CopilotAuth.openLoginModal();
        if (ok) refreshSignInState();
    }

    async function onSaveClicked() {
        const providerId = q("[data-ai-provider]").value;
        const prov = providerById(providerId);
        if (!prov) return;

        const patch = { provider: providerId, target: providerId };
        if (prov.baseUrlEditable) patch.baseUrl = q("[data-ai-baseurl]").value.trim();
        if (prov.auth !== "oauth-device") {
            const key = q("[data-ai-key]").value;
            // Omit the field entirely when untouched so the saved key survives.
            if (key !== "") patch.apiKey = key;
        }
        patch.model = q("[data-ai-model]").value.trim();

        setStatusText("Saving…");
        try {
            await save(patch);
        } catch (err) {
            setStatusText((err && err.message) || String(err), true);
            return;
        }
        catalogueCache = null;
        let s = null;
        try { s = await status(); } catch (_) {}
        notifyChanged(s);
        closeModal(true);
    }

    function closeModal(saved) {
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
        if (resolve) resolve(!!saved);
    }

    async function openSettings() {
        if (modalPromise) return modalPromise;
        const wrap = buildModal();
        modalPromise = new Promise((resolve) => { modalResolve = resolve; });

        let data;
        try {
            data = await catalogue(true);
        } catch (err) {
            setStatusText("Could not load providers: " + ((err && err.message) || err), true);
            data = { providers: [], settings: {} };
        }

        const select = q("[data-ai-provider]");
        select.innerHTML = "";
        for (const p of data.providers || []) {
            const opt = document.createElement("option");
            opt.value = p.id;
            opt.textContent = p.label;
            select.appendChild(opt);
        }
        const current = (data.settings && data.settings.provider) || (data.providers[0] && data.providers[0].id);
        if (current) select.value = current;
        renderFields(select.value);

        wrap.hidden = false;
        focusTrap.activate();
        if (window.ThreeSeventyWeb && window.ThreeSeventyWeb.pushModal) {
            window.ThreeSeventyWeb.pushModal(wrap, function () { closeModal(false); });
        }
        select.focus();
        return modalPromise;
    }

    // ensureReady resolves true when the selected provider can answer a chat.
    // Copilot needs an OAuth sign-in; everything else needs a key and/or an
    // endpoint, so each gets the prompt that actually unblocks it.
    async function ensureReady() {
        let s;
        try {
            s = await status();
        } catch (_) {
            return false;
        }
        if (s && s.ready) return true;
        if (s && s.needs === "login" && window.CopilotAuth) {
            const ok = await window.CopilotAuth.openLoginModal();
            if (ok) notifyChanged(await status().catch(() => null));
            return ok;
        }
        const saved = await openSettings();
        if (!saved) return false;
        try {
            const after = await status();
            return !!(after && after.ready);
        } catch (_) {
            return false;
        }
    }

    window.AIProvider = {
        status,
        catalogue,
        save,
        forget,
        openSettings,
        ensureReady,
        onChange(fn) { if (typeof fn === "function") listeners.push(fn); },
    };
})();
