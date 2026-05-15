// Copilot tool dispatcher. Each tool name maps to a function that calls the
// matching 3270Web HTTP endpoint and returns a JSON-serializable result that
// is fed back to the model as a "role: tool" message.
//
// All requests are same-origin and rely on the 3270Web_session cookie for
// session resolution, so we don't need to thread a sessionID through tool
// arguments.

(function () {
    "use strict";

    async function getJSON(url) {
        const resp = await fetch(url, { credentials: "same-origin" });
        const text = await resp.text();
        let data = null;
        try { data = text ? JSON.parse(text) : null; } catch (_) { data = { raw: text }; }
        if (!resp.ok) {
            const msg = (data && data.error) || resp.statusText || ("HTTP " + resp.status);
            return { ok: false, error: msg, status: resp.status };
        }
        return { ok: true, data };
    }

    async function postJSON(url, body) {
        const resp = await fetch(url, {
            method: "POST",
            credentials: "same-origin",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify(body || {}),
        });
        const text = await resp.text();
        let data = null;
        try { data = text ? JSON.parse(text) : null; } catch (_) { data = { raw: text }; }
        if (!resp.ok) {
            const msg = (data && data.error) || resp.statusText || ("HTTP " + resp.status);
            return { ok: false, error: msg, status: resp.status };
        }
        return { ok: true, data };
    }

    // Wait briefly for the host to settle so the next get_screen returns the
    // post-action state rather than the pre-action state. The 3270 status
    // line ("KB" field in the existing UI) is the canonical signal but isn't
    // exposed cheaply to JS here, so a small sleep is good enough for tool use.
    function settle(ms) {
        return new Promise((resolve) => setTimeout(resolve, ms == null ? 250 : ms));
    }

    function summariseScreen(s) {
        if (!s || typeof s !== "object") return s;
        // Drop very long screen text from the *fallback* summary, but always
        // include the full text in the tool result the model sees.
        const fieldCount = Array.isArray(s.fields) ? s.fields.length : 0;
        return {
            width: s.width,
            height: s.height,
            text: s.text,
            fields: s.fields || [],
            cursor: s.hasCursor ? { row: s.cursorRow, col: s.cursorCol } : null,
            status: s.status || "",
            formatted: !!s.formatted,
            fieldCount,
        };
    }

    const HANDLERS = {
        async get_screen(_args) {
            const res = await getJSON("/screen.json");
            if (!res.ok) return { error: res.error };
            return summariseScreen(res.data);
        },
        async send_key(args) {
            const key = (args && args.key) || "Enter";
            const res = await postJSON("/screen/key", { key });
            if (!res.ok) return { error: res.error };
            await settle();
            const screen = await getJSON("/screen.json");
            return {
                key: res.data && res.data.key,
                screen: screen.ok ? summariseScreen(screen.data) : null,
            };
        },
        async write_field(args) {
            const body = {
                row: args && Number(args.row) || 0,
                col: args && Number(args.col) || 0,
                text: (args && String(args.text != null ? args.text : "")) || "",
            };
            const res = await postJSON("/screen/write", body);
            if (!res.ok) return { error: res.error };
            return { ok: true };
        },
        async submit_screen(_args) {
            const res = await postJSON("/screen/submit", {});
            if (!res.ok) return { error: res.error };
            await settle();
            const screen = await getJSON("/screen.json");
            return {
                ok: true,
                screen: screen.ok ? summariseScreen(screen.data) : null,
            };
        },
        async chaos_status(_args) {
            const res = await getJSON("/chaos/status");
            if (!res.ok) return { error: res.error };
            return res.data;
        },
        async chaos_start(args) {
            const body = {};
            if (args && typeof args.max_steps === "number") body.maxSteps = args.max_steps;
            if (args && typeof args.time_budget_sec === "number") body.timeBudgetSec = args.time_budget_sec;
            if (args && typeof args.step_delay_sec === "number") body.stepDelaySec = args.step_delay_sec;
            if (args && typeof args.seed === "number") body.seed = args.seed;
            if (args && typeof args.max_field_length === "number") body.maxFieldLength = args.max_field_length;
            if (args && typeof args.screen_dedup_similarity === "number") body.screenDedupSimilarity = args.screen_dedup_similarity;
            const res = await postJSON("/chaos/start", body);
            if (!res.ok) return { error: res.error };
            return res.data;
        },
        async chaos_stop(_args) {
            const res = await postJSON("/chaos/stop");
            if (!res.ok) return { error: res.error };
            return res.data;
        },
        async chaos_resume(_args) {
            const res = await postJSON("/chaos/resume");
            if (!res.ok) return { error: res.error };
            return res.data;
        },
        async chaos_save_screen_hint(args) {
            const hash = (args && args.screen_hash) || "";
            if (!hash) return { error: "screen_hash required" };
            const hint = {};
            if (args.known_data) hint.knownData = args.known_data;
            if (args.known_keys) hint.knownKeys = args.known_keys;
            if (args.blocked_keys) hint.blockedKeys = args.blocked_keys;
            const body = { hints: {} };
            body.hints[hash] = hint;
            const res = await postJSON("/chaos/screen-hints", body);
            if (!res.ok) return { error: res.error };
            return res.data;
        },
    };

    async function runTool(name, args) {
        const handler = HANDLERS[name];
        if (!handler) return { error: "unknown tool: " + name };
        try {
            return await handler(args || {});
        } catch (err) {
            return { error: String(err && err.message || err) };
        }
    }

    function knownTools() { return Object.keys(HANDLERS); }

    window.CopilotTools = { runTool, knownTools };
})();
