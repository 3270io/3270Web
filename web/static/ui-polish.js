/* =====================================================================
   3270Web — UX enhancements that pair with ui-polish.css
   ---------------------------------------------------------------------
     1. Command palette (Ctrl/Cmd+K) over every toolbar + modal action
     2. Drag-to-resize Copilot panel, persisted
     3. Terminal bezel reflects the OIA keyboard-lock state
     4. Scroll affordances on long dialog bodies

   Everything here is additive: it drives the existing controls by
   clicking them, so behaviour stays owned by the modules that already
   implement it (ui.js, workflow.js, copilot-panel.js, ...).
   ===================================================================== */
(() => {
  "use strict";

  const ICONS = {
    terminal: "M4 4h16a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2zm2.7 4.3L5.3 9.7 7.6 12l-2.3 2.3 1.4 1.4L10.4 12 6.7 8.3zM12 15h6v-1.5h-6V15z",
    record: "M12 6a6 6 0 1 1 0 12 6 6 0 0 1 0-12z",
    play: "M8 5l12 7-12 7z",
    stop: "M6 6h12v12H6z",
    chaos: "M10.59 9.17L5.41 4 4 5.41l5.17 5.17 1.42-1.41zM14.5 4l2.04 2.04L4 18.59 5.41 20 17.96 7.46 20 9.5V4h-5.5zm.33 9.41l-1.41 1.41 3.13 3.13L14.5 20H20v-5.5l-2.04 2.04-3.13-3.13z",
    gear: "M19.14 12.94c.04-.31.06-.63.06-.94s-.02-.63-.06-.94l2.03-1.58a.5.5 0 0 0 .12-.64l-1.92-3.32a.5.5 0 0 0-.6-.22l-2.39.96a7.01 7.01 0 0 0-1.63-.94l-.36-2.54A.5.5 0 0 0 13.9 1h-3.8a.5.5 0 0 0-.49.41l-.36 2.54c-.59.23-1.13.54-1.63.94l-2.39-.96a.5.5 0 0 0-.6.22L2.71 7.47a.5.5 0 0 0 .12.64l2.03 1.58c-.04.31-.06.63-.06.94s.02.63.06.94l-2.03 1.58a.5.5 0 0 0-.12.64l1.92 3.32a.5.5 0 0 0 .6.22l2.39-.96c.5.4 1.04.71 1.63.94l.36 2.54a.5.5 0 0 0 .49.41h3.8a.5.5 0 0 0 .49-.41l.36-2.54c.59-.23 1.13-.54 1.63-.94l2.39.96a.5.5 0 0 0 .6-.22l1.92-3.32a.5.5 0 0 0-.12-.64l-2.03-1.58zM12 15.5A3.5 3.5 0 1 1 12 8a3.5 3.5 0 0 1 0 7.5z",
    doc: "M14 2H6c-1.1 0-2 .9-2 2v16c0 1.1.89 2 1.99 2H18c1.1 0 2-.9 2-2V8l-6-6zm2 16H8v-2h8v2zm0-4H8v-2h8v2zm-3-5V3.5L18.5 9H13z",
    chat: "M20 2H4c-1.1 0-2 .9-2 2v18l4-4h14c1.1 0 2-.9 2-2V4c0-1.1-.9-2-2-2z",
    palette: "M12 3a9 9 0 0 0 0 18c.83 0 1.5-.67 1.5-1.5 0-.39-.15-.74-.39-1-.24-.27-.39-.62-.39-1 0-.83.67-1.5 1.5-1.5H16a5 5 0 0 0 5-5c0-4.42-4.03-8-9-8zm-5.5 9a1.5 1.5 0 1 1 0-3 1.5 1.5 0 0 1 0 3zm3-4a1.5 1.5 0 1 1 0-3 1.5 1.5 0 0 1 0 3zm5 0a1.5 1.5 0 1 1 0-3 1.5 1.5 0 0 1 0 3zm3 4a1.5 1.5 0 1 1 0-3 1.5 1.5 0 0 1 0 3z",
    power: "M13 3h-2v10h2V3zm4.83 2.17-1.42 1.42A6.92 6.92 0 0 1 19 12a7 7 0 0 1-14 0 6.92 6.92 0 0 1 2.59-5.41L6.17 5.17A8.93 8.93 0 0 0 3 12a9 9 0 0 0 18 0 8.93 8.93 0 0 0-3.17-6.83z",
    eye: "M12 5c5.25 0 9.62 3.44 11 7-1.38 3.56-5.75 7-11 7S2.38 15.56 1 12c1.38-3.56 5.75-7 11-7zm0 2c-3.55 0-6.67 2.02-8.16 5 1.49 2.98 4.61 5 8.16 5s6.67-2.02 8.16-5C18.67 9.02 15.55 7 12 7zm0 2.5a2.5 2.5 0 1 1 0 5 2.5 2.5 0 0 1 0-5z",
    map: "M15 5.1 9 3 3 5.1v15.8L9 19l6 2.1 6-2.1V3zm-7 .7 6 2.1v13.4l-6-2.1zM5 6.5l2-.7v13.4l-2 .7zm14 12.7-3 1.1V6.9l3-1.1z",
    print: "M19 8H5c-1.66 0-3 1.34-3 3v6h4v4h12v-4h4v-6c0-1.66-1.34-3-3-3zm-3 11H8v-5h8v5zm3-7c-.55 0-1-.45-1-1s.45-1 1-1 1 .45 1 1-.45 1-1 1zm-1-9H6v4h12V3z",
    download: "M5 20h14v-2H5v2zM19 9h-4V3H9v6H5l7 7 7-7z",
    upload: "M10 4H4a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2V8h-8l-2-4zm2 6v4h3l-4 4-4-4h3v-4h2z",
    bulb: "M9 21h6v-1H9v1zm3-19a7 7 0 0 0-4 12.74V17a1 1 0 0 0 1 1h6a1 1 0 0 0 1-1v-2.26A7 7 0 0 0 12 2z",
    search: "M15.5 14h-.79l-.28-.27A6.47 6.47 0 0 0 16 9.5 6.5 6.5 0 1 0 9.5 16c1.61 0 3.09-.59 4.23-1.57l.27.28v.79l5 4.99L20.49 19l-4.99-5zm-6 0A4.5 4.5 0 1 1 14 9.5 4.5 4.5 0 0 1 9.5 14z",
    info: "M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20zm1 15h-2v-6h2v6zm0-8h-2V7h2v2z",
    trash: "M9 3h6l1 2h4v2H4V5h4l1-2zm1 6h2v8h-2V9zm4 0h2v8h-2V9zM7 9h2v8H7V9z",
    plus: "M19 3H5a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2V5a2 2 0 0 0-2-2zm-2 10h-4v4h-2v-4H7v-2h4V7h2v4h4v2z"
  };

  /* -------------------------------------------------------------------
     Command registry

     Each entry maps a human label onto an existing control. `sel` is the
     control that gets clicked; the palette hides entries whose control is
     absent, [hidden] or :disabled, so what you see is what you can run.
     ------------------------------------------------------------------- */
  const COMMANDS = [
    // Session
    { label: "New session — open another host", group: "Session", sel: "[data-session-new]", icon: "plus", hint: "Alt+N — this one keeps running" },
    { label: "Disconnect session", group: "Session", sel: "[data-disconnect-open]", icon: "power", hint: "End the 3270 connection" },
    { label: "Print screen", group: "Session", sel: "[data-print-screen]", icon: "print" },
    { label: "View logs", group: "Session", sel: "[data-logs-open]", icon: "doc" },
    { label: "Open settings", group: "Session", sel: "[data-settings-open]", icon: "gear" },
    { label: "About 3270Web", group: "Session", sel: "[data-about-open]", icon: "info" },
    { label: "Toggle AI chat", group: "Session", sel: "[data-copilot-toggle]", icon: "chat", hint: "Show or hide the assistant panel" },
    // run:, not sel: — the settings button lives inside the chat panel, so a
    // selector entry would only be reachable once the panel is already open.
    { label: "AI provider settings", group: "Session", icon: "gear", hint: "Choose Copilot, Claude, OpenAI, Ollama, …", run: () => window.CopilotPanel && window.CopilotPanel.settings() },
    { label: "Collapse the menu bar", group: "Session", sel: "[data-chrome-toggle]", icon: "eye", hint: "Distraction-free terminal" },

    // Recording
    { label: "Start recording", group: "Recording", sel: "[data-recording-start] button[type=submit]", icon: "record" },
    { label: "Stop recording", group: "Recording", sel: "[data-recording-stop] button[type=submit]", icon: "stop" },
    { label: "Load recording from file", group: "Recording", sel: "[data-workflow-trigger]", icon: "upload" },
    { label: "Play recording", group: "Recording", sel: ".workflow-controls form[action='/workflow/play'] button", icon: "play" },
    { label: "Debug recording step by step", group: "Recording", sel: ".workflow-controls form[action='/workflow/debug'] button", icon: "chaos" },
    { label: "View recording JSON", group: "Recording", sel: ".workflow-controls [data-modal-open]", icon: "doc" },
    { label: "Remove loaded recording", group: "Recording", sel: ".workflow-controls form[action='/workflow/remove'] button", icon: "trash" },
    { label: "Download recording", group: "Recording", sel: ".record-download-link", icon: "download" },

    // Playback
    { label: "Step playback", group: "Playback", sel: "[data-playback-debug-controls] form[action='/workflow/step'] button", icon: "play" },
    { label: "Pause or resume playback", group: "Playback", sel: "[data-playback-pause-button]", icon: "play" },
    { label: "Stop playback", group: "Playback", sel: "[data-playback-play-controls] form[action='/workflow/stop'] button", icon: "stop" },

    // Chaos
    { label: "Start chaos exploration", group: "Chaos", sel: "[data-chaos-start]", icon: "chaos" },
    { label: "Stop chaos exploration", group: "Chaos", sel: "[data-chaos-stop]", icon: "stop" },
    { label: "Load previous chaos run", group: "Chaos", sel: "[data-chaos-load]", icon: "upload" },
    { label: "Load recording into chaos", group: "Chaos", sel: "[data-chaos-load-recording]", icon: "upload" },
    { label: "Resume chaos run", group: "Chaos", sel: "[data-chaos-resume]", icon: "play" },
    { label: "Extend chaos run", group: "Chaos", sel: "[data-chaos-extend]", icon: "chaos" },
    { label: "Edit chaos hints", group: "Chaos", sel: "[data-chaos-hints-open]", icon: "bulb" },
    { label: "Open chaos map", group: "Chaos", sel: "[data-chaos-map-open]", icon: "map" },
    { label: "View chaos discovery report", group: "Chaos", sel: "[data-chaos-report]", icon: "doc" },
    { label: "Export chaos workflow JSON", group: "Chaos", sel: "[data-chaos-export]", icon: "download" },
    { label: "Remove chaos run", group: "Chaos", sel: "[data-chaos-remove]", icon: "trash" },

    // Tasks — the business-user surface, so it leads its own group rather
    // than sitting among the terminal tools.
    { label: "Run a business task", group: "Tasks", sel: "[data-tasks-open]", icon: "play", hint: "Fill in a form, get an answer" },
    { label: "Save recording as a task", group: "Tasks", sel: "[data-wizard-open]", icon: "record", hint: "Turn the flow you just recorded into a task" },

    // Terminal
    { label: "Focus mode — fill the screen", group: "Terminal", sel: "[data-focus-toggle]", icon: "eye", hint: "Alt+Enter" },
    { label: "Find on screen", group: "Terminal", sel: "[data-find-open]", icon: "search", hint: "Ctrl+F — searches input values too" },
    { label: "Screen history", group: "Terminal", sel: "[data-history-open]", icon: "doc", hint: "Look back at earlier screens" },
    // Matches the Terminal menu's entry rather than calling the panel directly,
    // so the palette keeps hiding it wherever that control is absent.
    { label: "Explain this screen", group: "Terminal", sel: "[data-explain-screen]", icon: "bulb", hint: "Ask the assistant what you are looking at" },
    { label: "File transfer (IND$FILE)", group: "Terminal", sel: "[data-transfer-open]", icon: "upload", hint: "Send or receive a host file" },
    { label: "Toggle hotspots", group: "Terminal", sel: "[data-hotspots-toggle]", icon: "bulb", hint: "Clickable PF labels and URLs" },
    { label: "Copy screen", group: "Terminal", sel: "[data-copy-screen]", icon: "doc", hint: "Whole screen as text" },
    { label: "Copy marked block", group: "Terminal", icon: "doc", hint: "Alt+drag marks a rectangle", run: () => runTerminal("copyBlock") },
    { label: "Reconnect to host", group: "Terminal", icon: "power", hint: "Re-dial the current host", run: () => runTerminal("reconnect") },
    { label: "Toggle insert mode", group: "Terminal", icon: "gear", hint: "Insert vs overtype", run: () => runTerminal("toggleInsertMode") },
    { label: "Reset keyboard", group: "Terminal", icon: "power", hint: "Clear an X -f operator error", run: () => runTerminal("reset") },
    { label: "Keyboard mapping", group: "Terminal", sel: "[data-keymap-open]", icon: "gear", hint: "Rebind keys; import a keymap file" },
    { label: "Connection details", group: "Terminal", sel: "[data-host-details-open]", icon: "bulb", hint: "Telnet options, TLS, LU name — what the screen cannot show" },
    { label: "Toggle virtual keypad", group: "Terminal", sel: "[data-keypad-toggle]", icon: "gear" },
    { label: "Switch workspace mode", group: "Terminal", sel: "[data-workspace-toggle]", icon: "gear", hint: "Business ↔ Engineering" }
  ];

  /* Host keys, as palette entries. There is no reasonable way to show
     twenty-four PF keys in a default list, so these are search-only: typing
     "pf3" or "attn" finds them instantly and nothing is crowded out for
     someone who came to the palette for something else. */
  const KEY_COMMANDS = (() => {
    const keys = [
      ["Enter", "Enter"],
      ["Clear", "Clear"],
      ["Reset", "Reset"],
      ["Attn", "Attn"],
      ["SysReq", "SysReq"],
      ["Erase EOF", "EraseEOF"],
      ["Erase input", "EraseInput"],
      ["Dup", "Dup"],
      ["Field mark", "FieldMark"],
      ["PA1", "PA1"],
      ["PA2", "PA2"],
      ["PA3", "PA3"]
    ];
    for (let n = 1; n <= 24; n++) keys.push([`PF${n}`, `PF${n}`]);
    return keys.map(([label, key]) => ({
      label: `Send ${label}`,
      group: "Host keys",
      icon: "play",
      searchOnly: true,
      run: () => {
        if (typeof window.sendKey === "function") window.sendKey(key, null);
      }
    }));
  })();

  /* Terminal actions live on window.ThreeSeventyWeb rather than behind a
     visible control, so the palette calls them directly. */
  function runSessionTabs(action) {
    const tabs = (window.ThreeSeventyWeb || {}).sessionTabs;
    if (tabs && typeof tabs[action] === "function") {
      return tabs[action]();
    }
    return undefined;
  }

  function runTerminal(action) {
    const api = window.ThreeSeventyWeb || {};
    if (action === "copyBlock" && api.screenCopy) return api.screenCopy.copyBlock();
    if (action === "reconnect" && typeof api.reconnect === "function") return api.reconnect();
    if (api.terminal && typeof api.terminal[action] === "function") {
      return api.terminal[action](null);
    }
    return undefined;
  }

  const svg = (path) =>
    `<svg viewBox="0 0 24 24" aria-hidden="true" focusable="false"><path d="${path}"/></svg>`;

  const isRunnable = (el) => {
    if (!el) return false;
    if (el.disabled) return false;
    // Walk up looking for a [hidden] ancestor — several controls are
    // toggled by hiding their wrapping <form>/<span> rather than themselves.
    for (let node = el; node && node !== document.body; node = node.parentElement) {
      // A closed menu panel is hidden, and everything the menus hold would
      // therefore drop out of the palette — which is most of the session.
      // "Not on screen right now" is not the same as "not available": the
      // palette exists precisely so you do not have to go and open the menu.
      if (node.hasAttribute && node.hasAttribute("data-app-menu-panel")) continue;
      if (node.hasAttribute && node.hasAttribute("hidden")) return false;
    }
    return true;
  };

  /* Subsequence fuzzy match. Consecutive and word-start hits score well;
     characters scattered far apart are penalised, so "chao" ranks the four
     chaos commands and not "Appearance / Theme: Amber Phosphor" (which does
     technically contain c-h-a-o spread across three words). */
  function fuzzy(needle, haystack) {
    if (!needle) return { score: 0, marks: [] };
    const hay = haystack.toLowerCase();
    const nee = needle.toLowerCase();
    let score = 0;
    let run = 0;
    let hi = 0;
    const marks = [];
    for (let ni = 0; ni < nee.length; ni++) {
      const ch = nee[ni];
      if (ch === " ") continue;
      const found = hay.indexOf(ch, hi);
      if (found === -1) return null;
      const atWordStart = found === 0 || /[\s\-_/:]/.test(hay[found - 1]);
      const gap = found - hi;
      run = gap === 0 ? run + 1 : 0;
      score += 1 + run * 4 + (atWordStart ? 6 : 0) - Math.min(gap, 12) * 1.5;
      marks.push(found);
      hi = found + 1;
    }
    // A whole-word substring hit is almost always the intended target.
    if (hay.includes(nee)) score += 14;
    // Shorter labels that match are more likely to be what you meant.
    score += Math.max(0, 24 - haystack.length) * 0.2;
    return { score, marks };
  }

  /* Not every label is a string this file wrote: theme names come from files
     in the themes folder, so anything rendered as markup has to be escaped
     first. The page's CSP would stop an injected handler from running, but a
     stylesheet is not the right place for that to be caught. */
  const esc = (text) =>
    String(text).replace(/[&<>"]/g, (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));

  const highlight = (text, marks) => {
    const set = new Set(marks);
    let out = "";
    for (let i = 0; i < text.length; i++) {
      const ch = esc(text[i]);
      out += set.has(i) ? `<mark>${ch}</mark>` : ch;
    }
    return out;
  };

  /* ---------------------------------------------------------------- */
  /* Command palette                                                   */
  /* ---------------------------------------------------------------- */
  const RECENT_KEY = "3270web.cmdk.recent";
  let root = null;
  let input = null;
  let list = null;
  let items = [];
  let active = 0;
  let lastFocus = null;
  let focusTrap = null;

  const readRecent = () => {
    try {
      const raw = JSON.parse(localStorage.getItem(RECENT_KEY) || "[]");
      return Array.isArray(raw) ? raw.filter((v) => typeof v === "string") : [];
    } catch (_) {
      return [];
    }
  };

  const pushRecent = (label) => {
    try {
      const next = [label].concat(readRecent().filter((l) => l !== label)).slice(0, 6);
      localStorage.setItem(RECENT_KEY, JSON.stringify(next));
    } catch (_) {
      /* storage unavailable — recents are a nicety, not a requirement */
    }
  };

  function themeCommands() {
    const api = window.ThreeSeventyWeb;
    if (!api || typeof api.listThemes !== "function") return [];
    const current = typeof api.getCurrentTheme === "function" ? api.getCurrentTheme() : "";
    return api.listThemes().map((theme) => ({
      label: `Theme: ${theme.name}`,
      group: "Appearance",
      icon: "palette",
      hint: theme.id === current ? "Current theme" : "",
      run: () => api.applyTheme(theme.id)
    }));
  }

  /* One entry per open session, so "switch" or a hostname finds the tab you
     want without going hunting along the bar. Built when the palette opens
     rather than at load: the roster changes as sessions come and go.

     Both of these need somewhere else to go. With a single session there is
     nothing to switch to, and closing it is disconnecting — which already
     has its own entry saying so in the words people expect. */
  function sessionCommands() {
    const tabs = (window.ThreeSeventyWeb || {}).sessionTabs;
    if (!tabs || typeof tabs.list !== "function") return [];
    const open = tabs.list();
    if (open.length < 2) return [];

    const out = open.map((s, i) => ({
      label: `Switch to session: ${s.label || s.target}`,
      group: "Session",
      icon: "terminal",
      hint: s.active ? "Current session" : `Alt+${i + 1} — ${s.target || ""}`,
      run: () => {
        if (!s.active) tabs.switchTo(s.id);
      }
    }));
    out.push({
      label: "Close this session",
      group: "Session",
      icon: "trash",
      hint: "Alt+W — the other sessions keep running",
      run: () => runSessionTabs("closeActive")
    });
    return out;
  }

  function collect(includeSearchOnly) {
    const out = [];
    COMMANDS.concat(KEY_COMMANDS).forEach((cmd) => {
      if (cmd.searchOnly && !includeSearchOnly) return;
      // Entries that drive an API directly rather than clicking a control.
      if (!cmd.sel) {
        out.push(cmd);
        return;
      }
      const el = document.querySelector(cmd.sel);
      if (!isRunnable(el)) return;
      out.push(Object.assign({}, cmd, {
        // Clicking the real control, wherever it lives. A control inside a
        // closed menu panel still runs when clicked — the panel is chrome,
        // not a gate — so there is nothing to open first.
        run: () => el.click()
      }));
    });
    return out.concat(sessionCommands()).concat(themeCommands());
  }

  function build() {
    root = document.createElement("div");
    root.className = "cmdk";
    root.hidden = true;
    root.innerHTML = `
      <div class="cmdk-panel" role="dialog" aria-modal="true" aria-label="Command palette">
        <div class="cmdk-search">
          <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M15.5 14h-.79l-.28-.27A6.47 6.47 0 0 0 16 9.5 6.5 6.5 0 1 0 9.5 16c1.61 0 3.09-.59 4.23-1.57l.27.28v.79l5 4.99L20.49 19l-4.99-5zm-6 0A4.5 4.5 0 1 1 14 9.5 4.5 4.5 0 0 1 9.5 14z"/></svg>
          <input class="cmdk-input" type="text" role="combobox" aria-expanded="true"
                 aria-controls="cmdk-list" aria-autocomplete="list" autocomplete="off"
                 spellcheck="false" placeholder="Search commands…">
          <span class="cmdk-hintkey">ESC</span>
        </div>
        <ul class="cmdk-list" id="cmdk-list" role="listbox" aria-label="Commands"></ul>
        <div class="cmdk-footer">
          <span><kbd>↑</kbd><kbd>↓</kbd> navigate</span>
          <span><kbd>↵</kbd> run</span>
          <span><kbd>esc</kbd> close</span>
        </div>
      </div>`;
    document.body.appendChild(root);
    input = root.querySelector(".cmdk-input");
    list = root.querySelector(".cmdk-list");

    root.addEventListener("mousedown", (e) => {
      if (e.target === root) close();
    });
    input.addEventListener("input", render);
    list.addEventListener("click", (e) => {
      const li = e.target.closest(".cmdk-item");
      if (!li) return;
      run(Number(li.dataset.index));
    });
    list.addEventListener("mousemove", (e) => {
      const li = e.target.closest(".cmdk-item");
      if (!li) return;
      const idx = Number(li.dataset.index);
      if (idx !== active) {
        active = idx;
        paintActive();
      }
    });
  }

  function render() {
    const query = input.value.trim();
    const all = collect(query.length > 0);
    let matched;

    if (!query) {
      const recent = readRecent();
      matched = all
        .map((cmd) => ({ cmd, marks: [], rank: recent.indexOf(cmd.label) }))
        .sort((a, b) => {
          const ar = a.rank === -1 ? 999 : a.rank;
          const br = b.rank === -1 ? 999 : b.rank;
          return ar - br;
        })
        .map((entry) => Object.assign({}, entry, {
          group: entry.rank !== -1 ? "Recent" : entry.cmd.group
        }));
    } else {
      matched = all
        .map((cmd) => {
          const direct = fuzzy(query, cmd.label);
          const viaGroup = fuzzy(query, `${cmd.group} ${cmd.label}`);
          if (!direct && !viaGroup) return null;
          return direct
            ? { cmd, marks: direct.marks, score: direct.score, group: cmd.group }
            : { cmd, marks: [], score: viaGroup.score * 0.4, group: cmd.group };
        })
        .filter(Boolean)
        .sort((a, b) => b.score - a.score);

      // Keep only what is plausibly relevant: positive scores, and within a
      // reasonable band of the best hit. Without this, a long enough query
      // eventually finds its letters scattered through almost every label.
      const best = matched.length ? matched[0].score : 0;
      const floor = Math.max(1, best * 0.4);
      matched = matched.filter((entry) => entry.score >= floor);
    }

    items = matched;
    active = 0;

    if (!items.length) {
      list.innerHTML = `<li class="cmdk-empty">No commands match “${esc(input.value)}”</li>`;
      return;
    }

    let html = "";
    let lastGroup = null;
    items.forEach((entry, index) => {
      if (entry.group !== lastGroup) {
        html += `<li class="cmdk-group" role="presentation">${esc(entry.group)}</li>`;
        lastGroup = entry.group;
      }
      html += `
        <li class="cmdk-item" role="option" data-index="${index}" id="cmdk-opt-${index}"
            aria-selected="${index === 0}">
          <span class="cmdk-item-icon">${svg(ICONS[entry.cmd.icon] || ICONS.terminal)}</span>
          <span class="cmdk-item-body">
            <span class="cmdk-item-label">${entry.marks.length ? highlight(entry.cmd.label, entry.marks) : esc(entry.cmd.label)}</span>
            ${entry.cmd.hint ? `<span class="cmdk-item-sub">${esc(entry.cmd.hint)}</span>` : ""}
          </span>
        </li>`;
    });
    list.innerHTML = html;
    paintActive();
  }

  function paintActive() {
    const nodes = list.querySelectorAll(".cmdk-item");
    nodes.forEach((node, i) => node.setAttribute("aria-selected", String(i === active)));
    const current = nodes[active];
    if (current) {
      current.scrollIntoView({ block: "nearest" });
      input.setAttribute("aria-activedescendant", current.id);
    }
  }

  function run(index) {
    const entry = items[index];
    if (!entry) return;
    pushRecent(entry.cmd.label);
    close();
    // Let the palette finish tearing down (focus restore, scroll unlock)
    // before the command opens its own modal.
    requestAnimationFrame(() => {
      try {
        entry.cmd.run();
      } catch (err) {
        if (window.console) console.error("Command failed:", entry.cmd.label, err);
      }
    });
  }

  function open() {
    if (!root) build();
    if (!root.hidden) return;
    lastFocus = document.activeElement;
    root.hidden = false;
    input.value = "";
    render();
    input.focus();
    if (window.ThreeSeventyWeb && window.ThreeSeventyWeb.createFocusTrap) {
      focusTrap = window.ThreeSeventyWeb.createFocusTrap(root);
      if (focusTrap && focusTrap.activate) focusTrap.activate();
    }
  }

  function close() {
    if (!root || root.hidden) return;
    root.hidden = true;
    if (focusTrap && focusTrap.deactivate) focusTrap.deactivate();
    focusTrap = null;
    // Terminal focus is managed by keyboard.js's focus lock; restoring the
    // previously focused element keeps typing where the operator left it.
    if (lastFocus && document.contains(lastFocus) && lastFocus.focus) {
      lastFocus.focus();
    }
    lastFocus = null;
  }

  const isOpen = () => root && !root.hidden;

  /* keyboard.js installs a window-level capture handler and swallows most
     keys for the terminal. Registering here at script-eval time (before
     installKeyHandler runs) keeps the palette's shortcut reachable even
     while the terminal holds focus. */
  window.addEventListener(
    "keydown",
    (event) => {
      if (isOpen()) {
        // Fully own the keyboard while the palette is up.
        if (event.key === "Escape") {
          event.preventDefault();
          event.stopImmediatePropagation();
          close();
          return;
        }
        if (event.key === "ArrowDown" || event.key === "ArrowUp") {
          event.preventDefault();
          event.stopImmediatePropagation();
          if (!items.length) return;
          active = (active + (event.key === "ArrowDown" ? 1 : -1) + items.length) % items.length;
          paintActive();
          return;
        }
        if (event.key === "Enter") {
          event.preventDefault();
          event.stopImmediatePropagation();
          run(active);
          return;
        }
        if (event.key === "Home" || event.key === "End") {
          event.preventDefault();
          event.stopImmediatePropagation();
          active = event.key === "Home" ? 0 : Math.max(0, items.length - 1);
          paintActive();
          return;
        }
        // Tab is trapped here rather than by a focus trap on the panel: this
        // handler stops the event at the window, so nothing bound to the panel
        // itself would ever see it. The search box is the only focusable thing
        // in the palette, so trapping means keeping focus on it.
        if (event.key === "Tab") {
          event.preventDefault();
          event.stopImmediatePropagation();
          input.focus();
          return;
        }
        // Everything else is typing into the search box — keep it away
        // from the 3270 key handler.
        event.stopImmediatePropagation();
        return;
      }

      const key = event.key ? event.key.toLowerCase() : "";
      if ((event.ctrlKey || event.metaKey) && !event.altKey && key === "k") {
        event.preventDefault();
        event.stopImmediatePropagation();
        open();
      }
    },
    true
  );

  /* ---------------------------------------------------------------- */
  /* Copilot panel: drag to resize                                     */
  /* ---------------------------------------------------------------- */
  const WIDTH_KEY = "3270web.copilot.width";
  const MIN_W = 320;
  const MAX_W = 900;

  function applyPanelWidth(px) {
    const clamped = Math.min(MAX_W, Math.max(MIN_W, Math.round(px)));
    document.documentElement.style.setProperty("--copilot-panel-width", `${clamped}px`);
    return clamped;
  }

  function initCopilotResize() {
    const panel = document.getElementById("copilot-panel");
    if (!panel || panel.querySelector(".copilot-resize-handle")) return;

    const stored = Number(localStorage.getItem(WIDTH_KEY));
    if (Number.isFinite(stored) && stored > 0) applyPanelWidth(stored);

    const handle = document.createElement("div");
    handle.className = "copilot-resize-handle";
    handle.setAttribute("role", "separator");
    handle.setAttribute("aria-orientation", "vertical");
    handle.setAttribute("aria-label", "Resize Copilot panel");
    handle.tabIndex = 0;
    panel.appendChild(handle);

    let dragging = false;

    const onMove = (event) => {
      if (!dragging) return;
      event.preventDefault();
      applyPanelWidth(window.innerWidth - event.clientX);
    };

    const onUp = () => {
      if (!dragging) return;
      dragging = false;
      document.body.classList.remove("copilot-resizing");
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
      const width = parseInt(
        getComputedStyle(document.documentElement).getPropertyValue("--copilot-panel-width"),
        10
      );
      if (Number.isFinite(width)) {
        try {
          localStorage.setItem(WIDTH_KEY, String(width));
        } catch (_) { /* ignore */ }
      }
    };

    handle.addEventListener("pointerdown", (event) => {
      dragging = true;
      document.body.classList.add("copilot-resizing");
      window.addEventListener("pointermove", onMove);
      window.addEventListener("pointerup", onUp);
      event.preventDefault();
    });

    // Keyboard-resizable too, so the handle isn't mouse-only.
    handle.addEventListener("keydown", (event) => {
      const step = event.shiftKey ? 48 : 16;
      let delta = 0;
      if (event.key === "ArrowLeft") delta = step;
      else if (event.key === "ArrowRight") delta = -step;
      else return;
      event.preventDefault();
      const current = panel.getBoundingClientRect().width;
      const next = applyPanelWidth(current + delta);
      try {
        localStorage.setItem(WIDTH_KEY, String(next));
      } catch (_) { /* ignore */ }
    });
  }

  /* ---------------------------------------------------------------- */
  /* Terminal bezel reflects the OIA keyboard state                     */
  /* ---------------------------------------------------------------- */
  /* Mirrors the OIA's own resolved state rather than re-deriving one from
     the host's keyboard label. keyboard.js already reconciles three inputs
     into that state — the host's lock, a client-side operator error, and the
     optimistic wait shown the moment an AID key is sent — and a second
     opinion here would clobber the two the host does not know about. */
  function initKeyboardStateMirror() {
    const shell = document.querySelector(".terminal-shell");
    const indicator = document.querySelector("[data-oia-indicator]");
    if (!shell || !indicator) return;

    const sync = () => {
      const state = indicator.getAttribute("data-oia-state") || "ready";
      shell.setAttribute("data-kb-locked", String(state === "wait" || state === "error"));
      shell.setAttribute("data-kb-state", state);
    };

    sync();
    new MutationObserver(sync).observe(indicator, { attributes: true, attributeFilter: ["data-oia-state"] });
  }

  /* ---------------------------------------------------------------- */
  /* Copilot tool calls: mirror status text into a state attribute      */
  /* ---------------------------------------------------------------- */
  /* copilot-panel.js writes the lifecycle into .copilot-tool-call-status
     as free text ("Pending approval", "Running...", "Done", "Failed",
     "Skipped"). CSS cannot match on text, so classify it here and let the
     stylesheet colour the card's status rail. */
  function classifyToolStatus(text) {
    const value = (text || "").trim().toLowerCase();
    if (!value) return "";
    if (value.startsWith("done")) return "done";
    if (value.startsWith("failed") || value.includes("error")) return "failed";
    if (value.startsWith("skipped")) return "skipped";
    if (value.startsWith("running")) return "running";
    if (value.includes("approval")) return "pending";
    return "";
  }

  function initToolCallStates() {
    const messages = document.querySelector("[data-copilot-messages]");
    if (!messages) return;

    const sync = (statusEl) => {
      const card = statusEl.closest(".copilot-tool-call");
      if (!card) return;
      card.setAttribute("data-tool-state", classifyToolStatus(statusEl.textContent));
    };

    const scan = () => {
      messages.querySelectorAll(".copilot-tool-call-status").forEach(sync);
    };

    scan();
    new MutationObserver(scan).observe(messages, {
      childList: true,
      subtree: true,
      characterData: true
    });
  }

  /* ---------------------------------------------------------------- */
  /* Scroll affordance on long dialog bodies                            */
  /* ---------------------------------------------------------------- */
  function initScrollShadows() {
    const targets = document.querySelectorAll(
      ".settings-modal-body, .logs-modal-body, .workflow-modal-body, .copilot-panel-messages"
    );
    targets.forEach((el) => {
      el.classList.add("polish-scroll-shadow");
      const update = () => {
        const more = el.scrollHeight - el.clientHeight - el.scrollTop > 12;
        el.classList.toggle("is-scrollable", more);
      };
      el.addEventListener("scroll", update, { passive: true });
      new ResizeObserver(update).observe(el);
      new MutationObserver(update).observe(el, { childList: true, subtree: true });
      update();
    });
  }

  /* ---------------------------------------------------------------- */
  /* Scroll affordance on the toolbars                                 */
  /* ---------------------------------------------------------------- */
  /* The toolbars scroll sideways with the scrollbar hidden, which reads on a
     phone as a row that simply ends — the controls past the right edge are
     reachable by swiping, and nothing says so. Fade whichever edge still has
     something behind it. */
  function initToolbarScrollHints() {
    document.querySelectorAll(".appbar-menus").forEach((el) => {
      el.classList.add("polish-hscroll");
      const update = () => {
        const max = el.scrollWidth - el.clientWidth;
        el.classList.toggle("has-scroll-start", el.scrollLeft > 4);
        el.classList.toggle("has-scroll-end", max > 4 && el.scrollLeft < max - 4);
      };
      el.addEventListener("scroll", update, { passive: true });
      new ResizeObserver(update).observe(el);
      new MutationObserver(update).observe(el, { childList: true, subtree: true });
      update();
    });
  }

  function initPaletteTrigger() {
    document.querySelectorAll("[data-command-palette-open]").forEach((btn) => {
      btn.addEventListener("click", open);
    });
  }

  function init() {
    initPaletteTrigger();
    initCopilotResize();
    initKeyboardStateMirror();
    initToolCallStates();
    initScrollShadows();
    initToolbarScrollHints();
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }

  window.ThreeSeventyWeb = window.ThreeSeventyWeb || {};
  window.ThreeSeventyWeb.commandPalette = { open, close };
})();
