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
    // Hard safety net on tool-call rounds per user message. The system prompt
    // asks the model to stay within ~30 rounds; this ceiling is a generous
    // backstop (well below the old effectively-unbounded 10000) so a runaway
    // never burns thousands of API calls. The real protection against tight
    // loops (e.g. chaos resume -> saturated -> resume) is the repeated-call
    // guard below, which stops as soon as no progress is being made.
    const MAX_TOOL_ROUNDS = 150;
    // If the model requests the exact same tool call(s) this many rounds in a
    // row, assume it is stuck in a no-progress loop and stop.
    const MAX_REPEATED_TOOL_ROUNDS = 5;
    let lastToolSignature = null;
    let repeatedToolRounds = 0;

    let toolSchema = null;        // cached from /api/copilot/tools
    let systemPrompt = "";
    let model = "claude-sonnet-4.6";
    try { model = localStorage.getItem(MODEL_KEY) || model; } catch (_) {}
    // Migrate the old dash-format ID that briefly shipped as the default but
    // doesn't match any real Copilot model.
    if (model === "claude-sonnet-4-6") {
        model = "claude-sonnet-4.6";
        try { localStorage.setItem(MODEL_KEY, model); } catch (_) {}
    }

    let history = loadHistory();
    let pendingAssistant = null;  // current streaming assistant message
    let toolRound = 0;
    let autoMode = false;
    let activeAbort = null;       // AbortController for the in-flight fetch
    let stopRequested = false;    // set by Stop button; breaks tool-round loop
    let stickToBottom = true;     // false once the user scrolls up to read history

    try { autoMode = localStorage.getItem(AUTO_MODE_KEY) === "1"; } catch (_) {}

    function loadHistory() {
        try {
            const raw = localStorage.getItem(HISTORY_KEY);
            if (!raw) return [];
            const arr = JSON.parse(raw);
            if (!Array.isArray(arr)) return [];
            // Drop leading orphaned tool results (history persisted by older
            // versions may have been cut mid-tool-round).
            let start = 0;
            while (start < arr.length && arr[start] && arr[start].role === "tool") start++;
            return start > 0 ? arr.slice(start) : arr;
        } catch (_) { return []; }
    }

    // Cap the persisted history so tool results (which can embed whole screen
    // dumps) cannot grow past the localStorage quota. The in-memory history is
    // untouched; only what is persisted across reloads is trimmed.
    const MAX_PERSISTED_MESSAGES = 200;

    // Trim to the last `max` messages, then advance past any leading
    // "role: tool" entries: a tool result whose parent assistant tool_calls
    // message was cut would make the restored payload invalid (the API
    // rejects tool messages with no preceding tool_calls).
    function trimHistoryForPersist(arr, max) {
        if (arr.length <= max) return arr;
        let start = arr.length - max;
        while (start < arr.length && arr[start] && arr[start].role === "tool") start++;
        return arr.slice(start);
    }

    function saveHistory() {
        let toPersist = trimHistoryForPersist(history, MAX_PERSISTED_MESSAGES);
        try {
            localStorage.setItem(HISTORY_KEY, JSON.stringify(toPersist));
        } catch (_) {
            // Quota exceeded: halve and retry once, then give up silently.
            try {
                toPersist = trimHistoryForPersist(toPersist, Math.floor(toPersist.length / 2));
                localStorage.setItem(HISTORY_KEY, JSON.stringify(toPersist));
            } catch (_) {}
        }
    }

    function ensurePanel() {
        let panel = document.getElementById("copilot-panel");
        if (panel) return panel;
        // The panel is rendered server-side from screen.html. If we land here
        // it means screen.html was not updated; bail out gracefully.
        return null;
    }

    function setConnectedState(loggedIn) {
        const panel = ensurePanel();
        if (!panel) return;
        panel.classList.toggle("copilot-disconnected", !loggedIn);
        const connectPrompt = panel.querySelector("[data-copilot-connect-prompt]");
        const modelBar = panel.querySelector(".copilot-model-bar");
        const form = panel.querySelector("[data-copilot-form]");
        const hint = panel.querySelector("[data-copilot-hint]");
        const signout = panel.querySelector("[data-copilot-signout]");
        if (connectPrompt) connectPrompt.hidden = !!loggedIn;
        if (modelBar) modelBar.hidden = !loggedIn;
        if (form) form.hidden = !loggedIn;
        if (hint) hint.hidden = !loggedIn;
        if (signout) signout.hidden = !loggedIn;
    }

    async function refreshConnectionState() {
        try {
            const s = await CopilotAuth.status();
            setConnectedState(s && s.logged_in);
        } catch (_) {
            setConnectedState(false);
        }
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
        // Render mermaid fences as inline diagrams; other fences as <pre><code>.
        html = html.replace(/```mermaid\n([\s\S]*?)```/g, function (_m, body) {
            const svg = renderMermaidFlowLR(body.trim());
            if (svg) return '<div class="copilot-mermaid-wrap">' + svg + '</div>';
            return "<pre><code>" + escapeHTML(body) + "</code></pre>";
        });
        html = html.replace(/```([\s\S]*?)```/g, function (_m, body) {
            return "<pre><code>" + body + "</code></pre>";
        });
        html = html.replace(/`([^`\n]+)`/g, "<code>$1</code>");
        html = html.replace(/\*\*([^*\n]+)\*\*/g, "<strong>$1</strong>");
        html = html.replace(/(^|\W)\*([^*\n]+)\*(?=\W|$)/g, "$1<em>$2</em>");
        html = html.replace(/\n/g, "<br>");
        return html;
    }

    // Lightweight renderer for `flowchart LR` diagrams generated by chaos_report.
    // Handles the specific subset emitted by the Go chaos engine; not a full parser.
    function renderMermaidFlowLR(code) {
        const nodeMap = new Map(); // id -> label
        const edges = [];         // {from, label, to}
        for (const raw of code.split("\n")) {
            const line = raw.trim();
            if (!line || /^flowchart\b/.test(line) || line.startsWith("%%")) continue;
            const nm = line.match(/^(\w+)\["(.+)"\]$/);
            if (nm) { nodeMap.set(nm[1], nm[2]); continue; }
            const em = line.match(/^(\w+)\s*-->\|([^|]*)\|\s*(\w+)$/);
            if (em) edges.push({ from: em[1], label: em[2], to: em[3] });
        }
        if (!nodeMap.size) return null;

        // BFS depth assignment (longest path wins to keep forward direction).
        const depths = new Map();
        for (const id of nodeMap.keys()) depths.set(id, 0);
        const topoQueue = Array.from(nodeMap.keys());
        for (let pass = 0; pass < nodeMap.size; pass++) {
            for (const e of edges) {
                const nd = (depths.get(e.from) || 0) + 1;
                if (!depths.has(e.to) || depths.get(e.to) < nd) depths.set(e.to, nd);
            }
        }

        // Group nodes by depth column.
        const cols = new Map();
        let maxDepth = 0;
        for (const [id, d] of depths) {
            if (!cols.has(d)) cols.set(d, []);
            cols.get(d).push(id);
            if (d > maxDepth) maxDepth = d;
        }

        const BOX_W = 150, BOX_H = 36, COL_GAP = 56, ROW_GAP = 16, PAD = 14;
        const maxRows = Math.max(...Array.from(cols.values()).map(function (n) { return n.length; }));
        const totalColH = maxRows * BOX_H + (maxRows - 1) * ROW_GAP;
        const positions = new Map();

        for (const [d, nodes] of cols) {
            const x = PAD + d * (BOX_W + COL_GAP);
            const colH = nodes.length * BOX_H + (nodes.length - 1) * ROW_GAP;
            const yOff = PAD + (totalColH - colH) / 2;
            nodes.forEach(function (id, i) {
                const y = yOff + i * (BOX_H + ROW_GAP);
                positions.set(id, { x: x, y: y, cx: x + BOX_W / 2, cy: y + BOX_H / 2 });
            });
        }

        const svgW = PAD + (maxDepth + 1) * (BOX_W + COL_GAP) - COL_GAP + PAD;
        const svgH = totalColH + PAD * 2;
        const uid = "ma" + Math.random().toString(36).slice(2, 7);

        let svg = '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ' + svgW + ' ' + svgH + '" width="' + svgW + '" height="' + svgH + '" class="copilot-mermaid-svg">';
        svg += '<defs><marker id="' + uid + '" markerWidth="8" markerHeight="6" refX="7" refY="3" orient="auto"><polygon points="0 0, 8 3, 0 6" fill="#6a8fc8"/></marker></defs>';

        for (const e of edges) {
            const fp = positions.get(e.from), tp = positions.get(e.to);
            if (!fp || !tp) continue;
            const x1 = fp.x + BOX_W, y1 = fp.cy, x2 = tp.x, y2 = tp.cy;
            const mx = (x1 + x2) / 2;
            svg += '<path d="M' + x1 + ',' + y1 + ' C' + mx + ',' + y1 + ' ' + mx + ',' + y2 + ' ' + x2 + ',' + y2 + '" fill="none" stroke="#6a8fc8" stroke-width="1.5" marker-end="url(#' + uid + ')"/>';
            if (e.label) {
                const lbl = e.label.length > 24 ? e.label.slice(0, 22) + "…" : e.label;
                svg += '<text x="' + mx + '" y="' + ((y1 + y2) / 2 - 3) + '" text-anchor="middle" font-size="9" fill="#aab" class="copilot-mermaid-elabel">' + escapeHTML(lbl) + '</text>';
            }
        }

        for (const [id, label] of nodeMap) {
            const p = positions.get(id);
            if (!p) continue;
            const disp = label.length > 24 ? label.slice(0, 22) + "…" : label;
            svg += '<rect x="' + p.x + '" y="' + p.y + '" width="' + BOX_W + '" height="' + BOX_H + '" rx="4" fill="#1e2d3d" stroke="#4a6fa5" stroke-width="1.5"/>';
            svg += '<text x="' + p.cx + '" y="' + (p.cy + 4) + '" text-anchor="middle" font-size="11" fill="#d0d8e8">' + escapeHTML(disp) + '</text>';
        }

        svg += '</svg>';
        return svg;
    }

    function messageList() {
        const panel = ensurePanel();
        return panel ? panel.querySelector("[data-copilot-messages]") : null;
    }

    // scrollToBottom keeps the transcript pinned to the latest message, but
    // respects the user: once they scroll up to read earlier history we stop
    // yanking them back down (stickToBottom=false). Pass force=true to override
    // (e.g. when the user sends a new message or opens the panel).
    function scrollToBottom(force) {
        const list = messageList();
        if (!list) return;
        if (force) stickToBottom = true;
        if (stickToBottom) list.scrollTop = list.scrollHeight;
        updateJumpButton();
    }

    function isNearBottom(list) {
        return list.scrollHeight - list.scrollTop - list.clientHeight < 80;
    }

    function attachScrollWatcher() {
        const list = messageList();
        if (!list || list.dataset.scrollWatch) return;
        list.dataset.scrollWatch = "1";
        list.addEventListener("scroll", function () {
            stickToBottom = isNearBottom(list);
            updateJumpButton();
        });
    }

    function jumpButton() {
        const panel = ensurePanel();
        return panel ? panel.querySelector("[data-copilot-jump]") : null;
    }

    function ensureJumpButton() {
        const panel = ensurePanel();
        if (!panel) return null;
        let btn = jumpButton();
        if (btn) return btn;
        const list = messageList();
        if (!list || !list.parentNode) return null;
        btn = document.createElement("button");
        btn.type = "button";
        btn.className = "copilot-jump-btn";
        btn.setAttribute("data-copilot-jump", "");
        btn.setAttribute("aria-label", "Jump to latest message");
        btn.textContent = "↓ Latest";
        btn.hidden = true;
        btn.addEventListener("click", function () { scrollToBottom(true); });
        list.parentNode.insertBefore(btn, list.nextSibling);
        return btn;
    }

    function updateJumpButton() {
        const btn = jumpButton();
        if (btn) btn.hidden = stickToBottom;
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

    function removeEmptyState() {
        const list = messageList();
        if (!list) return;
        const es = list.querySelector(".copilot-empty-state");
        if (es) es.remove();
    }

    function appendMessage(role, content, extra) {
        const list = messageList();
        if (!list) return null;
        removeEmptyState();
        const el = document.createElement("div");
        el.className = "copilot-msg copilot-msg-" + role;
        if (extra && extra.id) el.dataset.id = extra.id;
        // Errors are announced assertively so screen-reader users hear them
        // even mid-stream.
        if (role === "error") el.setAttribute("role", "alert");
        const inner = document.createElement("div");
        inner.className = "copilot-msg-body";
        inner.innerHTML = renderMarkdownLite(content || "");
        el.appendChild(inner);
        list.appendChild(el);
        // Decorate any rendered code blocks with a copy button.
        decoratePreBlocks(inner);
        // Force the view down for the user's own messages; assistant streaming
        // respects the scroll lock.
        scrollToBottom(role === "user");
        return el;
    }

    // -- Typing indicator --------------------------------------------------

    function showTyping() {
        const list = messageList();
        if (!list || list.querySelector(".copilot-typing")) return;
        removeEmptyState();
        const el = document.createElement("div");
        el.className = "copilot-typing";
        el.setAttribute("aria-label", "Assistant is thinking");
        el.innerHTML = '<span class="copilot-typing-dot"></span><span class="copilot-typing-dot"></span><span class="copilot-typing-dot"></span>';
        list.appendChild(el);
        scrollToBottom();
    }

    function hideTyping() {
        const list = messageList();
        if (!list) return;
        const el = list.querySelector(".copilot-typing");
        if (el) el.remove();
    }

    // -- Copy buttons ------------------------------------------------------

    // decoratePreBlocks adds a small "Copy" button to every <pre> inside the
    // given container that doesn't already have one. Used for assistant code
    // blocks and tool-result dumps (screen text, JSON) which are tedious to
    // select by hand in a narrow panel.
    function decoratePreBlocks(container) {
        if (!container) return;
        const blocks = container.tagName === "PRE" ? [container] : container.querySelectorAll("pre");
        blocks.forEach(function (pre) {
            if (pre.dataset.copyDecorated) return;
            pre.dataset.copyDecorated = "1";
            pre.classList.add("copilot-pre-copyable");
            const btn = document.createElement("button");
            btn.type = "button";
            btn.className = "copilot-copy-btn";
            btn.textContent = "Copy";
            btn.setAttribute("aria-label", "Copy to clipboard");
            btn.addEventListener("click", function () {
                // Copy the block's text without the button's own "Copy" label.
                const clone = pre.cloneNode(true);
                clone.querySelectorAll(".copilot-copy-btn").forEach(function (b) { b.remove(); });
                const text = clone.innerText || clone.textContent || "";
                const done = function () {
                    btn.textContent = "Copied";
                    setTimeout(function () { btn.textContent = "Copy"; }, 1200);
                };
                if (navigator.clipboard && navigator.clipboard.writeText) {
                    navigator.clipboard.writeText(text).then(done, function () { done(); });
                } else {
                    done();
                }
            });
            pre.appendChild(btn);
        });
    }

    // -- Empty state -------------------------------------------------------

    const EXAMPLE_PROMPTS = [
        "Explain the current screen",
        "Run chaos monkey to explore this app",
        "Map the business functions of this app",
    ];

    function renderEmptyState() {
        const list = messageList();
        if (!list) return;
        const wrap = document.createElement("div");
        wrap.className = "copilot-empty-state";
        const title = document.createElement("p");
        title.className = "copilot-empty-title";
        title.textContent = "Ask the assistant about this 3270 session";
        wrap.appendChild(title);
        const hint = document.createElement("p");
        hint.className = "copilot-empty-hint";
        hint.textContent = "It can read screens, drive transactions, and run the chaos monkey explorer. Try:";
        wrap.appendChild(hint);
        const chips = document.createElement("div");
        chips.className = "copilot-empty-chips";
        EXAMPLE_PROMPTS.forEach(function (p) {
            const chip = document.createElement("button");
            chip.type = "button";
            chip.className = "copilot-example-chip";
            chip.textContent = p;
            chip.addEventListener("click", function () { send(p); });
            chips.appendChild(chip);
        });
        wrap.appendChild(chips);
        list.appendChild(wrap);
    }

    function appendToolCallCard(call) {
        const list = messageList();
        if (!list) return null;
        removeEmptyState();
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
            <div class="copilot-tool-extras" data-tool-extras hidden></div>
        `;
        list.appendChild(card);
        scrollToBottom();
        return card;
    }

    function appendAskUserCard(tc, args) {
        const list = messageList();
        if (!list) return null;
        removeEmptyState();
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

    // Outgoing-payload size budgets. The whole history is replayed verbatim on
    // every chat call, so without these caps a long auto-mode chaos loop (which
    // polls chaos_status repeatedly, each result embedding the full mind map)
    // grows the request until it blows past the model's input window. Copilot
    // then rejects every subsequent request — including brand-new user
    // messages — with a bare "400 Bad Request", wedging the conversation for
    // good. Trimming is applied to a *copy* built for the request; the
    // in-memory/persisted history keeps the full content for the UI.
    const MAX_TOOL_RESULT_CHARS_RECENT = 24000; // tool results in the current turn
    const MAX_TOOL_RESULT_CHARS_OLD = 4000;     // tool results from earlier turns
    const MAX_TOTAL_CHARS = 200000;             // overall message budget (~50k tokens)

    function capToolContent(content, max) {
        const s = String(content == null ? "" : content);
        if (s.length <= max) return s;
        // Keep the head: for chaos_status JSON the alphabetically-early keys
        // (active, aidKeyCounts, coverageStats, error, firstScreenHash,
        // lastAttempt) carry the status the model needs; the bulky mindMap /
        // recentAttempts sit later and are the safe thing to drop.
        return s.slice(0, max) + "\n…[truncated " + (s.length - max) + " chars to fit the model context window]";
    }

    // sanitizeForRequest returns a structurally valid copy of `messages` that
    // Copilot will accept even when the stored history is malformed. It:
    //   - pairs every assistant `tool_calls` with exactly one `tool` result per
    //     id, synthesizing a placeholder for any missing result (a dangling
    //     assistant turn — e.g. persisted mid-round then reloaded — otherwise
    //     400s the whole request);
    //   - drops orphan `tool` messages with no matching preceding tool_call;
    //   - guarantees each tool_call carries valid JSON `arguments` (defaulting
    //     to "{}"), and strips the streaming-only `index` field;
    //   - caps tool-result sizes (older turns harder than the current one).
    function sanitizeForRequest(messages) {
        const out = [];
        // The current turn is everything after the last user message; its tool
        // results get the larger cap so the model still sees fresh detail.
        let lastUserIdx = -1;
        for (let k = messages.length - 1; k >= 0; k--) {
            if (messages[k] && messages[k].role === "user") { lastUserIdx = k; break; }
        }
        for (let i = 0; i < messages.length; i++) {
            const m = messages[i];
            if (!m || !m.role) continue;
            if (m.role === "assistant" && Array.isArray(m.tool_calls) && m.tool_calls.length) {
                const calls = [];
                for (const tc of m.tool_calls) {
                    if (!tc || !tc.id) continue;
                    const fn = tc.function || {};
                    let args = typeof fn.arguments === "string" ? fn.arguments : "";
                    try { JSON.parse(args); } catch (_) { args = "{}"; }
                    if (!args.trim()) args = "{}";
                    calls.push({ id: tc.id, type: "function", function: { name: fn.name || "", arguments: args } });
                }
                if (!calls.length) {
                    if (m.content) out.push({ role: "assistant", content: m.content });
                    continue;
                }
                // Consume the contiguous run of tool results that follows.
                const resultsById = Object.create(null);
                let j = i + 1;
                for (; j < messages.length; j++) {
                    const t = messages[j];
                    if (t && t.role === "tool" && t.tool_call_id != null) resultsById[t.tool_call_id] = t;
                    else break;
                }
                const cap = i > lastUserIdx ? MAX_TOOL_RESULT_CHARS_RECENT : MAX_TOOL_RESULT_CHARS_OLD;
                out.push({ role: "assistant", content: m.content || "", tool_calls: calls });
                for (const tc of calls) {
                    const r = resultsById[tc.id];
                    out.push({
                        role: "tool",
                        tool_call_id: tc.id,
                        content: r ? capToolContent(r.content, cap) : '{"error":"tool result missing"}',
                    });
                }
                i = j - 1; // skip the consumed tool messages
                continue;
            }
            if (m.role === "tool") continue; // orphan tool result: drop it
            out.push({ role: m.role, content: m.content || "" });
        }
        return out;
    }

    function messagesCharLength(messages) {
        let n = 0;
        for (const m of messages) {
            n += 16; // rough per-message envelope (role, braces, ...)
            if (m.content) n += m.content.length;
            if (m.tool_calls) n += JSON.stringify(m.tool_calls).length;
            if (m.tool_call_id) n += m.tool_call_id.length;
        }
        return n;
    }

    // enforceCharBudget drops the oldest turns until the payload fits MAX_TOTAL_CHARS,
    // keeping the system prompt and never dropping the final user message (the
    // current ask) or anything after it. Input is assumed already sanitized, so
    // an assistant `tool_calls` message is always immediately followed by its
    // tool results — they are dropped together to avoid orphaning either side.
    function enforceCharBudget(messages, budget) {
        if (messagesCharLength(messages) <= budget) return messages;
        const system = messages.length && messages[0].role === "system" ? messages[0] : null;
        const body = system ? messages.slice(1) : messages.slice();
        let lastUserIdx = -1;
        for (let k = body.length - 1; k >= 0; k--) {
            if (body[k].role === "user") { lastUserIdx = k; break; }
        }
        while (body.length && messagesCharLength((system ? [system] : []).concat(body)) > budget) {
            if (lastUserIdx <= 0) break; // protect the current ask and everything after it
            let drop = 1;
            if (body[0].role === "assistant" && Array.isArray(body[0].tool_calls)) {
                while (drop < body.length && body[drop].role === "tool") drop++;
            }
            if (drop > lastUserIdx) break; // would cross into the protected tail
            body.splice(0, drop);
            lastUserIdx -= drop;
        }
        while (body.length && body[0].role === "tool") body.shift(); // strip any leading orphan
        return (system ? [system] : []).concat(body);
    }

    function buildPayload() {
        const raw = [{ role: "system", content: systemPrompt || "" }];
        for (const m of history) raw.push(m);
        const messages = enforceCharBudget(sanitizeForRequest(raw), MAX_TOTAL_CHARS);
        return {
            model,
            messages,
            tools: toolSchema || [],
            tool_choice: "auto",
            stream: true,
            max_tokens: 4096,
        };
    }

    function setRunning(running) {
        const panel = ensurePanel();
        if (!panel) return;
        const sendBtn = panel.querySelector("[data-copilot-send]");
        const stopBtn = panel.querySelector("[data-copilot-stop]");
        const form = panel.querySelector("[data-copilot-form]");
        const textarea = panel.querySelector("[data-copilot-input]");
        if (sendBtn) {
            sendBtn.hidden = !!running;
            sendBtn.disabled = !!running;
        }
        if (stopBtn) stopBtn.hidden = !running;
        // Expose busy state to assistive tech so a send is announced as in
        // progress rather than appearing to do nothing.
        if (form) form.setAttribute("aria-busy", running ? "true" : "false");
        if (textarea) textarea.setAttribute("aria-busy", running ? "true" : "false");
    }

    function requestStop() {
        stopRequested = true;
        if (activeAbort) {
            try { activeAbort.abort(); } catch (_) {}
        }
    }

    // toolSignature returns a stable string identifying a set of tool calls
    // (names + arguments) so we can detect the model repeating itself.
    function toolSignature(toolCalls) {
        return toolCalls
            .map(function (tc) { return (tc.function && tc.function.name || "") + ":" + (tc.function && tc.function.arguments || ""); })
            .join("|");
    }

    // rollbackFailedTurn keeps history well-formed when a turn fails before the
    // assistant produced anything: it drops the trailing user message so the
    // next send doesn't create two user turns in a row (which the API rejects).
    // Returns the rolled-back user text, or null if there was nothing to roll.
    function rollbackFailedTurn() {
        if (toolRound <= 1 && history.length && history[history.length - 1].role === "user") {
            const failed = history.pop();
            saveHistory();
            return (failed && failed.content) || "";
        }
        return null;
    }

    function appendRetryAffordance(text) {
        if (text == null) return;
        const el = appendMessage("error", "The message above wasn't sent.");
        if (!el) return;
        const btn = document.createElement("button");
        btn.type = "button";
        btn.className = "copilot-btn-retry";
        btn.textContent = "Retry";
        btn.addEventListener("click", function () { el.remove(); send(text); });
        el.appendChild(btn);
        scrollToBottom();
    }

    function appendContinueAffordance() {
        const el = appendMessage("error", "The response was cut off (token limit). Continue to finish it.");
        if (!el) return;
        const btn = document.createElement("button");
        btn.type = "button";
        btn.className = "copilot-btn-retry";
        btn.textContent = "Continue";
        btn.addEventListener("click", function () { el.remove(); send("Please continue from where you stopped."); });
        el.appendChild(btn);
        scrollToBottom();
    }

    async function chatRound() {
        if (stopRequested) return;
        toolRound++;
        if (toolRound > MAX_TOOL_ROUNDS) {
            appendMessage("error", "Safety ceiling of " + MAX_TOOL_ROUNDS + " tool rounds reached; stopping. Send a new message to keep going.");
            return;
        }

        showTyping();
        const stream = await fetchChatStream();
        if (!stream) {
            hideTyping();
            // fetchChatStream already surfaced the error message; just keep
            // history clean and offer a one-click retry.
            appendRetryAffordance(rollbackFailedTurn());
            return;
        }
        const result = await consumeStream(stream);
        hideTyping();
        if (stopRequested) return;
        if (result.error) {
            appendMessage("error", "Chat error: " + friendlyChatError(result.error));
            appendRetryAffordance(rollbackFailedTurn());
            return;
        }

        // A truncated response (finish_reason "length") can leave half-streamed
        // tool calls with invalid JSON arguments. Don't execute those — persist
        // the partial text and let the user continue.
        const hasToolCalls = result.toolCalls && result.toolCalls.length;
        const truncated = result.finishReason === "length";
        const toolCallsActionable = hasToolCalls && !truncated;

        // Persist the assistant turn into history.
        const assistantMsg = { role: "assistant", content: result.content || "" };
        if (toolCallsActionable) {
            assistantMsg.tool_calls = result.toolCalls;
            // Per OpenAI spec, content may be empty when tool_calls are present.
            if (!assistantMsg.content) assistantMsg.content = "";
        }
        history.push(assistantMsg);
        saveHistory();

        if (truncated) {
            appendContinueAffordance();
            return;
        }

        if (toolCallsActionable) {
            // No-progress loop guard: if the model keeps asking for the exact
            // same tool call(s), stop instead of burning the round budget
            // (the classic chaos resume -> saturated -> resume loop).
            const sig = toolSignature(result.toolCalls);
            if (sig && sig === lastToolSignature) {
                repeatedToolRounds++;
            } else {
                repeatedToolRounds = 0;
                lastToolSignature = sig;
            }
            if (repeatedToolRounds >= MAX_REPEATED_TOOL_ROUNDS) {
                appendMessage("error", "Stopped: the assistant repeated the same action " + (MAX_REPEATED_TOOL_ROUNDS + 1) + " times without making progress. Adjust your request, update chaos hints, or try a different approach.");
                return;
            }

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

    // friendlyChatError converts upstream Copilot error bodies into a
    // human-readable explanation. Copilot returns OpenAI-shaped errors like
    //   {"error":{"message":"...","code":"model_not_supported",...}}
    // sometimes nested inside our own "copilot chat: status NNN: <body>".
    function friendlyChatError(raw) {
        const text = String(raw || "");
        // Try to extract a JSON error object even when wrapped in prose.
        let parsed = null;
        const braceAt = text.indexOf("{");
        if (braceAt >= 0) {
            try { parsed = JSON.parse(text.slice(braceAt)); } catch (_) {}
        }
        const errObj = parsed && parsed.error;
        const code = errObj && errObj.code;
        const message = (errObj && errObj.message) || text;
        if (code === "model_not_supported") {
            return "The selected model (" + model + ") is not available on your Copilot plan. Pick a different model from the dropdown above.";
        }
        if (code === "rate_limit_exceeded" || /rate.?limit/i.test(message)) {
            return "Copilot rate limit hit. Wait a moment and try again, or switch to a cheaper model (e.g. claude-haiku-4.5).";
        }
        if (/premium request|monthly.*limit|quota|exhaust/i.test(message)) {
            return "Your Copilot premium request quota is exhausted. Switch to a non-premium model (e.g. claude-haiku-4.5) or wait for the quota to reset.";
        }
        return message;
    }

    async function fetchChatStream() {
        const payload = buildPayload();
        activeAbort = new AbortController();
        let resp;
        try {
            resp = await fetch("/api/copilot/chat", {
                method: "POST",
                credentials: "same-origin",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify(payload),
                signal: activeAbort.signal,
            });
        } catch (err) {
            if (err && err.name === "AbortError") {
                appendMessage("error", "Stopped.");
            } else {
                appendMessage("error", "Network error: " + (err && err.message || err));
            }
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
            appendMessage("error", friendlyChatError(msg));
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
            // First bytes received: the model is responding, drop the "thinking"
            // indicator so it doesn't sit alongside streamed content.
            hideTyping();
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

        // Streaming finished: decorate the completed message's code blocks with
        // copy buttons (skipped during streaming since innerHTML is rebuilt on
        // every delta).
        if (assistantEl) decoratePreBlocks(assistantEl.querySelector(".copilot-msg-body"));

        const toolCalls = Array.from(toolCallsById.values()).sort((a, b) => a.index - b.index);
        return {
            content,
            toolCalls,
            finishReason,
            error: errorMsg,
        };
    }

    function downloadJSONFile(jsonText, fileName) {
        const blob = new Blob([jsonText], { type: "application/json" });
        const url = URL.createObjectURL(blob);
        const a = document.createElement("a");
        a.href = url;
        a.download = fileName;
        document.body.appendChild(a);
        a.click();
        document.body.removeChild(a);
        URL.revokeObjectURL(url);
    }

    function addToolExtras(toolName, args, result, card) {
        const extrasEl = card && card.querySelector("[data-tool-extras]");
        if (!extrasEl) return;

        function makeBtn(label, onClick) {
            const b = document.createElement("button");
            b.type = "button";
            b.className = "copilot-tool-extra-btn";
            b.textContent = label;
            b.addEventListener("click", onClick);
            return b;
        }

        if (toolName === "chaos_export_workflow") {
            const workflow = result && result.workflow;
            if (!workflow) return;
            const jsonText = typeof workflow === "string" ? workflow : JSON.stringify(workflow, null, 2);
            const btn = makeBtn("⬇ Download Workflow JSON", function () {
                if (typeof window.ChaosUI === "object" && typeof window.ChaosUI.downloadWorkflow === "function") {
                    window.ChaosUI.downloadWorkflow(jsonText, "");
                } else {
                    downloadJSONFile(jsonText, "chaos-workflow.json");
                }
            });
            extrasEl.appendChild(btn);
            extrasEl.hidden = false;
            scrollToBottom();
            return;
        }

        if (toolName === "business_generate_workflow") {
            const workflow = result && result.workflow;
            if (!workflow) return;
            const jsonText = typeof workflow === "string" ? workflow : JSON.stringify(workflow, null, 2);
            const fnName = (workflow && workflow.Name) || (args && args.name) || "business-workflow";
            const fileName = String(fnName).toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "") || "business-workflow";
            const btn = makeBtn("⬇ Download Workflow JSON", function () {
                downloadJSONFile(jsonText, fileName + ".json");
            });
            extrasEl.appendChild(btn);
            extrasEl.hidden = false;
            scrollToBottom();
            return;
        }

        if (toolName === "chaos_status" && args && args.verbose && result && result.mindMap) {
            const chaosUI = typeof window.ChaosUI === "object" ? window.ChaosUI : null;
            if (chaosUI && !chaosUI.isMapVisible()) {
                const btn = makeBtn("🗺 View Chaos Map", function () {
                    chaosUI.openChaosMap();
                });
                extrasEl.appendChild(btn);
                extrasEl.hidden = false;
                scrollToBottom();
            }
            return;
        }

        if (toolName === "chaos_report" && result && result.markdown) {
            const chaosUI = typeof window.ChaosUI === "object" ? window.ChaosUI : null;
            if (chaosUI && !chaosUI.isFlowVisible()) {
                const btn = makeBtn("📊 View Discovery Flow", function () {
                    chaosUI.openChaosFlow();
                });
                extrasEl.appendChild(btn);
            }
            if (chaosUI && !chaosUI.isReportVisible()) {
                const btn2 = makeBtn("📄 View Full Report", function () {
                    chaosUI.openChaosReport();
                });
                extrasEl.appendChild(btn2);
            }
            if (extrasEl.childNodes.length) {
                extrasEl.hidden = false;
                scrollToBottom();
            }
            return;
        }
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
                decoratePreBlocks(resultEl);
                runBtn.disabled = true;
                skipBtn.disabled = true;
                resolve({ id: tc.id, contentJSON: JSON.stringify(resultObj) });
            };

            async function runTool() {
                runBtn.disabled = true;
                skipBtn.disabled = true;
                statusEl.textContent = "Running...";
                let args = {};
                try {
                    args = JSON.parse(tc.function.arguments || "{}");
                } catch (e) {
                    // Malformed/truncated tool arguments: report the failure back
                    // to the model as the tool result (so it can retry) instead
                    // of silently running with empty args.
                    finish({ error: "invalid tool arguments JSON: " + (e && e.message || e) }, "Failed");
                    return;
                }
                const r = await CopilotTools.runTool(tc.function.name, args);
                finish(r, r && r.error ? "Failed" : "Done");
                if (r && !r.error && !r.skipped) {
                    addToolExtras(tc.function.name, args, r, card);
                }
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
            ensureJumpButton();
            attachScrollWatcher();
            scrollToBottom(true);
            refreshConnectionState().then(function () {
                populateModelSelect();
            });
        }
    }

    function clearHistory() {
        history = [];
        saveHistory();
        const list = messageList();
        if (list) list.innerHTML = "";
        toolRound = 0;
        lastToolSignature = null;
        repeatedToolRounds = 0;
        renderEmptyState();
    }

    function renderHistoryAfterLoad() {
        const list = messageList();
        if (!list) return;
        list.innerHTML = "";
        if (!history.length) {
            renderEmptyState();
            return;
        }
        for (const m of history) {
            if (m.role === "user") appendMessage("user", m.content || "");
            else if (m.role === "assistant" && m.content) appendMessage("assistant", m.content);
            // tool messages + tool_calls already executed in earlier sessions
            // are skipped so we don't reanimate the cards.
        }
        scrollToBottom(true);
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
        // Authenticate BEFORE committing the user turn so a cancelled sign-in
        // doesn't leave a dangling user message in history (and the typed text
        // stays in the box for another attempt).
        const ok = await CopilotAuth.ensureLogin();
        if (!ok) {
            appendMessage("error", "Sign-in cancelled.");
            return;
        }
        setConnectedState(true);

        history.push({ role: "user", content: text });
        saveHistory();
        appendMessage("user", text);
        clearInput();
        toolRound = 0;
        lastToolSignature = null;
        repeatedToolRounds = 0;

        await populateModelSelect();
        stopRequested = false;
        setRunning(true);
        try {
            await chatRound();
        } finally {
            activeAbort = null;
            stopRequested = false;
            setRunning(false);
            hideTyping();
        }
    }

    function clearInput() {
        const panel = ensurePanel();
        if (!panel) return;
        const ta = panel.querySelector("[data-copilot-input]");
        if (ta) ta.value = "";
    }

    function attachInputHandlers() {
        const panel = ensurePanel();
        if (!panel) return;
        const form = panel.querySelector("[data-copilot-form]");
        const textarea = panel.querySelector("[data-copilot-input]");
        const closeBtn = panel.querySelector("[data-copilot-close]");
        const clearBtn = panel.querySelector("[data-copilot-clear]");
        const signOutBtn = panel.querySelector("[data-copilot-signout]");
        const connectBtn = panel.querySelector("[data-copilot-connect]");
        const autoModeBtn = panel.querySelector("[data-copilot-automode]");
        const hintEl = panel.querySelector("[data-copilot-hint]");
        const modelSel = panel.querySelector("[data-copilot-model]");
        const stopBtn = panel.querySelector("[data-copilot-stop]");

        if (stopBtn) stopBtn.addEventListener("click", requestStop);

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
            try {
                await CopilotAuth.logout();
                // Clear the conversation on sign-out so the next user doesn't
                // inherit the previous session's transcript.
                clearHistory();
                appendMessage("error", "Signed out.");
                setConnectedState(false);
            } catch (e) { appendMessage("error", "Logout failed: " + e); }
        });
        if (connectBtn) connectBtn.addEventListener("click", async function () {
            const ok = await CopilotAuth.openLoginModal();
            if (ok) {
                setConnectedState(true);
                await populateModelSelect();
            }
        });

        if (form && textarea) {
            // The textarea is cleared by send() only once the turn is committed,
            // so a failed/cancelled send keeps the user's text.
            form.addEventListener("submit", function (ev) {
                ev.preventDefault();
                send(textarea.value);
            });
            textarea.addEventListener("keydown", function (ev) {
                if (ev.key === "Enter" && !ev.shiftKey) {
                    ev.preventDefault();
                    send(textarea.value);
                }
            });
        }
    }

    function openClearModal() {
        const modal = document.querySelector("[data-copilot-clear-modal]");
        if (!modal) { clearHistory(); return; }
        const returnFocus = document.activeElement;
        const trap =
            window.ThreeSeventyWeb && window.ThreeSeventyWeb.createFocusTrap
                ? window.ThreeSeventyWeb.createFocusTrap(modal)
                : { activate() {}, deactivate() {} };
        modal.hidden = false;
        trap.activate();
        const confirmBtn = modal.querySelector("[data-copilot-clear-confirm]");
        const cancelBtns = modal.querySelectorAll("[data-copilot-clear-cancel]");
        function close() {
            modal.hidden = true;
            trap.deactivate();
            modal.removeEventListener("keydown", onKeydown);
            if (returnFocus && typeof returnFocus.focus === "function") returnFocus.focus();
        }
        function onConfirm() { close(); clearHistory(); }
        function onKeydown(ev) { if (ev.key === "Escape") close(); }
        if (confirmBtn) { confirmBtn.onclick = onConfirm; confirmBtn.focus(); }
        cancelBtns.forEach(function (b) { b.onclick = close; });
        modal.onclick = function (ev) { if (ev.target === modal) close(); };
        modal.addEventListener("keydown", onKeydown);
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
