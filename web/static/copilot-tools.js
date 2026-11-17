// Copilot tool dispatcher. Each tool name maps to a function that calls the
// matching 3270Web HTTP endpoint and returns a JSON-serializable result that
// is fed back to the model as a "role: tool" message.
//
// All requests are same-origin and rely on the 3270Web_session cookie for
// session resolution, so we don't need to thread a sessionID through tool
// arguments. Each handler takes (args, ctx): ctx.signal is the turn's
// AbortSignal, and ctx.confirmed is true only when a person clicked Run on
// the card for this exact call.

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

    // The conversation this page is in. Sent on every tool and skill request
    // so the server can scope what belongs to one conversation (which skills
    // it has already handed over, which approvals it has seen) rather than to
    // the browser. The panel sets it, and replaces it on Clear.
    let conversationId = "";

    function setConversation(id) { conversationId = String(id || ""); }

    function requestHeaders(extra) {
        const headers = Object.assign({}, extra || {});
        if (conversationId) headers["X-3270Web-Conversation"] = conversationId;
        return headers;
    }

    // One request, one shape back: {ok, data} or {ok:false, error, status,
    // data}. `data` is kept on failure because a refusal can carry more than
    // its message — /screen/key answers 409 with requiresApproval and the key.
    //
    // ctx.signal is the turn's AbortSignal. Stop aborts it, and the abort
    // surfaces as an AbortError that runTool turns into a result; every
    // request a tool makes has to carry it or Stop only stops the next one.
    async function request(method, url, body, ctx) {
        const init = {
            method,
            credentials: "same-origin",
            headers: requestHeaders(body === undefined ? null : { "Content-Type": "application/json" }),
        };
        if (body !== undefined) init.body = JSON.stringify(body || {});
        if (ctx && ctx.signal) init.signal = ctx.signal;
        const resp = await fetch(url, init);
        const text = await resp.text();
        let data = null;
        try { data = text ? JSON.parse(text) : null; } catch (_) { data = { raw: text }; }
        if (!resp.ok) {
            const msg = (data && data.error) || resp.statusText || ("HTTP " + resp.status);
            return { ok: false, error: msg, status: resp.status, data };
        }
        return { ok: true, data };
    }

    function getJSON(url, ctx) { return request("GET", url, undefined, ctx); }
    function postJSON(url, body, ctx) { return request("POST", url, body || {}, ctx); }
    function deleteJSON(url, ctx) { return request("DELETE", url, undefined, ctx); }

    function abortError() {
        const err = new Error("stopped by user");
        err.name = "AbortError";
        return err;
    }

    // Wait briefly for the host to settle so the next get_screen returns the
    // post-action state rather than the pre-action state. The 3270 status
    // line ("KB" field in the existing UI) is the canonical signal but isn't
    // exposed cheaply to JS here, so a small sleep is good enough for tool use.
    //
    // The wait ends early, by rejecting, when the turn is stopped: run_task
    // sleeps between polls for up to five minutes, and a Stop that had to
    // wait for the sleep to finish was not one.
    function settle(ms, ctx) {
        const signal = ctx && ctx.signal;
        return new Promise((resolve, reject) => {
            if (signal && signal.aborted) { reject(abortError()); return; }
            const timer = setTimeout(function () {
                if (signal) signal.removeEventListener("abort", onAbort);
                resolve();
            }, ms == null ? 250 : ms);
            function onAbort() {
                clearTimeout(timer);
                reject(abortError());
            }
            if (signal) signal.addEventListener("abort", onAbort, { once: true });
        });
    }

    function refreshTerminal() {
        if (typeof window.refreshScreenContent === "function") window.refreshScreenContent();
    }

    // Tools that change what the Automation menu and the run strip show. The
    // chaos UI keeps its own state and polls on its own schedule, so a run the
    // assistant started sat unannounced in the menus until that poll came
    // round.
    function refreshChaosUI() {
        const ui = window.ChaosUI;
        if (ui && typeof ui.refresh === "function") {
            try { ui.refresh(); } catch (_) { /* the chat must not fail on the menu */ }
        }
    }

    function isNumber(v) { return typeof v === "number" && isFinite(v); }

    // What a screen looks like to the model.
    //
    // Protected fields keep their geometry and lose their value: every one of
    // those values is already in `text`, at the position the entry names, so
    // sending them again doubled the size of each screen-bearing result for
    // nothing. Input fields keep theirs (hidden ones arrive masked from the
    // server) because that is what the model edits.
    function summariseScreen(s) {
        if (!s || typeof s !== "object") return s;
        const fields = (Array.isArray(s.fields) ? s.fields : []).map(function (f) {
            if (!f || !f.protected) return f;
            const slim = Object.assign({}, f);
            delete slim.value;
            return slim;
        });
        const out = {
            width: s.width,
            height: s.height,
            text: s.text,
            fields,
            cursor: s.hasCursor ? { row: s.cursorRow, col: s.cursorCol } : null,
            status: s.status || "",
            formatted: !!s.formatted,
            fieldCount: fields.length,
        };
        if (s.screenHash) out.screenHash = s.screenHash;
        if (s.keyboard !== undefined) out.keyboard = s.keyboard;
        if (s.knownScreen) out.knownScreen = s.knownScreen;
        return out;
    }

    async function readScreen(ctx) {
        const screen = await getJSON("/screen.json", ctx);
        return screen.ok ? summariseScreen(screen.data) : null;
    }

    const HANDLERS = {
        async get_screen(_args, ctx) {
            const res = await getJSON("/screen.json", ctx);
            if (!res.ok) return { error: res.error };
            return summariseScreen(res.data);
        },
        async send_key(args, ctx) {
            // No default. A call with no key used to press Enter, which is
            // the one key that commits whatever is on the screen — the model
            // gets an error it can act on instead.
            const key = (args && typeof args.key === "string") ? args.key.trim() : "";
            if (!key) {
                return { error: "key required: name the key to press (for example Enter, PF3 or Tab). Nothing was sent." };
            }
            const body = { key };
            // Only ever set by the panel, and only after a person clicked Run
            // on this card. The server refuses a dangerous key without it.
            if (ctx && ctx.confirmed) body.confirmed = true;
            const res = await postJSON("/screen/key", body, ctx);
            if (!res.ok) {
                if (res.status === 409 && res.data && res.data.requiresApproval) {
                    return { error: res.error, requiresApproval: true, key: res.data.key || key };
                }
                return { error: res.error };
            }
            await settle(undefined, ctx);
            refreshTerminal();
            return {
                key: res.data && res.data.key,
                screen: await readScreen(ctx),
            };
        },
        async write_field(args, ctx) {
            const body = {
                text: (args && String(args.text != null ? args.text : "")) || "",
            };
            const fieldKey = args && args.field_key;
            if (fieldKey) {
                // 1-indexed R<row>C<col>L<len> key; the server resolves it to
                // 0-indexed row/col. Forward as-is — do not convert here.
                body.field_key = String(fieldKey);
            } else {
                // Never default these. Row 0, column 0 is a real position, so
                // a call that left one out used to type into the top-left
                // corner of whatever screen happened to be up.
                const row = args ? args.row : undefined;
                const col = args ? args.col : undefined;
                if (!isNumber(row) || !isNumber(col) || row < 0 || col < 0) {
                    return {
                        error: "row and col are required (0-indexed, as get_screen reports them), " +
                            "or pass field_key. Nothing was written.",
                    };
                }
                body.row = row;
                body.col = col;
            }
            if (args && typeof args.erase === "boolean") body.erase = args.erase;
            const res = await postJSON("/screen/write", body, ctx);
            if (!res.ok) return { error: res.error };
            refreshTerminal();
            const out = { ok: true };
            // What the field holds now, where the server reports it. Lets the
            // model see a truncated or rejected write without another read.
            if (res.data && res.data.field) out.field = res.data.field;
            return out;
        },
        async submit_screen(args, ctx) {
            const body = {};
            const aid = args && (args.aid || args.key);
            if (aid) body.aid = String(aid);
            const res = await postJSON("/screen/submit", body, ctx);
            if (!res.ok) return { error: res.error };
            await settle(undefined, ctx);
            refreshTerminal();
            // A server that presses the key itself returns the screen it
            // produced; an older one returns only ok, so read it.
            const screen = res.data && res.data.screen
                ? summariseScreen(res.data.screen)
                : await readScreen(ctx);
            return { ok: true, screen };
        },
        async connect_session(args, ctx) {
            const hostname = (args && String(args.hostname || "").trim()) || "";
            if (!hostname) return { error: "hostname required" };
            const res = await postJSON("/screen/connect", { hostname }, ctx);
            if (!res.ok) return { error: res.error };
            // The page's own idea of which host it is on (other modules read
            // it). The chat panel does not key its transcript off this any
            // more — see rehomeConversation in copilot-panel.js — so changing
            // it here no longer redirects a conversation into another host's
            // saved history.
            if (document.body && res.data) {
                const newHost = (res.data.targetHost || "") + ":" + (res.data.targetPort || "");
                document.body.dataset.targetHost = newHost;
            }
            refreshTerminal();
            return {
                ok: true,
                targetHost: res.data && res.data.targetHost,
                targetPort: res.data && res.data.targetPort,
                screen: await readScreen(ctx),
            };
        },
        async move_cursor(args, ctx) {
            const row = args && Number(args.row);
            const col = args && Number(args.col);
            if (!(row >= 0) || !(col >= 0)) return { error: "row and col (>= 0) required" };
            const res = await postJSON("/screen/cursor", { row, col }, ctx);
            if (!res.ok) return { error: res.error };
            return { ok: true };
        },
        async wait_for_unlock(args, ctx) {
            const body = {};
            if (args && typeof args.timeout_ms === "number") body.timeout_ms = args.timeout_ms;
            const res = await postJSON("/screen/wait", body, ctx);
            if (!res.ok) return { error: res.error };
            refreshTerminal();
            return {
                unlocked: !!(res.data && res.data.unlocked),
                timedOut: !!(res.data && res.data.timedOut),
                screen: res.data && res.data.screen ? summariseScreen(res.data.screen) : null,
            };
        },
        async chaos_status(args, ctx) {
            const verbose = !!(args && args.verbose);
            const url = verbose ? "/chaos/status?verbose=true" : "/chaos/status";
            const res = await getJSON(url, ctx);
            if (!res.ok) return { error: res.error };
            if (verbose && res.data && typeof window.ChaosUI === "object") {
                window.ChaosUI.updateFromStatus(res.data);
            }
            return res.data;
        },
        async chaos_start(args, ctx) {
            const body = {};
            if (args && typeof args.max_steps === "number") body.maxSteps = args.max_steps;
            if (args && typeof args.time_budget_sec === "number") body.timeBudgetSec = args.time_budget_sec;
            if (args && typeof args.step_delay_sec === "number") body.stepDelaySec = args.step_delay_sec;
            if (args && typeof args.seed === "number") body.seed = args.seed;
            if (args && typeof args.max_field_length === "number") body.maxFieldLength = args.max_field_length;
            if (args && typeof args.screen_dedup_similarity === "number") body.screenDedupSimilarity = args.screen_dedup_similarity;
            if (args && typeof args.auto_prefer_navigation_keys === "boolean") body.autoPreferNavigationKeys = args.auto_prefer_navigation_keys;
            if (args && typeof args.transition_log_path === "string") body.transitionLogPath = args.transition_log_path;
            const blocked = args && (args.blocked_keys || args.key_blacklist);
            if (Array.isArray(blocked) && blocked.length) body.keyBlacklist = blocked.map(String);
            const res = await postJSON("/chaos/start", body, ctx);
            if (!res.ok) return { error: res.error };
            return res.data;
        },
        async chaos_stop(_args, ctx) {
            const res = await postJSON("/chaos/stop", {}, ctx);
            if (!res.ok) return { error: res.error };
            return res.data;
        },
        async chaos_resume(_args, ctx) {
            const res = await postJSON("/chaos/resume", {}, ctx);
            if (!res.ok) return { error: res.error };
            return res.data;
        },
        async chaos_report(_args, ctx) {
            const resp = await fetch("/chaos/report", {
                method: "POST",
                credentials: "same-origin",
                headers: requestHeaders(),
                signal: ctx && ctx.signal,
            });
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
        async chaos_save_screen_hint(args, ctx) {
            const hash = (args && args.screen_hash) || "";
            if (!hash) return { error: "screen_hash required" };
            const body = { screen_hash: hash };
            if (args.known_data) body.known_data = args.known_data;
            if (args.known_keys) body.known_keys = args.known_keys;
            if (args.blocked_keys) body.blocked_keys = args.blocked_keys;
            if (args.key_assignments) body.key_assignments = args.key_assignments;
            const res = await postJSON("/chaos/screen-hints", body, ctx);
            if (!res.ok) return { error: res.error };
            return res.data;
        },
        async chaos_get_hints(_args, ctx) {
            const global = await getJSON("/chaos/hints", ctx);
            if (!global.ok) return { error: global.error };
            const screen = await getJSON("/chaos/screen-hints", ctx);
            return {
                hints: (global.data && global.data.hints) || [],
                keyBlacklist: (global.data && global.data.keyBlacklist) || [],
                firstScreenHint: (global.data && global.data.firstScreenHint) || null,
                screenHints: (screen.ok && screen.data && screen.data.screenHints) || {},
            };
        },
        async chaos_update_hints(args, ctx) {
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
            const res = await postJSON("/chaos/hints", body, ctx);
            if (!res.ok) return { error: res.error };
            return res.data;
        },
        async chaos_export_workflow(_args, ctx) {
            const res = await postJSON("/chaos/export", {}, ctx);
            if (!res.ok) return { error: res.error };
            return { workflow: res.data };
        },
        async chaos_list_screens(args, ctx) {
            const includePreviews = !(args && args.include_previews === false);
            const url = includePreviews ? "/chaos/screens" : "/chaos/screens?include_previews=false";
            const res = await getJSON(url, ctx);
            if (!res.ok) return { error: res.error };
            return res.data;
        },
        // One discovered screen in full, by the hash chaos_list_screens and
        // the turn context name it with. The listing carries previews; this
        // is the whole record, so the model can look at a screen it is not
        // on without navigating there.
        async chaos_get_screen(args, ctx) {
            const hash = (args && String(args.hash || args.screen_hash || "").trim()) || "";
            if (!hash) return { error: "hash required: the screen hash from chaos_list_screens or the turn context" };
            const res = await getJSON("/copilot/known-screen?hash=" + encodeURIComponent(hash), ctx);
            if (!res.ok) return { error: res.error };
            return res.data;
        },
        async chaos_annotate_screen(args, ctx) {
            const hash = (args && args.screen_hash) || "";
            if (!hash) return { error: "screen_hash required" };
            const body = { screen_hash: hash };
            if (args.business_purpose) body.business_purpose = args.business_purpose;
            if (args.notes) body.notes = args.notes;
            if (args.field_semantics) body.field_semantics = args.field_semantics;
            const res = await postJSON("/chaos/screens/annotate", body, ctx);
            if (!res.ok) return { error: res.error };
            return res.data;
        },
        async business_list_functions(_args, ctx) {
            const res = await getJSON("/chaos/business/functions", ctx);
            if (!res.ok) return { error: res.error };
            return res.data;
        },
        async business_app_overview(_args, ctx) {
            const res = await getJSON("/chaos/business/overview", ctx);
            if (!res.ok) return { error: res.error };
            return res.data;
        },
        async chaos_insights(_args, ctx) {
            const res = await getJSON("/chaos/insights", ctx);
            if (!res.ok) return { error: res.error };
            return res.data;
        },
        async business_save_function(args, ctx) {
            if (!args || !args.name) return { error: "name required" };
            const res = await postJSON("/chaos/business/functions", args, ctx);
            if (!res.ok) return { error: res.error };
            return res.data;
        },
        async business_generate_workflow(args, ctx) {
            if (!args || !args.name) return { error: "name required" };
            const body = { name: args.name };
            if (args.parameters) body.parameters = args.parameters;
            if (args.host) body.host = args.host;
            if (typeof args.port === "number") body.port = args.port;
            const res = await postJSON("/chaos/business/generate-workflow", body, ctx);
            if (!res.ok) return { error: res.error };
            return { workflow: res.data };
        },

        // Skills, instructions and extensions. The server owns the catalogue
        // and the per-conversation dedup, so these are thin: the "you already
        // have this" reply comes back as an ordinary body.
        async list_skills(_args, ctx) {
            const res = await getJSON("/skills", ctx);
            if (!res.ok) return { error: res.error };
            return res.data;
        },
        async load_skill(args, ctx) {
            const name = (args && String(args.name || "").trim()) || "";
            if (!name) return { error: "name required" };
            const res = await getJSON("/skills/load?name=" + encodeURIComponent(name), ctx);
            if (!res.ok) return { error: res.error };
            return res.data;
        },
        async list_instructions(_args, ctx) {
            const res = await getJSON("/instructions", ctx);
            if (!res.ok) return { error: res.error };
            return res.data;
        },
        async load_instruction(args, ctx) {
            const name = (args && String(args.name || "").trim()) || "";
            if (!name) return { error: "name required" };
            const res = await getJSON("/instructions/load?name=" + encodeURIComponent(name), ctx);
            if (!res.ok) return { error: res.error };
            return res.data;
        },
        async list_extensions(_args, ctx) {
            const res = await getJSON("/extensions", ctx);
            if (!res.ok) return { error: res.error };
            return res.data;
        },

        // What the terminal can do besides drive a screen: the connection's
        // own account of itself, the display toggles, screen snapshots, the
        // printer session and the saved task catalogue. Each of these was on
        // the HTTP API before it was a tool, which meant an assistant asked
        // whether 3270Web could do it had no way to find out that it could.
        async get_connection_details(_args, ctx) {
            const res = await getJSON("/host/query", ctx);
            if (!res.ok) return { error: res.error };
            return res.data;
        },
        async get_display_toggles(_args, ctx) {
            const res = await getJSON("/host/toggles", ctx);
            if (!res.ok) return { error: res.error };
            return res.data;
        },
        async set_display_toggle(args, ctx) {
            const name = (args && String(args.name || "").trim()) || "";
            if (!name) return { error: "name required" };
            if (typeof (args && args.value) !== "boolean") {
                return { error: "value required, and must be true or false" };
            }
            const res = await postJSON("/host/toggles", { name, value: args.value }, ctx);
            if (!res.ok) return { error: res.error };
            // The toggle changes how the terminal draws, so the page has to
            // re-read the screen or the operator sees the old rendering until
            // something else happens to refresh it.
            refreshTerminal();
            return res.data;
        },
        async snapshot_take(args, ctx) {
            const name = (args && String(args.name || "").trim()) || "";
            if (!name) return { error: "name required" };
            const res = await postJSON("/screen/snapshots", { name }, ctx);
            if (!res.ok) return { error: res.error };
            return res.data;
        },
        async snapshot_list(args, ctx) {
            const name = (args && String(args.name || "").trim()) || "";
            if (name) {
                const one = await getJSON("/screen/snapshots?name=" + encodeURIComponent(name), ctx);
                if (!one.ok) return { error: one.error };
                return one.data;
            }
            const res = await getJSON("/screen/snapshots", ctx);
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
        async snapshot_diff(args, ctx) {
            const left = (args && String(args.left || "").trim()) || "";
            if (!left) return { error: "left required" };
            const body = { left };
            const right = (args && String(args.right || "").trim()) || "";
            if (right) body.right = right;
            const res = await postJSON("/screen/snapshots/diff", body, ctx);
            if (!res.ok) return { error: res.error };
            return res.data;
        },
        async snapshot_delete(args, ctx) {
            const name = (args && String(args.name || "").trim()) || "";
            if (!name) return { error: "name required" };
            const res = await deleteJSON("/screen/snapshots?name=" + encodeURIComponent(name), ctx);
            if (!res.ok) return { error: res.error };
            return res.data;
        },
        async printer_status(_args, ctx) {
            const res = await getJSON("/printer/status", ctx);
            if (!res.ok) return { error: res.error };
            return res.data;
        },
        // The printer panel polls while it is open, so neither of these has to
        // tell it anything: a printer the assistant started shows up there on
        // its own within a poll.
        async printer_start(args, ctx) {
            const lu = (args && String(args.lu || "").trim()) || "";
            const res = await postJSON("/printer/start", lu ? { lu } : { associate: true }, ctx);
            if (!res.ok) return { error: res.error };
            return res.data;
        },
        async printer_stop(_args, ctx) {
            const res = await postJSON("/printer/stop", {}, ctx);
            if (!res.ok) return { error: res.error };
            return res.data;
        },
        async printer_read_job(args, ctx) {
            const name = (args && String(args.name || "").trim()) || "";
            if (!name) return { error: "name required" };
            const resp = await fetch("/printer/jobs/download?name=" + encodeURIComponent(name), {
                credentials: "same-origin",
                headers: requestHeaders(),
                signal: ctx && ctx.signal,
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
        async list_tasks(_args, ctx) {
            const res = await getJSON("/tasks", ctx);
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
        async run_task(args, ctx) {
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
            const started = await postJSON("/tasks/run", body, ctx);
            if (!started.ok) return { error: started.error };

            // The browser's run is asynchronous so the panel can show it in
            // flight; the model wants the answer. Poll to the same ceiling the
            // server gives a run, then report the timeout rather than claiming
            // an outcome nobody observed.
            for (let waited = 0; waited < TASK_RUN_TIMEOUT_MS; waited += TASK_POLL_MS) {
                await settle(TASK_POLL_MS, ctx);
                const status = await getJSON("/tasks/status", ctx);
                if (!status.ok) return { error: status.error };
                if (status.data && status.data.running) continue;
                refreshTerminal();
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

    // runTool runs one tool to a JSON-serializable result. It never throws:
    // a failure is a result the model can read.
    //
    // ctx carries {signal, confirmed}. A stopped turn aborts the signal, and
    // the tool reports that rather than an error the model would try to
    // recover from — nothing went wrong, a person said stop.
    async function runTool(name, args, ctx) {
        const handler = HANDLERS[name];
        if (!handler) return { error: "unknown tool: " + name };
        try {
            const result = await handler(args || {}, ctx || {});
            // Anything that starts, stops, resumes or loads a run changes the
            // Automation menu, whatever the tool is called.
            if (/^chaos_(start|stop|resume|load)/.test(name) && result && !result.error) refreshChaosUI();
            return result;
        } catch (err) {
            if (err && err.name === "AbortError") {
                return { skipped: true, stopped: true, reason: "stopped by user" };
            }
            return { error: String(err && err.message || err) };
        }
    }

    function knownTools() { return Object.keys(HANDLERS); }

    window.CopilotTools = { runTool, knownTools, setConversation, requestHeaders, summariseScreen };
})();
