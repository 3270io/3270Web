// Copilot tool dispatcher. Each tool name maps to a function that calls the
// matching 3270Web HTTP endpoint and returns a JSON-serializable result that
// is fed back to the model as a "role: tool" message.
//
// All requests are same-origin and rely on the 3270Web_session cookie for
// session resolution, so we don't need to thread a sessionID through tool
// arguments.

(function () {
    "use strict";

    // How much of one print job reaches the conversation. A job may be
    // megabytes — a nightly report is exactly what a printer LU receives — and
    // the download endpoint serves all of it, correctly, to a browser writing
    // a file. Matches maxJobChars in internal/mcptools/bindings.go, so the two
    // surfaces cut a long job at the same place.
    const MAX_JOB_CHARS = 20000;

    // How long to follow a task run, and how often to look. The ceiling is the
    // server's own maxTaskRunDuration: past it there is nothing left to wait
    // for, because the run's context has been cancelled.
    const TASK_RUN_TIMEOUT_MS = 5 * 60 * 1000;
    const TASK_POLL_MS = 750;

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

    async function deleteJSON(url) {
        const resp = await fetch(url, { method: "DELETE", credentials: "same-origin" });
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
            if (typeof window.refreshScreenContent === "function") window.refreshScreenContent();
            const screen = await getJSON("/screen.json");
            return {
                key: res.data && res.data.key,
                screen: screen.ok ? summariseScreen(screen.data) : null,
            };
        },
        async write_field(args) {
            const body = {
                text: (args && String(args.text != null ? args.text : "")) || "",
            };
            const fieldKey = args && args.field_key;
            if (fieldKey) {
                // 1-indexed R<row>C<col>L<len> key; the server resolves it to
                // 0-indexed row/col. Forward as-is — do not convert here.
                body.field_key = String(fieldKey);
            } else {
                body.row = args && Number(args.row) || 0;
                body.col = args && Number(args.col) || 0;
            }
            const res = await postJSON("/screen/write", body);
            if (!res.ok) return { error: res.error };
            if (typeof window.refreshScreenContent === "function") window.refreshScreenContent();
            return { ok: true };
        },
        async submit_screen(_args) {
            const res = await postJSON("/screen/submit", {});
            if (!res.ok) return { error: res.error };
            await settle();
            if (typeof window.refreshScreenContent === "function") window.refreshScreenContent();
            const screen = await getJSON("/screen.json");
            return {
                ok: true,
                screen: screen.ok ? summariseScreen(screen.data) : null,
            };
        },
        async connect_session(args) {
            const hostname = (args && String(args.hostname || "").trim()) || "";
            if (!hostname) return { error: "hostname required" };
            const res = await postJSON("/screen/connect", { hostname });
            if (!res.ok) return { error: res.error };
            // Chat history is scoped by document.body.dataset.targetHost (set
            // at page load); update it in place so a mid-conversation
            // reconnect doesn't keep writing to the OLD host's history slot
            // until the next full page load.
            if (document.body && res.data) {
                const newHost = (res.data.targetHost || "") + ":" + (res.data.targetPort || "");
                document.body.dataset.targetHost = newHost;
            }
            if (typeof window.refreshScreenContent === "function") window.refreshScreenContent();
            const screen = await getJSON("/screen.json");
            return {
                ok: true,
                targetHost: res.data && res.data.targetHost,
                targetPort: res.data && res.data.targetPort,
                screen: screen.ok ? summariseScreen(screen.data) : null,
            };
        },
        async move_cursor(args) {
            const row = args && Number(args.row);
            const col = args && Number(args.col);
            if (!(row >= 0) || !(col >= 0)) return { error: "row and col (>= 0) required" };
            const res = await postJSON("/screen/cursor", { row, col });
            if (!res.ok) return { error: res.error };
            return { ok: true };
        },
        async wait_for_unlock(args) {
            const body = {};
            if (args && typeof args.timeout_ms === "number") body.timeout_ms = args.timeout_ms;
            const res = await postJSON("/screen/wait", body);
            if (!res.ok) return { error: res.error };
            if (typeof window.refreshScreenContent === "function") window.refreshScreenContent();
            return {
                unlocked: !!(res.data && res.data.unlocked),
                timedOut: !!(res.data && res.data.timedOut),
                screen: res.data && res.data.screen ? summariseScreen(res.data.screen) : null,
            };
        },
        async chaos_status(args) {
            const verbose = !!(args && args.verbose);
            const url = verbose ? "/chaos/status?verbose=true" : "/chaos/status";
            const res = await getJSON(url);
            if (!res.ok) return { error: res.error };
            if (verbose && res.data && typeof window.ChaosUI === "object") {
                window.ChaosUI.updateFromStatus(res.data);
            }
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
        async chaos_report(_args) {
            const resp = await fetch("/chaos/report", { method: "POST" });
            if (!resp.ok) {
                let detail = "";
                try { detail = await resp.text(); } catch (_) { /* ignore */ }
                return { error: "HTTP " + resp.status + (detail ? ": " + detail : "") };
            }
            const markdown = await resp.text();
            if (typeof window.ChaosUI === "object") {
                window.ChaosUI.setChaosReport(markdown);
            }
            return { markdown };
        },
        async chaos_save_screen_hint(args) {
            const hash = (args && args.screen_hash) || "";
            if (!hash) return { error: "screen_hash required" };
            const body = { screen_hash: hash };
            if (args.known_data) body.known_data = args.known_data;
            if (args.known_keys) body.known_keys = args.known_keys;
            if (args.blocked_keys) body.blocked_keys = args.blocked_keys;
            if (args.key_assignments) body.key_assignments = args.key_assignments;
            const res = await postJSON("/chaos/screen-hints", body);
            if (!res.ok) return { error: res.error };
            return res.data;
        },
        async chaos_get_hints(_args) {
            const global = await getJSON("/chaos/hints");
            if (!global.ok) return { error: global.error };
            const screen = await getJSON("/chaos/screen-hints");
            return {
                hints: (global.data && global.data.hints) || [],
                keyBlacklist: (global.data && global.data.keyBlacklist) || [],
                firstScreenHint: (global.data && global.data.firstScreenHint) || null,
                screenHints: (screen.ok && screen.data && screen.data.screenHints) || {},
            };
        },
        async chaos_update_hints(args) {
            const body = {};
            if (args && Array.isArray(args.hints)) {
                body.hints = args.hints.map(function (h) {
                    return {
                        transaction: (h && h.transaction) || "",
                        knownData: (h && h.known_data) || [],
                    };
                });
            }
            if (args && Array.isArray(args.key_blacklist)) body.keyBlacklist = args.key_blacklist;
            const res = await postJSON("/chaos/hints", body);
            if (!res.ok) return { error: res.error };
            return res.data;
        },
        async chaos_export_workflow(_args) {
            const res = await postJSON("/chaos/export", {});
            if (!res.ok) return { error: res.error };
            return { workflow: res.data };
        },
        async chaos_list_screens(args) {
            const includePreviews = !(args && args.include_previews === false);
            const url = includePreviews ? "/chaos/screens" : "/chaos/screens?include_previews=false";
            const res = await getJSON(url);
            if (!res.ok) return { error: res.error };
            return res.data;
        },
        async chaos_annotate_screen(args) {
            const hash = (args && args.screen_hash) || "";
            if (!hash) return { error: "screen_hash required" };
            const body = { screen_hash: hash };
            if (args.business_purpose) body.business_purpose = args.business_purpose;
            if (args.notes) body.notes = args.notes;
            if (args.field_semantics) body.field_semantics = args.field_semantics;
            const res = await postJSON("/chaos/screens/annotate", body);
            if (!res.ok) return { error: res.error };
            return res.data;
        },
        async business_list_functions(_args) {
            const res = await getJSON("/chaos/business/functions");
            if (!res.ok) return { error: res.error };
            return res.data;
        },
        async business_app_overview(_args) {
            const res = await getJSON("/chaos/business/overview");
            if (!res.ok) return { error: res.error };
            return res.data;
        },
        async chaos_insights(_args) {
            const res = await getJSON("/chaos/insights");
            if (!res.ok) return { error: res.error };
            return res.data;
        },
        async business_save_function(args) {
            if (!args || !args.name) return { error: "name required" };
            const res = await postJSON("/chaos/business/functions", args);
            if (!res.ok) return { error: res.error };
            return res.data;
        },
        async business_generate_workflow(args) {
            if (!args || !args.name) return { error: "name required" };
            const body = { name: args.name };
            if (args.parameters) body.parameters = args.parameters;
            if (args.host) body.host = args.host;
            if (typeof args.port === "number") body.port = args.port;
            const res = await postJSON("/chaos/business/generate-workflow", body);
            if (!res.ok) return { error: res.error };
            return { workflow: res.data };
        },

        // Skills, instructions and extensions. The server owns the catalogue
        // and the per-conversation dedup, so these are thin: the "you already
        // have this" reply comes back as an ordinary body.
        async list_skills(_args) {
            const res = await getJSON("/skills");
            if (!res.ok) return { error: res.error };
            return res.data;
        },
        async load_skill(args) {
            const name = (args && String(args.name || "").trim()) || "";
            if (!name) return { error: "name required" };
            const res = await getJSON("/skills/load?name=" + encodeURIComponent(name));
            if (!res.ok) return { error: res.error };
            return res.data;
        },
        async list_instructions(_args) {
            const res = await getJSON("/instructions");
            if (!res.ok) return { error: res.error };
            return res.data;
        },
        async load_instruction(args) {
            const name = (args && String(args.name || "").trim()) || "";
            if (!name) return { error: "name required" };
            const res = await getJSON("/instructions/load?name=" + encodeURIComponent(name));
            if (!res.ok) return { error: res.error };
            return res.data;
        },
        async list_extensions(_args) {
            const res = await getJSON("/extensions");
            if (!res.ok) return { error: res.error };
            return res.data;
        },

        // What the terminal can do besides drive a screen: the connection's
        // own account of itself, the display toggles, screen snapshots, the
        // printer session and the saved task catalogue. Each of these was on
        // the HTTP API before it was a tool, which meant an assistant asked
        // whether 3270Web could do it had no way to find out that it could.
        async get_connection_details(_args) {
            const res = await getJSON("/host/query");
            if (!res.ok) return { error: res.error };
            return res.data;
        },
        async get_display_toggles(_args) {
            const res = await getJSON("/host/toggles");
            if (!res.ok) return { error: res.error };
            return res.data;
        },
        async set_display_toggle(args) {
            const name = (args && String(args.name || "").trim()) || "";
            if (!name) return { error: "name required" };
            if (typeof (args && args.value) !== "boolean") {
                return { error: "value required, and must be true or false" };
            }
            const res = await postJSON("/host/toggles", { name, value: args.value });
            if (!res.ok) return { error: res.error };
            // The toggle changes how the terminal draws, so the page has to
            // re-read the screen or the operator sees the old rendering until
            // something else happens to refresh it.
            if (typeof window.refreshScreenContent === "function") window.refreshScreenContent();
            return res.data;
        },
        async snapshot_take(args) {
            const name = (args && String(args.name || "").trim()) || "";
            if (!name) return { error: "name required" };
            const res = await postJSON("/screen/snapshots", { name });
            if (!res.ok) return { error: res.error };
            return res.data;
        },
        async snapshot_list(args) {
            const name = (args && String(args.name || "").trim()) || "";
            if (name) {
                const one = await getJSON("/screen/snapshots?name=" + encodeURIComponent(name));
                if (!one.ok) return { error: one.error };
                return one.data;
            }
            const res = await getJSON("/screen/snapshots");
            if (!res.ok) return { error: res.error };
            // Names and sizes only. The listing carries every snapshot's full
            // text, which is right for a caller that means to diff them itself
            // and wrong for a model that asked what it is holding.
            const held = (res.data && res.data.snapshots) || [];
            return {
                count: held.length,
                snapshots: held.map(function (entry) {
                    const snap = entry.snapshot || {};
                    return {
                        name: entry.name,
                        taken_at: entry.taken_at,
                        rows: snap.rows,
                        cols: snap.cols,
                        status: snap.status,
                    };
                }),
            };
        },
        async snapshot_diff(args) {
            const left = (args && String(args.left || "").trim()) || "";
            if (!left) return { error: "left required" };
            const body = { left };
            const right = (args && String(args.right || "").trim()) || "";
            if (right) body.right = right;
            const res = await postJSON("/screen/snapshots/diff", body);
            if (!res.ok) return { error: res.error };
            return res.data;
        },
        async snapshot_delete(args) {
            const name = (args && String(args.name || "").trim()) || "";
            if (!name) return { error: "name required" };
            const res = await deleteJSON("/screen/snapshots?name=" + encodeURIComponent(name));
            if (!res.ok) return { error: res.error };
            return res.data;
        },
        async printer_status(_args) {
            const res = await getJSON("/printer/status");
            if (!res.ok) return { error: res.error };
            return res.data;
        },
        // The printer panel polls while it is open, so neither of these has to
        // tell it anything: a printer the assistant started shows up there on
        // its own within a poll.
        async printer_start(args) {
            const lu = (args && String(args.lu || "").trim()) || "";
            const res = await postJSON("/printer/start", lu ? { lu } : { associate: true });
            if (!res.ok) return { error: res.error };
            return res.data;
        },
        async printer_stop(_args) {
            const res = await postJSON("/printer/stop", {});
            if (!res.ok) return { error: res.error };
            return res.data;
        },
        async printer_read_job(args) {
            const name = (args && String(args.name || "").trim()) || "";
            if (!name) return { error: "name required" };
            const resp = await fetch("/printer/jobs/download?name=" + encodeURIComponent(name), {
                credentials: "same-origin",
            });
            const text = await resp.text();
            if (!resp.ok) {
                let detail = text;
                try { detail = (JSON.parse(text) || {}).error || text; } catch (_) { /* keep the body */ }
                return { error: detail || ("HTTP " + resp.status) };
            }
            // A print job may be megabytes; a conversation may not be.
            if (text.length > MAX_JOB_CHARS) {
                return {
                    name,
                    truncated: true,
                    bytes: text.length,
                    text: text.slice(0, MAX_JOB_CHARS),
                    note: "Only the first " + MAX_JOB_CHARS + " characters are shown. " +
                        "The whole job is downloadable from the printer panel.",
                };
            }
            return { name, truncated: false, bytes: text.length, text };
        },
        async list_tasks(_args) {
            const res = await getJSON("/tasks");
            if (!res.ok) return { error: res.error };
            // The catalogue carries each task's recorded steps. What decides
            // whether a task answers the user's question is its name, what it
            // asks for and what it returns — the steps are how, and the model
            // does not drive them itself.
            const tasks = (res.data && res.data.tasks) || [];
            return {
                count: tasks.length,
                tasks: tasks.map(function (t) {
                    return {
                        name: t.name,
                        description: t.description,
                        source: t.source,
                        steps: Array.isArray(t.steps) ? t.steps.length : 0,
                        parameters: (t.parameters || []).map(function (p) {
                            return {
                                name: p.name, description: p.description,
                                required: !!p.required, example: p.example,
                                sensitive: !!p.sensitive,
                            };
                        }),
                        outputs: (t.outputs || []).map(function (o) { return o.label || o.name; }),
                    };
                }),
            };
        },
        async run_task(args) {
            const name = (args && String(args.name || "").trim()) || "";
            if (!name) return { error: "name required — call list_tasks to see what is saved" };
            const body = { name };
            if (args && args.parameters && typeof args.parameters === "object") {
                // Task parameters are strings. A model that sends an account
                // number as 12345678 gets it coerced rather than a type error
                // it cannot act on.
                const params = {};
                Object.keys(args.parameters).forEach(function (key) {
                    const value = args.parameters[key];
                    params[key] = value == null ? "" : String(value);
                });
                body.parameters = params;
            }
            const started = await postJSON("/tasks/run", body);
            if (!started.ok) return { error: started.error };

            // The browser's run is asynchronous so the panel can show it in
            // flight; the model wants the answer. Poll to the same ceiling the
            // server gives a run, then report the timeout rather than claiming
            // an outcome nobody observed.
            for (let waited = 0; waited < TASK_RUN_TIMEOUT_MS; waited += TASK_POLL_MS) {
                await settle(TASK_POLL_MS);
                const status = await getJSON("/tasks/status");
                if (!status.ok) return { error: status.error };
                if (status.data && status.data.running) continue;
                if (typeof window.refreshScreenContent === "function") window.refreshScreenContent();
                if (status.data && status.data.error) {
                    return { task: name, completed: false, error: status.data.error };
                }
                return { task: name, result: (status.data && status.data.result) || null };
            }
            return {
                task: name,
                error: "the run was still going after " + (TASK_RUN_TIMEOUT_MS / 1000) +
                    " seconds. Check the task panel; do not start it again.",
            };
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
