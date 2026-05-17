// Copilot side-panel chat UI.
//
// Renders a slide-in panel on the right that streams Copilot chat responses
// over SSE from POST /api/copilot/chat, executes tool calls via
// CopilotTools.runTool (one Run/Skip card per call), and loops until the
// model returns finish_reason=stop.
//
// The full message history lives in memory + localStorage; it is sent
// verbatim on every chat call so the backend stays stateless.

(function () {
    "use strict";

    const HISTORY_KEY = "copilot.panel.history.v1";
    const OPEN_KEY = "copilot.panel.open";
    const AUTO_MODE_KEY = "copilot.panel.automode";
    const MODEL_KEY = "copilot.panel.model";
    const MAX_TOOL_ROUNDS = 30;

    let toolSchema = null;        // cached from /api/copilot/tools
    let systemPrompt = "";
    let model = "claude-sonnet-4-5";
    try { model = localStorage.getItem(MODEL_KEY) || model; } catch (_) {}

    let history = loadHistory();
    let pendingAssistant = null;  // current streaming assistant message
    let toolRound = 0;
    let autoMode = false;

    try { autoMode = localStorage.getItem(AUTO_MODE_KEY) === "1"; } catch (_) {}

    function loadHistory() {
        try {
            const raw = localStorage.getItem(HISTORY_KEY);
            if (!raw) return [];
            const arr = JSON.parse(raw);
            return Array.isArray(arr) ? arr : [];
        } catch (_) { return []; }
    }

    function saveHistory() {
        try { localStorage.setItem(HISTORY_KEY, JSON.stringify(history)); }
        catch (_) {}
    }

    function ensurePanel() {
        let panel = document.getElementById("copilot-panel");
        if (panel) return panel;
        // The panel is rendered server-side from screen.html. If we land here
        // it means screen.html was not updated; bail out gracefully.
        return null;
    }

    // -- Rendering ---------------------------------------------------------

    function escapeHTML(s) {
        return String(s == null ? "" : s)
            .replace(/&/g, "&amp;")
            .replace(/</g, "&lt;")
            .replace(/>/g, "&gt;")
            .replace(/"/g, "&quot;")
            .replace(/'/g, "&#39;");
    }

    function renderMarkdownLite(text) {
        // Intentionally minimal: code fences, inline code, bold, italics,
        // newlines. The chat panel doesn't need full markdown.
        let html = escapeHTML(text);
        html = html.replace(/```([\s\S]*?)```/g, function (_m, body) {
            return "<pre><code>" + body + "</code></pre>";
        });
        html = html.replace(/`([^`\n]+)`/g, "<code>$1</code>");
        html = html.replace(/\*\*([^*\n]+)\*\*/g, "<strong>$1</strong>");
        html = html.replace(/(^|\W)\*([^*\n]+)\*(?=\W|$)/g, "$1<em>$2</em>");
        html = html.replace(/\n/g, "<br>");
        return html;
    }

    function messageList() {
        const panel = ensurePanel();
        return panel ? panel.querySelector("[data-copilot-messages]") : null;
    }

    function scrollToBottom() {
        const list = messageList();
        if (list) list.scrollTop = list.scrollHeight;
    }

    function modelSelect() {
        const panel = ensurePanel();
        return panel ? panel.querySelector("[data-copilot-model]") : null;
    }

    async function populateModelSelect() {
        const sel = modelSelect();
        if (!sel || sel.dataset.populated) return;
        try {
            const resp = await fetch("/api/copilot/models", { credentials: "same-origin" });
            if (!resp.ok) return;
            const data = await resp.json();
            const models = data.models || [];
            if (!models.length) return;
            sel.dataset.populated = "1"; // only lock after a successful fetch
            sel.innerHTML = "";
            for (const id of models) {
                const opt = document.createElement("option");
                opt.value = id;
                opt.textContent = id;
                if (id === model) opt.selected = true;
                sel.appendChild(opt);
            }
            // If saved model isn't in the list, default to first entry.
            if (!models.includes(model)) {
                model = models[0];
                sel.value = model;
            }
        } catch (_) {}
    }

    function appendMessage(role, content, extra) {
        const list = messageList();
        if (!list) return null;
        const el = document.createElement("div");
        el.className = "copilot-msg copilot-msg-" + role;
        if (extra && extra.id) el.dataset.id = extra.id;
        const inner = document.createElement("div");
        inner.className = "copilot-msg-body";
        inner.innerHTML = renderMarkdownLite(content || "");
        el.appendChild(inner);
        list.appendChild(el);
        scrollToBottom();
        return el;
    }

    function appendToolCallCard(call) {
        const list = messageList();
        if (!list) return null;
        const card = document.createElement("div");
        card.className = "copilot-tool-call";
        card.dataset.toolCallId = call.id;
        const args = call.function && call.function.arguments;
        let argsPretty = args;
        try { argsPretty = JSON.stringify(JSON.parse(args), null, 2); } catch (_) {}
        card.innerHTML = `
            <div class="copilot-tool-call-head">
                <span class="copilot-tool-call-name">${escapeHTML(call.function && call.function.name || "?")}</span>
                <span class="copilot-tool-call-status" data-status>Pending approval</span>
            </div>
            <pre class="copilot-tool-call-args">${escapeHTML(argsPretty || "{}")}</pre>
            <div class="copilot-tool-call-actions">
                <button type="button" class="copilot-btn-run" data-run>Run</button>
                <button type="button" class="copilot-btn-skip" data-skip>Skip</button>
            </div>
            <pre class="copilot-tool-call-result" data-result hidden></pre>
        `;
        list.appendChild(card);
        scrollToBottom();
        return card;
    }

    function appendAskUserCard(tc, args) {
        const list = messageList();
        if (!list) return null;
        const question = (args && args.question) || "Make a choice:";
        const options = (args && Array.isArray(args.options) && args.options.length) ? args.options : ["OK"];

        const card = document.createElement("div");
        card.className = "copilot-tool-call copilot-ask-user";
        card.dataset.toolCallId = tc.id;

        const qEl = document.createElement("div");
        qEl.className = "copilot-ask-user-question";
        qEl.textContent = question;
        card.appendChild(qEl);

        const optWrap = document.createElement("div");
        optWrap.className = "copilot-ask-user-options";
        options.forEach(function (opt) {
            const btn = document.createElement("button");
            btn.type = "button";
            btn.className = "copilot-btn-option";
            btn.textContent = opt;
            optWrap.appendChild(btn);
        });
        card.appendChild(optWrap);

        const chosenEl = document.createElement("div");
        chosenEl.className = "copilot-ask-user-chosen";
        chosenEl.hidden = true;
        card.appendChild(chosenEl);

        list.appendChild(card);
        scrollToBottom();
        return card;
    }

    // -- Chat round --------------------------------------------------------

    function buildPayload() {
        const messages = [{ role: "system", content: systemPrompt || "" }];
        for (const m of history) messages.push(m);
        return {
            model,
            messages,
            tools: toolSchema || [],
            tool_choice: "auto",
            stream: true,
            max_tokens: 4096,
        };
    }

    async function chatRound() {
        toolRound++;
        if (toolRound > MAX_TOOL_ROUNDS) {
            appendMessage("error", "Tool call budget exhausted; stopping.");
            return;
        }

        const stream = await fetchChatStream();
        if (!stream) return;
        const result = await consumeStream(stream);
        if (result.error) {
            appendMessage("error", "Chat error: " + result.error);
            return;
        }
        // Persist the assistant turn into history.
        const assistantMsg = { role: "assistant", content: result.content || "" };
        if (result.toolCalls && result.toolCalls.length) {
            assistantMsg.tool_calls = result.toolCalls;
            // Save content (often null/empty) and the tool_calls; per OpenAI
            // spec, content can be null when tool_calls are present.
            if (!assistantMsg.content) assistantMsg.content = "";
        }
        history.push(assistantMsg);
        saveHistory();

        if (result.toolCalls && result.toolCalls.length) {
            const toolResults = await collectToolResults(result.toolCalls);
            // Append each tool result as a message.
            for (const tr of toolResults) {
                history.push({
                    role: "tool",
                    tool_call_id: tr.id,
                    content: tr.contentJSON,
                });
            }
            saveHistory();
            await chatRound();
        }
    }

    async function fetchChatStream() {
        const payload = buildPayload();
        let resp;
        try {
            resp = await fetch("/api/copilot/chat", {
                method: "POST",
                credentials: "same-origin",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify(payload),
            });
        } catch (err) {
            appendMessage("error", "Network error: " + (err && err.message || err));
            return null;
        }
        if (!resp.ok) {
            let body = "";
            try { body = await resp.text(); } catch (_) {}
            let msg = body;
            try { msg = JSON.parse(body).error || body; } catch (_) {}
            if (resp.status === 401) {
                appendMessage("error", "Not signed in to Copilot. Click \"Sign in\" in the panel header.");
                CopilotAuth.openLoginModal();
                return null;
            }
            appendMessage("error", "HTTP " + resp.status + ": " + msg);
            return null;
        }
        return resp.body;
    }

    async function consumeStream(body) {
        const reader = body.getReader();
        const decoder = new TextDecoder();
        let buffer = "";
        let content = "";
        const toolCallsById = new Map();   // index -> partial tool_call
        let assistantEl = null;
        let errorMsg = "";
        let finishReason = null;

        while (true) {
            const { value, done } = await reader.read();
            if (done) break;
            buffer += decoder.decode(value, { stream: true });

            let idx;
            while ((idx = buffer.indexOf("\n\n")) !== -1) {
                const raw = buffer.slice(0, idx);
                buffer = buffer.slice(idx + 2);
                const lines = raw.split("\n");
                let event = "message";
                const dataLines = [];
                for (const line of lines) {
                    if (line.startsWith("event:")) event = line.slice(6).trim();
                    else if (line.startsWith("data:")) dataLines.push(line.slice(5).trim());
                }
                const data = dataLines.join("\n");
                if (event === "error") { errorMsg = data; continue; }
                if (!data || data === "[DONE]") continue;
                let chunk = null;
                try { chunk = JSON.parse(data); } catch (_) { continue; }
                const choice = chunk.choices && chunk.choices[0];
                if (!choice) continue;
                if (choice.finish_reason) finishReason = choice.finish_reason;
                const delta = choice.delta || {};
                if (typeof delta.content === "string" && delta.content.length) {
                    content += delta.content;
                    if (!assistantEl) assistantEl = appendMessage("assistant", "", { id: chunk.id });
                    if (assistantEl) {
                        assistantEl.querySelector(".copilot-msg-body").innerHTML = renderMarkdownLite(content);
                        scrollToBottom();
                    }
                }
                if (Array.isArray(delta.tool_calls)) {
                    for (const tc of delta.tool_calls) {
                        const idx2 = tc.index != null ? tc.index : 0;
                        let current = toolCallsById.get(idx2);
                        if (!current) {
                            current = {
                                index: idx2,
                                id: tc.id || ("call_" + idx2),
                                type: "function",
                                function: { name: "", arguments: "" },
                            };
                            toolCallsById.set(idx2, current);
                        }
                        if (tc.id) current.id = tc.id;
                        if (tc.function) {
                            if (tc.function.name) current.function.name += tc.function.name;
                            if (tc.function.arguments) current.function.arguments += tc.function.arguments;
                        }
                    }
                }
            }
        }

        const toolCalls = Array.from(toolCallsById.values()).sort((a, b) => a.index - b.index);
        return {
            content,
            toolCalls,
            finishReason,
            error: errorMsg,
        };
    }

    function collectToolResults(toolCalls) {
        // Render a card per tool call and return a promise that resolves once
        // every card has been Run or Skipped (or answered, for ask_user).
        const promises = toolCalls.map((tc) => new Promise((resolve) => {
            const name = tc.function && tc.function.name;

            // ask_user: render a question + option buttons; always requires
            // user interaction (never auto-runs) regardless of autoMode.
            if (name === "ask_user") {
                let args = {};
                try { args = JSON.parse(tc.function.arguments || "{}"); } catch (_) {}
                const card = appendAskUserCard(tc, args);
                if (!card) {
                    resolve({ id: tc.id, contentJSON: JSON.stringify({ error: "panel not ready" }) });
                    return;
                }
                const optWrap = card.querySelector(".copilot-ask-user-options");
                const chosenEl = card.querySelector(".copilot-ask-user-chosen");
                optWrap.querySelectorAll(".copilot-btn-option").forEach(function (btn) {
                    btn.addEventListener("click", function () {
                        const choice = btn.textContent;
                        optWrap.querySelectorAll(".copilot-btn-option").forEach(function (b) { b.disabled = true; });
                        chosenEl.textContent = "You chose: " + choice;
                        chosenEl.hidden = false;
                        scrollToBottom();
                        resolve({ id: tc.id, contentJSON: JSON.stringify({ choice }) });
                    });
                });
                return;
            }

            const card = appendToolCallCard(tc);
            if (!card) {
                resolve({ id: tc.id, contentJSON: JSON.stringify({ error: "panel not ready" }) });
                return;
            }
            const statusEl = card.querySelector("[data-status]");
            const resultEl = card.querySelector("[data-result]");
            const runBtn = card.querySelector("[data-run]");
            const skipBtn = card.querySelector("[data-skip]");

            const finish = function (resultObj, label) {
                statusEl.textContent = label;
                resultEl.hidden = false;
                resultEl.textContent = JSON.stringify(resultObj, null, 2);
                runBtn.disabled = true;
                skipBtn.disabled = true;
                resolve({ id: tc.id, contentJSON: JSON.stringify(resultObj) });
            };

            async function runTool() {
                runBtn.disabled = true;
                skipBtn.disabled = true;
                statusEl.textContent = "Running...";
                let args = {};
                try { args = JSON.parse(tc.function.arguments || "{}"); } catch (_) {}
                const r = await CopilotTools.runTool(tc.function.name, args);
                finish(r, r && r.error ? "Failed" : "Done");
            }

            runBtn.addEventListener("click", runTool);
            skipBtn.addEventListener("click", function () {
                finish({ skipped: true }, "Skipped");
            });

            if (autoMode) {
                runTool();
            }
        }));
        return Promise.all(promises);
    }

    // -- Panel open/close / input -----------------------------------------

    function setOpen(open) {
        const panel = ensurePanel();
        if (!panel) return;
        document.body.classList.toggle("copilot-open", !!open);
        panel.hidden = !open;
        try { localStorage.setItem(OPEN_KEY, open ? "1" : "0"); } catch (_) {}
        if (open) {
            const ta = panel.querySelector("[data-copilot-input]");
            if (ta) ta.focus();
            scrollToBottom();
            populateModelSelect();
        }
    }

    function clearHistory() {
        history = [];
        saveHistory();
        const list = messageList();
        if (list) list.innerHTML = "";
        toolRound = 0;
    }

    function renderHistoryAfterLoad() {
        const list = messageList();
        if (!list) return;
        list.innerHTML = "";
        for (const m of history) {
            if (m.role === "user") appendMessage("user", m.content || "");
            else if (m.role === "assistant" && m.content) appendMessage("assistant", m.content);
            // tool messages + tool_calls already executed in earlier sessions
            // are skipped so we don't reanimate the cards.
        }
        scrollToBottom();
    }

    async function send(text) {
        text = String(text || "").trim();
        if (!text) return;
        if (!toolSchema) {
            try {
                const resp = await fetch("/api/copilot/tools", { credentials: "same-origin" });
                const data = await resp.json();
                toolSchema = data.tools || [];
                systemPrompt = data.system_prompt || systemPrompt;
                model = data.model || model;
            } catch (err) {
                appendMessage("error", "Could not load tool schema: " + (err && err.message || err));
                return;
            }
        }
        history.push({ role: "user", content: text });
        saveHistory();
        appendMessage("user", text);
        toolRound = 0;

        const ok = await CopilotAuth.ensureLogin();
        if (!ok) {
            appendMessage("error", "Sign-in cancelled.");
            return;
        }
        await populateModelSelect();
        await chatRound();
    }

    function attachInputHandlers() {
        const panel = ensurePanel();
        if (!panel) return;
        const form = panel.querySelector("[data-copilot-form]");
        const textarea = panel.querySelector("[data-copilot-input]");
        const closeBtn = panel.querySelector("[data-copilot-close]");
        const clearBtn = panel.querySelector("[data-copilot-clear]");
        const signOutBtn = panel.querySelector("[data-copilot-signout]");
        const autoModeBtn = panel.querySelector("[data-copilot-automode]");
        const hintEl = panel.querySelector("[data-copilot-hint]");
        const modelSel = panel.querySelector("[data-copilot-model]");

        if (modelSel) {
            modelSel.value = model;
            modelSel.addEventListener("change", function () {
                model = modelSel.value;
                try { localStorage.setItem(MODEL_KEY, model); } catch (_) {}
            });
        }

        function syncAutoModeUI() {
            if (!autoModeBtn) return;
            autoModeBtn.setAttribute("aria-pressed", autoMode ? "true" : "false");
            autoModeBtn.classList.toggle("active", autoMode);
            if (hintEl) {
                hintEl.innerHTML = autoMode
                    ? "Auto Mode is <strong>on</strong> — tools run without approval."
                    : "Each tool call waits for you to click <strong>Run</strong>.";
            }
        }

        if (autoModeBtn) {
            autoModeBtn.addEventListener("click", function () {
                autoMode = !autoMode;
                try { localStorage.setItem(AUTO_MODE_KEY, autoMode ? "1" : "0"); } catch (_) {}
                syncAutoModeUI();
            });
        }
        syncAutoModeUI();

        if (closeBtn) closeBtn.addEventListener("click", () => setOpen(false));
        if (clearBtn) clearBtn.addEventListener("click", () => openClearModal());
        if (signOutBtn) signOutBtn.addEventListener("click", async () => {
            try { await CopilotAuth.logout(); appendMessage("error", "Signed out."); } catch (e) { appendMessage("error", "Logout failed: " + e); }
        });

        if (form && textarea) {
            form.addEventListener("submit", function (ev) {
                ev.preventDefault();
                const text = textarea.value;
                textarea.value = "";
                send(text);
            });
            textarea.addEventListener("keydown", function (ev) {
                if (ev.key === "Enter" && !ev.shiftKey) {
                    ev.preventDefault();
                    const text = textarea.value;
                    textarea.value = "";
                    send(text);
                }
            });
        }
    }

    function openClearModal() {
        const modal = document.querySelector("[data-copilot-clear-modal]");
        if (!modal) { clearHistory(); return; }
        modal.hidden = false;
        const confirmBtn = modal.querySelector("[data-copilot-clear-confirm]");
        const cancelBtns = modal.querySelectorAll("[data-copilot-clear-cancel]");
        function close() { modal.hidden = true; }
        function onConfirm() { close(); clearHistory(); }
        if (confirmBtn) confirmBtn.onclick = onConfirm;
        cancelBtns.forEach(function (b) { b.onclick = close; });
        modal.onclick = function (ev) { if (ev.target === modal) close(); };
    }

    function attachToolbarToggle() {
        const toggle = document.querySelector("[data-copilot-toggle]");
        if (!toggle) return;
        toggle.addEventListener("click", function () {
            const panel = ensurePanel();
            if (!panel) return;
            setOpen(!!panel.hidden);
        });
    }

    function init() {
        attachToolbarToggle();
        attachInputHandlers();
        renderHistoryAfterLoad();
        let open = false;
        try { open = localStorage.getItem(OPEN_KEY) === "1"; } catch (_) {}
        if (open) setOpen(true);
    }

    if (document.readyState === "loading") {
        document.addEventListener("DOMContentLoaded", init);
    } else {
        init();
    }

    window.CopilotPanel = { open: () => setOpen(true), close: () => setOpen(false), clear: clearHistory };
})();
