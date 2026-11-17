/**
 * Palette: UI/UX Enhancements
 * Handles generic UI interactions like loading states for icon buttons.
 */
(() => {
    // Track buttons currently in loading state to allow restoration
    const loadingButtons = new Set();

    // Apply loading state to submit buttons
    document.addEventListener('submit', (event) => {
        if (event.defaultPrevented) {
            return;
        }

        const submitter = event.submitter;
        if (!submitter) {
            return;
        }

        // Handle icon-only buttons
        if (submitter.classList.contains('icon-button')) {
            // Lock width to prevent layout shift
            const width = submitter.offsetWidth;
            if (width > 0) {
                submitter.style.width = `${width}px`;
            }

            // Store original state
            const originalContent = submitter.innerHTML;
            const originalLabel = submitter.getAttribute('aria-label');

            // Save restoration logic
            submitter._restoreState = () => {
                submitter.innerHTML = originalContent;
                if (originalLabel) {
                    submitter.setAttribute('aria-label', originalLabel);
                } else {
                    submitter.removeAttribute('aria-label');
                }
                submitter.removeAttribute('aria-busy');
                submitter.style.width = '';
                submitter.disabled = false;
                delete submitter._restoreState;
            };

            loadingButtons.add(submitter);

            // Replace content with spinner (no margin for icon buttons to keep it centered)
            // Note: The spinner CSS class is defined in style.css
            submitter.innerHTML = '<span class="spinner" aria-hidden="true" style="margin-right: 0"></span>';

            // Accessibility updates
            submitter.setAttribute('aria-label', 'Loading...');
            submitter.setAttribute('aria-busy', 'true');

            // Disable to prevent double submission
            submitter.disabled = true;
        }
        // Handle regular buttons
        else {
            // Store original state
            const originalContent = submitter.innerHTML;

            // Save restoration logic
            submitter._restoreState = () => {
                submitter.innerHTML = originalContent;
                submitter.removeAttribute('aria-busy');
                submitter.disabled = false;
                delete submitter._restoreState;
            };

            loadingButtons.add(submitter);

            // Prepend spinner
            submitter.innerHTML = '<span class="spinner" aria-hidden="true"></span>' + originalContent;

            submitter.setAttribute('aria-busy', 'true');
            submitter.disabled = true;
        }
    });

    // Restore state when page is shown (e.g. back button navigation/bfcache)
    window.addEventListener('pageshow', (event) => {
        // If the page is being restored from bfcache or just shown again
        loadingButtons.forEach(button => {
            if (typeof button._restoreState === 'function') {
                button._restoreState();
            }
        });
        loadingButtons.clear();
    });
})();

(() => {
    const modal = document.querySelector('[data-settings-modal]');
    if (!modal) {
        return;
    }

    // Focus, the Tab trap, the background scroll lock and the focus restore
    // all belong to pushModal/popModal — see modal-utils.js. This dialog used
    // to activate a focus trap without ever moving focus into itself, so the
    // trap listened on a dialog that never saw a Tab and the page behind the
    // backdrop stayed reachable.
    const openButton = document.querySelector('[data-settings-open]');
    const closeButtons = modal.querySelectorAll('[data-settings-close]');
    const refreshButton = modal.querySelector('[data-settings-refresh]');
    const maximizeButton = modal.querySelector('[data-settings-maximize]');
    const minimizeButton = modal.querySelector('[data-settings-minimize]');
    const form = modal.querySelector('[data-settings-form]');
    const tabsContainer = modal.querySelector('[data-settings-tabs]');
    const groupsContainer = modal.querySelector('[data-settings-groups]');
    const status = modal.querySelector('[data-settings-status]');
    const restartConfirmModal = modal.querySelector('[data-settings-restart-confirm]');
    const restartConfirmButton = modal.querySelector('[data-settings-restart-accept]');
    const restartCancelButtons = modal.querySelectorAll('[data-settings-restart-cancel]');
    const maximizedStorageKey = 'h3270SettingsModalMaximized';
    const extraOptionsPrefix = 'APP_SETTINGS_OPTIONS_';

    const defaults = {
        S3270_PORT: '23',
        S3270_PREFER_IPV4: 'false',
        S3270_PREFER_IPV6: 'false',
        S3270_CONNECT_TIMEOUT: '',
        S3270_PROXY: '',
        S3270_CALLBACK: '',
        S3270_SCRIPT_PORT: '',
        S3270_SCRIPT_PORT_ONCE: 'false',
        S3270_SOCKET: 'false',
        S3270_NO_VERIFY_CERT: 'false',
        S3270_TLS_MIN_PROTOCOL: '',
        S3270_TLS_MAX_PROTOCOL: '',
        S3270_CERT_FILE: '',
        S3270_CERT_FILE_TYPE: 'pem',
        S3270_KEY_FILE: '',
        S3270_KEY_FILE_TYPE: 'pem',
        S3270_KEY_PASSWORD: '',
        S3270_CA_FILE: '',
        S3270_CA_DIR: '',
        S3270_CHAIN_FILE: '',
        S3270_ACCEPT_HOSTNAME: '',
        S3270_CLIENT_CERT: '',
        S3270_MODEL: '3279-4-E',
        S3270_CODE_PAGE: 'bracket',
        S3270_TERMINAL_NAME: '',
        S3270_NVT: 'false',
        S3270_OVERSIZE: '',
        S3270_DEV_NAME: '',
        S3270_USER: '',
        S3270_EXEC_COMMAND: '',
        S3270_LOGIN_MACRO: '',
        S3270_HTTPD: '',
        S3270_MIN_VERSION: '',
        S3270_TRACE: 'false',
        S3270_TRACE_FILE: '',
        S3270_TRACE_FILE_SIZE: '',
        S3270_HELP: 'false',
        S3270_VERSION: 'false',
        S3270_UTENV: 'false',
        ALLOW_LOG_ACCESS: 'true',
        APP_USE_KEYPAD: 'false',
        CHAOS_MAX_STEPS: '100',
        CHAOS_TIME_BUDGET_SEC: '300',
        CHAOS_STEP_DELAY_SEC: '0.5',
        CHAOS_SEED: '0',
        CHAOS_MAX_FIELD_LENGTH: '40',
        CHAOS_SCREEN_DEDUP_SIMILARITY: '0.95',
        CHAOS_LEARNED_INPUT_REUSE_BIAS: '1.0',
        CHAOS_LEARNED_KEY_REUSE_BIAS: '1.0',
        CHAOS_EXPORT_SUCCESS_BALANCE: '1.0',
        CHAOS_OUTPUT_FILE: '',
        CHAOS_FORCE_OVERRIDE_EXISTING_INPUTS: 'true',
        CHAOS_EXCLUDE_NO_PROGRESS_EVENTS: 'true',
    };

    const modelOptions = [
        '3279-2-E',
        '3279-3-E',
        '3279-4-E',
        '3279-5-E',
        '3278-2',
        '3278-3',
        '3278-4',
        '3278-5',
        '3279-2',
        '3279-3',
        '3279-4',
        '3279-5',
    ];

    const codePageOptions = [
        { value: 'bracket', label: 'bracket - US bracket variant (default)' },
        { value: 'cp037', label: 'cp037 - US/Canada' },
        { value: 'cp273', label: 'cp273 - German' },
        { value: 'cp277', label: 'cp277 - Danish/Norwegian' },
        { value: 'cp278', label: 'cp278 - Finnish/Swedish' },
        { value: 'cp280', label: 'cp280 - Italian' },
        { value: 'cp284', label: 'cp284 - Spanish' },
        { value: 'cp285', label: 'cp285 - UK' },
        { value: 'cp297', label: 'cp297 - French' },
        { value: 'cp500', label: 'cp500 - International' },
        { value: 'cp870', label: 'cp870 - Multilingual Latin-2' },
        { value: 'cp871', label: 'cp871 - Icelandic' },
        { value: 'cp875', label: 'cp875 - Greek' },
        { value: 'cp1026', label: 'cp1026 - Turkish' },
        { value: 'cp1047', label: 'cp1047 - Open Systems Latin-1' },
        { value: 'cp1140', label: 'cp1140 - US/Canada Euro' },
        { value: 'cp1141', label: 'cp1141 - German Euro' },
        { value: 'cp1142', label: 'cp1142 - Danish/Norwegian Euro' },
        { value: 'cp1143', label: 'cp1143 - Finnish/Swedish Euro' },
        { value: 'cp1144', label: 'cp1144 - Italian Euro' },
        { value: 'cp1145', label: 'cp1145 - Spanish Euro' },
        { value: 'cp1146', label: 'cp1146 - UK Euro' },
        { value: 'cp1147', label: 'cp1147 - French Euro' },
        { value: 'cp1148', label: 'cp1148 - International Euro' },
        { value: 'cp1149', label: 'cp1149 - Icelandic Euro' },
    ];

    const tlsProtocolOptions = [
        'ssl3',
        'tls1.0',
        'tls1.1',
        'tls1.2',
        'tls1.3',
    ];

    const groups = [
        {
            id: 'app',
            title: 'App',
            description: 'Application-level behaviors.',
            sections: [
                { id: 'app_ui', title: 'Interface', fieldKeys: ['APP_USE_KEYPAD'] },
                { id: 'app_access', title: 'Access & Tools', fieldKeys: ['ALLOW_LOG_ACCESS'] },
            ],
            fields: [
                { key: 'ALLOW_LOG_ACCESS', label: 'Allow log access', type: 'checkbox', helper: 'Enable viewing log output in the UI.' },
                { key: 'APP_USE_KEYPAD', label: 'Use keypad', type: 'checkbox', helper: 'Show the virtual keypad by default.' },
            ],
        },
        {
            id: 'chaos',
            title: 'Exploration',
            description: 'Defaults for exploration runs (the CHAOS_* settings). The step and time limits can be changed for a single run in the start dialog.',
            sections: [
                { id: 'chaos_limits', title: 'Run Limits', fieldKeys: ['CHAOS_MAX_STEPS', 'CHAOS_TIME_BUDGET_SEC', 'CHAOS_STEP_DELAY_SEC', 'CHAOS_SEED'] },
                { id: 'chaos_explore', title: 'Exploration Behavior', fieldKeys: ['CHAOS_MAX_FIELD_LENGTH', 'CHAOS_FORCE_OVERRIDE_EXISTING_INPUTS', 'CHAOS_SCREEN_DEDUP_SIMILARITY'] },
                { id: 'chaos_learning', title: 'Learning & Hints', fieldKeys: ['CHAOS_LEARNED_INPUT_REUSE_BIAS', 'CHAOS_LEARNED_KEY_REUSE_BIAS'] },
                { id: 'chaos_history', title: 'Event History', fieldKeys: ['CHAOS_EXCLUDE_NO_PROGRESS_EVENTS'] },
                { id: 'chaos_export', title: 'Export', fieldKeys: ['CHAOS_EXPORT_SUCCESS_BALANCE', 'CHAOS_OUTPUT_FILE'] },
            ],
            fields: [
                { key: 'CHAOS_MAX_STEPS', label: 'Max steps', type: 'text', helper: 'Maximum number of AID key submissions before stopping (0 = unlimited).' },
                { key: 'CHAOS_TIME_BUDGET_SEC', label: 'Time budget (seconds)', type: 'text', helper: 'Maximum wall-clock seconds before stopping (0 = unlimited).' },
                { key: 'CHAOS_STEP_DELAY_SEC', label: 'Step delay (seconds)', type: 'text', helper: 'Pause between submissions in seconds (e.g. 0.5).' },
                { key: 'CHAOS_SEED', label: 'Seed', type: 'text', helper: 'Random seed (0 = use current time).' },
                { key: 'CHAOS_MAX_FIELD_LENGTH', label: 'Max field length', type: 'text', helper: 'Maximum characters generated per input field.' },
                { key: 'CHAOS_FORCE_OVERRIDE_EXISTING_INPUTS', label: 'Force override existing inputs', type: 'checkbox', helper: 'Overwrite prefilled input fields more aggressively to maximise exploration (clear trailing text and avoid reusing the same visible value).' },
                { key: 'CHAOS_LEARNED_INPUT_REUSE_BIAS', label: 'Learned input reuse bias', type: 'text', helper: 'Balance between exploration and reuse of learned working input values. Range 0..1 (0 = explore more, 1 = reuse learned inputs more often).' },
                { key: 'CHAOS_LEARNED_KEY_REUSE_BIAS', label: 'Learned key reuse bias', type: 'text', helper: 'Balance between exploration and reuse of learned working keys (Enter/PF/etc.). Range 0..1 (0 = explore more keys, 1 = reuse learned keys more often).' },
                { key: 'CHAOS_SCREEN_DEDUP_SIMILARITY', label: 'Screen dedup similarity', type: 'text', helper: 'Similarity threshold (0-1] for merging near-duplicate screens in the screen map. Higher = stricter (0.995 keeps more separate screens), lower = more merging (0.95 merges value-echo variants more aggressively). Default 0.95 is a balanced starting point.' },
                { key: 'CHAOS_EXCLUDE_NO_PROGRESS_EVENTS', label: 'Exclude no-progress events', type: 'checkbox', helper: 'Leave steps that changed nothing on screen out of the exploration event history and step details.' },
                { key: 'CHAOS_EXPORT_SUCCESS_BALANCE', label: 'Export success balance', type: 'text', helper: 'Workflow export balance (0-1]. 1 exports only successful navigation steps. Lower values add sampled unsuccessful attempts as safe CheckValue steps while preserving the successful path.' },
                { key: 'CHAOS_OUTPUT_FILE', label: 'Output file', type: 'text', helper: 'Path to save the learned workflow JSON on stop (leave empty to skip).' },
            ],
        },
        {
            id: 'connectivity',
            title: 'Connectivity',
            description: 'Network address and connection options.',
            sections: [
                { id: 'conn_endpoint', title: 'Host Endpoint', fieldKeys: ['S3270_PORT', 'S3270_CONNECT_TIMEOUT'] },
                { id: 'conn_network', title: 'Network Family', fieldKeys: ['S3270_PREFER_IPV4', 'S3270_PREFER_IPV6'] },
                { id: 'conn_proxy', title: 'Proxy', fieldKeys: ['S3270_PROXY'] },
                { id: 'conn_callbacks', title: 'Protocol Sessions', fieldKeys: ['S3270_CALLBACK', 'S3270_SCRIPT_PORT', 'S3270_SCRIPT_PORT_ONCE', 'S3270_SOCKET'] },
            ],
            fields: [
                { key: 'S3270_PORT', label: 'Port', type: 'text', helper: 'TCP port used when connecting to a host.' },
                { key: 'S3270_PREFER_IPV4', label: 'Prefer IPv4', type: 'checkbox', helper: 'Force IPv4 when multiple addresses are available.' },
                { key: 'S3270_PREFER_IPV6', label: 'Prefer IPv6', type: 'checkbox', helper: 'Force IPv6 when multiple addresses are available.' },
                { key: 'S3270_CONNECT_TIMEOUT', label: 'Connect timeout', type: 'text', helper: 'Timeout before giving up on a host connection.' },
                { key: 'S3270_PROXY', label: 'Proxy', type: 'text', helper: 'Proxy type and server to use.' },
                { key: 'S3270_CALLBACK', label: 'Callback port', type: 'text', helper: 'Connect back for an s3270 protocol session.' },
                { key: 'S3270_SCRIPT_PORT', label: 'Script port', type: 'text', helper: 'Accept TCP connections for s3270 protocol sessions.' },
                { key: 'S3270_SCRIPT_PORT_ONCE', label: 'Script port once', type: 'checkbox', helper: 'Accept only one -scriptport session.' },
                { key: 'S3270_SOCKET', label: 'Unix socket', type: 'checkbox', helper: 'Accept protocol sessions on the Unix-domain socket.' },
            ],
        },
        {
            id: 'tls',
            title: 'TLS/Security',
            description: 'Certificates, verification, and TLS settings.',
            sections: [
                { id: 'tls_verify', title: 'Verification', fieldKeys: ['S3270_NO_VERIFY_CERT', 'S3270_ACCEPT_HOSTNAME'] },
                { id: 'tls_protocol', title: 'Protocol Versions', fieldKeys: ['S3270_TLS_MIN_PROTOCOL', 'S3270_TLS_MAX_PROTOCOL'] },
                { id: 'tls_client', title: 'Client Credentials', fieldKeys: ['S3270_CERT_FILE', 'S3270_CERT_FILE_TYPE', 'S3270_KEY_FILE', 'S3270_KEY_FILE_TYPE', 'S3270_KEY_PASSWORD', 'S3270_CLIENT_CERT'] },
                { id: 'tls_trust', title: 'Trust Store', fieldKeys: ['S3270_CA_FILE', 'S3270_CA_DIR', 'S3270_CHAIN_FILE'] },
            ],
            fields: [
                { key: 'S3270_NO_VERIFY_CERT', label: 'Disable cert verification', type: 'checkbox', helper: 'Do not verify the TLS host certificate.' },
                { key: 'S3270_TLS_MIN_PROTOCOL', label: 'TLS min protocol', type: 'select', options: tlsProtocolOptions, allowEmpty: true, helper: 'Lowest TLS protocol to allow.' },
                { key: 'S3270_TLS_MAX_PROTOCOL', label: 'TLS max protocol', type: 'select', options: tlsProtocolOptions, allowEmpty: true, helper: 'Highest TLS protocol to allow.' },
                { key: 'S3270_CERT_FILE', label: 'Client cert file', type: 'text', helper: 'Path to the TLS client certificate file.' },
                { key: 'S3270_CERT_FILE_TYPE', label: 'Cert file type', type: 'select', options: ['pem', 'asn1'], helper: 'Type of client certificate file.' },
                { key: 'S3270_KEY_FILE', label: 'Key file', type: 'text', helper: 'Path to the TLS client key file.' },
                { key: 'S3270_KEY_FILE_TYPE', label: 'Key file type', type: 'select', options: ['pem', 'asn1'], helper: 'Type of client key file.' },
                { key: 'S3270_KEY_PASSWORD', label: 'Key password', type: 'password', helper: 'Password for the key or certificate file.' },
                { key: 'S3270_CA_FILE', label: 'CA file', type: 'text', helper: 'File containing CA root certificate for TLS.' },
                { key: 'S3270_CA_DIR', label: 'CA directory', type: 'text', helper: 'Directory containing CA root certificates.' },
                { key: 'S3270_CHAIN_FILE', label: 'Chain file', type: 'text', helper: 'File containing chain of CA certificates.' },
                { key: 'S3270_ACCEPT_HOSTNAME', label: 'Accept hostname', type: 'text', helper: 'Name to match in the host TLS certificate.' },
                { key: 'S3270_CLIENT_CERT', label: 'Client cert name', type: 'text', helper: 'Name of the client certificate for TLS.' },
            ],
        },
        {
            id: 'emulation',
            title: 'Emulation',
            description: 'Terminal model, code page, and emulation flags.',
            sections: [
                { id: 'emu_terminal', title: 'Terminal Profile', fieldKeys: ['S3270_MODEL', 'S3270_CODE_PAGE', 'S3270_TERMINAL_NAME'] },
                { id: 'emu_display', title: 'Display Behavior', fieldKeys: ['S3270_NVT', 'S3270_OVERSIZE'] },
                { id: 'emu_identity', title: 'Host Identity Fields', fieldKeys: ['S3270_DEV_NAME', 'S3270_USER'] },
            ],
            fields: [
                { key: 'S3270_MODEL', label: 'Model', type: 'select', options: modelOptions, helper: 'Model of 3270 to emulate.' },
                {
                    key: 'S3270_CODE_PAGE',
                    label: 'Code page',
                    type: 'select',
                    options: codePageOptions,
                    helper: 'Host EBCDIC code page. See the s3270 documentation for the full list.',
                    helperLink: 'https://x3270.miraheze.org/wiki/Host_code_page'
                },
                { key: 'S3270_TERMINAL_NAME', label: 'Terminal name', type: 'text', helper: 'Override the terminal name reported to the host.' },
                { key: 'S3270_NVT', label: 'Force NVT mode', type: 'checkbox', helper: 'Do not negotiate 3270 mode.' },
                { key: 'S3270_OVERSIZE', label: 'Oversize', type: 'text', helper: 'Make the display larger than the default model.' },
                { key: 'S3270_DEV_NAME', label: 'Device name', type: 'text', helper: 'Workstation ID response to TELNET NEW-ENVIRON.' },
                { key: 'S3270_USER', label: 'User', type: 'text', helper: 'User name for TELNET NEW-ENVIRON.' },
            ],
        },
    ];

    const fieldMap = new Map();
    const fieldMetaMap = new Map();
    const fieldGroupMap = new Map();
    const fieldBaseOptionsMap = new Map();
    const fieldExtraOptionsMap = new Map();
    const fieldExtrasListMap = new Map();
    // Settings the server will not write, named by GET /api/settings. The form
    // posts every field it renders, so one key the server refuses fails the
    // whole save — including the fields the operator did change, on a tab they
    // may never have opened. These are shown as the fixed values they are and
    // left out of the payload.
    const readOnlyKeys = new Set();
    let activeGroupId = '';
    let maximized = false;
    let restartConfirmResolver = null;
    let chaosDefaultsHintsList = null;
    let chaosDefaultsKeyBlacklistInput = null;
    let chaosDefaultsStatus = null;
    let chaosDefaultsHintRowSequence = 0;
    let chaosDefaultsSubModal = null;
    let previousChaosDefaultsFocus = null;

    const setStatus = (message, isError = false) => {
        if (!status) {
            return;
        }
        if (!message) {
            status.textContent = '';
            status.classList.remove('is-visible', 'is-error');
            return;
        }
        status.textContent = message;
        status.classList.add('is-visible');
        status.classList.toggle('is-error', isError);
    };

    const setChaosDefaultsStatus = (message, isError = false) => {
        if (!chaosDefaultsStatus) {
            return;
        }
        chaosDefaultsStatus.textContent = message || '';
        chaosDefaultsStatus.classList.toggle('is-error', !!isError);
    };

    const closeChaosDefaultsSubModal = () => {
        if (!chaosDefaultsSubModal || chaosDefaultsSubModal.hidden) {
            return;
        }
        chaosDefaultsSubModal.hidden = true;
        if (window.ThreeSeventyWeb && window.ThreeSeventyWeb.popModal) {
            window.ThreeSeventyWeb.popModal(chaosDefaultsSubModal);
        }
        if (previousChaosDefaultsFocus && typeof previousChaosDefaultsFocus.focus === 'function') {
            previousChaosDefaultsFocus.focus();
        }
        previousChaosDefaultsFocus = null;
    };

    const openChaosDefaultsSubModal = () => {
        if (!chaosDefaultsSubModal) {
            return;
        }
        previousChaosDefaultsFocus = document.activeElement;
        chaosDefaultsSubModal.hidden = false;
        if (window.ThreeSeventyWeb && window.ThreeSeventyWeb.pushModal) {
            window.ThreeSeventyWeb.pushModal(chaosDefaultsSubModal, closeChaosDefaultsSubModal);
        }
        const focusTarget = chaosDefaultsSubModal.querySelector('[data-settings-chaos-hint-transaction], [data-settings-chaos-defaults-add-hint], [data-settings-chaos-defaults-load]');
        if (focusTarget && typeof focusTarget.focus === 'function') {
            focusTarget.focus();
        }
    };

    const parseChaosDefaultsListLines = (value) => {
        if (!value) {
            return [];
        }
        const seen = new Set();
        const out = [];
        String(value)
            .split(/[\n,]/)
            .map((item) => item.trim())
            .filter((item) => item.length > 0)
            .forEach((item) => {
                if (seen.has(item)) {
                    return;
                }
                seen.add(item);
                out.push(item);
            });
        return out;
    };

    const formatChaosDefaultsListLines = (values) => (
        Array.isArray(values)
            ? values.map((v) => String(v || '').trim()).filter(Boolean).join('\n')
            : ''
    );

    const parseChaosDefaultsKeyAssignments = (value) => {
        if (!value) {
            return {};
        }
        const out = {};
        String(value)
            .split(/\n+/)
            .map((line) => line.trim())
            .filter((line) => line.length > 0)
            .forEach((line) => {
                const sepIndex = line.indexOf('=>') >= 0
                    ? line.indexOf('=>')
                    : (line.indexOf('=') >= 0 ? line.indexOf('=') : line.indexOf(':'));
                if (sepIndex < 0) {
                    return;
                }
                const sepLen = line.slice(sepIndex, sepIndex + 2) === '=>' ? 2 : 1;
                const label = line.slice(0, sepIndex).trim();
                const key = line.slice(sepIndex + sepLen).trim();
                if (!label || !key) {
                    return;
                }
                out[label] = key;
            });
        return out;
    };

    const normalizeChaosDefaultsKeyAssignments = (raw) => {
        if (!raw || typeof raw !== 'object') {
            return {};
        }
        const out = {};
        Object.entries(raw).forEach(([rawLabel, rawKey]) => {
            const label = String(rawLabel || '').trim();
            const key = String(rawKey || '').trim();
            if (!label || !key) {
                return;
            }
            out[label] = key;
        });
        return out;
    };

    const formatChaosDefaultsKeyAssignments = (raw) => {
        const assignments = normalizeChaosDefaultsKeyAssignments(raw);
        return Object.keys(assignments)
            .sort((a, b) => a.localeCompare(b))
            .map((label) => `${label} = ${assignments[label]}`)
            .join('\n');
    };

    const normalizeChaosDefaultsPayloadForSettings = (raw) => {
        if (Array.isArray(raw)) {
            return { hints: raw, keyBlacklist: [] };
        }
        if (!raw || typeof raw !== 'object') {
            return { hints: [], keyBlacklist: [] };
        }
        return {
            hints: Array.isArray(raw.hints) ? raw.hints : [],
            keyBlacklist: Array.isArray(raw.keyBlacklist) ? raw.keyBlacklist : [],
        };
    };

    const normalizeChaosDefaultsHints = (rawHints) => {
        if (!Array.isArray(rawHints)) {
            return [];
        }
        const normalized = [];
        rawHints.forEach((entry) => {
            if (!entry || typeof entry !== 'object') {
                return;
            }
            const transaction = String(entry.transaction || '').trim();
            const knownData = Array.isArray(entry.knownData)
                ? entry.knownData.map((item) => String(item || '').trim()).filter((item) => item.length > 0)
                : parseChaosDefaultsListLines(entry.knownData || '');
            const keyAssignments = normalizeChaosDefaultsKeyAssignments(entry.keyAssignments || {});
            if (!transaction && knownData.length === 0 && Object.keys(keyAssignments).length === 0) {
                return;
            }
            normalized.push({ transaction, knownData, keyAssignments });
        });
        return normalized;
    };

    const createChaosDefaultsHintRow = (hint = {}) => {
        if (!chaosDefaultsHintsList) {
            return;
        }
        const row = document.createElement('div');
        row.className = 'chaos-hint-row';

        const rowID = ++chaosDefaultsHintRowSequence;

        const txField = document.createElement('div');
        txField.className = 'chaos-hint-field';
        const txLabel = document.createElement('label');
        txLabel.className = 'chaos-hint-field-label';
        txLabel.textContent = 'Transaction';
        const txInput = document.createElement('input');
        txInput.type = 'text';
        txInput.id = `settings-chaos-hint-transaction-${rowID}`;
        txInput.placeholder = 'Transaction (e.g., CEMT)';
        txInput.value = hint.transaction || '';
        txInput.dataset.settingsChaosHintTransaction = '1';
        txInput.addEventListener('input', () => setChaosDefaultsStatus('Unsaved changes to the default hints.'));
        txLabel.setAttribute('for', txInput.id);
        txField.appendChild(txLabel);
        txField.appendChild(txInput);

        const dataField = document.createElement('div');
        dataField.className = 'chaos-hint-field';
        const dataLabel = document.createElement('label');
        dataLabel.className = 'chaos-hint-field-label';
        dataLabel.textContent = 'Known Working Data';
        const dataInput = document.createElement('textarea');
        dataInput.id = `settings-chaos-hint-data-${rowID}`;
        dataInput.placeholder = 'Known data values (comma or newline separated)';
        dataInput.value = Array.isArray(hint.knownData) ? hint.knownData.join('\n') : '';
        dataInput.dataset.settingsChaosHintData = '1';
        dataInput.addEventListener('input', () => setChaosDefaultsStatus('Unsaved changes to the default hints.'));
        dataLabel.setAttribute('for', dataInput.id);
        dataField.appendChild(dataLabel);
        dataField.appendChild(dataInput);

        const keysField = document.createElement('div');
        keysField.className = 'chaos-hint-field';
        const keysLabel = document.createElement('label');
        keysLabel.className = 'chaos-hint-field-label';
        keysLabel.textContent = 'Key Assignments';
        const keysInput = document.createElement('textarea');
        keysInput.id = `settings-chaos-hint-keys-${rowID}`;
        keysInput.placeholder = 'Label = Key (one per line)\nReturn = PF3\nConfirm = Enter';
        keysInput.value = formatChaosDefaultsKeyAssignments(hint.keyAssignments || {});
        keysInput.dataset.settingsChaosHintKeyAssignments = '1';
        keysInput.addEventListener('input', () => setChaosDefaultsStatus('Unsaved changes to the default hints.'));
        keysLabel.setAttribute('for', keysInput.id);
        keysField.appendChild(keysLabel);
        keysField.appendChild(keysInput);

        const removeBtn = document.createElement('button');
        removeBtn.type = 'button';
        removeBtn.className = 'chaos-hint-remove';
        removeBtn.textContent = 'Remove';
        removeBtn.addEventListener('click', () => {
            row.remove();
            if (!chaosDefaultsHintsList.children.length) {
                createChaosDefaultsHintRow();
            }
            setChaosDefaultsStatus('Unsaved changes to the default hints.');
        });

        row.appendChild(txField);
        row.appendChild(dataField);
        row.appendChild(keysField);
        row.appendChild(removeBtn);
        chaosDefaultsHintsList.appendChild(row);
    };

    const renderChaosDefaultsHintsRows = (hints) => {
        if (!chaosDefaultsHintsList) {
            return;
        }
        chaosDefaultsHintsList.innerHTML = '';
        const list = normalizeChaosDefaultsHints(hints);
        if (!list.length) {
            createChaosDefaultsHintRow();
            return;
        }
        list.forEach((hint) => createChaosDefaultsHintRow(hint));
    };

    const collectChaosDefaultsHintsFromUI = () => {
        if (!chaosDefaultsHintsList) {
            return [];
        }
        const rows = Array.from(chaosDefaultsHintsList.querySelectorAll('.chaos-hint-row'));
        return normalizeChaosDefaultsHints(rows.map((row) => {
            const tx = row.querySelector('[data-settings-chaos-hint-transaction]');
            const knownData = row.querySelector('[data-settings-chaos-hint-data]');
            const keyAssignments = row.querySelector('[data-settings-chaos-hint-key-assignments]');
            return {
                transaction: tx ? tx.value : '',
                knownData: knownData ? parseChaosDefaultsListLines(knownData.value) : [],
                keyAssignments: keyAssignments ? parseChaosDefaultsKeyAssignments(keyAssignments.value) : {},
            };
        }));
    };

    const renderChaosDefaultsEditorPayload = (payload) => {
        const normalized = normalizeChaosDefaultsPayloadForSettings(payload);
        renderChaosDefaultsHintsRows(normalized.hints);
        if (chaosDefaultsKeyBlacklistInput) {
            chaosDefaultsKeyBlacklistInput.value = formatChaosDefaultsListLines(normalized.keyBlacklist);
        }
    };

    const collectChaosDefaultsEditorPayload = () => ({
        hints: collectChaosDefaultsHintsFromUI(),
        keyBlacklist: chaosDefaultsKeyBlacklistInput ? parseChaosDefaultsListLines(chaosDefaultsKeyBlacklistInput.value) : [],
    });

    const markChaosDefaultsDirty = () => {
        setChaosDefaultsStatus('Unsaved changes to the default hints.');
    };

    const loadChaosDefaultsFromServer = async () => {
        if (!chaosDefaultsHintsList) {
            return;
        }
        setChaosDefaultsStatus('Loading the default hints...');
        try {
            const response = await fetch('/chaos/hints');
            let payload = {};
            try {
                payload = await response.json();
            } catch (_parseErr) {
                payload = {};
            }
            if (!response.ok) {
                throw new Error(payload.error || 'Failed to load the default hints.');
            }
            renderChaosDefaultsEditorPayload(payload);
            setChaosDefaultsStatus('Default hints loaded.');
        } catch (error) {
            setChaosDefaultsStatus((error && error.message) ? error.message : 'Failed to load the default hints.', true);
        }
    };

    const saveChaosDefaultsToServer = async () => {
        if (!chaosDefaultsHintsList) {
            return;
        }
        const payload = collectChaosDefaultsEditorPayload();
        setChaosDefaultsStatus('Saving the default hints...');
        try {
            const response = await fetch('/chaos/hints', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify(payload),
            });
            let data = {};
            try {
                data = await response.json();
            } catch (_parseErr) {
                data = {};
            }
            if (!response.ok) {
                throw new Error(data.error || 'Failed to save the default hints.');
            }
            renderChaosDefaultsEditorPayload(data);
            setChaosDefaultsStatus('Default hints saved.');
        } catch (error) {
            setChaosDefaultsStatus((error && error.message) ? error.message : 'Failed to save the default hints.', true);
        }
    };

    const buildChaosDefaultsSettingsField = () => {
        const wrapper = document.createElement('div');
        wrapper.className = 'settings-field';
        wrapper.dataset.settingsChaosDefaults = '1';

        const title = document.createElement('label');
        title.textContent = 'Default Hints';
        wrapper.appendChild(title);

        const help = document.createElement('div');
        help.className = 'settings-helper';
        help.textContent = 'Saved separately from .env settings via /chaos/hints. Explorations use these defaults when a request brings no hints or blocked keys of its own.';
        wrapper.appendChild(help);

        const openBtn = document.createElement('button');
        openBtn.type = 'button';
        openBtn.textContent = 'Open Default Hints Editor';
        openBtn.addEventListener('click', () => {
            openChaosDefaultsSubModal();
        });
        wrapper.appendChild(openBtn);

        const summary = document.createElement('div');
        summary.className = 'settings-helper';
        summary.textContent = 'Opens a dedicated editor for default hints and blocked keys.';
        wrapper.appendChild(summary);

        const subModal = document.createElement('div');
        subModal.className = 'modal-backdrop';
        subModal.hidden = true;
        subModal.dataset.settingsChaosDefaultsModal = '1';

        const dialog = document.createElement('div');
        dialog.className = 'modal modal-chaos-hints';
        dialog.setAttribute('role', 'dialog');
        dialog.setAttribute('aria-modal', 'true');
        dialog.setAttribute('aria-labelledby', 'settings-chaos-defaults-modal-title');
        dialog.tabIndex = -1;

        const header = document.createElement('div');
        header.className = 'modal-header';
        const h3 = document.createElement('h3');
        h3.id = 'settings-chaos-defaults-modal-title';
        h3.textContent = 'Default exploration hints';
        const headerClose = document.createElement('button');
        headerClose.type = 'button';
        headerClose.className = 'modal-close';
        headerClose.textContent = 'Close';
        headerClose.dataset.settingsChaosDefaultsClose = '1';
        header.appendChild(h3);
        header.appendChild(headerClose);
        dialog.appendChild(header);

        const stack = document.createElement('div');
        stack.className = 'stack';

        const modalHelp = document.createElement('p');
        modalHelp.className = 'subtle chaos-hints-help';
        modalHelp.textContent = 'Edit the saved default hints and blocked keys. An exploration uses these when it is started without its own.';
        stack.appendChild(modalHelp);

        const blockedField = document.createElement('div');
        blockedField.className = 'chaos-hint-field';
        const blockedLabel = document.createElement('label');
        blockedLabel.className = 'chaos-hint-field-label';
        blockedLabel.htmlFor = 'settings-chaos-key-blacklist';
        blockedLabel.textContent = 'Blocked Keys';
        const blockedInput = document.createElement('textarea');
        blockedInput.id = 'settings-chaos-key-blacklist';
        blockedInput.rows = 6;
        blockedInput.placeholder = 'Keys exploration must never press (one per line)\nPF3\nPF12';
        blockedInput.addEventListener('input', markChaosDefaultsDirty);
        chaosDefaultsKeyBlacklistInput = blockedInput;
        blockedField.appendChild(blockedLabel);
        blockedField.appendChild(blockedInput);
        stack.appendChild(blockedField);

        const hintsList = document.createElement('div');
        hintsList.className = 'chaos-hints-list';
        hintsList.dataset.settingsChaosHintsList = '1';
        chaosDefaultsHintsList = hintsList;
        stack.appendChild(hintsList);

        const rowActions = document.createElement('div');
        rowActions.className = 'modal-actions';
        const addHintBtn = document.createElement('button');
        addHintBtn.type = 'button';
        addHintBtn.textContent = 'Add Hint';
        addHintBtn.dataset.settingsChaosDefaultsAddHint = '1';
        addHintBtn.addEventListener('click', () => {
            createChaosDefaultsHintRow();
            markChaosDefaultsDirty();
        });
        rowActions.appendChild(addHintBtn);
        stack.appendChild(rowActions);

        const subStatus = document.createElement('div');
        subStatus.className = 'settings-error';
        stack.appendChild(subStatus);
        dialog.appendChild(stack);

        const actions = document.createElement('div');
        actions.className = 'modal-actions';

        const loadBtn = document.createElement('button');
        loadBtn.type = 'button';
        loadBtn.textContent = 'Load default hints';
        loadBtn.dataset.settingsChaosDefaultsLoad = '1';
        loadBtn.addEventListener('click', () => {
            loadChaosDefaultsFromServer();
        });
        actions.appendChild(loadBtn);

        const saveBtn = document.createElement('button');
        saveBtn.type = 'button';
        saveBtn.textContent = 'Save default hints';
        saveBtn.addEventListener('click', () => {
            saveChaosDefaultsToServer();
        });
        actions.appendChild(saveBtn);

        const footerClose = document.createElement('button');
        footerClose.type = 'button';
        footerClose.textContent = 'Close';
        footerClose.dataset.settingsChaosDefaultsClose = '1';
        actions.appendChild(footerClose);
        dialog.appendChild(actions);

        subModal.appendChild(dialog);
        wrapper.appendChild(subModal);

        chaosDefaultsSubModal = subModal;
        chaosDefaultsStatus = subStatus;

        subModal.addEventListener('click', (event) => {
            if (event.target === subModal) {
                closeChaosDefaultsSubModal();
            }
        });
        subModal.querySelectorAll('[data-settings-chaos-defaults-close]').forEach((btn) => {
            btn.addEventListener('click', () => {
                closeChaosDefaultsSubModal();
            });
        });

        renderChaosDefaultsEditorPayload({ hints: [], keyBlacklist: [] });
        return wrapper;
    };

    const setMaximizeUi = (enabled) => {
        maximized = enabled;
        modal.classList.toggle('is-maximized', enabled);
        if (maximizeButton) {
            maximizeButton.hidden = enabled;
            maximizeButton.setAttribute('aria-expanded', enabled ? 'true' : 'false');
        }
        if (minimizeButton) {
            minimizeButton.hidden = !enabled;
            minimizeButton.setAttribute('aria-expanded', enabled ? 'true' : 'false');
        }
    };

    const ensureSettingsTooltips = () => {
        if (!window.tippy || !modal) {
            return;
        }
        const tooltipTargets = modal.querySelectorAll('[data-tippy-content]');
        for (let i = 0; i < tooltipTargets.length; i += 1) {
            const target = tooltipTargets[i];
            if (target.hasAttribute('title')) {
                target.removeAttribute('title');
            }
            if (target._tippy) {
                continue;
            }
            window.tippy(target, {
                delay: [150, 0],
                placement: 'bottom',
                trigger: 'mouseenter',
            });
        }
    };

    const setActiveGroup = (groupId) => {
        activeGroupId = groupId || '';
        const tabButtons = tabsContainer ? tabsContainer.querySelectorAll('[data-settings-tab]') : [];
        for (let i = 0; i < tabButtons.length; i += 1) {
            const tab = tabButtons[i];
            const isActive = tab.dataset.groupId === activeGroupId;
            tab.classList.toggle('is-active', isActive);
            tab.setAttribute('aria-selected', isActive ? 'true' : 'false');
            tab.setAttribute('tabindex', isActive ? '0' : '-1');
        }
        const panels = groupsContainer ? groupsContainer.querySelectorAll('[data-settings-group]') : [];
        for (let i = 0; i < panels.length; i += 1) {
            const panel = panels[i];
            panel.classList.toggle('is-active', panel.dataset.groupId === activeGroupId);
        }
    };

    const openSettingsModal = () => {
        modal.hidden = false;
        if (window.ThreeSeventyWeb && window.ThreeSeventyWeb.pushModal) {
            window.ThreeSeventyWeb.pushModal(modal, closeSettingsModal);
        }
        ensureSettingsTooltips();
        if (typeof loadSettings === 'function') {
            loadSettings();
        }
    };

    const closeSettingsModal = () => {
        closeChaosDefaultsSubModal();
        closeRestartConfirm(false);
        modal.hidden = true;
        if (window.ThreeSeventyWeb && window.ThreeSeventyWeb.popModal) {
            window.ThreeSeventyWeb.popModal(modal);
        }
        setStatus('');
    };

    const closeRestartConfirm = (confirmed) => {
        if (!restartConfirmModal || restartConfirmModal.hidden) {
            if (restartConfirmResolver) {
                const resolve = restartConfirmResolver;
                restartConfirmResolver = null;
                resolve(!!confirmed);
            }
            return;
        }
        restartConfirmModal.hidden = true;
        if (window.ThreeSeventyWeb && window.ThreeSeventyWeb.popModal) {
            window.ThreeSeventyWeb.popModal(restartConfirmModal);
        }
        if (restartConfirmResolver) {
            const resolve = restartConfirmResolver;
            restartConfirmResolver = null;
            resolve(!!confirmed);
        }
    };

    const askRestartConfirmation = () => {
        if (!restartConfirmModal || !restartConfirmButton) {
            return Promise.resolve(false);
        }
        if (restartConfirmResolver) {
            closeRestartConfirm(false);
        }
        restartConfirmModal.hidden = false;
        if (window.ThreeSeventyWeb && window.ThreeSeventyWeb.pushModal) {
            window.ThreeSeventyWeb.pushModal(restartConfirmModal, () => closeRestartConfirm(false),
                { initialFocus: restartConfirmButton });
        } else {
            restartConfirmButton.focus();
        }
        return new Promise((resolve) => {
            restartConfirmResolver = resolve;
        });
    };

    if (openButton) {
        openButton.addEventListener('click', openSettingsModal);
    }

    closeButtons.forEach((button) => {
        button.addEventListener('click', closeSettingsModal);
    });

    const setFieldValue = (field, value) => {
        if (!field) {
            return;
        }
        if (field.dataset.kind === 'checkbox') {
            field.checked = String(value).toLowerCase() === 'true';
        } else {
            field.value = value ?? '';
        }
    };

    const normalizeOptionEntry = (option) => {
        if (typeof option === 'string') {
            return { value: option, label: option };
        }
        if (option && typeof option === 'object') {
            return {
                value: String(option.value ?? ''),
                label: String(option.label ?? option.value ?? ''),
            };
        }
        return { value: String(option ?? ''), label: String(option ?? '') };
    };

    const appendSelectOption = (select, value, label) => {
        if (!select) {
            return;
        }
        const optionEl = document.createElement('option');
        optionEl.value = value;
        optionEl.textContent = label || value;
        select.appendChild(optionEl);
    };

    const hasSelectOption = (select, value) => {
        if (!select) {
            return false;
        }
        return Array.from(select.options).some((option) => option.value === value);
    };

    const ensureSelectOption = (select, value, label) => {
        if (!select || !value) {
            return;
        }
        if (!hasSelectOption(select, value)) {
            appendSelectOption(select, value, label || value);
        }
    };

    const extraOptionsSettingKey = (fieldKey) => `${extraOptionsPrefix}${fieldKey}`;

    const parseExtraOptions = (rawValue) => {
        if (!rawValue) {
            return [];
        }
        const parts = String(rawValue)
            .split(',')
            .map((part) => part.trim())
            .filter((part) => part.length > 0);
        return Array.from(new Set(parts));
    };

    const serializeExtraOptions = (values) => {
        if (!Array.isArray(values) || values.length === 0) {
            return '';
        }
        return values
            .map((value) => String(value || '').trim())
            .filter((value) => value.length > 0)
            .filter((value, index, list) => list.indexOf(value) === index)
            .join(', ');
    };

    const addExtraOptionForField = (fieldKey, value) => {
        const entry = fieldMap.get(fieldKey);
        if (!entry || !entry.input || entry.input.tagName !== 'SELECT') {
            return;
        }
        const trimmed = String(value || '').trim();
        if (!trimmed) {
            return;
        }
        const baseSet = fieldBaseOptionsMap.get(fieldKey) || new Set();
        if (!baseSet.has(trimmed)) {
            const extras = fieldExtraOptionsMap.get(fieldKey) || [];
            if (!extras.includes(trimmed)) {
                extras.push(trimmed);
                fieldExtraOptionsMap.set(fieldKey, extras);
            }
        }
        ensureSelectOption(entry.input, trimmed, trimmed);
        entry.input.value = trimmed;
        renderExtraOptionsForField(fieldKey);
    };

    const removeExtraOptionForField = (fieldKey, value) => {
        const entry = fieldMap.get(fieldKey);
        if (!entry || !entry.input || entry.input.tagName !== 'SELECT') {
            return;
        }
        const trimmed = String(value || '').trim();
        if (!trimmed) {
            return;
        }
        const extras = (fieldExtraOptionsMap.get(fieldKey) || []).filter((item) => item !== trimmed);
        fieldExtraOptionsMap.set(fieldKey, extras);

        const baseSet = fieldBaseOptionsMap.get(fieldKey) || new Set();
        if (!baseSet.has(trimmed) && !extras.includes(trimmed)) {
            const options = Array.from(entry.input.options);
            const option = options.find((item) => item.value === trimmed);
            if (option) {
                option.remove();
            }
        }

        if (entry.input.value === trimmed) {
            const field = fieldMetaMap.get(fieldKey);
            if (field && field.allowEmpty) {
                entry.input.value = '';
            } else if (entry.input.options.length > 0) {
                entry.input.value = entry.input.options[0].value;
            } else {
                entry.input.value = '';
            }
        }

        renderExtraOptionsForField(fieldKey);
    };

    const renderExtraOptionsForField = (fieldKey) => {
        const container = fieldExtrasListMap.get(fieldKey);
        if (!container) {
            return;
        }
        container.innerHTML = '';
        const extras = fieldExtraOptionsMap.get(fieldKey) || [];
        if (extras.length === 0) {
            container.hidden = true;
            return;
        }
        container.hidden = false;
        extras.forEach((value) => {
            const chip = document.createElement('span');
            chip.className = 'settings-extra-chip';
            const label = document.createElement('span');
            label.className = 'settings-extra-chip-label';
            label.textContent = value;
            const removeBtn = document.createElement('button');
            removeBtn.type = 'button';
            removeBtn.className = 'settings-extra-chip-remove';
            removeBtn.setAttribute('aria-label', `Remove custom option ${value}`);
            removeBtn.textContent = 'Remove';
            removeBtn.addEventListener('click', () => {
                removeExtraOptionForField(fieldKey, value);
                const field = fieldMetaMap.get(fieldKey);
                const fieldLabel = field ? field.label : fieldKey;
                setStatus(`Removed custom option from ${fieldLabel}.`);
            });
            chip.appendChild(label);
            chip.appendChild(removeBtn);
            container.appendChild(chip);
        });
    };

    const buildHelper = (field) => {
        if (!field.helper) {
            return null;
        }
        const helper = document.createElement('div');
        helper.className = 'settings-helper';
        if (field.helperLink) {
            const link = document.createElement('a');
            link.href = field.helperLink;
            link.textContent = 'Docs';
            link.target = '_blank';
            link.rel = 'noopener';
            helper.textContent = field.helper + ' ';
            helper.appendChild(link);
        } else {
            helper.textContent = field.helper;
        }
        return helper;
    };

    const buildSettingsSubgroup = (title, description = '') => {
        const fieldset = document.createElement('fieldset');
        fieldset.className = 'settings-subgroup';

        const legend = document.createElement('legend');
        legend.className = 'settings-subgroup-legend';
        legend.textContent = title;
        fieldset.appendChild(legend);

        if (description) {
            const desc = document.createElement('div');
            desc.className = 'settings-subgroup-description subtle';
            desc.textContent = description;
            fieldset.appendChild(desc);
        }

        const grid = document.createElement('div');
        grid.className = 'settings-subgroup-fields';
        fieldset.appendChild(grid);

        return { fieldset, grid };
    };

    const buildGroups = () => {
        if (!groupsContainer || !tabsContainer) {
            return;
        }
        const themeSlot = form ? form.querySelector('[data-settings-theme-slot]') : null;
        const builtGroupIds = [];
        const previousActive = activeGroupId;
        tabsContainer.innerHTML = '';
        groupsContainer.innerHTML = '';

        groups.forEach((group) => {
            const tabId = `settings-tab-${group.id}`;
            const panelId = `settings-panel-${group.id}`;

            const tabButton = document.createElement('button');
            tabButton.type = 'button';
            tabButton.className = 'settings-tab';
            tabButton.dataset.settingsTab = '1';
            tabButton.dataset.groupId = group.id;
            tabButton.id = tabId;
            tabButton.setAttribute('role', 'tab');
            tabButton.setAttribute('aria-controls', panelId);
            tabButton.textContent = group.title;
            tabButton.addEventListener('click', () => {
                setActiveGroup(group.id);
            });
            tabsContainer.appendChild(tabButton);

            const fieldset = document.createElement('fieldset');
            fieldset.className = 'settings-group';
            fieldset.id = panelId;
            fieldset.dataset.settingsGroup = '1';
            fieldset.dataset.groupId = group.id;
            fieldset.setAttribute('role', 'tabpanel');
            fieldset.setAttribute('aria-labelledby', tabId);

            const header = document.createElement('div');
            header.className = 'settings-group-header';

            const title = document.createElement('span');
            title.className = 'settings-group-title';
            title.textContent = group.title;

            const resetButton = document.createElement('button');
            resetButton.type = 'button';
            resetButton.className = 'settings-group-reset';
            resetButton.textContent = 'Reset to defaults';
            resetButton.addEventListener('click', () => {
                group.fields.forEach((field) => {
                    const entry = fieldMap.get(field.key);
                    // A read-only field is not this form's to reset: it would
                    // show a value the save cannot deliver.
                    if (entry && !readOnlyKeys.has(field.key)) {
                        setFieldValue(entry.input, defaults[field.key] ?? '');
                        entry.error.textContent = '';
                    }
                });
                setStatus(`${group.title} reset to defaults.`);
            });

            header.appendChild(title);
            header.appendChild(resetButton);

            const description = document.createElement('div');
            description.className = 'subtle';
            description.textContent = group.description;

            const fieldsWrap = document.createElement('div');
            fieldsWrap.className = 'settings-fields';
            const fieldWrappersByKey = new Map();

            group.fields.forEach((field) => {
                const wrapper = document.createElement('div');
                wrapper.className = 'settings-field';

                const inputId = `setting-${field.key.toLowerCase()}`;
                let input;
                if (field.type === 'select') {
                    input = document.createElement('select');
                    const normalizedOptions = (field.options || []).map(normalizeOptionEntry);
                    fieldBaseOptionsMap.set(field.key, new Set(normalizedOptions.map((option) => option.value)));
                    if (field.allowEmpty) {
                        appendSelectOption(input, '', 'Default');
                    }
                    normalizedOptions.forEach((option) => {
                        appendSelectOption(input, option.value, option.label);
                    });
                } else {
                    input = document.createElement('input');
                    input.type = field.type === 'password' ? 'password' : 'text';
                }

                input.id = inputId;
                input.dataset.settingKey = field.key;
                if (field.type === 'checkbox') {
                    input.type = 'checkbox';
                    input.dataset.kind = 'checkbox';
                }

                const label = document.createElement('label');
                label.htmlFor = inputId;
                label.textContent = field.label;

                if (field.type === 'checkbox') {
                    const row = document.createElement('div');
                    row.className = 'settings-checkbox-row';
                    row.appendChild(input);
                    row.appendChild(label);
                    wrapper.appendChild(row);
                } else {
                    wrapper.appendChild(label);
                    wrapper.appendChild(input);
                    if (field.type === 'select') {
                        const customRow = document.createElement('div');
                        customRow.className = 'settings-select-custom';
                        const customInput = document.createElement('input');
                        customInput.type = 'text';
                        customInput.placeholder = 'Add custom option';
                        customInput.setAttribute('aria-label', `Add option for ${field.label}`);
                        const customAdd = document.createElement('button');
                        customAdd.type = 'button';
                        customAdd.textContent = 'Add';
                        const handleAdd = () => {
                            const value = customInput.value.trim();
                            if (!value) {
                                return;
                            }
                            addExtraOptionForField(field.key, value);
                            customInput.value = '';
                            setStatus(`Added custom option for ${field.label}.`);
                        };
                        customAdd.addEventListener('click', handleAdd);
                        customInput.addEventListener('keydown', (event) => {
                            if (event.key === 'Enter') {
                                event.preventDefault();
                                handleAdd();
                            }
                        });
                        customRow.appendChild(customInput);
                        customRow.appendChild(customAdd);
                        wrapper.appendChild(customRow);
                        const extrasList = document.createElement('div');
                        extrasList.className = 'settings-select-extras';
                        extrasList.hidden = true;
                        wrapper.appendChild(extrasList);
                        fieldExtrasListMap.set(field.key, extrasList);
                    }
                }

                const helper = buildHelper(field);
                if (helper) {
                    wrapper.appendChild(helper);
                }

                const error = document.createElement('div');
                error.className = 'settings-error';
                wrapper.appendChild(error);

                fieldMap.set(field.key, { input, error, wrapper });
                fieldMetaMap.set(field.key, field);
                fieldGroupMap.set(field.key, group.id);
                fieldWrappersByKey.set(field.key, wrapper);
            });
            const appendedFieldKeys = new Set();
            const appendFieldTo = (container, key) => {
                if (!key || appendedFieldKeys.has(key)) {
                    return false;
                }
                const wrapper = fieldWrappersByKey.get(key);
                if (!wrapper) {
                    return false;
                }
                container.appendChild(wrapper);
                appendedFieldKeys.add(key);
                return true;
            };

            const groupSections = Array.isArray(group.sections) ? group.sections : [];
            if (groupSections.length > 0) {
                groupSections.forEach((sectionDef) => {
                    if (!sectionDef || !Array.isArray(sectionDef.fieldKeys) || sectionDef.fieldKeys.length === 0) {
                        return;
                    }
                    const section = buildSettingsSubgroup(sectionDef.title || 'Settings', sectionDef.description || '');
                    let appendedAny = false;
                    sectionDef.fieldKeys.forEach((key) => {
                        appendedAny = appendFieldTo(section.grid, key) || appendedAny;
                    });
                    if (group.id === 'chaos' && sectionDef.id === 'chaos_learning') {
                        section.grid.appendChild(buildChaosDefaultsSettingsField());
                        appendedAny = true;
                    }
                    if (appendedAny) {
                        fieldsWrap.appendChild(section.fieldset);
                    }
                });
            }

            // Append any remaining fields not explicitly assigned to a subgroup.
            const remainingKeys = [];
            group.fields.forEach((field) => {
                if (field && field.key && !appendedFieldKeys.has(field.key)) {
                    remainingKeys.push(field.key);
                }
            });
            if (remainingKeys.length > 0) {
                const fallbackSection = buildSettingsSubgroup('Additional Settings');
                remainingKeys.forEach((key) => appendFieldTo(fallbackSection.grid, key));
                fieldsWrap.appendChild(fallbackSection.fieldset);
            }

            fieldset.appendChild(header);
            fieldset.appendChild(description);
            fieldset.appendChild(fieldsWrap);
            groupsContainer.appendChild(fieldset);
            builtGroupIds.push(group.id);
        });

        if (themeSlot) {
            const groupId = 'theme';
            const tabId = `settings-tab-${groupId}`;
            const panelId = `settings-panel-${groupId}`;

            const tabButton = document.createElement('button');
            tabButton.type = 'button';
            tabButton.className = 'settings-tab';
            tabButton.dataset.settingsTab = '1';
            tabButton.dataset.groupId = groupId;
            tabButton.id = tabId;
            tabButton.setAttribute('role', 'tab');
            tabButton.setAttribute('aria-controls', panelId);
            tabButton.textContent = 'Theme';
            tabButton.addEventListener('click', () => {
                setActiveGroup(groupId);
            });
            tabsContainer.appendChild(tabButton);

            const fieldset = document.createElement('fieldset');
            fieldset.className = 'settings-group';
            fieldset.id = panelId;
            fieldset.dataset.settingsGroup = '1';
            fieldset.dataset.groupId = groupId;
            fieldset.setAttribute('role', 'tabpanel');
            fieldset.setAttribute('aria-labelledby', tabId);

            const header = document.createElement('div');
            header.className = 'settings-group-header';
            const title = document.createElement('span');
            title.className = 'settings-group-title';
            title.textContent = 'Theme';
            header.appendChild(title);

            const description = document.createElement('div');
            description.className = 'subtle';
            description.textContent = 'Select a system theme, then create, load, or save custom themes.';

            fieldset.appendChild(header);
            fieldset.appendChild(description);
            fieldset.appendChild(themeSlot);
            groupsContainer.appendChild(fieldset);
            builtGroupIds.push(groupId);
        }

        const hasPrevious = builtGroupIds.includes(previousActive);
        const initialGroupId = hasPrevious ? previousActive : (builtGroupIds[0] || '');
        if (initialGroupId) {
            setActiveGroup(initialGroupId);
        }
    };

    const populateSettings = (settings) => {
        fieldMetaMap.forEach((field, key) => {
            if (field.type !== 'select') {
                return;
            }
            const extras = parseExtraOptions(settings[extraOptionsSettingKey(key)] || '');
            fieldExtraOptionsMap.set(key, extras);
            const entry = fieldMap.get(key);
            if (!entry || !entry.input) {
                return;
            }
            extras.forEach((value) => ensureSelectOption(entry.input, value, value));
            renderExtraOptionsForField(key);
        });

        fieldMap.forEach((entry, key) => {
            entry.error.textContent = '';
            if (Object.prototype.hasOwnProperty.call(settings, key)) {
                const field = fieldMetaMap.get(key);
                if (field && field.type === 'select') {
                    const selectedValue = String(settings[key] || '').trim();
                    if (selectedValue) {
                        ensureSelectOption(entry.input, selectedValue, selectedValue);
                        const baseSet = fieldBaseOptionsMap.get(key) || new Set();
                        if (!baseSet.has(selectedValue)) {
                            const extras = fieldExtraOptionsMap.get(key) || [];
                            if (!extras.includes(selectedValue)) {
                                extras.push(selectedValue);
                                fieldExtraOptionsMap.set(key, extras);
                                renderExtraOptionsForField(key);
                            }
                        }
                    }
                }
                setFieldValue(entry.input, settings[key]);
            }
        });
    };

    // Marks the fields the server reports as read-only. Disabling the input is
    // what keeps them out of the payload: collectSettings skips them, so the
    // save carries only what this form is allowed to change.
    const applyReadOnly = (keys) => {
        const incoming = new Set(Array.isArray(keys) ? keys : []);
        readOnlyKeys.clear();
        incoming.forEach((key) => readOnlyKeys.add(key));

        fieldMap.forEach((entry, key) => {
            const locked = readOnlyKeys.has(key);
            entry.input.disabled = locked;
            if (entry.wrapper) {
                entry.wrapper.classList.toggle('is-readonly', locked);
                let note = entry.wrapper.querySelector('[data-settings-readonly-note]');
                if (locked && !note) {
                    note = document.createElement('div');
                    note.className = 'settings-helper settings-readonly-note';
                    note.dataset.settingsReadonlyNote = '1';
                    note.textContent = 'Set outside the web interface. Change it in the environment or .env file.';
                    entry.wrapper.appendChild(note);
                } else if (!locked && note) {
                    note.remove();
                }
            }
            const custom = entry.wrapper ? entry.wrapper.querySelector('.settings-select-custom') : null;
            if (custom) {
                custom.hidden = locked;
            }
        });
    };

    const loadSettings = async () => {
        setStatus('Loading settings...');
        try {
            const response = await fetch('/api/settings?includeSensitive=true');
            if (!response.ok) {
                throw new Error('Failed to load settings.');
            }
            const data = await response.json();
            populateSettings(data.settings || {});
            applyReadOnly(data.readOnly);
            setStatus('Settings loaded.');
            loadChaosDefaultsFromServer();
        } catch (error) {
            setStatus(error.message || 'Failed to load settings.', true);
            setChaosDefaultsStatus('');
        }
    };

    const collectSettings = () => {
        const settings = {};
        fieldMap.forEach((entry, key) => {
            if (readOnlyKeys.has(key)) {
                return;
            }
            if (entry.input.dataset.kind === 'checkbox') {
                settings[key] = entry.input.checked ? 'true' : 'false';
            } else {
                settings[key] = entry.input.value.trim();
            }
        });
        fieldMetaMap.forEach((field, key) => {
            if (field.type === 'select') {
                settings[extraOptionsSettingKey(key)] = serializeExtraOptions(fieldExtraOptionsMap.get(key) || []);
            }
        });
        return settings;
    };

    const saveSettings = async () => {
        const payload = { settings: collectSettings() };
        setStatus('Saving settings...');
        try {
            const response = await fetch('/api/settings?includeSensitive=true', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify(payload),
            });

            const data = await response.json();
            if (!response.ok) {
                const details = data.details || {};
                fieldMap.forEach((entry, key) => {
                    entry.error.textContent = details[key] || '';
                });
                // A rejected field is usually on a tab the operator never
                // opened — the banner alone leaves them staring at a form
                // with nothing wrong on it. Open the tab that owns the first
                // failure and name the field.
                const failedKeys = Object.keys(details).filter((key) => fieldMap.has(key));
                if (failedKeys.length > 0) {
                    const firstKey = failedKeys[0];
                    const groupId = fieldGroupMap.get(firstKey);
                    if (groupId) {
                        setActiveGroup(groupId);
                    }
                    const entry = fieldMap.get(firstKey);
                    if (entry && entry.input && typeof entry.input.focus === 'function') {
                        entry.input.focus();
                    }
                    const labels = failedKeys
                        .map((key) => (fieldMetaMap.get(key) || {}).label || key)
                        .join(', ');
                    throw new Error(`Check ${labels}: ${details[firstKey]}`);
                }
                throw new Error(data.error || 'Failed to save settings.');
            }

            populateSettings(data.settings || payload.settings);
            applyReadOnly(data.readOnly);
            setStatus('Settings saved.');
            if (window.ThreeSeventyWeb && window.ThreeSeventyWeb.notify) {
                window.ThreeSeventyWeb.notify('Settings saved.', 'success');
            }
            const onConnectScreen = window.location.pathname === '/';
            if (onConnectScreen) {
                const restartNow = await askRestartConfirmation();
                if (restartNow) {
                    setStatus('Restarting 3270Web...');
                    const restartResponse = await fetch('/app/restart', { method: 'POST' });
                    if (!restartResponse.ok) {
                        throw new Error('Failed to restart 3270Web.');
                    }
                }
            }
        } catch (error) {
            setStatus(error.message || 'Failed to save settings.', true);
            if (window.ThreeSeventyWeb && window.ThreeSeventyWeb.notify) {
                window.ThreeSeventyWeb.notify(error.message || 'Failed to save settings.', 'error');
            }
        }
    };

    try {
        const storedMaximized = window.localStorage.getItem(maximizedStorageKey);
        setMaximizeUi(storedMaximized === '1');
        buildGroups();
    } catch (error) {
        setStatus('Failed to initialize settings form.', true);
    }

    if (refreshButton) {
        refreshButton.addEventListener('click', () => {
            loadSettings();
        });
    }

    if (form) {
        form.addEventListener('submit', async (event) => {
            event.preventDefault();
            if (typeof window.h3270ThemeConfirmBeforeSettingsSave === 'function') {
                const proceed = await window.h3270ThemeConfirmBeforeSettingsSave();
                if (!proceed) {
                    return;
                }
            }
            saveSettings();
        });
    }

    if (maximizeButton) {
        maximizeButton.addEventListener('click', () => {
            setMaximizeUi(!maximized);
            try {
                window.localStorage.setItem(maximizedStorageKey, maximized ? '1' : '0');
            } catch (err) {
                // ignore persistence errors
            }
        });
    }
    if (minimizeButton) {
        minimizeButton.addEventListener('click', () => {
            setMaximizeUi(false);
            try {
                window.localStorage.setItem(maximizedStorageKey, '0');
            } catch (err) {
                // ignore persistence errors
            }
        });
    }

    restartCancelButtons.forEach((button) => {
        button.addEventListener('click', () => closeRestartConfirm(false));
    });

    if (restartConfirmButton) {
        restartConfirmButton.addEventListener('click', () => closeRestartConfirm(true));
    }

})();

/**
 * Chaos exploration toolbar controls.
 * Handles start/stop/export/load/resume for chaos runs and polls /chaos/status.
 */
(() => {
    const body = document.body;
    const chaosControls = document.querySelector('[data-chaos-controls]');
    const startBtn = document.querySelector('[data-chaos-start]');
    const stopBtn = document.querySelector('[data-chaos-stop]');
    const exportBtn = document.querySelector('[data-chaos-export]');
    const reportBtn = document.querySelector('[data-chaos-report]');
    const removeBtn = document.querySelector('[data-chaos-remove]');
    const loadBtn = document.querySelector('[data-chaos-load]');
    const loadRecordingBtn = document.querySelector('[data-chaos-load-recording]');
    const resumeBtn = document.querySelector('[data-chaos-resume]');
    const flowMenuOpenBtn = document.querySelector('[data-chaos-flow-menu-open]');
    const taskOpenBtn = document.querySelector('[data-chaos-task-open]');
    const indicator = document.querySelector('[data-chaos-indicator]');
    const completeIndicator = document.querySelector('[data-chaos-complete-indicator]');
    const statsIndicator = document.querySelector('[data-chaos-stats-indicator]');
    const statsText = document.querySelector('[data-chaos-stats-text]');
    const runsModal = document.querySelector('[data-chaos-runs-modal]');
    const runsModalClose = document.querySelectorAll('[data-chaos-runs-close]');
    const runsList = document.querySelector('[data-chaos-runs-list]');
    const runsSelectAllBtn = document.querySelector('[data-chaos-runs-select-all]');
    const runsClearSelectionBtn = document.querySelector('[data-chaos-runs-clear-selection]');
    const runsDeleteSelectedBtn = document.querySelector('[data-chaos-runs-delete-selected]');
    const runsCompareBtn = document.querySelector('[data-chaos-runs-compare]');
    const runsStatus = document.querySelector('[data-chaos-runs-status]');
    const compareModal = document.querySelector('[data-chaos-compare-modal]');
    const compareModalClose = document.querySelectorAll('[data-chaos-compare-close]');
    const compareStatus = document.querySelector('[data-chaos-compare-status]');
    const compareContent = document.querySelector('[data-chaos-compare-content]');
    const compareTitle = document.querySelector('#chaos-compare-modal-title');
    const confirmModal = document.querySelector('[data-chaos-confirm-modal]');
    const confirmTitle = document.querySelector('[data-chaos-confirm-title]');
    const confirmMessage = document.querySelector('[data-chaos-confirm-message]');
    const confirmCancelButtons = document.querySelectorAll('[data-chaos-confirm-cancel]');
    const confirmOkButton = document.querySelector('[data-chaos-confirm-ok]');
    const confirmAltButton = document.querySelector('[data-chaos-confirm-alt]');
    const confirmCancelLabelButton = document.querySelector('[data-chaos-confirm-cancel-label]');
    const startModal = document.querySelector('[data-chaos-start-modal]');
    const startForm = document.querySelector('[data-chaos-start-form]');
    const startModalTitle = document.querySelector('[data-chaos-start-title]');
    const startModalCancelButtons = document.querySelectorAll('[data-chaos-start-cancel]');
    const startModalConfirmBtn = document.querySelector('[data-chaos-start-confirm]');
    const startModalHost = document.querySelector('[data-chaos-start-host]');
    const startModalRunRow = document.querySelector('[data-chaos-start-run-row]');
    const startModalRun = document.querySelector('[data-chaos-start-run]');
    const startModalMaxSteps = document.querySelector('[data-chaos-start-max-steps]');
    const startModalStepsLabel = document.querySelector('[data-chaos-start-steps-label]');
    const startModalStepsHelp = document.querySelector('[data-chaos-start-steps-help]');
    const startModalTimeBudget = document.querySelector('[data-chaos-start-time-budget]');
    const startModalBlockedKeys = document.querySelector('[data-chaos-start-blocked-keys]');
    const startModalBlockedWarning = document.querySelector('[data-chaos-start-blocked-warning]');
    const startModalKeys = document.querySelector('[data-chaos-start-keys]');
    const startModalKeysDefaultBtn = document.querySelector('[data-chaos-start-keys-default]');
    const startModalKeysAllBtn = document.querySelector('[data-chaos-start-keys-all]');
    const startModalStatus = document.querySelector('[data-chaos-start-status]');
    const hintsOpenBtn = document.querySelector('[data-chaos-hints-open]');
    const hintsModal = document.querySelector('[data-chaos-hints-modal]');
    const hintsModalClose = document.querySelectorAll('[data-chaos-hints-close]');
    const hintsMaximizeBtn = document.querySelector('[data-chaos-hints-maximize]');
    const hintsMinimizeBtn = document.querySelector('[data-chaos-hints-minimize]');
    const hintsList = document.querySelector('[data-chaos-hints-list]');
    const hintsKeyBlacklistInput = document.querySelector('[data-chaos-key-blacklist]');
    const firstScreenKnownDataInput = document.querySelector('[data-chaos-first-screen-known-data]');
    const firstScreenKnownKeysInput = document.querySelector('[data-chaos-first-screen-known-keys]');
    const firstScreenBlockedKeysInput = document.querySelector('[data-chaos-first-screen-blocked-keys]');
    const firstScreenKeyAssignmentsInput = document.querySelector('[data-chaos-first-screen-key-assignments]');
    const hintsAddBtn = document.querySelector('[data-chaos-hints-add]');
    const hintsLoadRecordingBtn = document.querySelector('[data-chaos-hints-load-recording]');
    const hintsRecordingInput = document.querySelector('[data-chaos-hints-recording-input]');
    const hintsReloadBtn = document.querySelector('[data-chaos-hints-reload]');
    const hintsSaveBtn = document.querySelector('[data-chaos-hints-save]');
    const hintsStatus = document.querySelector('[data-chaos-hints-status]');
    const mapOpenBtn = document.querySelector('[data-chaos-map-open]');
    const mapModal = document.querySelector('[data-chaos-map-modal]');
    const mapModalClose = document.querySelectorAll('[data-chaos-map-close]');
    const mapMaximizeBtn = document.querySelector('[data-chaos-map-maximize]');
    const mapMinimizeBtn = document.querySelector('[data-chaos-map-minimize]');
    const mapViewBtn = document.querySelector('[data-chaos-map-view]');
    const mapList = document.querySelector('[data-chaos-map-list]');
    const mapStatus = document.querySelector('[data-chaos-map-status]');
    const mapRefreshBtn = document.querySelector('[data-chaos-map-refresh]');
    const mapExportBtn = document.querySelector('[data-chaos-map-export]');
    const mapImportBtn = document.querySelector('[data-chaos-map-import]');
    const mapImportInput = document.querySelector('[data-chaos-map-import-input]');
    const mapTaskBtn = document.querySelector('[data-chaos-map-task]');
    const activeRunContainer = document.querySelector('[data-active-run-container]');
    const activeRunStopBtn = document.querySelector('[data-active-run-stop]');
    const activeRunContinueBtn = document.querySelector('[data-active-run-extend]');
    const momentumEl = document.querySelector('[data-chaos-momentum]');
    const chaosLiveRegion = document.querySelector('[data-chaos-live]');
    const runPanel = document.querySelector('[data-chaos-run-panel]');
    const runPanelIcon = document.querySelector('[data-chaos-run-panel-icon]');
    const runPanelTitle = document.querySelector('[data-chaos-run-panel-title]');
    const runPanelReason = document.querySelector('[data-chaos-run-panel-reason]');
    const runPanelCounts = document.querySelector('[data-chaos-run-panel-counts]');
    const runPanelReportBtn = document.querySelector('[data-chaos-run-panel-report]');
    const runPanelMapBtn = document.querySelector('[data-chaos-run-panel-map]');
    const runPanelExportBtn = document.querySelector('[data-chaos-run-panel-export]');
    const runPanelContinueBtn = document.querySelector('[data-chaos-run-panel-continue]');
    const runPanelNext = document.querySelector('[data-chaos-run-panel-next]');
    const runPanelNextList = document.querySelector('[data-chaos-run-panel-next-list]');
    const flowModal = document.querySelector('[data-chaos-flow-modal]');
    const flowModalClose = document.querySelectorAll('[data-chaos-flow-close]');
    const flowMaximizeBtn = document.querySelector('[data-chaos-flow-maximize]');
    const flowMinimizeBtn = document.querySelector('[data-chaos-flow-minimize]');
    const flowContent = document.querySelector('[data-chaos-flow-content]');
    const flowStatus = document.querySelector('[data-chaos-flow-status]');
    const flowRefreshButtons = Array.from(document.querySelectorAll('[data-chaos-flow-refresh]'));
    const flowScreenHintsModal = document.querySelector('[data-chaos-flow-screen-hints-modal]');
    const flowScreenHintsCloseButtons = document.querySelectorAll('[data-chaos-flow-screen-hints-close]');
    const flowScreenHintsMaximizeBtn = document.querySelector('[data-chaos-flow-screen-hints-maximize]');
    const flowScreenHintsMinimizeBtn = document.querySelector('[data-chaos-flow-screen-hints-minimize]');
    const flowScreenHintsMeta = document.querySelector('[data-chaos-flow-screen-hints-meta]');
    const flowScreenHintsPreview = document.querySelector('[data-chaos-flow-screen-hints-preview]');
    const flowScreenHintsKnownData = document.querySelector('[data-chaos-flow-screen-known-data]');
    const flowScreenHintsKnownKeys = document.querySelector('[data-chaos-flow-screen-known-keys]');
    const flowScreenHintsBlockedKeys = document.querySelector('[data-chaos-flow-screen-blocked-keys]');
    const flowScreenHintsKeyAssignments = document.querySelector('[data-chaos-flow-screen-key-assignments]');
    const flowScreenHintsStatus = document.querySelector('[data-chaos-flow-screen-hints-status]');
    const flowScreenHintsSaveBtn = document.querySelector('[data-chaos-flow-screen-hints-save]');
    const reportModal = document.querySelector('[data-chaos-report-modal]');
    const reportModalClose = document.querySelectorAll('[data-chaos-report-close]');
    const reportMaximizeBtn = document.querySelector('[data-chaos-report-maximize]');
    const reportMinimizeBtn = document.querySelector('[data-chaos-report-minimize]');
    const reportContent = document.querySelector('[data-chaos-report-content]');
    const reportStatus = document.querySelector('[data-chaos-report-status]');
    const reportRefreshBtn = document.querySelector('[data-chaos-report-refresh]');
    const reportRawToggleBtn = document.querySelector('[data-chaos-report-raw-toggle]');
    const reportDownloadBtn = document.querySelector('[data-chaos-report-download]');
    const startLogConfirmModal = document.querySelector('[data-chaos-start-log-confirm-modal]');
    const startLogConfirmCancelButtons = document.querySelectorAll('[data-chaos-start-log-confirm-cancel]');
    const startLogConfirmContinueBtn = document.querySelector('[data-chaos-start-log-confirm-continue]');
    const startLogConfirmDisableBtn = document.querySelector('[data-chaos-start-log-confirm-disable]');
    const startLogConfirmMessage = document.querySelector('[data-chaos-start-log-confirm-message]');
    const chaosSections = chaosControls ? Array.from(chaosControls.querySelectorAll('[data-chaos-section]')) : [];
    const chaosDividers = chaosControls ? Array.from(chaosControls.querySelectorAll('[data-chaos-divider]')) : [];
    const recordingIndicator = document.querySelector('[data-recording-indicator]');
    const recordingStartButton = document.querySelector('form[data-recording-start] button');
    const workflowLoadButton = document.querySelector('[data-workflow-trigger]');
    const logsToggleCheckbox = document.querySelector('[data-logs-toggle-checkbox]');

    if (!startBtn && !stopBtn && !exportBtn && !removeBtn) {
        return;
    }

    let pollTimer = null;
    let pollFailureCount = 0;
    const pollBaseDelayMs = 1000;
    const pollMaxDelayMs = 15000;
    // The run strip is fed by the workflow-status poll (workflow.js), which
    // already reports whether an exploration is running and how far it has
    // got, and which tells this module the moment that changes. So this poll
    // only has to keep the coverage figures and the menu fresh, and need not
    // run every second to do it.
    const pollActiveDelayMs = 3000;
    const pollHiddenDelayMs = 10000;
    // A run that has ended is polled until it has been written to disk and
    // comes back with a run ID. If that never happens, stop asking.
    const pollFinalizeMaxTries = 20;
    let pollFinalizeTries = 0;
    let lastWorkflowSyncKey = null;
    // The limits of the current run as the workflow-status poll reports them,
    // for working out how a run ended on a server that does not say.
    const knownRunLimits = { maxSteps: 0, timeBudgetMs: 0, startedAt: '', stoppedAt: '' };
    let fullMapUnavailable = false;
    let latestAreaCount = 0;
    const groupedScreenCount = { areaCount: -1, screens: 0 };
    let chaosSessionExpired = false;
    // The full screen map is large and is only fetched when a view that draws
    // it is open (?verbose=true). latestMindMapIsFull says which kind is held.
    let latestMindMapIsFull = false;
    let fullMapFetchedAt = 0;
    let fullMapFetchInFlight = null;
    let latestSlimSignature = '';
    let fullMapSlimSignature = '';
    const fullMapMinIntervalMs = 3000;
    let mapRenderPending = false;
    let flowRenderedSignature = '';
    let flowPointerActive = false;
    let userRequestedStop = false;
    let lastOutcome = null;
    let lastInsightsKey = '';
    let lastMomentumText = '';
    let lastMomentumAnnouncedAt = 0;
    const momentumAnnounceIntervalMs = 20000;
    let startDialogMode = 'start';
    let startDialogBusy = false;
    let hasData = false;
    let loadedRunID = null;
    let lastStatus = { active: false, stepsRun: 0, transitions: 0 };
    let lastChaosRunning = false;
    let chaosHints = [];
    let chaosKeyBlacklist = [];
    let chaosFirstScreenHint = { knownData: [], knownKeys: [], blockedKeys: [], keyAssignments: {} };
    let hintsDirty = false;
    let hintsModalMaximized = false;
    let mapModalMaximized = false;
    let flowModalMaximized = false;
    let flowScreenHintsModalMaximized = false;
    let reportModalMaximized = false;
    let reportRawMarkdownMode = false;
    let hintRowSequence = 0;
    let latestMindMap = null;
    let latestFirstScreenHash = '';
    let screenHintsByHash = {};
    let screenHintDrafts = {};
    let latestChaosReportMarkdown = '';
    let activeChaosModal = null;
    let previousChaosFocus = null;
    const chaosModalParentStack = [];
    const chaosModalReturnFocusStack = [];
    let pendingStartLogConfirmResolve = null;
    let pendingChaosConfirmResolve = null;
    let currentRuns = [];
    let selectedChaosRunIDs = new Set();
    let activeFlowScreenHintsTarget = null;

    const chaosFocusableSelector = [
        'button:not([disabled])',
        'a[href]',
        'input:not([disabled]):not([type="hidden"])',
        'select:not([disabled])',
        'textarea:not([disabled])',
        '[tabindex]:not([tabindex="-1"])',
    ].join(', ');

    const getModalFocusableElements = (modal) => {
        if (!modal) {
            return [];
        }
        return Array.from(modal.querySelectorAll(chaosFocusableSelector))
            .filter((el) => !el.hidden && !el.closest('[hidden]'));
    };

    const focusModalElement = (modal, preferredSelector = '') => {
        if (!modal || modal.hidden) {
            return;
        }
        if (preferredSelector) {
            const preferred = modal.querySelector(preferredSelector);
            if (preferred && typeof preferred.focus === 'function' && !preferred.hidden) {
                preferred.focus();
                return;
            }
        }
        const focusables = getModalFocusableElements(modal);
        if (focusables.length && typeof focusables[0].focus === 'function') {
            focusables[0].focus();
            return;
        }
        const dialog = modal.querySelector('.modal');
        if (dialog && typeof dialog.focus === 'function') {
            dialog.focus();
        }
    };

    const setChaosModalBackgrounded = (modal, backgrounded) => {
        if (!modal) {
            return;
        }
        modal.classList.toggle('is-backgrounded', !!backgrounded);
        if (backgrounded) {
            modal.setAttribute('aria-hidden', 'true');
        } else {
            modal.removeAttribute('aria-hidden');
        }
    };

    // Most of these dialogs are opened from the Automation menu, which has
    // closed by the time they do: the item that had focus is no longer on
    // screen, and handing focus back to it dropped it on the page body. Fall
    // back to the menu's own trigger, which is always there.
    const restoreChaosFocus = (el) => {
        const visible = (node) => !!(node && node.isConnected && typeof node.focus === 'function'
            && !node.disabled && node.getClientRects().length);
        if (visible(el)) {
            el.focus();
            return;
        }
        const fallback = document.querySelector('[data-menu-automation] [data-app-menu-trigger]');
        if (visible(fallback)) {
            fallback.focus();
        }
    };

    const closeChaosModal = (modal, options = {}) => {
        if (!modal) {
            return;
        }
        if (modal === confirmModal && pendingChaosConfirmResolve) {
            const resolve = pendingChaosConfirmResolve;
            pendingChaosConfirmResolve = null;
            resolve(false);
        }
        if (modal === startLogConfirmModal && pendingStartLogConfirmResolve) {
            const resolve = pendingStartLogConfirmResolve;
            pendingStartLogConfirmResolve = null;
            resolve('cancel');
        }
        const restoreFocus = options.restoreFocus !== false;
        setChaosModalBackgrounded(modal, false);
        modal.hidden = true;
        if (activeChaosModal === modal) {
            if (chaosModalParentStack.length) {
                activeChaosModal = chaosModalParentStack.pop() || null;
                const returnFocus = chaosModalReturnFocusStack.pop() || null;
                setChaosModalBackgrounded(activeChaosModal, false);
                if (restoreFocus && returnFocus && typeof returnFocus.focus === 'function') {
                    returnFocus.focus();
                }
                return;
            }
            activeChaosModal = null;
            if (restoreFocus) {
                restoreChaosFocus(previousChaosFocus);
            }
            previousChaosFocus = null;
            return;
        }
        const idx = chaosModalParentStack.lastIndexOf(modal);
        if (idx >= 0) {
            chaosModalParentStack.splice(idx, 1);
            chaosModalReturnFocusStack.splice(idx, 1);
        }
    };

    const openChaosModal = (modal, preferredSelector = '', options = {}) => {
        if (!modal) {
            return;
        }
        const keepPrevious = !!options.keepPrevious;
        const hasPreviousActiveModal = !!(activeChaosModal && activeChaosModal !== modal);
        const nestOnOpen = keepPrevious && hasPreviousActiveModal;
        const resetModalScroll = () => {
            try {
                modal.scrollTop = 0;
                const modalPanel = modal.querySelector('.modal');
                if (modalPanel) {
                    modalPanel.scrollTop = 0;
                }
                modal.querySelectorAll('.stack, [data-chaos-map-list], [data-chaos-flow-content], [data-chaos-report-content]').forEach((el) => {
                    if (el && typeof el.scrollTop === 'number') {
                        el.scrollTop = 0;
                    }
                });
            } catch (_err) {
                // ignore
            }
        };
        if (hasPreviousActiveModal) {
            if (nestOnOpen) {
                setChaosModalBackgrounded(activeChaosModal, true);
                chaosModalParentStack.push(activeChaosModal);
            } else {
                while (chaosModalParentStack.length) {
                    const prev = chaosModalParentStack.pop();
                    chaosModalReturnFocusStack.pop();
                    if (prev) {
                        setChaosModalBackgrounded(prev, false);
                        prev.hidden = true;
                    }
                }
                closeChaosModal(activeChaosModal, { restoreFocus: false });
            }
        }
        const activeElement = document.activeElement;
        if (!activeChaosModal || !keepPrevious) {
            previousChaosFocus = activeElement instanceof HTMLElement ? activeElement : null;
        }
        if (nestOnOpen) {
            chaosModalReturnFocusStack.push(activeElement instanceof HTMLElement ? activeElement : null);
        }
        modal.hidden = false;
        activeChaosModal = modal;
        resetModalScroll();
        if (activeElement && typeof activeElement.blur === 'function') {
            activeElement.blur();
        }
        window.requestAnimationFrame(() => {
            resetModalScroll();
            focusModalElement(modal, preferredSelector);
        });
    };

    document.addEventListener('keydown', (event) => {
        const modal = activeChaosModal && !activeChaosModal.hidden ? activeChaosModal : null;
        if (!modal) {
            return;
        }
        if (event.key === 'Escape') {
            event.preventDefault();
            requestCloseChaosModal(modal);
            return;
        }
        if (event.key !== 'Tab') {
            return;
        }
        const focusables = getModalFocusableElements(modal);
        if (!focusables.length) {
            event.preventDefault();
            focusModalElement(modal);
            return;
        }
        const first = focusables[0];
        const last = focusables[focusables.length - 1];
        const active = document.activeElement;
        const outsideModal = !active || !modal.contains(active);
        if (event.shiftKey) {
            if (outsideModal || active === first) {
                event.preventDefault();
                last.focus();
            }
            return;
        }
        if (outsideModal || active === last) {
            event.preventDefault();
            first.focus();
        }
    }, true);

    // Closing by Escape, by the backdrop or by a Close button all ask first
    // when the dialog has something to lose. Only the hints editor does.
    const requestCloseChaosModal = (modal) => {
        if (modal && modal === hintsModal) {
            requestCloseHintsModal();
            return;
        }
        if (modal && modal === startModal && startDialogBusy) {
            return;
        }
        closeChaosModal(modal);
    };

    const setButtonBusy = (btn, busy) => {
        if (!btn) {
            return;
        }
        btn.disabled = busy;
        btn.setAttribute('aria-busy', busy ? 'true' : 'false');
    };

    const defaultTooltip = (btn) => {
        if (!btn) {
            return '';
        }
        if (!Object.prototype.hasOwnProperty.call(btn.dataset, 'defaultTooltip')) {
            btn.dataset.defaultTooltip = btn.getAttribute('data-tippy-content') || '';
        }
        return btn.dataset.defaultTooltip;
    };

    const setButtonDisabledState = (btn, disabled, disabledTooltip) => {
        if (!btn) {
            return;
        }
        if (btn.getAttribute('aria-busy') === 'true') {
            return;
        }
        btn.disabled = !!disabled;
        const fallback = defaultTooltip(btn);
        const nextTooltip = disabled && disabledTooltip ? disabledTooltip : fallback;
        if (nextTooltip) {
            btn.setAttribute('data-tippy-content', nextTooltip);
            if (btn._tippy) {
                btn._tippy.setContent(nextTooltip);
            }
        }
    };

    // Every status line in these dialogs goes through here. An error used to
    // be the same text in a different colour, which says nothing to anybody
    // who cannot tell the colours apart or is not looking at the screen: it
    // now carries the word, and the class adds an icon.
    const setStatusLine = (el, message, isError = false) => {
        if (!el) {
            return;
        }
        const text = String(message || '');
        const failed = !!isError && !!text;
        el.style.color = '';
        el.classList.toggle('is-error', failed);
        el.textContent = failed && !/^(error|failed|could not|unable)\b/i.test(text) ? `Error: ${text}` : text;
    };

    const setHintsStatus = (message, isError = false) => setStatusLine(hintsStatus, message, isError);

    const notifyUi = (message, type = 'info', options) => {
        if (!message) {
            return;
        }
        if (window.ThreeSeventyWeb && typeof window.ThreeSeventyWeb.notify === 'function') {
            window.ThreeSeventyWeb.notify(message, type, options);
        }
    };

    const setTooltipContent = (el, content) => {
        if (!el || !content) {
            return;
        }
        el.setAttribute('data-tippy-content', content);
        if (el._tippy && typeof el._tippy.setContent === 'function') {
            el._tippy.setContent(content);
        }
    };

    const chaosCountsSummary = (counts = {}) => {
        const steps = Number(counts.steps || 0);
        const screens = Number(counts.screens || 0);
        const inputs = Number(counts.inputs || 0);
        const keys = Number(counts.keys || 0);
        const parts = [];
        if (steps > 0) {
            parts.push(`${steps} step${steps === 1 ? '' : 's'}`);
        }
        if (screens > 0) {
            parts.push(screenCountLabel(screens));
        }
        if (inputs > 0) {
            parts.push(`${inputs} input${inputs === 1 ? '' : 's'}`);
        }
        if (keys > 0) {
            parts.push(`${keys} key${keys === 1 ? '' : 's'}`);
        }
        return parts.join(', ');
    };

    const setFlowStatus = (message, isError = false) => setStatusLine(flowStatus, message, isError);

    const setFlowScreenHintsStatus = (message, isError = false) => setStatusLine(flowScreenHintsStatus, message, isError);

    const renderFlowScreenHintsDraft = (hint) => {
        const normalized = normalizeScreenHint(hint || {});
        if (flowScreenHintsKnownData) {
            flowScreenHintsKnownData.value = formatListLines(normalized.knownData || []);
        }
        if (flowScreenHintsKnownKeys) {
            flowScreenHintsKnownKeys.value = formatListLines(normalized.knownKeys || []);
        }
        if (flowScreenHintsBlockedKeys) {
            flowScreenHintsBlockedKeys.value = formatListLines(normalized.blockedKeys || []);
        }
        if (flowScreenHintsKeyAssignments) {
            flowScreenHintsKeyAssignments.value = formatKeyAssignments(normalized.keyAssignments || {});
        }
    };

    const openFlowScreenHintsEditor = (nodeInfo) => {
        if (!flowScreenHintsModal || !nodeInfo) {
            return;
        }
        const memberHashes = Array.isArray(nodeInfo.memberHashes)
            ? nodeInfo.memberHashes.map((h) => String(h || '').trim()).filter(Boolean)
            : [];
        const normalizedInfo = {
            nodeID: String(nodeInfo.nodeID || '').trim(),
            label: String(nodeInfo.label || 'Screen').trim() || 'Screen',
            sampleHash: String(nodeInfo.sampleHash || '').trim(),
            memberHashes,
        };
        const hint = mergeScreenHintsForHashes(memberHashes);
        activeFlowScreenHintsTarget = normalizedInfo;
        if (flowScreenHintsMeta) {
            const parts = [
                normalizedInfo.label,
                normalizedInfo.sampleHash ? `sample ${normalizedInfo.sampleHash}` : '',
                memberHashes.length > 1 ? `${memberHashes.length} merged variants` : (memberHashes[0] ? `hash ${memberHashes[0]}` : ''),
            ].filter(Boolean);
            flowScreenHintsMeta.textContent = parts.join(' | ');
        }
        if (flowScreenHintsPreview) {
            // Use flow areas (includes the first screen) so the first discovered
            // screen can still show its captured preview in the hints editor.
            const areas = getChaosFlowAreas();
            const sampleArea = areas.find((area) => String(area && area.hash || '').trim() === normalizedInfo.sampleHash)
                || areas.find((area) => memberHashes.includes(String(area && area.hash || '').trim()))
                || null;
            flowScreenHintsPreview.innerHTML = sampleArea
                ? chaosMapCardScreenPreviewMarkup(sampleArea)
                : '<div class="chaos-map-card-screen-preview is-empty"><div class="chaos-map-card-screen-preview-meta">No captured screen preview</div></div>';
        }
        renderFlowScreenHintsDraft(hint);
        setFlowScreenHintsStatus('');
        openChaosModal(flowScreenHintsModal, '[data-chaos-flow-screen-known-data]', { keepPrevious: true });
    };

    const collectFlowScreenHintsDraft = () => normalizeScreenHint({
        knownData: flowScreenHintsKnownData ? flowScreenHintsKnownData.value : '',
        knownKeys: flowScreenHintsKnownKeys ? flowScreenHintsKnownKeys.value : '',
        blockedKeys: flowScreenHintsBlockedKeys ? flowScreenHintsBlockedKeys.value : '',
        keyAssignments: parseKeyAssignments(flowScreenHintsKeyAssignments ? flowScreenHintsKeyAssignments.value : ''),
    });

    const setReportStatus = (message, isError = false) => setStatusLine(reportStatus, message, isError);

    const setStartLogConfirmMessage = (message) => {
        if (!startLogConfirmMessage) {
            return;
        }
        startLogConfirmMessage.textContent = message || '';
    };

    const setStartLogConfirmDisableAvailable = (enabled) => {
        if (!startLogConfirmDisableBtn) {
            return;
        }
        const canDisable = !!enabled;
        startLogConfirmDisableBtn.hidden = !canDisable;
        startLogConfirmDisableBtn.disabled = !canDisable;
    };

    const settleStartLogConfirm = (choice) => {
        const resolve = pendingStartLogConfirmResolve;
        pendingStartLogConfirmResolve = null;
        closeChaosModal(startLogConfirmModal);
        if (resolve) {
            resolve(choice || 'cancel');
        }
    };

    const promptForLoggingActiveChaosStart = (options = {}) => {
        if (!startLogConfirmModal) {
            const canDisable = options.canDisable !== false;
            return (async () => {
                if (canDisable) {
                    const disable = await promptChaosConfirm({
                        title: 'Verbose Logging Enabled',
                        message: 'Verbose logging is enabled. Disable logging and start the exploration?',
                        confirmLabel: 'Disable & Start',
                    });
                    if (disable) {
                        return 'disable';
                    }
                }
                const proceed = await promptChaosConfirm({
                    title: 'Verbose Logging Enabled',
                    message: 'Start the exploration with verbose logging still enabled?',
                    confirmLabel: 'Start exploration',
                    danger: false,
                });
                return proceed ? 'continue' : 'cancel';
            })();
        }

        const canDisable = options.canDisable !== false;
        setStartLogConfirmDisableAvailable(canDisable);
        setStartLogConfirmMessage(
            canDisable
                ? 'Continue with logging enabled, or disable logging and then start the exploration?'
                : 'Logging is enabled, but log access is disabled, so it cannot be turned off from here. Start the exploration with logging enabled, or cancel?'
        );

        if (pendingStartLogConfirmResolve) {
            const resolve = pendingStartLogConfirmResolve;
            pendingStartLogConfirmResolve = null;
            resolve('cancel');
        }

        return new Promise((resolve) => {
            pendingStartLogConfirmResolve = resolve;
            openChaosModal(
                startLogConfirmModal,
                canDisable
                    ? '[data-chaos-start-log-confirm-disable], [data-chaos-start-log-confirm-continue]'
                    : '[data-chaos-start-log-confirm-continue]',
                { keepPrevious: true }
            );
        });
    };

    const syncVerboseLoggingUIState = (enabled) => {
        if (logsToggleCheckbox) {
            logsToggleCheckbox.checked = !!enabled;
        }
        try {
            window.localStorage.setItem('3270Web.verboseLogging', enabled ? '1' : '0');
        } catch (_err) {
            // ignore persistence failures
        }
    };

    const fetchChaosLoggingState = async () => {
        try {
            const accessResp = await fetch('/logs/access', {
                headers: {
                    Accept: 'application/json',
                    'Cache-Control': 'no-cache',
                },
            });
            if (!accessResp.ok) {
                return { known: false, accessEnabled: false, verboseLogging: false };
            }
            const accessData = await accessResp.json();
            const accessEnabled = !!(accessData && accessData.enabled);
            if (accessData && typeof accessData.verboseLogging === 'boolean') {
                return {
                    known: true,
                    accessEnabled,
                    verboseLogging: !!accessData.verboseLogging,
                };
            }
            if (!accessEnabled) {
                return { known: false, accessEnabled: false, verboseLogging: false };
            }
            const logsResp = await fetch('/logs', {
                headers: {
                    Accept: 'application/json',
                    'Cache-Control': 'no-cache',
                },
            });
            if (!logsResp.ok) {
                return { known: false, accessEnabled, verboseLogging: false };
            }
            const logsData = await logsResp.json();
            return {
                known: true,
                accessEnabled,
                verboseLogging: !!(logsData && logsData.enabled),
            };
        } catch (_err) {
            return { known: false, accessEnabled: false, verboseLogging: false };
        }
    };

    const setVerboseLoggingEnabled = async (enabled) => {
        const formData = new FormData();
        formData.append('enabled', enabled ? 'true' : 'false');
        const resp = await fetch('/logs/toggle', {
            method: 'POST',
            body: formData,
        });
        if (!resp.ok) {
            return { ok: false, enabled };
        }
        let data = null;
        try {
            data = await resp.json();
        } catch (_err) {
            data = null;
        }
        const nextEnabled = data && typeof data.enabled === 'boolean' ? !!data.enabled : !!enabled;
        syncVerboseLoggingUIState(nextEnabled);
        return { ok: true, enabled: nextEnabled };
    };

    const confirmLoggingBeforeChaosStart = async () => {
        const loggingState = await fetchChaosLoggingState();
        if (!loggingState.known || !loggingState.verboseLogging) {
            return true;
        }
        const choice = await promptForLoggingActiveChaosStart({ canDisable: loggingState.accessEnabled });
        if (choice === 'continue') {
            return true;
        }
        if (choice === 'disable') {
            try {
                const result = await setVerboseLoggingEnabled(false);
                if (result.ok && !result.enabled) {
                    return true;
                }
            } catch (_err) {
                // fall through to user-facing message
            }
            setStartStatus('Verbose logging could not be turned off, so the exploration was not started.', true);
            return false;
        }
        return false;
    };

    const chaosMapTopFocusSelector = '[data-chaos-map-view], [data-chaos-map-maximize], [data-chaos-map-close]';
    const chaosReportTopFocusSelector = '[data-chaos-report-refresh], [data-chaos-report-raw-toggle], [data-chaos-report-download], [data-chaos-report-maximize], [data-chaos-report-close]';

    const setHintsModalMaximized = (maximized) => {
        hintsModalMaximized = !!maximized;
        if (hintsModal) {
            hintsModal.classList.toggle('is-maximized', hintsModalMaximized);
        }
        if (hintsMaximizeBtn) {
            hintsMaximizeBtn.hidden = hintsModalMaximized;
            hintsMaximizeBtn.setAttribute('aria-expanded', hintsModalMaximized ? 'true' : 'false');
        }
        if (hintsMinimizeBtn) {
            hintsMinimizeBtn.hidden = !hintsModalMaximized;
            hintsMinimizeBtn.setAttribute('aria-expanded', hintsModalMaximized ? 'true' : 'false');
        }
        try {
            window.localStorage.setItem('h3270ChaosHintsModalMaximized', hintsModalMaximized ? '1' : '0');
        } catch (_err) {
            // ignore persistence failures
        }
    };

    const setFlowModalMaximized = (maximized) => {
        flowModalMaximized = !!maximized;
        if (flowModal) {
            flowModal.classList.toggle('is-maximized', flowModalMaximized);
        }
        if (flowMaximizeBtn) {
            flowMaximizeBtn.hidden = flowModalMaximized;
            flowMaximizeBtn.setAttribute('aria-expanded', flowModalMaximized ? 'true' : 'false');
        }
        if (flowMinimizeBtn) {
            flowMinimizeBtn.hidden = !flowModalMaximized;
            flowMinimizeBtn.setAttribute('aria-expanded', flowModalMaximized ? 'true' : 'false');
        }
        try {
            window.localStorage.setItem('h3270ChaosFlowModalMaximized', flowModalMaximized ? '1' : '0');
        } catch (_err) {
            // ignore persistence failures
        }
    };

    const markHintsDirty = () => {
        if (hintsModal && !hintsModal.hidden) {
            hintsDirty = true;
        }
    };

    const collectChaosKeyBlacklistFromUI = () => {
        if (!hintsKeyBlacklistInput) {
            return [];
        }
        const seen = new Set();
        return parseListLines(hintsKeyBlacklistInput.value).filter((key) => {
            const normalized = String(key || '').trim();
            if (!normalized || seen.has(normalized)) {
                return false;
            }
            seen.add(normalized);
            return true;
        });
    };

    const renderChaosKeyBlacklist = (keys) => {
        if (!hintsKeyBlacklistInput) {
            return;
        }
        hintsKeyBlacklistInput.value = formatListLines(keys);
    };

    const collectFirstScreenHintFromUI = () => {
        return normalizeScreenHint({
            knownData: firstScreenKnownDataInput ? firstScreenKnownDataInput.value : '',
            knownKeys: firstScreenKnownKeysInput ? firstScreenKnownKeysInput.value : '',
            blockedKeys: firstScreenBlockedKeysInput ? firstScreenBlockedKeysInput.value : '',
            keyAssignments: parseKeyAssignments(firstScreenKeyAssignmentsInput ? firstScreenKeyAssignmentsInput.value : ''),
        });
    };

    const renderFirstScreenHint = (hint) => {
        const normalized = normalizeScreenHint(hint || {});
        if (firstScreenKnownDataInput) {
            firstScreenKnownDataInput.value = formatListLines(normalized.knownData);
        }
        if (firstScreenKnownKeysInput) {
            firstScreenKnownKeysInput.value = formatListLines(normalized.knownKeys);
        }
        if (firstScreenBlockedKeysInput) {
            firstScreenBlockedKeysInput.value = formatListLines(normalized.blockedKeys);
        }
        if (firstScreenKeyAssignmentsInput) {
            firstScreenKeyAssignmentsInput.value = formatKeyAssignments(normalized.keyAssignments || {});
        }
    };

    const parseKnownData = (value) => {
        if (!value) {
            return [];
        }
        return String(value)
            .split(/[\n,]/)
            .map((item) => item.trim())
            .filter((item) => item.length > 0);
    };

    const parseKeyAssignments = (value) => {
        if (!value) {
            return {};
        }
        const out = {};
        String(value)
            .split(/\n+/)
            .map((line) => line.trim())
            .filter((line) => line.length > 0)
            .forEach((line) => {
                const sepIndex = line.indexOf('=>') >= 0
                    ? line.indexOf('=>')
                    : (line.indexOf('=') >= 0 ? line.indexOf('=') : line.indexOf(':'));
                if (sepIndex < 0) {
                    return;
                }
                const sepLen = line.slice(sepIndex, sepIndex + 2) === '=>' ? 2 : 1;
                const label = line.slice(0, sepIndex).trim();
                const key = line.slice(sepIndex + sepLen).trim();
                if (!label || !key) {
                    return;
                }
                out[label] = key;
            });
        return out;
    };

    const normalizeKeyAssignments = (raw) => {
        if (!raw || typeof raw !== 'object') {
            return {};
        }
        const out = {};
        Object.entries(raw).forEach(([rawLabel, rawKey]) => {
            const label = String(rawLabel || '').trim();
            const key = String(rawKey || '').trim();
            if (!label || !key) {
                return;
            }
            out[label] = key;
        });
        return out;
    };

    const formatKeyAssignments = (raw) => {
        const assignments = normalizeKeyAssignments(raw);
        return Object.keys(assignments)
            .sort((a, b) => a.localeCompare(b))
            .map((label) => `${label} = ${assignments[label]}`)
            .join('\n');
    };

    const parseListLines = (value) => {
        if (!value) {
            return [];
        }
        return String(value)
            .split(/[\n,]/)
            .map((item) => item.trim())
            .filter((item) => item.length > 0);
    };

    const formatListLines = (values) => {
        return Array.isArray(values) ? values.map((v) => String(v || '').trim()).filter(Boolean).join('\n') : '';
    };

    const normalizeScreenHint = (raw) => {
        if (!raw || typeof raw !== 'object') {
            return { knownData: [], knownKeys: [], blockedKeys: [], keyAssignments: {} };
        }
        const knownData = Array.isArray(raw.knownData)
            ? raw.knownData.map((v) => String(v || '').trim()).filter(Boolean)
            : parseListLines(raw.knownData || '');
        const knownKeys = Array.isArray(raw.knownKeys)
            ? raw.knownKeys.map((v) => String(v || '').trim()).filter(Boolean)
            : parseListLines(raw.knownKeys || '');
        const blockedKeys = Array.isArray(raw.blockedKeys)
            ? raw.blockedKeys.map((v) => String(v || '').trim()).filter(Boolean)
            : parseListLines(raw.blockedKeys || '');
        return {
            knownData,
            knownKeys,
            blockedKeys,
            keyAssignments: normalizeKeyAssignments(raw.keyAssignments || {}),
        };
    };

    const normalizeScreenHintsMap = (raw) => {
        if (!raw || typeof raw !== 'object') {
            return {};
        }
        const out = {};
        Object.entries(raw).forEach(([hash, hint]) => {
            const key = String(hash || '').trim();
            if (!key) {
                return;
            }
            const norm = normalizeScreenHint(hint);
            if (!norm.knownData.length && !norm.knownKeys.length && !norm.blockedKeys.length && !Object.keys(norm.keyAssignments).length) {
                return;
            }
            out[key] = norm;
        });
        return out;
    };

    const setChaosMapStatus = (message, isError = false) => setStatusLine(mapStatus, message, isError);

    const escapeHtml = (value) => String(value == null ? '' : value)
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&#39;');

    const renderMarkdownInline = (value) => {
        const raw = String(value == null ? '' : value);
        let out = escapeHtml(raw);
        out = out.replace(/`([^`]+?)`/g, (_m, code) => `<code>${code}</code>`);
        out = out.replace(/\*\*([^*]+?)\*\*/g, '<strong>$1</strong>');
        out = out.replace(/\*([^*]+?)\*/g, '<em>$1</em>');
        // The report writes line breaks inside table cells as <br>. Everything
        // was escaped above, so the only tag that comes back is this one.
        out = out.replace(/&lt;br\s*\/?&gt;/gi, '<br>');
        return out;
    };

    const parseMarkdownTableRow = (line) => {
        const src = String(line || '').trim();
        if (!src.includes('|')) {
            return [];
        }
        // A pipe the report escaped (\|) is part of a cell, not the end of one.
        const escapedPipe = '\u0000';
        let body = src.replace(/\\\|/g, escapedPipe);
        if (body.startsWith('|')) {
            body = body.slice(1);
        }
        if (body.endsWith('|')) {
            body = body.slice(0, -1);
        }
        return body.split('|').map((cell) => cell.split(escapedPipe).join('|').trim());
    };

    const isMarkdownTableSeparator = (line) => {
        const cells = parseMarkdownTableRow(line);
        if (!cells.length) {
            return false;
        }
        return cells.every((cell) => /^:?-{3,}:?$/.test(cell.replace(/\s+/g, '')));
    };

    const renderMarkdownTable = (headerLine, bodyLines) => {
        const headers = parseMarkdownTableRow(headerLine);
        if (!headers.length) {
            return '';
        }
        const rows = (Array.isArray(bodyLines) ? bodyLines : [])
            .map((line) => parseMarkdownTableRow(line))
            .filter((cells) => cells.length > 0);
        const th = headers.map((cell) => `<th>${renderMarkdownInline(cell)}</th>`).join('');
        const tbody = rows.map((cells) => {
            const tds = headers.map((_, idx) => `<td>${renderMarkdownInline(cells[idx] || '')}</td>`).join('');
            return `<tr>${tds}</tr>`;
        }).join('');
        return `<table><thead><tr>${th}</tr></thead><tbody>${tbody}</tbody></table>`;
    };

    const renderMarkdownList = (items) => {
        const list = Array.isArray(items) ? items : [];
        if (!list.length) {
            return '';
        }
        const root = { indent: -1, items: [] };
        const stack = [root];
        list.forEach((item) => {
            const indent = Math.max(0, Number(item && item.indent) || 0);
            const text = String(item && item.text || '').trim();
            if (!text) {
                return;
            }
            while (stack.length > 1 && indent < stack[stack.length - 1].indent) {
                stack.pop();
            }
            const isTopLevelRootItem = stack.length === 1 && indent === 0;
            if (indent > stack[stack.length - 1].indent && !isTopLevelRootItem) {
                const parentItems = stack[stack.length - 1].items;
                const parent = parentItems[parentItems.length - 1];
                if (parent) {
                    parent.children = parent.children || { indent, items: [] };
                    stack.push(parent.children);
                }
            } else if (indent < stack[stack.length - 1].indent) {
                while (stack.length > 1 && indent < stack[stack.length - 1].indent) {
                    stack.pop();
                }
            }
            stack[stack.length - 1].items.push({ text, children: null });
        });
        const renderNodeList = (nodeList) => {
            if (!nodeList || !Array.isArray(nodeList.items) || !nodeList.items.length) {
                return '';
            }
            const itemsHtml = nodeList.items.map((item) => (
                `<li>${renderMarkdownInline(item.text)}${item.children ? renderNodeList(item.children) : ''}</li>`
            )).join('');
            return `<ul>${itemsHtml}</ul>`;
        };
        return renderNodeList(root);
    };

    const renderChaosReportMarkdown = (markdown) => {
        const src = String(markdown == null ? '' : markdown).replace(/\r/g, '');
        const lines = src.split('\n');
        const html = [];
        let i = 0;

        const flushParagraph = (paragraphLines) => {
            const joined = paragraphLines.map((line) => String(line || '').trim()).join(' ');
            if (joined) {
                html.push(`<p>${renderMarkdownInline(joined)}</p>`);
            }
        };

        while (i < lines.length) {
            const line = lines[i];
            const trimmed = String(line || '').trim();

            if (!trimmed) {
                i += 1;
                continue;
            }

            if (/^```/.test(trimmed)) {
                const fenceLang = trimmed.slice(3).trim();
                const codeLines = [];
                i += 1;
                while (i < lines.length && !/^```/.test(String(lines[i] || '').trim())) {
                    codeLines.push(lines[i]);
                    i += 1;
                }
                if (i < lines.length) {
                    i += 1;
                }
                if (fenceLang.toLowerCase() === 'mermaid') {
                    // The report file carries the flow as diagram source for
                    // tools that can draw it. Shown here it is a page of
                    // arrows nobody can read, and the same flow is already
                    // drawn, interactively, one button away.
                    html.push(`
                        <div class="chaos-report-flow-callout">
                            <p><strong>Flow diagram.</strong> The downloaded report includes this as diagram source (${codeLines.length} line${codeLines.length === 1 ? '' : 's'}; see Raw Markdown). Open it here as an interactive diagram instead.</p>
                            <button type="button" data-chaos-report-open-flow>Open flow diagram</button>
                        </div>
                    `);
                    continue;
                }
                const langAttr = fenceLang ? ` data-lang="${escapeHtml(fenceLang)}"` : '';
                html.push(`<pre><code${langAttr}>${escapeHtml(codeLines.join('\n'))}</code></pre>`);
                continue;
            }

            const headingMatch = trimmed.match(/^(#{1,4})\s+(.+)$/);
            if (headingMatch) {
                const level = Math.min(4, headingMatch[1].length);
                html.push(`<h${level}>${renderMarkdownInline(headingMatch[2])}</h${level}>`);
                i += 1;
                continue;
            }

            if ((i + 1) < lines.length && String(lines[i + 1] || '').includes('|') && isMarkdownTableSeparator(lines[i + 1])) {
                const headerLine = line;
                i += 2; // skip header + separator
                const bodyLines = [];
                while (i < lines.length) {
                    const rowLine = String(lines[i] || '');
                    if (!rowLine.trim() || !rowLine.includes('|')) {
                        break;
                    }
                    bodyLines.push(rowLine);
                    i += 1;
                }
                html.push(renderMarkdownTable(headerLine, bodyLines));
                continue;
            }

            if (/^\s*-\s+/.test(line)) {
                const items = [];
                while (i < lines.length && /^\s*-\s+/.test(String(lines[i] || ''))) {
                    const listLine = String(lines[i] || '');
                    const indent = (listLine.match(/^\s*/) || [''])[0].length;
                    const text = listLine.replace(/^\s*-\s+/, '');
                    items.push({ indent, text });
                    i += 1;
                }
                html.push(renderMarkdownList(items));
                continue;
            }

            const paragraphLines = [line];
            i += 1;
            while (i < lines.length) {
                const next = String(lines[i] || '');
                const nextTrimmed = next.trim();
                if (!nextTrimmed) {
                    break;
                }
                if (/^```/.test(nextTrimmed) || /^(#{1,4})\s+/.test(nextTrimmed) || /^\s*-\s+/.test(next)) {
                    break;
                }
                if ((i + 1) < lines.length && next.includes('|') && isMarkdownTableSeparator(String(lines[i + 1] || ''))) {
                    break;
                }
                paragraphLines.push(next);
                i += 1;
            }
            flushParagraph(paragraphLines);
        }

        return `<article class="chaos-report-markdown">${html.join('')}</article>`;
    };

    const markScreenHintDraftDirty = (hash, draft) => {
        if (!hash) {
            return;
        }
        screenHintDrafts[hash] = {
            ...normalizeScreenHint(draft || {}),
            _dirty: true,
        };
    };

    const effectiveScreenHintForHash = (hash) => {
        if (hash && screenHintDrafts[hash]) {
            return normalizeScreenHint(screenHintDrafts[hash]);
        }
        if (hash && screenHintsByHash[hash]) {
            return normalizeScreenHint(screenHintsByHash[hash]);
        }
        return { knownData: [], knownKeys: [], keyAssignments: {} };
    };

    const mergeScreenHintsForHashes = (hashes) => {
        const out = { knownData: [], knownKeys: [], blockedKeys: [], keyAssignments: {} };
        const dataSeen = new Set();
        const keySeen = new Set();
        const blockedKeySeen = new Set();
        (Array.isArray(hashes) ? hashes : []).forEach((hash) => {
            const hint = effectiveScreenHintForHash(hash);
            (hint.knownData || []).forEach((value) => {
                const v = String(value || '').trim();
                if (!v || dataSeen.has(v)) {
                    return;
                }
                dataSeen.add(v);
                out.knownData.push(v);
            });
            (hint.knownKeys || []).forEach((value) => {
                const v = String(value || '').trim();
                if (!v || keySeen.has(v)) {
                    return;
                }
                keySeen.add(v);
                out.knownKeys.push(v);
            });
            (hint.blockedKeys || []).forEach((value) => {
                const v = String(value || '').trim();
                if (!v || blockedKeySeen.has(v)) {
                    return;
                }
                blockedKeySeen.add(v);
                out.blockedKeys.push(v);
            });
            Object.entries(normalizeKeyAssignments(hint.keyAssignments || {})).forEach(([label, key]) => {
                if (!out.keyAssignments[label]) {
                    out.keyAssignments[label] = key;
                }
            });
        });
        return out;
    };

    const markScreenHintDraftDirtyForHashes = (hashes, draft) => {
        (Array.isArray(hashes) ? hashes : []).forEach((hash) => {
            markScreenHintDraftDirty(hash, draft);
        });
    };

    const saveScreenHint = async (screenHash, draft, options = {}) => {
        if (!screenHash) {
            return false;
        }
        const payload = {
            screenHash,
            knownData: draft.knownData || [],
            knownKeys: draft.knownKeys || [],
            blockedKeys: draft.blockedKeys || [],
            keyAssignments: draft.keyAssignments || {},
        };
        try {
            const resp = await fetch('/chaos/screen-hints', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(payload),
            });
            let body = {};
            try {
                body = await resp.json();
            } catch (_parseErr) {
                body = {};
            }
            if (!resp.ok) {
                throw new Error(body.error || 'request failed');
            }
            screenHintsByHash = normalizeScreenHintsMap(body.screenHints || {});
            if (screenHintDrafts[screenHash]) {
                screenHintDrafts[screenHash] = {
                    ...effectiveScreenHintForHash(screenHash),
                    _dirty: false,
                };
            }
            if (!options.quiet) {
                const msg = options.successMessage || `Saved screen hints for ${screenHash}.`;
                setChaosMapStatus(msg);
                notifyUi(msg, 'success');
            }
            if (!options.skipRender && mapModal && !mapModal.hidden) {
                renderChaosMap();
            }
            return true;
        } catch (err) {
            if (!options.quiet) {
                const msg = (err && err.message) ? err.message : 'Failed to save screen hints.';
                setChaosMapStatus(msg, true);
                notifyUi(msg, 'error');
            }
            return false;
        }
    };

    const saveScreenHintsForHashes = async (hashes, draft) => {
        const targets = Array.isArray(hashes)
            ? hashes.map((h) => String(h || '').trim()).filter(Boolean)
            : [];
        if (!targets.length) {
            return false;
        }
        let okCount = 0;
        for (let i = 0; i < targets.length; i += 1) {
            const hash = targets[i];
            const ok = await saveScreenHint(hash, draft, { quiet: true, skipRender: true });
            if (!ok) {
                const msg = `Failed to save screen hints for ${hash}.`;
                setChaosMapStatus(msg, true);
                notifyUi(msg, 'error');
                if (mapModal && !mapModal.hidden) {
                    renderChaosMap();
                }
                return false;
            }
            okCount += 1;
        }
        {
            const msg = okCount > 1
                ? `Saved screen hints to ${okCount} merged screen variants.`
                : `Saved screen hints for ${targets[0]}.`;
            setChaosMapStatus(msg);
            notifyUi(msg, 'success');
        }
        if (mapModal && !mapModal.hidden) {
            renderChaosMap();
        }
        return true;
    };

    const minimapMarkupForArea = (area) => {
        const fields = area && area.fieldMetadata && typeof area.fieldMetadata === 'object'
            ? Object.values(area.fieldMetadata).filter((f) => f && typeof f === 'object')
            : [];
        if (!fields.length) {
            return '<div class="chaos-map-minimap"></div>';
        }
        let maxRow = 24;
        let maxCol = 80;
        fields.forEach((f) => {
            const row = Number(f.row) || 1;
            const col = Number(f.column) || 1;
            const len = Math.max(1, Number(f.length) || 1);
            if (row > maxRow) {
                maxRow = row;
            }
            const endCol = col + len - 1;
            if (endCol > maxCol) {
                maxCol = endCol;
            }
        });
        const inner = fields.map((f) => {
            const row = Math.max(1, Number(f.row) || 1);
            const col = Math.max(1, Number(f.column) || 1);
            const len = Math.max(1, Number(f.length) || 1);
            const left = ((col - 1) / maxCol) * 100;
            const top = ((row - 1) / maxRow) * 100;
            const width = Math.max(2, (len / maxCol) * 100);
            const height = Math.max(6, (1 / maxRow) * 100 * 1.25);
            const classes = [
                'chaos-map-field-rect',
                f.numeric ? 'is-numeric' : '',
                f.hidden ? 'is-hidden' : '',
            ].filter(Boolean).join(' ');
            const title = `R${row} C${col} L${len}${f.numeric ? ' numeric' : ''}${f.hidden ? ' hidden' : ''}`;
            return `<div class="${classes}" title="${title}" style="left:${left}%;top:${top}%;width:${width}%;height:${height}%"></div>`;
        }).join('');
        return `<div class="chaos-map-minimap">${inner}</div>`;
    };

    const chaosMapFlowTemplatePreviewSignature = (area) => {
        if (!area || typeof area !== 'object') {
            return '';
        }
        const rawText = String(area.previewText || '');
        if (!rawText) {
            return '';
        }
        const width = Math.max(0, Number(area.previewWidth) || Number(area.screenWidth) || 0);
        const height = Math.max(0, Number(area.previewHeight) || Number(area.screenHeight) || 0);
        if (width <= 0 || height <= 0) {
            return rawText.replace(/\r/g, '');
        }

        const lines = rawText.replace(/\r/g, '').split('\n');
        const grid = Array.from({ length: height }, (_, y) =>
            Array.from(String(lines[y] || '').padEnd(width, ' ').slice(0, width))
        );
        const fieldMeta = area.fieldMetadata && typeof area.fieldMetadata === 'object'
            ? Object.values(area.fieldMetadata).filter((f) => f && typeof f === 'object')
            : [];

        fieldMeta.forEach((f) => {
            let x = Math.max(1, Number(f.column) || 1) - 1;
            let y = Math.max(1, Number(f.row) || 1) - 1;
            let remaining = Math.max(1, Number(f.length) || 1);
            while (remaining > 0) {
                if (y < 0 || y >= height) {
                    break;
                }
                if (x >= 0 && x < width) {
                    grid[y][x] = ' ';
                    remaining -= 1;
                }
                x += 1;
                if (x >= width) {
                    x = 0;
                    y += 1;
                }
            }
        });

        return grid
            .map((row) => row.join('').replace(/\s+$/g, ''))
            .join('\n');
    };

    const chaosMapFlowTemplateKey = (area) => {
        if (!area || typeof area !== 'object') {
            return '';
        }
        const dedupSignature = String(area.dedupSignature || '').trim();
        if (dedupSignature) {
            const screenWidth = Number(area.screenWidth) || 0;
            const screenHeight = Number(area.screenHeight) || 0;
            return [
                'dedup',
                `sw:${screenWidth}`,
                `sh:${screenHeight}`,
                `sig:${dedupSignature}`,
            ].join('|');
        }
        const label = String(area.label || '').trim().toLowerCase();
        const screenWidth = Number(area.screenWidth) || 0;
        const screenHeight = Number(area.screenHeight) || 0;
        const previewWidth = Number(area.previewWidth) || 0;
        const previewHeight = Number(area.previewHeight) || 0;
        const fieldCount = Number(area.fieldCount) || 0;
        const inputFieldCount = Number(area.inputFieldCount) || 0;
        const numericFieldCount = Number(area.numericFieldCount) || 0;
        const hiddenFieldCount = Number(area.hiddenFieldCount) || 0;
        const previewSignature = chaosMapFlowTemplatePreviewSignature(area);
        const fieldMeta = area.fieldMetadata && typeof area.fieldMetadata === 'object'
            ? Object.values(area.fieldMetadata).filter((f) => f && typeof f === 'object')
            : [];
        const metaParts = fieldMeta
            .map((f) => ({
                row: Math.max(0, Number(f.row) || 0),
                col: Math.max(0, Number(f.column) || 0),
                len: Math.max(0, Number(f.length) || 0),
                num: !!f.numeric,
                hid: !!f.hidden,
                mul: !!f.multiLine,
            }))
            .sort((a, b) =>
                a.row - b.row ||
                a.col - b.col ||
                a.len - b.len ||
                Number(a.num) - Number(b.num) ||
                Number(a.hid) - Number(b.hid) ||
                Number(a.mul) - Number(b.mul)
            )
            .map((f) => `${f.row},${f.col},${f.len},${f.num ? 1 : 0},${f.hid ? 1 : 0},${f.mul ? 1 : 0}`);
        return [
            `l:${label}`,
            `sw:${screenWidth}`,
            `sh:${screenHeight}`,
            `pw:${previewWidth}`,
            `ph:${previewHeight}`,
            `fc:${fieldCount}`,
            `ifc:${inputFieldCount}`,
            `nfc:${numericFieldCount}`,
            `hfc:${hiddenFieldCount}`,
            `fm:${metaParts.join(';')}`,
            `pv:${previewSignature}`,
        ].join('|');
    };

    // Settles with 'confirm', 'alt' or false (cancelled, Escape, backdrop).
    const settleChaosConfirm = (choice) => {
        const resolve = pendingChaosConfirmResolve;
        pendingChaosConfirmResolve = null;
        closeChaosModal(confirmModal);
        if (resolve) {
            resolve(choice === true ? 'confirm' : (choice || false));
        }
    };

    const setFlowScreenHintsModalMaximized = (maximized) => {
        flowScreenHintsModalMaximized = !!maximized;
        if (flowScreenHintsModal) {
            flowScreenHintsModal.classList.toggle('is-maximized', flowScreenHintsModalMaximized);
        }
        if (flowScreenHintsMaximizeBtn) {
            flowScreenHintsMaximizeBtn.hidden = flowScreenHintsModalMaximized;
            flowScreenHintsMaximizeBtn.setAttribute('aria-expanded', flowScreenHintsModalMaximized ? 'true' : 'false');
        }
        if (flowScreenHintsMinimizeBtn) {
            flowScreenHintsMinimizeBtn.hidden = !flowScreenHintsModalMaximized;
            flowScreenHintsMinimizeBtn.setAttribute('aria-expanded', flowScreenHintsModalMaximized ? 'true' : 'false');
        }
        try {
            window.localStorage.setItem('h3270ChaosFlowScreenHintsModalMaximized', flowScreenHintsModalMaximized ? '1' : '0');
        } catch (_err) {
            // ignore persistence failures
        }
    };

    // One confirmation dialog for the whole feature. With altLabel it offers a
    // third way out (Save / Discard / Keep editing); the promise settles with
    // 'confirm', 'alt' or false.
    const promptChaosChoice = ({
        title,
        message,
        confirmLabel = 'Confirm',
        altLabel = '',
        cancelLabel = 'Cancel',
        danger = true,
        altDanger = false,
    } = {}) => {
        if (!confirmModal || !confirmOkButton) {
            return Promise.resolve(false);
        }
        if (pendingChaosConfirmResolve) {
            const resolve = pendingChaosConfirmResolve;
            pendingChaosConfirmResolve = null;
            resolve(false);
        }
        if (confirmTitle) {
            confirmTitle.textContent = title || 'Confirm action';
        }
        if (confirmMessage) {
            confirmMessage.textContent = message || 'Are you sure?';
        }
        confirmOkButton.textContent = confirmLabel || 'Confirm';
        confirmOkButton.classList.toggle('danger', !!danger);
        if (confirmCancelLabelButton) {
            confirmCancelLabelButton.textContent = cancelLabel || 'Cancel';
        }
        if (confirmAltButton) {
            confirmAltButton.hidden = !altLabel;
            confirmAltButton.textContent = altLabel || '';
            confirmAltButton.classList.toggle('danger', !!altDanger);
        }
        return new Promise((resolve) => {
            pendingChaosConfirmResolve = resolve;
            // A destructive confirmation opens on the way out, not on the
            // button that destroys something: Enter on a dialog nobody read
            // should not delete anything.
            openChaosModal(
                confirmModal,
                danger ? '[data-chaos-confirm-cancel-label]' : '[data-chaos-confirm-ok]',
                { keepPrevious: true }
            );
        });
    };

    const promptChaosConfirm = (options = {}) => promptChaosChoice({ ...options, altLabel: '' })
        .then((choice) => choice === 'confirm');

    const syncRunsSelectionUI = () => {
        const selectedCount = selectedChaosRunIDs.size;
        if (runsDeleteSelectedBtn) {
            runsDeleteSelectedBtn.disabled = selectedCount === 0;
            runsDeleteSelectedBtn.textContent = selectedCount > 0 ? `Delete selected (${selectedCount})` : 'Delete selected';
        }
        if (runsCompareBtn) {
            // Comparing is between two runs, and the button says so until
            // exactly two are ticked.
            runsCompareBtn.disabled = selectedCount !== 2;
            runsCompareBtn.textContent = selectedCount === 2 ? 'Compare the two selected' : 'Compare (select two)';
        }
        if (runsClearSelectionBtn) {
            runsClearSelectionBtn.disabled = selectedCount === 0;
        }
        if (runsSelectAllBtn) {
            const selectableCount = Array.isArray(currentRuns) ? currentRuns.length : 0;
            runsSelectAllBtn.disabled = selectableCount === 0 || selectedCount === selectableCount;
        }
        if (runsList) {
            runsList.querySelectorAll('[data-chaos-run-select]').forEach((cb) => {
                const rid = String(cb.getAttribute('data-chaos-run-select') || '');
                cb.checked = selectedChaosRunIDs.has(rid);
                const row = cb.closest('.chaos-run-item');
                if (row) {
                    row.classList.toggle('is-selected', cb.checked);
                }
            });
        }
    };

    const setRunSelected = (rid, selected) => {
        const id = String(rid || '').trim();
        if (!id) {
            return;
        }
        if (selected) {
            selectedChaosRunIDs.add(id);
        } else {
            selectedChaosRunIDs.delete(id);
        }
        syncRunsSelectionUI();
    };

    const chaosMapCardScreenPreviewMarkup = (area) => {
        if (!area || typeof area !== 'object') {
            return `
                <div class="chaos-map-card-screen-preview is-empty">
                    <div class="chaos-map-card-screen-preview-meta">No captured screen preview</div>
                </div>
            `;
        }

        const rawText = String(area.previewText || '');
        const screenW = Math.max(0, Number(area.previewWidth) || Number(area.screenWidth) || 0);
        const screenH = Math.max(0, Number(area.previewHeight) || Number(area.screenHeight) || 0);
        const maxCols = 64;
        const maxRows = 10;
        let lines = chaosFlowPreviewLines(rawText, maxCols, maxRows);
        let placeholder = false;
        if (!lines.length) {
            placeholder = true;
            lines = [
                'No captured screen preview'.padEnd(24, ' '),
                'Continue exploring to capture one'.padEnd(30, ' '),
            ];
        }

        const charWidth = 6.1;
        const lineHeight = 9.0;
        const padX = 9;
        const padY = 10;
        const cols = Math.max(1, lines.reduce((max, line) => Math.max(max, String(line || '').length), 1));
        const rows = Math.max(1, lines.length);
        const svgW = Math.round((cols * charWidth) + (padX * 2));
        const svgH = Math.round((rows * lineHeight) + (padY * 2));
        const lineMarkup = lines.map((line, index) => {
            const y = padY + 7 + (index * lineHeight);
            return `<tspan x="${padX}" y="${y.toFixed(1)}">${chaosFlowSvgPreserveLine(line)}</tspan>`;
        }).join('');

        const totalRows = rawText ? rawText.replace(/\r/g, '').split('\n').length : 0;
        const truncatedRows = Math.max(0, totalRows - rows);
        const dimsText = screenW > 0 && screenH > 0 ? `${screenH}x${screenW}` : '';
        const metaParts = [
            dimsText,
            placeholder ? 'preview unavailable' : 'captured preview',
            truncatedRows > 0 ? `+${truncatedRows} more row${truncatedRows === 1 ? '' : 's'}` : '',
        ].filter(Boolean);

        return `
            <div class="chaos-map-card-screen-preview${placeholder ? ' is-empty' : ''}">
                <svg class="chaos-map-card-screen-preview-svg" viewBox="0 0 ${svgW} ${svgH}" preserveAspectRatio="xMinYMin meet" aria-hidden="true">
                    <rect class="chaos-map-card-screen-preview-bg${placeholder ? ' is-placeholder' : ''}" x="0" y="0" width="${svgW}" height="${svgH}" rx="8" ry="8"></rect>
                    <text class="chaos-map-card-screen-preview-text${placeholder ? ' is-placeholder' : ''}" xml:space="preserve">${lineMarkup}</text>
                </svg>
                <div class="chaos-map-card-screen-preview-meta">${escapeHtml(metaParts.join(' | '))}</div>
            </div>
        `;
    };

    const chaosMapFlowTemplateId = (key) => {
        const raw = String(key || '');
        let hash = 2166136261;
        for (let i = 0; i < raw.length; i += 1) {
            hash ^= raw.charCodeAt(i);
            hash = Math.imul(hash, 16777619);
        }
        return `tpl-${(hash >>> 0).toString(16).padStart(8, '0')}`;
    };

    const buildChaosMapTemplateGroups = (areas) => {
        const allAreas = Array.isArray(areas) ? areas : [];
        const groupsById = new Map();
        const rawHashToTemplateId = new Map();

        allAreas.forEach((area) => {
            if (!area || typeof area !== 'object') {
                return;
            }
            const rawHash = String(area.hash || '').trim();
            if (!rawHash) {
                return;
            }
            const templateKey = chaosMapFlowTemplateKey(area) || `hash:${rawHash}`;
            const templateId = chaosMapFlowTemplateId(templateKey);
            rawHashToTemplateId.set(rawHash, templateId);
            let group = groupsById.get(templateId);
            if (!group) {
                group = {
                    templateId,
                    templateKey,
                    label: String(area.label || rawHash || 'Screen').trim() || 'Screen',
                    visits: 0,
                    fieldCount: Number(area.fieldCount) || 0,
                    inputFieldCount: Number(area.inputFieldCount) || 0,
                    numericFieldCount: Number(area.numericFieldCount) || 0,
                    hiddenFieldCount: Number(area.hiddenFieldCount) || 0,
                    representativeHash: rawHash,
                    representativeArea: area,
                    lastSeenMs: Date.parse(area.lastSeen || '') || 0,
                    memberHashes: [],
                    memberAreas: [],
                    keyPresses: {},
                    destinationLabelByTemplateId: {},
                };
                groupsById.set(templateId, group);
            }
            group.visits += Number(area.visits) || 0;
            group.fieldCount = Math.max(group.fieldCount, Number(area.fieldCount) || 0);
            group.inputFieldCount = Math.max(group.inputFieldCount, Number(area.inputFieldCount) || 0);
            group.numericFieldCount = Math.max(group.numericFieldCount, Number(area.numericFieldCount) || 0);
            group.hiddenFieldCount = Math.max(group.hiddenFieldCount, Number(area.hiddenFieldCount) || 0);
            group.memberHashes.push(rawHash);
            group.memberAreas.push(area);

            const areaVisits = Number(area.visits) || 0;
            const repVisits = Number(group.representativeArea && group.representativeArea.visits) || 0;
            const areaLastSeenMs = Date.parse(area.lastSeen || '') || 0;
            if (!group.representativeArea
                || areaVisits > repVisits
                || (areaVisits === repVisits && areaLastSeenMs > group.lastSeenMs)
                || (areaVisits === repVisits && areaLastSeenMs === group.lastSeenMs && rawHash < group.representativeHash)) {
                group.representativeArea = area;
                group.representativeHash = rawHash;
                group.lastSeenMs = areaLastSeenMs;
                if (String(area.label || '').trim()) {
                    group.label = String(area.label || '').trim();
                }
            } else if (!String(group.label || '').trim() && String(area.label || '').trim()) {
                group.label = String(area.label || '').trim();
            }
        });

        groupsById.forEach((group) => {
            group.destinationLabelByTemplateId = {};
        });
        groupsById.forEach((group) => {
            group.memberAreas.forEach((area) => {
                const keyPresses = area && area.keyPresses && typeof area.keyPresses === 'object' ? area.keyPresses : {};
                Object.entries(keyPresses).forEach(([key, kp]) => {
                    if (!kp || typeof kp !== 'object') {
                        return;
                    }
                    if (!group.keyPresses[key]) {
                        group.keyPresses[key] = { presses: 0, progressions: 0, destinations: {} };
                    }
                    const target = group.keyPresses[key];
                    target.presses += Number(kp.presses) || 0;
                    target.progressions += Number(kp.progressions) || 0;
                    const dests = kp.destinations && typeof kp.destinations === 'object' ? kp.destinations : {};
                    Object.entries(dests).forEach(([toRawHash, countRaw]) => {
                        const toHash = String(toRawHash || '').trim();
                        const toTemplateId = rawHashToTemplateId.get(toHash) || '';
                        const count = Number(countRaw) || 0;
                        if (!toTemplateId || count <= 0 || toTemplateId === group.templateId) {
                            return;
                        }
                        target.destinations[toTemplateId] = (target.destinations[toTemplateId] || 0) + count;
                        if (!group.destinationLabelByTemplateId[toTemplateId]) {
                            const destGroup = groupsById.get(toTemplateId);
                            group.destinationLabelByTemplateId[toTemplateId] = destGroup && destGroup.label ? destGroup.label : toTemplateId;
                        }
                    });
                });
            });
        });

        const groups = Array.from(groupsById.values()).sort((a, b) =>
            b.visits - a.visits ||
            b.lastSeenMs - a.lastSeenMs ||
            a.templateId.localeCompare(b.templateId)
        );

        return { groups, rawHashToTemplateId };
    };

    const buildChaosMapFlowModel = (areas) => {
        const allAreas = Array.isArray(areas) ? areas : [];
        const groupedByTemplate = new Map();
        const rawHashToTemplateId = new Map();

        allAreas.forEach((area) => {
            if (!area || typeof area !== 'object') {
                return;
            }
            const rawHash = String(area.hash || '').trim();
            if (!rawHash) {
                return;
            }
            const areaVisits = Number(area.visits) || 0;
            const areaFirstSeenMs = Date.parse(area.firstSeen || '') || 0;
            const areaLastSeenMs = Date.parse(area.lastSeen || '') || 0;
            const templateKey = chaosMapFlowTemplateKey(area) || `hash:${rawHash}`;
            const templateId = chaosMapFlowTemplateId(templateKey);
            rawHashToTemplateId.set(rawHash, templateId);
            let group = groupedByTemplate.get(templateId);
            if (!group) {
                group = {
                    templateId,
                    templateKey,
                    label: String(area.label || rawHash || 'Screen').trim() || 'Screen',
                    visits: 0,
                    inputs: Number(area.inputFieldCount) || 0,
                    keysSet: new Set(),
                    variants: 0,
                    sampleHash: rawHash,
                    sampleArea: area,
                    sampleVisits: areaVisits,
                    sampleLastSeenMs: areaLastSeenMs,
                    firstSeenMs: areaFirstSeenMs,
                    memberHashes: [],
                };
                groupedByTemplate.set(templateId, group);
            }
            group.visits += areaVisits;
            group.memberHashes.push(rawHash);
            group.inputs = Math.max(group.inputs, Number(area.inputFieldCount) || 0);
            group.variants += 1;
            if (areaFirstSeenMs > 0 && (!group.firstSeenMs || areaFirstSeenMs < group.firstSeenMs)) {
                group.firstSeenMs = areaFirstSeenMs;
            }
            if (!String(group.label || '').trim() && String(area.label || '').trim()) {
                group.label = String(area.label || '').trim();
            }
            const hasPreview = String(area.previewText || '').trim() !== '';
            const sampleHasPreview = !!(group.sampleArea && String(group.sampleArea.previewText || '').trim() !== '');
            const shouldReplaceSample = !group.sampleArea
                || (hasPreview && !sampleHasPreview)
                || (hasPreview === sampleHasPreview && (
                    areaVisits > (Number(group.sampleVisits) || 0)
                    || (areaVisits === (Number(group.sampleVisits) || 0) && areaLastSeenMs > (Number(group.sampleLastSeenMs) || 0))
                ));
            if (shouldReplaceSample) {
                group.sampleArea = area;
                group.sampleHash = rawHash;
                group.sampleVisits = areaVisits;
                group.sampleLastSeenMs = areaLastSeenMs;
            }
            const keyPresses = area.keyPresses && typeof area.keyPresses === 'object' ? area.keyPresses : {};
            Object.keys(keyPresses).forEach((key) => group.keysSet.add(String(key)));
        });

        const groupedNodes = Array.from(groupedByTemplate.values())
            .map((group) => {
                const sampleArea = group.sampleArea && typeof group.sampleArea === 'object' ? group.sampleArea : {};
                return {
                    hash: group.templateId,
                    label: group.label || 'Screen',
                    visits: group.visits,
                    inputs: group.inputs,
                    keys: group.keysSet.size,
                    variants: group.variants,
                    sampleHash: group.sampleHash,
                    firstSeenMs: Number(group.firstSeenMs) || 0,
                    memberHashes: Array.isArray(group.memberHashes) ? group.memberHashes.slice() : [],
                    previewText: typeof sampleArea.previewText === 'string' ? sampleArea.previewText : '',
                    previewWidth: Number(sampleArea.previewWidth) || 0,
                    previewHeight: Number(sampleArea.previewHeight) || 0,
                    screenWidth: Number(sampleArea.screenWidth) || 0,
                    screenHeight: Number(sampleArea.screenHeight) || 0,
                };
            })
            .sort((a, b) =>
                compareChaosFlowTimestampAscUnknownLast(
                    Number(a && a.firstSeenMs) || 0,
                    Number(b && b.firstSeenMs) || 0
                ) || String(a && a.hash || '').localeCompare(String(b && b.hash || ''))
            );

        groupedNodes.forEach((node) => {
            const nodeHash = String(node && node.hash || '').trim();
            if (!nodeHash) {
                return;
            }
            if (!chaosFlowStableDiscoveryRankByHash.has(nodeHash)) {
                chaosFlowStableDiscoveryRankByHash.set(nodeHash, chaosFlowStableDiscoveryRankSeq);
                chaosFlowStableDiscoveryRankSeq += 1;
            }
            node.stableDiscoveryRank = chaosFlowStableDiscoveryRankByHash.get(nodeHash);
        });
        groupedNodes.sort((a, b) => compareChaosFlowNodesByDiscovery(a, b));

        const maxNodes = 28;
        const nodes = groupedNodes.slice(0, maxNodes);
        const includedTemplateIds = new Set(nodes.map((node) => node.hash));

        const edgeMap = new Map();
        allAreas.forEach((area) => {
            const fromRawHash = String(area && area.hash || '').trim();
            const fromTemplateId = rawHashToTemplateId.get(fromRawHash);
            if (!fromTemplateId || !includedTemplateIds.has(fromTemplateId)) {
                return;
            }
            const keyPresses = area && area.keyPresses && typeof area.keyPresses === 'object' ? area.keyPresses : {};
            Object.entries(keyPresses).forEach(([key, kp]) => {
                if (!kp || typeof kp !== 'object') {
                    return;
                }
                const dests = kp.destinations && typeof kp.destinations === 'object' ? kp.destinations : {};
                Object.entries(dests).forEach(([toHashRaw, countRaw]) => {
                    const toRawHash = String(toHashRaw || '').trim();
                    const toTemplateId = rawHashToTemplateId.get(toRawHash);
                    const count = Number(countRaw) || 0;
                    if (!toTemplateId || count <= 0 || !includedTemplateIds.has(toTemplateId) || toTemplateId === fromTemplateId) {
                        return;
                    }
                    const edgeKey = `${fromTemplateId}->${toTemplateId}`;
                    const existing = edgeMap.get(edgeKey) || {
                        fromHash: fromTemplateId,
                        toHash: toTemplateId,
                        count: 0,
                        keys: new Map(),
                    };
                    existing.count += count;
                    existing.keys.set(key, (existing.keys.get(key) || 0) + count);
                    edgeMap.set(edgeKey, existing);
                });
            });
        });

        const nodeRankForEdges = new Map(nodes.map((node, idx) => [node.hash, Number(node.stableDiscoveryRank) >= 0 ? Number(node.stableDiscoveryRank) : idx]));
        const edges = Array.from(edgeMap.values())
            .sort((a, b) => {
                const aFrom = nodeRankForEdges.has(a.fromHash) ? nodeRankForEdges.get(a.fromHash) : Number.MAX_SAFE_INTEGER;
                const bFrom = nodeRankForEdges.has(b.fromHash) ? nodeRankForEdges.get(b.fromHash) : Number.MAX_SAFE_INTEGER;
                if (aFrom !== bFrom) {
                    return aFrom - bFrom;
                }
                const aTo = nodeRankForEdges.has(a.toHash) ? nodeRankForEdges.get(a.toHash) : Number.MAX_SAFE_INTEGER;
                const bTo = nodeRankForEdges.has(b.toHash) ? nodeRankForEdges.get(b.toHash) : Number.MAX_SAFE_INTEGER;
                if (aTo !== bTo) {
                    return aTo - bTo;
                }
                return (Number(b.count) || 0) - (Number(a.count) || 0)
                    || a.fromHash.localeCompare(b.fromHash)
                    || a.toHash.localeCompare(b.toHash);
            })
            .slice(0, 256)
            .map((edge) => {
                const topKeys = Array.from(edge.keys.entries())
                    .sort((a, b) => b[1] - a[1] || String(a[0]).localeCompare(String(b[0])))
                    .slice(0, 2)
                    .map(([key, count]) => `${key}${count > 1 ? ` (${count})` : ''}`);
                return {
                    fromHash: edge.fromHash,
                    toHash: edge.toHash,
                    count: edge.count,
                    keySummary: topKeys.join(', '),
                };
            });

        const incomingTransitionsByNode = new Map();
        edges.forEach((edge) => {
            if (!edge || !edge.toHash) {
                return;
            }
            incomingTransitionsByNode.set(
                edge.toHash,
                (incomingTransitionsByNode.get(edge.toHash) || 0) + (Number(edge.count) || 0)
            );
        });
        nodes.forEach((node) => {
            if (!node || typeof node !== 'object') {
                return;
            }
            node.incomingTransitions = incomingTransitionsByNode.get(node.hash) || 0;
            if (!Number.isFinite(Number(node.stableDiscoveryRank))) {
                const nodeHash = String(node.hash || '').trim();
                if (nodeHash && chaosFlowStableDiscoveryRankByHash.has(nodeHash)) {
                    node.stableDiscoveryRank = chaosFlowStableDiscoveryRankByHash.get(nodeHash);
                }
            }
        });
        nodes.sort((a, b) => compareChaosFlowNodesByDiscovery(a, b));
        nodes.forEach((node, index) => {
            node.discoveryOrder = index;
        });

        return {
            nodes,
            edges,
            truncatedNodes: groupedNodes.length > nodes.length,
            totalAreas: groupedNodes.length,
            totalRawAreas: allAreas.length,
        };
    };

    const setMapModalMaximized = (maximized) => {
        mapModalMaximized = !!maximized;
        if (mapModal) {
            mapModal.classList.toggle('is-maximized', mapModalMaximized);
        }
        if (mapMaximizeBtn) {
            mapMaximizeBtn.hidden = mapModalMaximized;
            mapMaximizeBtn.setAttribute('aria-expanded', mapModalMaximized ? 'true' : 'false');
        }
        if (mapMinimizeBtn) {
            mapMinimizeBtn.hidden = !mapModalMaximized;
            mapMinimizeBtn.setAttribute('aria-expanded', mapModalMaximized ? 'true' : 'false');
        }
        try {
            window.localStorage.setItem('h3270ChaosMapModalMaximized', mapModalMaximized ? '1' : '0');
        } catch (_err) {
            // ignore persistence failures
        }
    };

    const setReportModalMaximized = (maximized) => {
        reportModalMaximized = !!maximized;
        if (reportModal) {
            reportModal.classList.toggle('is-maximized', reportModalMaximized);
        }
        if (reportMaximizeBtn) {
            reportMaximizeBtn.hidden = reportModalMaximized;
            reportMaximizeBtn.setAttribute('aria-expanded', reportModalMaximized ? 'true' : 'false');
        }
        if (reportMinimizeBtn) {
            reportMinimizeBtn.hidden = !reportModalMaximized;
            reportMinimizeBtn.setAttribute('aria-expanded', reportModalMaximized ? 'true' : 'false');
        }
        try {
            window.localStorage.setItem('h3270ChaosReportModalMaximized', reportModalMaximized ? '1' : '0');
        } catch (_err) {
            // ignore persistence failures
        }
    };

    const edgeKeyForFlow = (edge) => `${String(edge && edge.fromHash || '')}->${String(edge && edge.toHash || '')}`;

    const compareChaosFlowTimestampAscUnknownLast = (aMs, bMs) => {
        const a = Number(aMs) || 0;
        const b = Number(bMs) || 0;
        if (a > 0 && b > 0 && a !== b) {
            return a - b;
        }
        if (a > 0 && b <= 0) {
            return -1;
        }
        if (a <= 0 && b > 0) {
            return 1;
        }
        return 0;
    };

    const compareChaosFlowNodesByDiscovery = (a, b) => {
        const aOrder = Number(a && a.discoveryOrder);
        const bOrder = Number(b && b.discoveryOrder);
        const aHasOrder = Number.isFinite(aOrder) && aOrder >= 0;
        const bHasOrder = Number.isFinite(bOrder) && bOrder >= 0;
        if (aHasOrder && bHasOrder && aOrder !== bOrder) {
            return aOrder - bOrder;
        }
        if (aHasOrder && !bHasOrder) {
            return -1;
        }
        if (!aHasOrder && bHasOrder) {
            return 1;
        }

        const aStable = Number(a && a.stableDiscoveryRank);
        const bStable = Number(b && b.stableDiscoveryRank);
        const aHasStable = Number.isFinite(aStable) && aStable >= 0;
        const bHasStable = Number.isFinite(bStable) && bStable >= 0;
        if (aHasStable && bHasStable && aStable !== bStable) {
            return aStable - bStable;
        }
        if (aHasStable && !bHasStable) {
            return -1;
        }
        if (!aHasStable && bHasStable) {
            return 1;
        }

        const firstSeenCmp = compareChaosFlowTimestampAscUnknownLast(
            Number(a && a.firstSeenMs) || 0,
            Number(b && b.firstSeenMs) || 0
        );
        if (firstSeenCmp !== 0) {
            return firstSeenCmp;
        }

        const aIncoming = Number(a && a.incomingTransitions) || 0;
        const bIncoming = Number(b && b.incomingTransitions) || 0;
        if (aIncoming !== bIncoming) {
            return aIncoming - bIncoming;
        }

        const aVisits = Number(a && a.visits) || 0;
        const bVisits = Number(b && b.visits) || 0;
        if (bVisits !== aVisits) {
            return bVisits - aVisits;
        }

        return String(a && a.hash || '').localeCompare(String(b && b.hash || ''));
    };

    const mergeChaosFlowPreviewVariants = (variants) => {
        const list = (Array.isArray(variants) ? variants : [])
            .filter((v) => v && typeof v === 'object' && String(v.text || '').length > 0);
        if (!list.length) {
            return { text: '', width: 0, height: 0 };
        }

        const parsed = list.map((v) => {
            const lines = String(v.text || '').replace(/\r/g, '').split('\n');
            const widthFromLines = lines.reduce((max, line) => Math.max(max, String(line || '').length), 0);
            return {
                lines,
                width: Math.max(0, Number(v.width) || widthFromLines),
                height: Math.max(0, Number(v.height) || lines.length),
            };
        });

        let width = parsed[0].width || 0;
        let height = parsed[0].height || 0;
        parsed.forEach((p) => {
            if (p.width > 0) {
                width = width > 0 ? Math.min(width, p.width) : p.width;
            }
            if (p.height > 0) {
                height = height > 0 ? Math.min(height, p.height) : p.height;
            }
        });
        width = Math.max(0, width);
        height = Math.max(0, height);
        if (width <= 0 || height <= 0) {
            return {
                text: parsed[0].lines.join('\n'),
                width: parsed[0].width,
                height: parsed[0].height,
            };
        }

        const base = Array.from({ length: height }, (_, y) =>
            Array.from(String(parsed[0].lines[y] || '').padEnd(width, ' ').slice(0, width))
        );

        for (let i = 1; i < parsed.length; i += 1) {
            const current = parsed[i];
            for (let y = 0; y < height; y += 1) {
                const row = String(current.lines[y] || '').padEnd(width, ' ').slice(0, width);
                for (let x = 0; x < width; x += 1) {
                    if (base[y][x] !== row[x]) {
                        base[y][x] = ' ';
                    }
                }
            }
        }

        const mergedLines = base.map((row) => row.join('').replace(/\s+$/g, ''));
        return {
            text: mergedLines.join('\n'),
            width,
            height,
        };
    };

    const chaosFlowNodePreviewConfig = (isModal) => ({
        maxCols: isModal ? 54 : 34,
        maxRows: isModal ? 10 : 5,
        charWidth: isModal ? 6.5 : 5.2,
        lineHeight: isModal ? 9.1 : 7.6,
        insetX: isModal ? 12 : 10,
        insetTop: isModal ? 12 : 10,
        insetBottom: isModal ? 10 : 8,
        previewPadX: isModal ? 9 : 7,
        previewPadTop: isModal ? 8 : 6,
        footerHeight: isModal ? 44 : 28,
    });

    const chaosFlowPreviewLines = (text, maxCols, maxRows) => {
        const cols = Math.max(1, Number(maxCols) || 1);
        const rows = Math.max(1, Number(maxRows) || 1);
        const raw = String(text || '');
        if (!raw.trim()) {
            return [];
        }
        return raw
            .replace(/\r/g, '')
            .split('\n')
            .slice(0, rows)
            .map((line) => String(line || '')
                .replace(/\t/g, ' ')
                .slice(0, cols)
                .padEnd(cols, ' '));
    };

    const chaosFlowSvgPreserveLine = (line) => {
        const value = String(line == null ? '' : line);
        const withNbsp = value === '' ? '\u00A0' : value.replace(/ /g, '\u00A0');
        return escapeHtml(withNbsp);
    };

    const chaosFlowNodeBodyMarkup = (node, isModal) => {
        const cfg = chaosFlowNodePreviewConfig(isModal);
        const outerW = Math.max(40, Number(node && node.w) || 0);
        const outerH = Math.max(40, Number(node && node.h) || 0);
        const footerHeight = Math.min(cfg.footerHeight, Math.max(24, outerH - 24));
        const previewX = cfg.insetX;
        const previewY = cfg.insetTop;
        const previewW = Math.max(20, outerW - (cfg.insetX * 2));
        const previewH = Math.max(20, outerH - footerHeight - cfg.insetTop - cfg.insetBottom);
        const textX = previewX + cfg.previewPadX;
        const textY = previewY + cfg.previewPadTop + (isModal ? 0 : 0);
        const footerY = outerH - footerHeight;
        const label = String(node && node.label || 'Screen');
        const visits = Number(node && node.visits) || 0;
        const inputs = Number(node && node.inputs) || 0;
        const variants = Number(node && node.variants) || 0;
        const discoveryOrder = Number(node && node.discoveryOrder);
        const discoveryIndex = Number.isFinite(discoveryOrder) && discoveryOrder >= 0 ? (discoveryOrder + 1) : 0;
        const discoveryText = discoveryIndex > 0 ? `Discovered #${discoveryIndex}` : '';
        const screenW = Number(node && node.screenWidth) || 0;
        const screenH = Number(node && node.screenHeight) || 0;
        const dimsText = screenW > 0 && screenH > 0 ? `${screenH}x${screenW}` : '';
        const footerLabel = discoveryIndex > 0 ? `#${discoveryIndex} ${label}` : label;
        const metaLine = [
            discoveryText,
            dimsText,
            `${visits} visit${visits === 1 ? '' : 's'}`,
            `${inputs} input${inputs === 1 ? '' : 's'}`,
            variants > 1 ? `${variants} merged` : '',
        ].filter(Boolean).join(' | ');

        let lines = chaosFlowPreviewLines(node && node.previewText, cfg.maxCols, cfg.maxRows);
        let placeholder = false;
        if (!lines.length) {
            placeholder = true;
            const fallbackCols = Math.max(8, Math.min(cfg.maxCols, Math.floor((previewW - (cfg.previewPadX * 2)) / cfg.charWidth)));
            lines = [
                'No screen snapshot in this run'.slice(0, fallbackCols).padEnd(fallbackCols, ' '),
                'Continue exploring to capture it'.slice(0, fallbackCols).padEnd(fallbackCols, ' '),
            ];
        }

        const lineMarkup = lines.map((line, index) => {
            const y = textY + (index * cfg.lineHeight);
            return `<tspan x="${textX.toFixed(1)}" y="${y.toFixed(1)}">${chaosFlowSvgPreserveLine(line)}</tspan>`;
        }).join('');

        return `
            <g class="chaos-map-flow-node-screen" aria-hidden="true">
                <rect class="chaos-map-flow-node-screen-bg${placeholder ? ' is-placeholder' : ''}" x="${previewX}" y="${previewY}" width="${previewW}" height="${previewH}" rx="8" ry="8"></rect>
                <text class="chaos-map-flow-node-screen-text${placeholder ? ' is-placeholder' : ''}" xml:space="preserve">${lineMarkup}</text>
                <rect class="chaos-map-flow-node-footer-bg" x="1" y="${footerY}" width="${Math.max(1, outerW - 2)}" height="${Math.max(1, footerHeight - 1)}" rx="0" ry="0"></rect>
                <text class="chaos-map-flow-node-footer-label" x="${previewX}" y="${(footerY + (isModal ? 16 : 13)).toFixed(1)}">${escapeHtml(footerLabel.slice(0, isModal ? 44 : 28))}</text>
                <text class="chaos-map-flow-node-footer-meta" x="${previewX}" y="${(outerH - (isModal ? 11 : 9)).toFixed(1)}">${escapeHtml(metaLine.slice(0, isModal ? 72 : 44))}</text>
            </g>
        `;
    };

    const buildChaosMapPrimaryFlowGraph = (model) => {
        const nodes = Array.isArray(model && model.nodes) ? model.nodes : [];
        const edges = Array.isArray(model && model.edges) ? model.edges : [];
        if (!nodes.length || !edges.length) {
            return {
                graphEdges: [],
                extraEdges: edges.slice(),
                graphEdgeKeys: new Set(),
            };
        }

        const nodeByHash = new Map(nodes.map((node) => [node.hash, node]));
        const outgoing = new Map();
        const incoming = new Map();
        edges.forEach((edge) => {
            if (!edge || !nodeByHash.has(edge.fromHash) || !nodeByHash.has(edge.toHash) || edge.fromHash === edge.toHash) {
                return;
            }
            if (!outgoing.has(edge.fromHash)) {
                outgoing.set(edge.fromHash, []);
            }
            if (!incoming.has(edge.toHash)) {
                incoming.set(edge.toHash, []);
            }
            outgoing.get(edge.fromHash).push(edge);
            incoming.get(edge.toHash).push(edge);
        });
        const sortedNodes = nodes.slice().sort((a, b) => compareChaosFlowNodesByDiscovery(a, b));
        const nodeOrderByHash = new Map(sortedNodes.map((node, idx) => [node.hash, idx]));
        const compareEdgesForDiscoveryPath = (a, b) => {
            const aFrom = nodeOrderByHash.get(a && a.fromHash);
            const bFrom = nodeOrderByHash.get(b && b.fromHash);
            const aTo = nodeOrderByHash.get(a && a.toHash);
            const bTo = nodeOrderByHash.get(b && b.toHash);
            const aFromOrder = Number.isFinite(aFrom) ? aFrom : Number.MAX_SAFE_INTEGER;
            const bFromOrder = Number.isFinite(bFrom) ? bFrom : Number.MAX_SAFE_INTEGER;
            if (aFromOrder !== bFromOrder) {
                return aFromOrder - bFromOrder;
            }
            const aToOrder = Number.isFinite(aTo) ? aTo : Number.MAX_SAFE_INTEGER;
            const bToOrder = Number.isFinite(bTo) ? bTo : Number.MAX_SAFE_INTEGER;
            if (aToOrder !== bToOrder) {
                return aToOrder - bToOrder;
            }
            const aCount = Number(a && a.count) || 0;
            const bCount = Number(b && b.count) || 0;
            if (bCount !== aCount) {
                return bCount - aCount;
            }
            return edgeKeyForFlow(a).localeCompare(edgeKeyForFlow(b));
        };
        outgoing.forEach((list) => list.sort(compareEdgesForDiscoveryPath));
        incoming.forEach((list) => list.sort(compareEdgesForDiscoveryPath));

        const root = sortedNodes[0];
        const selectedKeys = new Set();
        const visited = new Set();
        if (root && root.hash) {
            visited.add(root.hash);
        }

        sortedNodes.forEach((node, idx) => {
            if (!node || !node.hash || idx === 0) {
                return;
            }
            const incomingEdges = (incoming.get(node.hash) || []).filter((edge) => {
                const fromOrder = nodeOrderByHash.get(edge && edge.fromHash);
                return Number.isFinite(fromOrder) && fromOrder < idx;
            });
            incomingEdges.sort((a, b) => {
                const aVisited = visited.has(a.fromHash) ? 0 : 1;
                const bVisited = visited.has(b.fromHash) ? 0 : 1;
                if (aVisited !== bVisited) {
                    return aVisited - bVisited;
                }
                return compareEdgesForDiscoveryPath(a, b);
            });
            const bestIncoming = incomingEdges[0] || (incoming.get(node.hash) || [])[0];
            if (bestIncoming) {
                selectedKeys.add(edgeKeyForFlow(bestIncoming));
            }
            visited.add(node.hash);
        });

        if (!selectedKeys.size && edges.length) {
            selectedKeys.add(edgeKeyForFlow(edges[0]));
        }

        const graphEdges = [];
        const extraEdges = [];
        edges.forEach((edge) => {
            if (!edge || edge.fromHash === edge.toHash) {
                extraEdges.push(edge);
                return;
            }
            if (selectedKeys.has(edgeKeyForFlow(edge))) {
                graphEdges.push(edge);
            } else {
                extraEdges.push(edge);
            }
        });

        return {
            graphEdges,
            extraEdges,
            graphEdgeKeys: selectedKeys,
        };
    };

    const layoutChaosMapFlow = (model, options = {}) => {
        const spacious = !!options.spacious;
        const nodes = Array.isArray(model && model.nodes) ? model.nodes : [];
        const edges = Array.isArray(model && model.edges) ? model.edges : [];
        if (!nodes.length) {
            return { nodes: [], edges: [], width: 900, height: 180 };
        }

        const nodeByHash = new Map(nodes.map((node) => [node.hash, { ...node }]));
        const outgoing = new Map();
        const incoming = new Map();
        edges.forEach((edge) => {
            if (!nodeByHash.has(edge.fromHash) || !nodeByHash.has(edge.toHash)) {
                return;
            }
            if (!outgoing.has(edge.fromHash)) {
                outgoing.set(edge.fromHash, []);
            }
            outgoing.get(edge.fromHash).push(edge);
            incoming.set(edge.toHash, (incoming.get(edge.toHash) || 0) + edge.count);
        });
        outgoing.forEach((list) => list.sort((a, b) => b.count - a.count));

        const sorted = [...nodeByHash.values()].sort((a, b) => compareChaosFlowNodesByDiscovery(a, b));
        const root = sorted[0];
        const depth = new Map();
        const queue = [];
        if (root) {
            depth.set(root.hash, 0);
            queue.push(root.hash);
        }
        while (queue.length) {
            const from = queue.shift();
            const fromDepth = depth.get(from) || 0;
            (outgoing.get(from) || []).forEach((edge) => {
                if (depth.has(edge.toHash)) {
                    return;
                }
                depth.set(edge.toHash, fromDepth + 1);
                queue.push(edge.toHash);
            });
        }
        let maxDepth = 0;
        sorted.forEach((node) => {
            if (!depth.has(node.hash)) {
                maxDepth += 1;
                depth.set(node.hash, maxDepth);
            } else if ((depth.get(node.hash) || 0) > maxDepth) {
                maxDepth = depth.get(node.hash) || 0;
            }
        });

        const columns = new Map();
        sorted.forEach((node) => {
            const d = depth.get(node.hash) || 0;
            if (!columns.has(d)) {
                columns.set(d, []);
            }
            columns.get(d).push(node);
        });
        columns.forEach((list) => list.sort((a, b) => compareChaosFlowNodesByDiscovery(a, b)));

        const nodesHavePreview = nodes.some((node) => String(node && node.previewText || '').trim() !== '');
        const nodeW = spacious ? (nodesHavePreview ? 440 : 360) : 272;
        const nodeH = spacious ? (nodesHavePreview ? 192 : 90) : 74;
        const padX = spacious ? 44 : 34;
        const padY = spacious ? 34 : 24;
        const colGap = spacious ? (nodesHavePreview ? 320 : 300) : 220;
        const rowGap = spacious ? (nodesHavePreview ? 112 : 84) : 56;
        const colKeys = Array.from(columns.keys()).sort((a, b) => a - b);
        const maxRows = colKeys.reduce((max, key) => Math.max(max, (columns.get(key) || []).length), 1);
        const colHeights = new Map(colKeys.map((key) => {
            const colNodes = columns.get(key) || [];
            const colHeight = (colNodes.length * nodeH) + (Math.max(0, colNodes.length - 1) * rowGap);
            return [key, colHeight];
        }));
        const colBandGapY = spacious ? (nodesHavePreview ? 180 : 140) : rowGap;

        let width = (colKeys.length * nodeW) + ((Math.max(0, colKeys.length - 1)) * colGap) + (padX * 2);
        let height = (maxRows * nodeH) + ((Math.max(0, maxRows - 1)) * rowGap) + (padY * 2);

        let placementBands = [{ keys: colKeys.slice() }];
        if (spacious && colKeys.length >= 5) {
            const targetAspect = 1.9;
            const maxBandRows = Math.min(4, colKeys.length);
            let best = null;
            for (let bandRows = 1; bandRows <= maxBandRows; bandRows += 1) {
                const colsPerBand = Math.ceil(colKeys.length / bandRows);
                const bands = [];
                for (let i = 0; i < bandRows; i += 1) {
                    const start = i * colsPerBand;
                    const keys = colKeys.slice(start, start + colsPerBand);
                    if (!keys.length) {
                        continue;
                    }
                    bands.push({ keys });
                }
                if (!bands.length) {
                    continue;
                }
                const bandHeights = bands.map((band) => band.keys.reduce((m, key) => Math.max(m, colHeights.get(key) || nodeH), nodeH));
                const testWidth = (colsPerBand * nodeW) + (Math.max(0, colsPerBand - 1) * colGap) + (padX * 2);
                const testHeight = (bandHeights.reduce((sum, v) => sum + v, 0))
                    + (Math.max(0, bands.length - 1) * colBandGapY)
                    + (padY * 2);
                const aspect = testWidth / Math.max(1, testHeight);
                const score = Math.abs(aspect - targetAspect) + (bandRows > 1 ? 0.08 * (bandRows - 1) : 0);
                if (!best || score < best.score) {
                    best = {
                        score,
                        bands,
                        colsPerBand,
                        bandHeights,
                        width: testWidth,
                        height: testHeight,
                    };
                }
            }
            if (best && best.bands.length > 1) {
                placementBands = best.bands;
                width = best.width;
                height = best.height;
            }
        }

        let bandY = padY;
        placementBands.forEach((band) => {
            const bandKeys = Array.isArray(band.keys) ? band.keys : [];
            if (!bandKeys.length) {
                return;
            }
            const bandHeight = bandKeys.reduce((m, key) => Math.max(m, colHeights.get(key) || nodeH), nodeH);
            const bandStartX = padX;
            bandKeys.forEach((col, colIndex) => {
                const colNodes = columns.get(col) || [];
                const startY = bandY;
                colNodes.forEach((node, rowIndex) => {
                    const n = nodeByHash.get(node.hash);
                    n.x = bandStartX + (colIndex * (nodeW + colGap));
                    n.y = startY + (rowIndex * (nodeH + rowGap));
                    n.w = nodeW;
                    n.h = nodeH;
                });
            });
            bandY += bandHeight + colBandGapY;
        });

        const laidOutEdges = edges
            .filter((edge) => nodeByHash.has(edge.fromHash) && nodeByHash.has(edge.toHash))
            .map((edge, edgeIndex) => {
                const from = nodeByHash.get(edge.fromHash);
                const to = nodeByHash.get(edge.toHash);
                return { ...edge, from, to, edgeIndex };
            });

        return {
            nodes: Array.from(nodeByHash.values()),
            edges: laidOutEdges,
            width: Math.max(spacious ? 1640 : 1260, width),
            height: Math.max(spacious ? 360 : 290, height),
        };
    };

    const chaosMapFlowMarkup = (areas, options = {}) => {
        const isModal = !!options.isModal;
        const showOpenButton = !!options.showOpenButton;
        const markerId = String(options.markerId || 'chaos-map-flow-arrow');
        const model = buildChaosMapFlowModel(areas);
        const primaryGraph = buildChaosMapPrimaryFlowGraph(model);
        const layout = layoutChaosMapFlow({
            ...model,
            edges: primaryGraph.graphEdges,
        }, { spacious: isModal });
        if (!layout.nodes.length) {
            return '';
        }
        const showEdgeLabels = false;
        const allEdges = Array.isArray(model.edges) ? model.edges : [];
        const maxEdgeCount = allEdges.reduce((max, edge) => Math.max(max, edge.count || 0), 1) || 1;
        const edgeMarkup = layout.edges.map((edge) => {
            const geom = computeChaosMapFlowEdgeGeometry(edge, { isModal, maxEdgeCount });
            return `
                <g
                    class="chaos-map-flow-edge"
                    aria-hidden="true"
                    data-chaos-flow-edge
                    data-chaos-edge-from="${escapeHtml(edge.from.hash)}"
                    data-chaos-edge-to="${escapeHtml(edge.to.hash)}"
                    data-chaos-edge-index="${edge.edgeIndex}"
                    data-chaos-edge-count="${edge.count}"
                >
                    <path class="chaos-map-flow-edge-underlay" data-chaos-flow-edge-underlay d="${geom.path}" stroke-width="${(geom.strokeWidth + 5.5).toFixed(2)}"></path>
                    <path class="chaos-map-flow-edge-core" data-chaos-flow-edge-core d="${geom.path}" stroke-width="${geom.strokeWidth.toFixed(2)}"></path>
                    <path class="chaos-map-flow-arrowhead" data-chaos-flow-edge-arrow d="${geom.arrowHeadPath}"></path>
                    <circle class="chaos-map-flow-edge-dot is-from" data-chaos-flow-edge-dot-from cx="${geom.x1.toFixed(1)}" cy="${geom.y1.toFixed(1)}" r="${isModal ? 2.8 : 2.2}"></circle>
                    <circle class="chaos-map-flow-edge-dot is-to" data-chaos-flow-edge-dot-to cx="${geom.targetAnchorX.toFixed(1)}" cy="${geom.y2.toFixed(1)}" r="${isModal ? 2.8 : 2.2}"></circle>
                    ${showEdgeLabels && edge.keySummary ? '' : ''}
                </g>
            `;
        }).join('');
        const nodeMarkup = layout.nodes.map((node) => {
            const discoveryOrder = Number(node && node.discoveryOrder);
            const discoveryIndex = Number.isFinite(discoveryOrder) && discoveryOrder >= 0 ? (discoveryOrder + 1) : 0;
            const discoveryText = discoveryIndex > 0 ? `Discovered #${discoveryIndex}` : '';
            return `
                <g
                    class="chaos-map-flow-node"
                    transform="translate(${node.x},${node.y})"
                    data-chaos-flow-node
                    role="button"
                    tabindex="0"
                    aria-label="${escapeHtml(`Open screen hints for ${node.label || 'Screen'}`)}"
                    data-tippy-content="Drag to move or click to edit hints"
                    data-chaos-node-id="${escapeHtml(node.hash)}"
                    data-chaos-node-sample-hash="${escapeHtml(node.sampleHash || '')}"
                    data-chaos-node-label="${escapeHtml(node.label || 'Screen')}"
                    data-chaos-node-member-hashes="${escapeHtml((Array.isArray(node.memberHashes) ? node.memberHashes : []).join(','))}"
                    data-chaos-node-x="${node.x}"
                    data-chaos-node-y="${node.y}"
                    data-chaos-node-w="${node.w}"
                    data-chaos-node-h="${node.h}"
                >
                    <rect width="${node.w}" height="${node.h}" rx="10" ry="10"></rect>
                    ${chaosFlowNodeBodyMarkup(node, isModal)}
                </g>
            `;
        }).join('');

        const summaryBits = [
            formatChaosScreenCount(model.totalAreas, model.totalRawAreas),
            `${layout.edges.length} primary flow edge${layout.edges.length === 1 ? '' : 's'} on canvas`,
            primaryGraph.extraEdges.length ? `${primaryGraph.extraEdges.length} additional transition${primaryGraph.extraEdges.length === 1 ? '' : 's'} listed` : '',
            model.truncatedNodes ? `top ${layout.nodes.length} screens displayed` : '',
        ].filter(Boolean).join(' | ');

        const modelNodeByHash = new Map((Array.isArray(model.nodes) ? model.nodes : []).map((node) => [node.hash, node]));
        const legendEdges = [
            ...layout.edges,
            ...primaryGraph.extraEdges.map((edge) => ({
                ...edge,
                from: modelNodeByHash.get(edge.fromHash) || { hash: edge.fromHash, label: 'Screen' },
                to: modelNodeByHash.get(edge.toHash) || { hash: edge.toHash, label: 'Screen' },
            })),
        ];
        const onCanvasEdgeKeys = new Set(layout.edges.map((edge) => edgeKeyForFlow(edge)));
        const edgeLegend = legendEdges
            .slice(0, 16)
            .map((edge) => {
                const fromLabel = String(edge.from.label || edge.from.hash || 'Screen').slice(0, 26);
                const toLabel = String(edge.to.label || edge.to.hash || 'Screen').slice(0, 26);
                const keyText = edge.keySummary ? ` | ${edge.keySummary}` : '';
                const canvasTag = onCanvasEdgeKeys.has(edgeKeyForFlow(edge)) ? 'Canvas' : 'Listed';
                const extraClass = onCanvasEdgeKeys.has(edgeKeyForFlow(edge)) ? 'is-on-canvas' : 'is-listed-only';
                return `<div class="chaos-map-flow-legend-row ${extraClass}"><span class="chaos-map-flow-legend-route"><span class="chaos-map-flow-legend-swatch" aria-hidden="true"></span>${escapeHtml(fromLabel)} -> ${escapeHtml(toLabel)}</span><span class="chaos-map-flow-legend-meta">${escapeHtml(canvasTag)} | ${edge.count} transition${edge.count === 1 ? '' : 's'}${escapeHtml(keyText)}</span></div>`;
            })
            .join('');

        return `
            <section class="chaos-map-flow-panel${isModal ? ' is-modal' : ''}" aria-label="Flow diagram of the discovered screens" data-chaos-flow-canvas="${isModal ? 'modal' : 'inline'}">
                <div class="chaos-map-flow-header">
                    <div class="chaos-map-flow-header-top">
                        <strong>Flow diagram</strong>
                        <div class="chaos-map-flow-header-actions">
                            <div class="chaos-map-flow-zoom-controls" role="group" aria-label="Flow diagram zoom controls">
                                <button type="button" data-chaos-flow-zoom-out aria-label="Zoom out" data-tippy-content="Zoom out">-</button>
                                <button type="button" data-chaos-flow-zoom-in aria-label="Zoom in" data-tippy-content="Zoom in">+</button>
                                <button type="button" data-chaos-flow-zoom-fit aria-label="Fit graph to viewport" data-tippy-content="Fit graph to viewport">Fit</button>
                                <button type="button" data-chaos-flow-layout-reset aria-label="Reset moved screen positions" data-tippy-content="Reset moved screen positions">Layout</button>
                                <button type="button" data-chaos-flow-zoom-reset aria-label="Reset zoom to 100%" data-tippy-content="Reset zoom to 100%">100%</button>
                                <span class="chaos-map-flow-zoom-value subtle" data-chaos-flow-zoom-value>100%</span>
                            </div>
                            ${showOpenButton ? '<button type="button" data-chaos-flow-open>Open flow diagram</button>' : ''}
                        </div>
                    </div>
                    <div class="chaos-map-flow-meta-row">
                        <span class="subtle">${escapeHtml(summaryBits)}</span>
                    </div>
                    <div class="chaos-map-flow-meta-row">
                        <span class="subtle">Each preview is one captured example of a screen. Other visits may differ in the data shown.</span>
                    </div>
                </div>
                <div class="chaos-map-flow-viewport" data-chaos-flow-viewport>
                    <div class="chaos-map-flow-stage" data-chaos-flow-stage data-chaos-flow-stage-width="${layout.width}" data-chaos-flow-stage-height="${layout.height}" style="width:${layout.width}px;height:${layout.height}px;">
                        <svg class="chaos-map-flow-svg" data-chaos-flow-svg data-chaos-flow-scope="${isModal ? 'modal' : 'inline'}" data-chaos-flow-max-edge-count="${maxEdgeCount}" data-chaos-flow-spacious="${isModal ? '1' : '0'}" viewBox="0 0 ${layout.width} ${layout.height}" width="${layout.width}" height="${layout.height}" role="img" aria-label="Discovered screen flow graph">
                            ${edgeMarkup}
                            ${nodeMarkup}
                        </svg>
                    </div>
                </div>
                ${edgeLegend ? `
                    <div class="chaos-map-flow-legend">
                        <div class="chaos-map-flow-legend-header">
                            <strong>Transitions</strong>
                            <span class="subtle">Canvas shows primary discovery path. Extra/loopback transitions are listed below.</span>
                        </div>
                        <div class="chaos-map-flow-legend-list">${edgeLegend}</div>
                    </div>
                ` : ''}
            </section>
        `;
    };

    const chaosMapFlowLauncherMarkup = (areas) => {
        const model = buildChaosMapFlowModel(areas);
        const summaryBits = [
            formatChaosScreenCount(model.totalAreas, model.totalRawAreas),
            `${(Array.isArray(model.edges) ? model.edges.length : 0)} transition${(Array.isArray(model.edges) ? model.edges.length : 0) === 1 ? '' : 's'} discovered`,
            model.truncatedNodes ? `top ${(Array.isArray(model.nodes) ? model.nodes.length : 0)} screens sampled` : '',
        ].filter(Boolean).join(' | ');

        return `
            <section class="chaos-map-flow-panel chaos-map-flow-panel--launcher" aria-label="Flow diagram">
                <div class="chaos-map-flow-header">
                    <strong>Flow diagram</strong>
                    <div class="chaos-map-flow-header-actions">
                        <span class="subtle">${escapeHtml(summaryBits || 'No screens mapped yet.')}</span>
                        <button type="button" data-chaos-flow-open>Open flow diagram</button>
                    </div>
                </div>
                <p class="subtle chaos-map-flow-launcher-copy">Open the flow diagram for the interactive canvas (pan, drag, zoom) and the full transition list.</p>
            </section>
        `;
    };

    const getChaosMapAreas = (options = {}) => {
        const includeFirstScreen = !!options.includeFirstScreen;
        const excludedFirstHash = includeFirstScreen ? '' : String(latestFirstScreenHash || '').trim();
        const areas = latestMindMap && latestMindMap.areas && typeof latestMindMap.areas === 'object'
            ? Object.values(latestMindMap.areas).filter((a) => {
                if (!a || typeof a !== 'object') {
                    return false;
                }
                if (!excludedFirstHash) {
                    return true;
                }
                return String(a.hash || '').trim() !== excludedFirstHash;
            })
            : [];
        areas.sort((a, b) => {
            const aVisits = Number(a.visits) || 0;
            const bVisits = Number(b.visits) || 0;
            if (bVisits !== aVisits) {
                return bVisits - aVisits;
            }
            const aSeen = Date.parse(a.lastSeen || '') || 0;
            const bSeen = Date.parse(b.lastSeen || '') || 0;
            return bSeen - aSeen;
        });
        return areas;
    };

    const getChaosMapCardAreas = () => getChaosMapAreas({ includeFirstScreen: false });
    const getChaosFlowAreas = () => getChaosMapAreas({ includeFirstScreen: true });

    const chaosFlowNodePositionOverrides = {
        inline: new Map(),
        modal: new Map(),
    };
    const chaosFlowCanvasState = {
        inline: { zoom: 1 },
        modal: { zoom: 1 },
    };
    const chaosFlowStableDiscoveryRankByHash = new Map();
    let chaosFlowStableDiscoveryRankSeq = 0;

    const clamp = (value, min, max) => Math.max(min, Math.min(max, value));

    const computeChaosMapFlowEdgeGeometry = (edge, options = {}) => {
        const isModal = !!options.isModal;
        const maxEdgeCount = Math.max(1, Number(options.maxEdgeCount) || 1);
        const flowsRight = edge.to.x >= edge.from.x;
        const sourceInset = isModal ? 12 : 9;
        const targetGap = isModal ? 62 : 44;
        const x1 = flowsRight
            ? (edge.from.x + edge.from.w + sourceInset)
            : (edge.from.x - sourceInset);
        const y1 = edge.from.y + (edge.from.h / 2);
        const x2 = flowsRight
            ? (edge.to.x - targetGap)
            : (edge.to.x + edge.to.w + targetGap);
        const y2 = edge.to.y + (edge.to.h / 2);
        const targetAnchorX = flowsRight ? edge.to.x : (edge.to.x + edge.to.w);
        const span = Math.max(12, Math.abs(x2 - x1));
        const sameRow = Math.abs(y2 - y1) <= (isModal ? 10 : 8);
        const dir = flowsRight ? 1 : -1;
        const straightPath = sameRow && span >= (isModal ? 110 : 78);
        const elbowPadDesired = Math.min(isModal ? 150 : 112, Math.max(isModal ? 52 : 38, span * 0.42));
        const elbowPadMax = Math.max(10, span - (isModal ? 26 : 18));
        const elbowPad = Math.min(elbowPadDesired, elbowPadMax);
        const elbowX = x1 + (dir * elbowPad);
        const path = straightPath
            ? `M ${x1} ${y1} H ${x2}`
            : `M ${x1} ${y1} H ${elbowX} V ${y2} H ${x2}`;
        const strokeWidth = 1.5 + (((Number(edge.count) || 0) / maxEdgeCount) * 4.5);
        const headLen = Math.max(isModal ? 12 : 10, strokeWidth * 2.2);
        const headHalf = Math.max(isModal ? 6 : 5, strokeWidth * 1.1);
        const headBaseX = x2 - (dir * headLen);
        const headNotchX = x2 - (dir * (headLen * 0.62));
        const arrowHeadPath = [
            `M ${x2} ${y2}`,
            `L ${headBaseX} ${y2 - headHalf}`,
            `L ${headNotchX} ${y2}`,
            `L ${headBaseX} ${y2 + headHalf}`,
            'Z',
        ].join(' ');
        return {
            x1,
            y1,
            x2,
            y2,
            targetAnchorX,
            path,
            strokeWidth,
            arrowHeadPath,
        };
    };

    const summarizeChaosGroupFieldDiscovery = (group) => {
        const out = {
            fields: 0,
            triedValues: 0,
            workingValues: 0,
            progressions: 0,
        };
        const fieldSeen = new Set();
        const triedSeen = new Set();
        const workingSeen = new Set();
        const areas = group && Array.isArray(group.memberAreas) ? group.memberAreas : [];
        areas.forEach((area) => {
            if (!area || typeof area !== 'object') {
                return;
            }
            const fieldDiscovery = area.fieldDiscovery && typeof area.fieldDiscovery === 'object' ? area.fieldDiscovery : {};
            Object.entries(fieldDiscovery).forEach(([fieldKey, meta]) => {
                if (fieldKey && !fieldSeen.has(fieldKey)) {
                    fieldSeen.add(fieldKey);
                    out.fields += 1;
                }
                if (meta && typeof meta === 'object') {
                    out.progressions += Number(meta.progressions) || 0;
                }
            });
            const tried = area.knownTriedValues && typeof area.knownTriedValues === 'object' ? area.knownTriedValues : {};
            Object.entries(tried).forEach(([fieldKey, values]) => {
                (Array.isArray(values) ? values : []).forEach((value) => {
                    const v = String(value || '').trim();
                    if (!v) {
                        return;
                    }
                    const key = `${fieldKey}\u001f${v}`;
                    if (triedSeen.has(key)) {
                        return;
                    }
                    triedSeen.add(key);
                    out.triedValues += 1;
                });
            });
            const working = area.knownWorkingValues && typeof area.knownWorkingValues === 'object' ? area.knownWorkingValues : {};
            Object.entries(working).forEach(([fieldKey, values]) => {
                (Array.isArray(values) ? values : []).forEach((value) => {
                    const v = String(value || '').trim();
                    if (!v) {
                        return;
                    }
                    const key = `${fieldKey}\u001f${v}`;
                    if (workingSeen.has(key)) {
                        return;
                    }
                    workingSeen.add(key);
                    out.workingValues += 1;
                });
            });
        });
        return out;
    };

    const parseChaosFieldKey = (fieldKey) => {
        const raw = String(fieldKey || '').trim();
        const m = raw.match(/^R(\d+)C(\d+)L(\d+)$/i);
        if (!m) {
            return { key: raw, row: 0, col: 0, len: 0 };
        }
        return {
            key: raw,
            row: Number(m[1]) || 0,
            col: Number(m[2]) || 0,
            len: Number(m[3]) || 0,
        };
    };

    const buildChaosGroupFieldDiscoveryRows = (group) => {
        const byField = new Map();
        const areas = group && Array.isArray(group.memberAreas) ? group.memberAreas : [];
        const ensureRow = (fieldKey) => {
            const key = String(fieldKey || '').trim();
            if (!key) {
                return null;
            }
            if (!byField.has(key)) {
                const parsed = parseChaosFieldKey(key);
                byField.set(key, {
                    fieldKey: key,
                    row: parsed.row,
                    col: parsed.col,
                    len: parsed.len,
                    numeric: false,
                    hidden: false,
                    multiLine: false,
                    writes: 0,
                    writeSuccesses: 0,
                    progressions: 0,
                    lastValue: '',
                    lastWorkedValue: '',
                    triedValues: [],
                    workingValues: [],
                    _triedSet: new Set(),
                    _workingSet: new Set(),
                });
            }
            return byField.get(key);
        };

        areas.forEach((area) => {
            if (!area || typeof area !== 'object') {
                return;
            }
            const fieldMeta = area.fieldMetadata && typeof area.fieldMetadata === 'object' ? area.fieldMetadata : {};
            Object.entries(fieldMeta).forEach(([fieldKey, meta]) => {
                const row = ensureRow(fieldKey);
                if (!row || !meta || typeof meta !== 'object') {
                    return;
                }
                row.row = row.row || (Number(meta.row) || 0);
                row.col = row.col || (Number(meta.column) || 0);
                row.len = row.len || (Number(meta.length) || 0);
                row.numeric = row.numeric || !!meta.numeric;
                row.hidden = row.hidden || !!meta.hidden;
                row.multiLine = row.multiLine || !!meta.multiLine;
            });

            const fieldDiscovery = area.fieldDiscovery && typeof area.fieldDiscovery === 'object' ? area.fieldDiscovery : {};
            Object.entries(fieldDiscovery).forEach(([fieldKey, meta]) => {
                const row = ensureRow(fieldKey);
                if (!row || !meta || typeof meta !== 'object') {
                    return;
                }
                row.writes += Number(meta.writes) || 0;
                row.writeSuccesses += Number(meta.writeSuccesses) || 0;
                row.progressions += Number(meta.progressions) || 0;
                if (!row.lastValue && meta.lastValue) {
                    row.lastValue = String(meta.lastValue || '');
                }
                if (!row.lastWorkedValue && meta.lastWorkedValue) {
                    row.lastWorkedValue = String(meta.lastWorkedValue || '');
                }
            });

            const tried = area.knownTriedValues && typeof area.knownTriedValues === 'object' ? area.knownTriedValues : {};
            Object.entries(tried).forEach(([fieldKey, values]) => {
                const row = ensureRow(fieldKey);
                if (!row) {
                    return;
                }
                (Array.isArray(values) ? values : []).forEach((value) => {
                    const v = String(value || '').trim();
                    if (!v || row._triedSet.has(v)) {
                        return;
                    }
                    row._triedSet.add(v);
                    row.triedValues.push(v);
                });
            });

            const working = area.knownWorkingValues && typeof area.knownWorkingValues === 'object' ? area.knownWorkingValues : {};
            Object.entries(working).forEach(([fieldKey, values]) => {
                const row = ensureRow(fieldKey);
                if (!row) {
                    return;
                }
                (Array.isArray(values) ? values : []).forEach((value) => {
                    const v = String(value || '').trim();
                    if (!v || row._workingSet.has(v)) {
                        return;
                    }
                    row._workingSet.add(v);
                    row.workingValues.push(v);
                });
            });
        });

        return Array.from(byField.values())
            .map((row) => ({
                ...row,
                triedCount: row.triedValues.length,
                workingCount: row.workingValues.length,
            }))
            .sort((a, b) =>
                b.progressions - a.progressions ||
                b.workingCount - a.workingCount ||
                b.writes - a.writes ||
                a.row - b.row ||
                a.col - b.col ||
                a.fieldKey.localeCompare(b.fieldKey)
            );
    };

    const chaosMapFieldDiscoveryTableMarkup = (group) => {
        const rows = buildChaosGroupFieldDiscoveryRows(group);
        if (!rows.length) {
            return '';
        }
        const maxRows = 8;
        const initialShown = Math.min(rows.length, maxRows);
        const body = rows.map((row, rowIndex) => {
            const flags = [
                row.numeric ? 'num' : '',
                row.hidden ? 'hidden' : '',
                row.multiLine ? 'multi' : '',
            ].filter(Boolean).join(', ');
            const triedExample = row.triedValues.length ? row.triedValues.slice(0, 2).join(', ') : '';
            const workingExample = row.workingValues.length ? row.workingValues.slice(0, 2).join(', ') : '';
            const lastExample = row.lastWorkedValue || row.lastValue || '';
            const label = row.row > 0 && row.col > 0
                ? `R${row.row} C${row.col} L${row.len || 0}`
                : row.fieldKey;
            return `
                <tr${rowIndex >= maxRows ? ' class="chaos-map-field-row-extra" hidden' : ''}>
                    <td class="chaos-map-field-cell-label">
                        <div class="chaos-map-field-label-main">${escapeHtml(label)}</div>
                        ${flags ? `<div class="chaos-map-field-label-flags">${escapeHtml(flags)}</div>` : ''}
                    </td>
                    <td>${row.writes}</td>
                    <td>${row.progressions}</td>
                    <td title="${escapeHtml(triedExample)}">${row.triedCount}</td>
                    <td title="${escapeHtml(workingExample)}">${row.workingCount}</td>
                    <td class="chaos-map-field-cell-value" title="${escapeHtml(lastExample)}">${escapeHtml(String(lastExample || '').slice(0, 18))}</td>
                </tr>
            `;
        }).join('');
        return `
            <div class="chaos-map-field-discovery" data-chaos-map-field-discovery data-field-mode="top">
                <div class="chaos-map-field-discovery-header">
                    <strong>Field Discovery</strong>
                    <div class="chaos-map-field-discovery-header-actions">
                        <span class="subtle" data-chaos-map-field-count>${initialShown}${rows.length > initialShown ? ` / ${rows.length}` : ''} field${rows.length === 1 ? '' : 's'} shown</span>
                        ${rows.length > maxRows ? `<button type="button" class="chaos-map-field-toggle" data-chaos-map-field-toggle data-top-count="${maxRows}" data-total-count="${rows.length}" aria-pressed="false">All fields</button>` : ''}
                    </div>
                </div>
                <div class="chaos-map-field-discovery-table-wrap">
                    <table class="chaos-map-field-discovery-table">
                        <thead>
                            <tr>
                                <th>Field</th>
                                <th>Writes</th>
                                <th>Prog</th>
                                <th>Tried</th>
                                <th>Work</th>
                                <th>Last</th>
                            </tr>
                        </thead>
                        <tbody>${body}</tbody>
                    </table>
                </div>
                ${rows.length > maxRows ? `<div class="subtle" data-chaos-map-field-note>Showing top ${maxRows} fields by progression/writes.</div>` : ''}
            </div>
        `;
    };

    const initChaosFlowCanvasInteractions = (root) => {
        if (!root || !root.querySelectorAll) {
            return;
        }
        const panels = root.matches && root.matches('[data-chaos-flow-canvas]')
            ? [root]
            : Array.from(root.querySelectorAll('[data-chaos-flow-canvas]'));
        panels.forEach((panel) => {
            if (!panel || panel.dataset.chaosFlowInteractive === '1') {
                return;
            }
            const viewport = panel.querySelector('[data-chaos-flow-viewport]');
            const stage = panel.querySelector('[data-chaos-flow-stage]');
            const svg = panel.querySelector('[data-chaos-flow-svg]');
            if (!viewport || !stage || !svg) {
                return;
            }
            const outerFlowScroller = panel.closest('[data-chaos-flow-content]');
            panel.dataset.chaosFlowInteractive = '1';
            const zoomInBtn = panel.querySelector('[data-chaos-flow-zoom-in]');
            const zoomOutBtn = panel.querySelector('[data-chaos-flow-zoom-out]');
            const zoomFitBtn = panel.querySelector('[data-chaos-flow-zoom-fit]');
            const layoutResetBtn = panel.querySelector('[data-chaos-flow-layout-reset]');
            const zoomResetBtn = panel.querySelector('[data-chaos-flow-zoom-reset]');
            const zoomValueEl = panel.querySelector('[data-chaos-flow-zoom-value]');

            const scope = String(svg.dataset.chaosFlowScope || 'inline');
            const isModal = svg.dataset.chaosFlowSpacious === '1';
            const maxEdgeCount = Math.max(1, Number(svg.dataset.chaosFlowMaxEdgeCount) || 1);
            const overrides = chaosFlowNodePositionOverrides[scope] || (chaosFlowNodePositionOverrides[scope] = new Map());
            const canvasState = chaosFlowCanvasState[scope] || (chaosFlowCanvasState[scope] = { zoom: 1 });
            const viewBox = svg.viewBox && svg.viewBox.baseVal
                ? svg.viewBox.baseVal
                : { x: 0, y: 0, width: 0, height: 0 };
            const baseCanvasWidth = Math.max(1, Number(stage.dataset.chaosFlowStageWidth) || Number(viewBox.width) || 1);
            const baseCanvasHeight = Math.max(1, Number(stage.dataset.chaosFlowStageHeight) || Number(viewBox.height) || 1);
            const pad = isModal ? 8 : 6;
            const minZoom = 0.4;
            const maxZoom = 3.2;

            const renderedSize = () => {
                const rect = stage.getBoundingClientRect();
                return {
                    width: Math.max(1, rect.width),
                    height: Math.max(1, rect.height),
                };
            };

            const updateZoomUI = (zoom) => {
                if (zoomValueEl) {
                    zoomValueEl.textContent = `${Math.round(zoom * 100)}%`;
                }
                if (zoomOutBtn) {
                    zoomOutBtn.disabled = zoom <= minZoom + 0.001;
                }
                if (zoomInBtn) {
                    zoomInBtn.disabled = zoom >= maxZoom - 0.001;
                }
            };

            const applyZoom = (nextZoom, options = {}) => {
                const requested = Number(nextZoom);
                const zoom = clamp(Number.isFinite(requested) ? requested : 1, minZoom, maxZoom);
                const preserveCenter = options.preserveCenter !== false;
                const before = renderedSize();
                let centerXRatio = 0.5;
                let centerYRatio = 0.5;
                if (preserveCenter && before.width > 0 && before.height > 0) {
                    const anchorClientX = Number(options.anchorClientX);
                    const anchorClientY = Number(options.anchorClientY);
                    if (Number.isFinite(anchorClientX) && Number.isFinite(anchorClientY)) {
                        const viewportRect = viewport.getBoundingClientRect();
                        const localX = clamp(anchorClientX - viewportRect.left, 0, viewport.clientWidth);
                        const localY = clamp(anchorClientY - viewportRect.top, 0, viewport.clientHeight);
                        centerXRatio = clamp((viewport.scrollLeft + localX) / before.width, 0, 1);
                        centerYRatio = clamp((viewport.scrollTop + localY) / before.height, 0, 1);
                    } else {
                        centerXRatio = clamp((viewport.scrollLeft + (viewport.clientWidth / 2)) / before.width, 0, 1);
                        centerYRatio = clamp((viewport.scrollTop + (viewport.clientHeight / 2)) / before.height, 0, 1);
                    }
                }
                const nextCanvasWidth = Math.max(1, Math.round(baseCanvasWidth * zoom));
                const nextCanvasHeight = Math.max(1, Math.round(baseCanvasHeight * zoom));
                stage.style.width = `${nextCanvasWidth}px`;
                stage.style.height = `${nextCanvasHeight}px`;
                svg.style.width = `${baseCanvasWidth}px`;
                svg.style.height = `${baseCanvasHeight}px`;
                svg.style.maxWidth = 'none';
                svg.style.minWidth = '0px';
                svg.style.transformOrigin = '0 0';
                svg.style.transform = `scale(${zoom})`;
                canvasState.zoom = zoom;
                updateZoomUI(zoom);
                if (preserveCenter) {
                    const after = renderedSize();
                    const anchorClientX = Number(options.anchorClientX);
                    const anchorClientY = Number(options.anchorClientY);
                    if (Number.isFinite(anchorClientX) && Number.isFinite(anchorClientY)) {
                        const viewportRect = viewport.getBoundingClientRect();
                        const localX = clamp(anchorClientX - viewportRect.left, 0, viewport.clientWidth);
                        const localY = clamp(anchorClientY - viewportRect.top, 0, viewport.clientHeight);
                        viewport.scrollLeft = Math.max(0, (centerXRatio * after.width) - localX);
                        viewport.scrollTop = Math.max(0, (centerYRatio * after.height) - localY);
                    } else {
                        viewport.scrollLeft = Math.max(0, (centerXRatio * after.width) - (viewport.clientWidth / 2));
                        viewport.scrollTop = Math.max(0, (centerYRatio * after.height) - (viewport.clientHeight / 2));
                    }
                }
            };

            const fitZoom = () => {
                const style = window.getComputedStyle ? window.getComputedStyle(viewport) : null;
                const padLeft = style ? (parseFloat(style.paddingLeft) || 0) : 0;
                const padRight = style ? (parseFloat(style.paddingRight) || 0) : 0;
                const padTop = style ? (parseFloat(style.paddingTop) || 0) : 0;
                const padBottom = style ? (parseFloat(style.paddingBottom) || 0) : 0;
                const viewportInnerWidth = Math.max(1, viewport.clientWidth - padLeft - padRight);
                const viewportInnerHeight = Math.max(1, viewport.clientHeight - padTop - padBottom);

                let bounds = null;
                try {
                    const box = svg.getBBox ? svg.getBBox() : null;
                    if (box && Number.isFinite(box.width) && Number.isFinite(box.height) && box.width > 0 && box.height > 0) {
                        bounds = {
                            x: Math.max(0, box.x),
                            y: Math.max(0, box.y),
                            width: Math.max(1, box.width),
                            height: Math.max(1, box.height),
                        };
                    }
                } catch (_err) {
                    // Some SVG implementations can throw if bbox is unavailable.
                }

                if (!bounds) {
                    const nodes = Array.from(nodeStates.values());
                    if (nodes.length) {
                        const minX = Math.min(...nodes.map((n) => n.x));
                        const minY = Math.min(...nodes.map((n) => n.y));
                        const maxX = Math.max(...nodes.map((n) => n.x + n.w));
                        const maxY = Math.max(...nodes.map((n) => n.y + n.h));
                        bounds = {
                            x: Math.max(0, minX),
                            y: Math.max(0, minY),
                            width: Math.max(1, maxX - minX),
                            height: Math.max(1, maxY - minY),
                        };
                    }
                }

                if (!bounds) {
                    bounds = { x: 0, y: 0, width: baseCanvasWidth, height: baseCanvasHeight };
                }

                const fitPad = isModal ? 18 : 12;
                const targetWidth = Math.min(baseCanvasWidth, bounds.width + (fitPad * 2));
                const targetHeight = Math.min(baseCanvasHeight, bounds.height + (fitPad * 2));
                const fitX = viewportInnerWidth / Math.max(1, targetWidth);
                const fitY = viewportInnerHeight / Math.max(1, targetHeight);
                const fit = clamp(Math.min(fitX, fitY), minZoom, maxZoom);

                applyZoom(fit, { preserveCenter: false });

                const scaledLeft = Math.max(0, (bounds.x - fitPad) * fit);
                const scaledTop = Math.max(0, (bounds.y - fitPad) * fit);
                const scaledWidth = Math.max(1, (bounds.width + (fitPad * 2)) * fit);
                const scaledHeight = Math.max(1, (bounds.height + (fitPad * 2)) * fit);
                viewport.scrollLeft = Math.max(0, scaledLeft - Math.max(0, (viewportInnerWidth - scaledWidth) / 2));
                viewport.scrollTop = Math.max(0, scaledTop - Math.max(0, (viewportInnerHeight - scaledHeight) / 2));
            };

            const nodeStates = new Map();
            panel.querySelectorAll('[data-chaos-flow-node]').forEach((el) => {
                const id = String(el.dataset.chaosNodeId || '').trim();
                if (!id) {
                    return;
                }
                if (window.tippy && !el._tippy) {
                    if (el.hasAttribute('title')) {
                        el.removeAttribute('title');
                    }
                    window.tippy(el, {
                        delay: [150, 0],
                        placement: 'top',
                        trigger: 'mouseenter',
                    });
                }
                const state = {
                    id,
                    el,
                    x: Number(el.dataset.chaosNodeX) || 0,
                    y: Number(el.dataset.chaosNodeY) || 0,
                    baseX: Number(el.dataset.chaosNodeX) || 0,
                    baseY: Number(el.dataset.chaosNodeY) || 0,
                    w: Number(el.dataset.chaosNodeW) || 0,
                    h: Number(el.dataset.chaosNodeH) || 0,
                };
                const saved = overrides.get(id);
                if (saved && typeof saved === 'object') {
                    state.x = Number(saved.x) || state.x;
                    state.y = Number(saved.y) || state.y;
                }
                nodeStates.set(id, state);
            });

            const edgeStates = [];
            const edgesByNodeId = new Map();
            const addEdgeRef = (nodeID, edgeState) => {
                if (!nodeID) {
                    return;
                }
                if (!edgesByNodeId.has(nodeID)) {
                    edgesByNodeId.set(nodeID, []);
                }
                edgesByNodeId.get(nodeID).push(edgeState);
            };
            panel.querySelectorAll('[data-chaos-flow-edge]').forEach((el) => {
                const fromId = String(el.dataset.chaosEdgeFrom || '').trim();
                const toId = String(el.dataset.chaosEdgeTo || '').trim();
                if (!fromId || !toId || !nodeStates.has(fromId) || !nodeStates.has(toId)) {
                    return;
                }
                const edgeState = {
                    el,
                    fromId,
                    toId,
                    edgeIndex: Number(el.dataset.chaosEdgeIndex) || 0,
                    count: Number(el.dataset.chaosEdgeCount) || 0,
                    underlayEl: el.querySelector('[data-chaos-flow-edge-underlay]'),
                    coreEl: el.querySelector('[data-chaos-flow-edge-core]'),
                    arrowEl: el.querySelector('[data-chaos-flow-edge-arrow]'),
                    dotFromEl: el.querySelector('[data-chaos-flow-edge-dot-from]'),
                    dotToEl: el.querySelector('[data-chaos-flow-edge-dot-to]'),
                };
                edgeStates.push(edgeState);
                addEdgeRef(fromId, edgeState);
                addEdgeRef(toId, edgeState);
            });

            const applyNodeTransform = (node) => {
                if (!node || !node.el) {
                    return;
                }
                node.el.setAttribute('transform', `translate(${node.x},${node.y})`);
                node.el.dataset.chaosNodeX = String(node.x);
                node.el.dataset.chaosNodeY = String(node.y);
            };

            const applyEdgeGeometry = (edgeState) => {
                if (!edgeState) {
                    return;
                }
                const from = nodeStates.get(edgeState.fromId);
                const to = nodeStates.get(edgeState.toId);
                if (!from || !to) {
                    return;
                }
                const geom = computeChaosMapFlowEdgeGeometry({
                    from,
                    to,
                    edgeIndex: edgeState.edgeIndex,
                    count: edgeState.count,
                }, {
                    isModal,
                    maxEdgeCount,
                });
                if (edgeState.underlayEl) {
                    edgeState.underlayEl.setAttribute('d', geom.path);
                    edgeState.underlayEl.setAttribute('stroke-width', (geom.strokeWidth + 5.5).toFixed(2));
                }
                if (edgeState.coreEl) {
                    edgeState.coreEl.setAttribute('d', geom.path);
                    edgeState.coreEl.setAttribute('stroke-width', geom.strokeWidth.toFixed(2));
                }
                if (edgeState.arrowEl) {
                    edgeState.arrowEl.setAttribute('d', geom.arrowHeadPath);
                }
                if (edgeState.dotFromEl) {
                    edgeState.dotFromEl.setAttribute('cx', geom.x1.toFixed(1));
                    edgeState.dotFromEl.setAttribute('cy', geom.y1.toFixed(1));
                }
                if (edgeState.dotToEl) {
                    edgeState.dotToEl.setAttribute('cx', geom.targetAnchorX.toFixed(1));
                    edgeState.dotToEl.setAttribute('cy', geom.y2.toFixed(1));
                }
            };

            const rerouteEdgesForNode = (nodeID) => {
                (edgesByNodeId.get(nodeID) || []).forEach((edgeState) => applyEdgeGeometry(edgeState));
            };
            const rerouteAllEdges = () => {
                edgeStates.forEach((edgeState) => applyEdgeGeometry(edgeState));
            };
            const resetNodeLayout = () => {
                overrides.clear();
                nodeStates.forEach((node) => {
                    node.x = node.baseX;
                    node.y = node.baseY;
                    applyNodeTransform(node);
                });
                rerouteAllEdges();
            };

            nodeStates.forEach((node) => applyNodeTransform(node));
            edgeStates.forEach((edgeState) => applyEdgeGeometry(edgeState));
            applyZoom(canvasState.zoom || 1, { preserveCenter: false });

            if (zoomOutBtn) {
                zoomOutBtn.addEventListener('click', () => {
                    applyZoom((canvasState.zoom || 1) / 1.2);
                });
            }
            if (zoomInBtn) {
                zoomInBtn.addEventListener('click', () => {
                    applyZoom((canvasState.zoom || 1) * 1.2);
                });
            }
            if (zoomResetBtn) {
                zoomResetBtn.addEventListener('click', () => {
                    applyZoom(1);
                });
            }
            if (zoomFitBtn) {
                zoomFitBtn.addEventListener('click', () => {
                    fitZoom();
                });
            }
            if (layoutResetBtn) {
                layoutResetBtn.addEventListener('click', () => {
                    resetNodeLayout();
                });
            }

            const svgScale = () => {
                const rect = svg.getBoundingClientRect();
                const width = Math.max(1, rect.width);
                const height = Math.max(1, rect.height);
                return {
                    x: (viewBox.width || width) / width,
                    y: (viewBox.height || height) / height,
                };
            };

            let panState = null;
            const stopPan = () => {
                if (!panState) {
                    return;
                }
                try {
                    if (viewport.hasPointerCapture && viewport.hasPointerCapture(panState.pointerId)) {
                        viewport.releasePointerCapture(panState.pointerId);
                    }
                } catch (_err) {
                    // ignore
                }
                panState = null;
                viewport.classList.remove('is-panning');
            };
            viewport.addEventListener('pointerdown', (event) => {
                if (event.button !== 0) {
                    return;
                }
                if (event.target && event.target.closest && event.target.closest('[data-chaos-flow-node]')) {
                    return;
                }
                panState = {
                    pointerId: event.pointerId,
                    startX: event.clientX,
                    startY: event.clientY,
                    scrollLeft: viewport.scrollLeft,
                    scrollTop: viewport.scrollTop,
                    outerScrollTop: outerFlowScroller ? outerFlowScroller.scrollTop : 0,
                };
                try {
                    if (viewport.setPointerCapture) {
                        viewport.setPointerCapture(event.pointerId);
                    }
                } catch (_err) {
                    // ignore
                }
                viewport.classList.add('is-panning');
                event.preventDefault();
            });
            viewport.addEventListener('pointermove', (event) => {
                if (!panState || event.pointerId !== panState.pointerId) {
                    return;
                }
                const dx = event.clientX - panState.startX;
                const dy = event.clientY - panState.startY;
                const desiredLeft = panState.scrollLeft - dx;
                const desiredTop = panState.scrollTop - dy;
                const maxLeft = Math.max(0, viewport.scrollWidth - viewport.clientWidth);
                const maxTop = Math.max(0, viewport.scrollHeight - viewport.clientHeight);
                const clampedLeft = clamp(desiredLeft, 0, maxLeft);
                const clampedTop = clamp(desiredTop, 0, maxTop);
                viewport.scrollLeft = clampedLeft;
                viewport.scrollTop = clampedTop;

                // When the viewport hits its vertical scroll limit, continue the
                // same drag gesture by scrolling the outer modal content area so
                // the user can keep panning the full flow canvas context.
                if (outerFlowScroller && outerFlowScroller !== viewport) {
                    const verticalOvershoot = desiredTop - clampedTop;
                    if (verticalOvershoot !== 0) {
                        const outerMaxTop = Math.max(0, outerFlowScroller.scrollHeight - outerFlowScroller.clientHeight);
                        outerFlowScroller.scrollTop = clamp(
                            panState.outerScrollTop + verticalOvershoot,
                            0,
                            outerMaxTop,
                        );
                    }
                }
                event.preventDefault();
            });
            viewport.addEventListener('pointerup', stopPan);
            viewport.addEventListener('pointercancel', stopPan);
            viewport.addEventListener('pointerleave', (event) => {
                if (panState && event.buttons === 0) {
                    stopPan();
                }
            });
            viewport.addEventListener('wheel', (event) => {
                const targetInLegend = event.target && event.target.closest && event.target.closest('.chaos-map-flow-legend');
                if (targetInLegend) {
                    return;
                }
                const targetInSvg = !!(event.target && svg && (event.target === svg || (event.target.closest && event.target.closest('[data-chaos-flow-svg]'))));
                if (!targetInSvg) {
                    return;
                }
                if (dragState || panState) {
                    return;
                }
                if (event.shiftKey) {
                    return;
                }
                const dominantDelta = Math.abs(event.deltaY) >= Math.abs(event.deltaX) ? event.deltaY : event.deltaX;
                if (!Number.isFinite(dominantDelta) || dominantDelta === 0) {
                    return;
                }
                const ctrlLike = event.ctrlKey || event.metaKey;
                const explicitZoomGesture = ctrlLike || event.altKey;
                if (!explicitZoomGesture) {
                    return;
                }
                const step = dominantDelta < 0 ? 1.12 : (1 / 1.12);
                applyZoom((canvasState.zoom || 1) * step, {
                    anchorClientX: event.clientX,
                    anchorClientY: event.clientY,
                });
                event.preventDefault();
            }, { passive: false });

            let dragState = null;
            let lastNodeDragAt = 0;
            const stopDrag = () => {
                if (!dragState) {
                    return;
                }
                if (dragState.moved) {
                    lastNodeDragAt = Date.now();
                }
                try {
                    if (dragState.node && dragState.node.el && dragState.node.el.hasPointerCapture && dragState.node.el.hasPointerCapture(dragState.pointerId)) {
                        dragState.node.el.releasePointerCapture(dragState.pointerId);
                    }
                } catch (_err) {
                    // ignore
                }
                if (dragState.node && dragState.node.el) {
                    dragState.node.el.classList.remove('is-dragging');
                }
                dragState = null;
                panel.classList.remove('is-dragging-node');
            };
            svg.addEventListener('pointerdown', (event) => {
                if (event.button !== 0) {
                    return;
                }
                const nodeEl = event.target && event.target.closest ? event.target.closest('[data-chaos-flow-node]') : null;
                if (!nodeEl) {
                    return;
                }
                const nodeID = String(nodeEl.dataset.chaosNodeId || '').trim();
                const node = nodeID ? nodeStates.get(nodeID) : null;
                if (!node) {
                    return;
                }
                dragState = {
                    pointerId: event.pointerId,
                    node,
                    startClientX: event.clientX,
                    startClientY: event.clientY,
                    startX: node.x,
                    startY: node.y,
                    moved: false,
                };
                node.el.classList.add('is-dragging');
                panel.classList.add('is-dragging-node');
                try {
                    if (node.el.setPointerCapture) {
                        node.el.setPointerCapture(event.pointerId);
                    }
                } catch (_err) {
                    // ignore
                }
                stopPan();
                event.preventDefault();
                event.stopPropagation();
            });
            svg.addEventListener('pointermove', (event) => {
                if (!dragState || event.pointerId !== dragState.pointerId) {
                    return;
                }
                const scale = svgScale();
                const dx = (event.clientX - dragState.startClientX) * scale.x;
                const dy = (event.clientY - dragState.startClientY) * scale.y;
                if (!dragState.moved && ((Math.abs(dx) + Math.abs(dy)) > 3)) {
                    dragState.moved = true;
                }
                const node = dragState.node;
                node.x = clamp(dragState.startX + dx, pad, Math.max(pad, (viewBox.width || 0) - node.w - pad));
                node.y = clamp(dragState.startY + dy, pad, Math.max(pad, (viewBox.height || 0) - node.h - pad));
                applyNodeTransform(node);
                rerouteEdgesForNode(node.id);
                overrides.set(node.id, { x: node.x, y: node.y });
                event.preventDefault();
            });
            svg.addEventListener('pointerup', stopDrag);
            svg.addEventListener('pointercancel', stopDrag);
            svg.addEventListener('pointerleave', (event) => {
                if (dragState && event.buttons === 0) {
                    stopDrag();
                }
            });
            const openFlowNodeHintsFromElement = (nodeEl) => {
                if (!nodeEl) {
                    return;
                }
                const nodeID = String(nodeEl.dataset.chaosNodeId || '').trim();
                const label = String(nodeEl.dataset.chaosNodeLabel || 'Screen').trim() || 'Screen';
                const sampleHash = String(nodeEl.dataset.chaosNodeSampleHash || '').trim();
                const memberHashes = String(nodeEl.dataset.chaosNodeMemberHashes || '')
                    .split(',')
                    .map((h) => h.trim())
                    .filter(Boolean);
                openFlowScreenHintsEditor({ nodeID, label, sampleHash, memberHashes });
            };
            svg.addEventListener('click', (event) => {
                const nodeEl = event.target && event.target.closest ? event.target.closest('[data-chaos-flow-node]') : null;
                if (!nodeEl) {
                    return;
                }
                if ((Date.now() - lastNodeDragAt) < 250) {
                    return;
                }
                openFlowNodeHintsFromElement(nodeEl);
                event.preventDefault();
                event.stopPropagation();
            });
            svg.addEventListener('keydown', (event) => {
                if (event.key !== 'Enter' && event.key !== ' ') {
                    return;
                }
                const nodeEl = event.target && event.target.closest ? event.target.closest('[data-chaos-flow-node]') : null;
                if (!nodeEl) {
                    return;
                }
                openFlowNodeHintsFromElement(nodeEl);
                event.preventDefault();
                event.stopPropagation();
            });
        });
    };

    const chaosNoMapMarkup = '<p class="subtle" data-chaos-map-empty>No screens mapped yet. Start an exploration or load a saved run.</p>';

    // force redraws regardless; otherwise the diagram is only rebuilt when the
    // map behind it changed, and not while somebody is dragging it about.
    const renderChaosFlowModal = (options = {}) => {
        if (!flowContent) {
            return;
        }
        const force = options.force !== false;
        const signature = `${fullMapSlimSignature}|${latestFirstScreenHash}`;
        if (!force) {
            if (signature === flowRenderedSignature) {
                return;
            }
            if (flowPointerActive) {
                return;
            }
        }
        const areas = getChaosFlowAreas();
        flowRenderedSignature = signature;
        if (!areas.length) {
            flowContent.innerHTML = chaosNoMapMarkup;
            return;
        }
        // Keep whatever the reader had scrolled to.
        const scrolled = Array.from(flowContent.querySelectorAll('*'))
            .filter((el) => el.scrollTop || el.scrollLeft)
            .map((el) => ({ className: String(el.className && el.className.baseVal !== undefined ? el.className.baseVal : el.className), top: el.scrollTop, left: el.scrollLeft }));
        const ownTop = flowContent.scrollTop;
        flowContent.innerHTML = chaosMapFlowMarkup(areas, {
            isModal: true,
            markerId: 'chaos-map-flow-arrow-modal',
        });
        initChaosFlowCanvasInteractions(flowContent);
        if (!force) {
            flowContent.scrollTop = ownTop;
            scrolled.forEach((entry) => {
                if (!entry.className) {
                    return;
                }
                const match = Array.from(flowContent.querySelectorAll('*'))
                    .find((el) => String(el.className && el.className.baseVal !== undefined ? el.className.baseVal : el.className) === entry.className);
                if (match) {
                    match.scrollTop = entry.top;
                    match.scrollLeft = entry.left;
                }
            });
        }
    };

    const chaosReportFileName = () => (loadedRunID ? `discovery-report-${loadedRunID}.md` : 'discovery-report.md');

    const setReportRawToggleState = (rawMode) => {
        reportRawMarkdownMode = !!rawMode;
        if (!reportRawToggleBtn) {
            return;
        }
        reportRawToggleBtn.setAttribute('aria-pressed', reportRawMarkdownMode ? 'true' : 'false');
        reportRawToggleBtn.textContent = reportRawMarkdownMode ? 'Formatted View' : 'Raw Markdown';
    };

    const renderChaosReportCurrentView = () => {
        if (!reportContent) {
            return;
        }
        const text = String(latestChaosReportMarkdown || '');
        if (!text.trim()) {
            reportContent.classList.remove('is-raw');
            reportContent.innerHTML = '<p class="subtle">No report content returned.</p>';
            return;
        }
        if (reportRawMarkdownMode) {
            reportContent.classList.add('is-raw');
            reportContent.innerHTML = '';
            const textarea = document.createElement('textarea');
            textarea.className = 'chaos-report-raw-textarea';
            textarea.setAttribute('aria-label', 'Discovery report, raw Markdown');
            textarea.setAttribute('spellcheck', 'false');
            textarea.readOnly = true;
            textarea.value = text;
            reportContent.appendChild(textarea);
            try {
                textarea.focus();
                textarea.select();
            } catch (_err) {
                // ignore
            }
        } else {
            reportContent.classList.remove('is-raw');
            reportContent.innerHTML = renderChaosReportMarkdown(text);
        }
        reportContent.scrollTop = 0;
    };

    const setChaosReportContent = (markdown) => {
        latestChaosReportMarkdown = String(markdown || '');
        renderChaosReportCurrentView();
    };

    const fetchChaosReportMarkdown = async () => {
        const resp = await fetch('/chaos/report', { method: 'POST' });
        if (!resp.ok) {
            throw new Error(await chaosErrorMessage(resp, `The server answered ${resp.status}.`));
        }
        return resp.text();
    };

    const refreshChaosReportView = async (options = {}) => {
        if (!reportContent) {
            return '';
        }
        setReportStatus(options.statusMessage || 'Loading the discovery report\u2026');
        reportContent.innerHTML = '<p class="subtle">Loading report\u2026</p>';
        try {
            const markdown = await fetchChaosReportMarkdown();
            latestChaosReportMarkdown = String(markdown || '');
            setChaosReportContent(latestChaosReportMarkdown);
            // The status line a few pixels above the report already says this.
            // A toast as well landed on top of the dialog's own buttons.
            setReportStatus('Discovery report loaded.');
            return latestChaosReportMarkdown;
        } catch (err) {
            const message = (err && err.message) ? err.message : 'Failed to load the discovery report.';
            setReportStatus(message, true);
            reportContent.classList.remove('is-raw');
            reportContent.innerHTML = `
                <div class="chaos-inline-error" role="alert">
                    <svg viewBox="0 0 24 24" aria-hidden="true" focusable="false"><path d="M1 21h22L12 2 1 21zm12-3h-2v-2h2v2zm0-4h-2v-4h2v4z"/></svg>
                    <div>
                        <strong>The report could not be loaded.</strong>
                        <p>${escapeHtml(message)}</p>
                        <p class="subtle">Choose Refresh to try again.</p>
                    </div>
                </div>
            `;
            throw err;
        }
    };

    const downloadChaosReportMarkdown = async () => {
        let markdown = String(latestChaosReportMarkdown || '');
        if (!markdown.trim()) {
            markdown = await fetchChaosReportMarkdown();
            latestChaosReportMarkdown = String(markdown || '');
        }
        const blob = new Blob([markdown], { type: 'text/markdown' });
        const url = URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = chaosReportFileName();
        document.body.appendChild(a);
        a.click();
        document.body.removeChild(a);
        URL.revokeObjectURL(url);
    };

    const openChaosReportModal = async () => {
        if (!reportModal) {
            return;
        }
        openChaosModal(reportModal, chaosReportTopFocusSelector);
        try {
            if (reportContent) {
                reportContent.scrollTop = 0;
            }
        } catch (_err) {
            // ignore
        }
        await refreshChaosReportView();
    };

    // One card of the screen map: its markup, and the wiring that markup needs.
    const chaosMapCardModel = (group) => {
        const hash = String(group.representativeHash || '').trim();
        const cardKey = String(group.templateId || hash || 'screen').trim();
        const memberHashes = Array.isArray(group.memberHashes) ? group.memberHashes.slice() : (hash ? [hash] : []);
        const hint = mergeScreenHintsForHashes(memberHashes);
        const mergedKeyAssignments = normalizeKeyAssignments(hint.keyAssignments || {});
        const fieldDiscovery = summarizeChaosGroupFieldDiscovery(group);
        const dirty = memberHashes.some((memberHash) => !!(screenHintDrafts[memberHash] && screenHintDrafts[memberHash]._dirty));

        const edgeRows = [];
        const keyPresses = group.keyPresses && typeof group.keyPresses === 'object' ? group.keyPresses : {};
        Object.entries(keyPresses).forEach(([key, kp]) => {
            if (!kp || typeof kp !== 'object') {
                return;
            }
            const dests = kp.destinations && typeof kp.destinations === 'object' ? kp.destinations : {};
            const destList = Object.entries(dests)
                .sort((a, b) => (Number(b[1]) || 0) - (Number(a[1]) || 0))
                .slice(0, 3)
                .map(([toTemplateID, count]) => {
                    const destLabel = String((group.destinationLabelByTemplateId && group.destinationLabelByTemplateId[toTemplateID]) || toTemplateID || '').trim();
                    const compactLabel = destLabel || String(toTemplateID || '').slice(0, 8);
                    return `${escapeHtml(compactLabel.slice(0, 28))} (${Number(count) || 0})`;
                })
                .join(', ');
            const presses = Number(kp.presses) || 0;
            const progs = Number(kp.progressions) || 0;
            edgeRows.push(`<div class="chaos-map-edge"><strong>${escapeHtml(key)}</strong> | ${presses} press${presses === 1 ? '' : 'es'} | ${progs} progression${progs === 1 ? '' : 's'}${destList ? ` | to ${destList}` : ''}</div>`);
        });
        edgeRows.sort();

        const titleLabel = String(group.label || hash || 'Screen').trim() || 'Screen';
        const hashMeta = memberHashes.length > 1
            ? `${memberHashes.length} variants merged`
            : (hash || 'unknown-hash');
        const businessPurpose = (group.memberAreas || [])
            .map((a) => String((a && a.businessPurpose) || '').trim())
            .find(Boolean) || '';

        const html = `
                <div class="chaos-map-card-header">
                    <div class="chaos-map-card-title">
                        <strong>${escapeHtml(titleLabel)}</strong>
                        ${businessPurpose ? `<span class="chaos-map-card-purpose">${escapeHtml(businessPurpose)}</span>` : ''}
                        <span class="chaos-map-card-hash">${escapeHtml(hashMeta)}</span>
                    </div>
                    <span class="chaos-map-chip">${Number(group.visits) || 0} visits</span>
                </div>
                ${chaosMapCardScreenPreviewMarkup(group.representativeArea)}
                <div class="chaos-map-chip-row">
                    <span class="chaos-map-chip">${Number(group.fieldCount) || 0} fields</span>
                    <span class="chaos-map-chip">${Number(group.inputFieldCount) || 0} inputs</span>
                    <span class="chaos-map-chip">${Number(group.numericFieldCount) || 0} numeric</span>
                    <span class="chaos-map-chip">${Object.keys(keyPresses).length} keys</span>
                    ${memberHashes.length > 1 ? `<span class="chaos-map-chip">${memberHashes.length} variants</span>` : ''}
                    ${fieldDiscovery.fields > 0 ? `<span class="chaos-map-chip">${fieldDiscovery.fields} discovered fields</span>` : ''}
                    ${fieldDiscovery.triedValues > 0 ? `<span class="chaos-map-chip">${fieldDiscovery.triedValues} tried values</span>` : ''}
                    ${fieldDiscovery.workingValues > 0 ? `<span class="chaos-map-chip">${fieldDiscovery.workingValues} working values</span>` : ''}
                    ${fieldDiscovery.progressions > 0 ? `<span class="chaos-map-chip">${fieldDiscovery.progressions} field progressions</span>` : ''}
                </div>
                <div class="chaos-map-edges">${edgeRows.length ? edgeRows.join('') : '<div class="chaos-map-edge subtle">No key usage yet.</div>'}</div>
                ${chaosMapFieldDiscoveryTableMarkup(group)}
                <div class="chaos-map-hints">
                    <div class="chaos-hint-field">
                        <label class="chaos-hint-field-label" for="chaos-map-data-${escapeHtml(cardKey)}">Known Data</label>
                        <textarea id="chaos-map-data-${escapeHtml(cardKey)}" data-chaos-map-known-data placeholder="Known values for this screen (comma or newline separated)">${escapeHtml(formatListLines(hint.knownData))}</textarea>
                    </div>
                    <div class="chaos-hint-field">
                        <label class="chaos-hint-field-label" for="chaos-map-keys-${escapeHtml(cardKey)}">Known Keys</label>
                        <textarea id="chaos-map-keys-${escapeHtml(cardKey)}" data-chaos-map-known-keys placeholder="Known keys for this screen (e.g. PF3, Enter, Down)">${escapeHtml(formatListLines(hint.knownKeys))}</textarea>
                    </div>
                </div>
                <div class="chaos-map-card-actions">
                    <span class="subtle" data-chaos-map-card-status aria-live="polite">${dirty ? 'Unsaved changes' : ''}</span>
                    <button type="button" data-chaos-map-save aria-label="Save screen hints for ${escapeHtml(titleLabel)}">Save screen hints</button>
                </div>
            `;
        return { hash, cardKey, memberHashes, mergedKeyAssignments, html };
    };

    const buildChaosMapCard = (model) => {
        const { hash, cardKey, memberHashes, mergedKeyAssignments, html } = model;
        const card = document.createElement('section');
        card.className = 'chaos-map-card';
        card.dataset.chaosMapHash = hash;
        card.dataset.chaosMapTemplate = cardKey;
        card.innerHTML = html;
        card._chaosMarkup = html;

        const knownDataEl = card.querySelector('[data-chaos-map-known-data]');
        const knownKeysEl = card.querySelector('[data-chaos-map-known-keys]');
        const saveBtn = card.querySelector('[data-chaos-map-save]');
        const cardStatus = card.querySelector('[data-chaos-map-card-status]');

        const collectDraft = () => ({
            knownData: parseListLines(knownDataEl ? knownDataEl.value : ''),
            knownKeys: parseListLines(knownKeysEl ? knownKeysEl.value : ''),
            keyAssignments: mergedKeyAssignments,
        });
        const syncDraft = () => {
            const draft = collectDraft();
            markScreenHintDraftDirtyForHashes(memberHashes, draft);
            if (cardStatus) {
                cardStatus.textContent = 'Unsaved changes';
            }
        };
        if (knownDataEl) {
            knownDataEl.addEventListener('input', syncDraft);
        }
        if (knownKeysEl) {
            knownKeysEl.addEventListener('input', syncDraft);
        }
        if (saveBtn) {
            saveBtn.addEventListener('click', async () => {
                if (cardStatus) {
                    cardStatus.textContent = 'Saving...';
                }
                const draft = collectDraft();
                memberHashes.forEach((memberHash) => {
                    screenHintDrafts[memberHash] = { ...draft, _dirty: false };
                });
                const saved = await saveScreenHintsForHashes(memberHashes, draft);
                const liveStatus = card.isConnected
                    ? cardStatus
                    : (mapList ? mapList.querySelector(`[data-chaos-map-template="${window.CSS && CSS.escape ? CSS.escape(cardKey) : cardKey}"] [data-chaos-map-card-status]`) : null);
                if (liveStatus) {
                    liveStatus.textContent = saved ? 'Saved' : 'Save failed';
                }
            });
        }
        return card;
    };

    const chaosMapFocusedCard = () => {
        const active = document.activeElement;
        if (!mapList || !active || !mapList.contains(active)) {
            return null;
        }
        return active.closest ? active.closest('.chaos-map-card') : null;
    };

    // The list is patched, not rebuilt. While an exploration runs this is
    // called every few seconds, and rebuilding it threw away the caret, the
    // focus and the scroll position of anybody typing a hint into it. A card
    // is only replaced when what it shows has changed; the card being edited
    // is left alone until focus moves on; cards keep their place, with newly
    // found screens added at the end. reset (opening the dialog, Refresh)
    // rebuilds from scratch and re-sorts by visits.
    const renderChaosMap = (options = {}) => {
        if (!mapList) {
            return;
        }
        const reset = !!options.reset;
        const areas = getChaosMapCardAreas();
        const hasAnyAreas = getChaosFlowAreas().length > 0;
        if (mapViewBtn) {
            mapViewBtn.disabled = !hasAnyAreas;
        }
        if (!areas.length) {
            if (chaosMapFocusedCard() && !reset) {
                mapRenderPending = true;
                return;
            }
            mapRenderPending = false;
            mapList.innerHTML = hasAnyAreas
                ? '<p class="subtle" data-chaos-map-empty>Only the first screen has been mapped so far. Its hints are set under Exploration hints.</p>'
                : chaosNoMapMarkup;
            return;
        }
        const focusedCard = reset ? null : chaosMapFocusedCard();
        if (reset) {
            mapList.innerHTML = '';
        } else {
            mapList.querySelectorAll('[data-chaos-map-empty]').forEach((el) => el.remove());
        }
        const existing = new Map();
        mapList.querySelectorAll('.chaos-map-card').forEach((card) => {
            existing.set(String(card.dataset.chaosMapTemplate || ''), card);
        });
        let pending = false;
        const seen = new Set();
        const templateGroups = buildChaosMapTemplateGroups(areas).groups;
        templateGroups.forEach((group) => {
            const model = chaosMapCardModel(group);
            seen.add(model.cardKey);
            const current = existing.get(model.cardKey);
            if (!current) {
                mapList.appendChild(buildChaosMapCard(model));
                return;
            }
            if (current._chaosMarkup === model.html) {
                return;
            }
            if (current === focusedCard) {
                pending = true;
                return;
            }
            const next = buildChaosMapCard(model);
            const discovery = current.querySelector('[data-chaos-map-field-discovery]');
            const showedAllFields = !!(discovery && discovery.dataset.fieldMode === 'all');
            current.replaceWith(next);
            if (showedAllFields) {
                const toggle = next.querySelector('[data-chaos-map-field-toggle]');
                if (toggle) {
                    toggle.click();
                }
            }
        });
        existing.forEach((card, key) => {
            if (seen.has(key)) {
                return;
            }
            if (card === focusedCard) {
                pending = true;
                return;
            }
            card.remove();
        });
        mapRenderPending = pending;
    };

    // What the two counts mean, said the same way everywhere: a "screen" is a
    // layout, and its "variants" are the copies of it that differ only in the
    // data on them. The server counts variants; the map draws screens.
    const formatChaosScreenCount = (screens, variants) => {
        const s = Number(screens) || 0;
        const v = Number(variants) || 0;
        if (s <= 0) {
            return v > 0 ? `${v} screen variant${v === 1 ? '' : 's'}` : '';
        }
        return `${s} screen${s === 1 ? '' : 's'}${v > s ? ` (${v} variants)` : ''}`;
    };

    const chaosMapCountsText = () => {
        const all = getChaosFlowAreas();
        if (!all.length) {
            return '';
        }
        if (!latestMindMapIsFull) {
            return formatChaosScreenCount(0, all.length);
        }
        return formatChaosScreenCount(buildChaosMapTemplateGroups(all).groups.length, all.length);
    };

    const chaosMapStatusText = () => {
        const counts = chaosMapCountsText();
        if (!counts) {
            return 'No screens mapped yet.';
        }
        const parts = [`${counts} mapped.`];
        if (String(latestFirstScreenHash || '').trim()) {
            parts.push('The first screen is not listed here; its hints are under Exploration hints.');
        }
        if (lastStatus && lastStatus.active) {
            parts.push(mapRenderPending
                ? 'Still exploring: the screen you are editing updates when you leave it.'
                : 'Still exploring: this list updates as screens are found.');
        }
        return parts.join(' ');
    };

    const chaosFlowStatusText = () => {
        const counts = chaosMapCountsText();
        if (!counts) {
            return 'No screens mapped yet.';
        }
        return `${counts} in the diagram.${lastStatus && lastStatus.active ? ' Still exploring: it redraws as screens are found.' : ''}`;
    };

    const normalizeHints = (rawHints) => {
        if (!Array.isArray(rawHints)) {
            return [];
        }
        const normalized = [];
        rawHints.forEach((entry) => {
            if (!entry || typeof entry !== 'object') {
                return;
            }
            const transaction = String(entry.transaction || '').trim();
            const knownData = Array.isArray(entry.knownData)
                ? entry.knownData.map((item) => String(item || '').trim()).filter((item) => item.length > 0)
                : parseKnownData(entry.knownData || '');
            const keyAssignments = normalizeKeyAssignments(entry.keyAssignments || {});
            if (!transaction && knownData.length === 0 && Object.keys(keyAssignments).length === 0) {
                return;
            }
            normalized.push({ transaction, knownData, keyAssignments });
        });
        return normalized;
    };

    const mergeHints = (baseHints, extractedHints) => {
        const merged = normalizeHints([...(baseHints || []), ...(extractedHints || [])]);
        const seen = new Set();
        const out = [];
        merged.forEach((hint) => {
            const tx = String(hint.transaction || '').trim();
            const knownData = Array.isArray(hint.knownData)
                ? hint.knownData.map((item) => String(item || '').trim()).filter((item) => item.length > 0)
                : [];
            const keyAssignments = normalizeKeyAssignments(hint.keyAssignments || {});
            const assignmentsKey = Object.keys(keyAssignments)
                .sort((a, b) => a.localeCompare(b))
                .map((label) => `${label}=${keyAssignments[label]}`)
                .join('\u001e');
            const key = `${tx.toUpperCase()}|${knownData.join('\u001f')}|${assignmentsKey}`;
            if (seen.has(key)) {
                return;
            }
            seen.add(key);
            out.push({ transaction: tx, knownData, keyAssignments });
        });
        return out;
    };

    const createHintRow = (hint = {}) => {
        if (!hintsList) {
            return;
        }
        const row = document.createElement('div');
        row.className = 'chaos-hint-row';

        const rowID = ++hintRowSequence;
        const txField = document.createElement('div');
        txField.className = 'chaos-hint-field';
        const txLabel = document.createElement('label');
        txLabel.className = 'chaos-hint-field-label';
        txLabel.textContent = 'Transaction';

        const txInput = document.createElement('input');
        txInput.type = 'text';
        txInput.placeholder = 'Transaction (e.g., CEMT)';
        txInput.value = hint.transaction || '';
        txInput.id = `chaos-hint-transaction-${rowID}`;
        txInput.dataset.chaosHintTransaction = '1';
        txInput.addEventListener('input', markHintsDirty);
        txLabel.setAttribute('for', txInput.id);
        txField.appendChild(txLabel);
        txField.appendChild(txInput);

        const knownDataField = document.createElement('div');
        knownDataField.className = 'chaos-hint-field';
        const knownDataLabel = document.createElement('label');
        knownDataLabel.className = 'chaos-hint-field-label';
        knownDataLabel.textContent = 'Known Working Data';

        const knownDataInput = document.createElement('textarea');
        knownDataInput.placeholder = 'Known data values (comma or newline separated)';
        knownDataInput.value = Array.isArray(hint.knownData) ? hint.knownData.join('\n') : '';
        knownDataInput.id = `chaos-hint-data-${rowID}`;
        knownDataInput.dataset.chaosHintData = '1';
        knownDataInput.addEventListener('input', markHintsDirty);
        knownDataLabel.setAttribute('for', knownDataInput.id);
        knownDataField.appendChild(knownDataLabel);
        knownDataField.appendChild(knownDataInput);

        const keyAssignmentsField = document.createElement('div');
        keyAssignmentsField.className = 'chaos-hint-field';
        const keyAssignmentsLabel = document.createElement('label');
        keyAssignmentsLabel.className = 'chaos-hint-field-label';
        keyAssignmentsLabel.textContent = 'Key Assignments';

        const keyAssignmentsInput = document.createElement('textarea');
        keyAssignmentsInput.placeholder = 'Label = Key (one per line)\nReturn = PF3\nConfirm = Enter';
        keyAssignmentsInput.value = formatKeyAssignments(hint.keyAssignments || {});
        keyAssignmentsInput.id = `chaos-hint-keys-${rowID}`;
        keyAssignmentsInput.dataset.chaosHintKeyAssignments = '1';
        keyAssignmentsInput.addEventListener('input', markHintsDirty);
        keyAssignmentsLabel.setAttribute('for', keyAssignmentsInput.id);
        keyAssignmentsField.appendChild(keyAssignmentsLabel);
        keyAssignmentsField.appendChild(keyAssignmentsInput);

        const removeBtn = document.createElement('button');
        removeBtn.type = 'button';
        removeBtn.className = 'chaos-hint-remove';
        removeBtn.textContent = 'Remove';
        removeBtn.setAttribute('aria-label', 'Remove this hint');
        removeBtn.addEventListener('click', () => {
            markHintsDirty();
            row.remove();
            if (!hintsList.children.length) {
                createHintRow();
            }
        });

        row.appendChild(txField);
        row.appendChild(knownDataField);
        row.appendChild(keyAssignmentsField);
        row.appendChild(removeBtn);
        hintsList.appendChild(row);
    };

    const collectHintsFromUI = () => {
        if (!hintsList) {
            return [];
        }
        const rows = Array.from(hintsList.querySelectorAll('.chaos-hint-row'));
        return normalizeHints(rows.map((row) => {
            const tx = row.querySelector('[data-chaos-hint-transaction]');
            const knownData = row.querySelector('[data-chaos-hint-data]');
            const keyAssignments = row.querySelector('[data-chaos-hint-key-assignments]');
            return {
                transaction: tx ? tx.value : '',
                knownData: knownData ? parseKnownData(knownData.value) : [],
                keyAssignments: keyAssignments ? parseKeyAssignments(keyAssignments.value) : {},
            };
        }));
    };

    const renderHints = (hints) => {
        if (!hintsList) {
            return;
        }
        hintsList.innerHTML = '';
        const list = normalizeHints(hints);
        if (!list.length) {
            createHintRow();
            hintsDirty = false;
            return;
        }
        list.forEach((hint) => createHintRow(hint));
        hintsDirty = false;
    };

    // quiet suppresses the toast. Hints load on every page init, so
    // announcing the result there put a chaos-mode message ("No saved hints
    // yet.") in front of every user on every screen load, including business
    // users who never open chaos. The modal's own status line still updates,
    // which is where someone who asked for hints is looking.
    const loadHints = async ({ quiet = false } = {}) => {
        try {
            const resp = await fetch('/chaos/hints');
            if (!resp.ok) {
                throw new Error('request failed');
            }
            const payload = await resp.json();
            const loadedHints = normalizeHints(payload.hints || []);
            const loadedKeyBlacklist = parseListLines(payload.keyBlacklist || []);
            const loadedFirstScreenHint = normalizeScreenHint(payload.firstScreenHint || {});
            chaosHints = loadedHints;
            chaosFirstScreenHint = loadedFirstScreenHint;
            if (hintsModal && !hintsModal.hidden && hintsDirty) {
                setHintsStatus('Loaded saved hints; unsaved edits were kept.');
                return chaosHints;
            }
            chaosKeyBlacklist = loadedKeyBlacklist;
            renderHints(chaosHints);
            renderChaosKeyBlacklist(chaosKeyBlacklist);
            renderFirstScreenHint(chaosFirstScreenHint);
            // The dialog's own status line carries this; quiet is kept for
            // callers that load in the background.
            if (!quiet || (hintsModal && !hintsModal.hidden)) {
                setHintsStatus(chaosHints.length ? `Loaded ${chaosHints.length} saved hint(s).` : 'No saved hints yet.');
            }
            return chaosHints;
        } catch (_err) {
            setHintsStatus('Failed to load hints.', true);
            return chaosHints;
        }
    };

    const saveHints = async () => {
        const draftHints = collectHintsFromUI();
        const draftKeyBlacklist = collectChaosKeyBlacklistFromUI();
        const draftFirstScreenHint = collectFirstScreenHintFromUI();
        try {
            const resp = await fetch('/chaos/hints', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    hints: draftHints,
                    keyBlacklist: draftKeyBlacklist,
                    firstScreenHint: draftFirstScreenHint,
                }),
            });
            if (!resp.ok) {
                throw new Error(await chaosErrorMessage(resp, 'Failed to save hints.'));
            }
            const payload = await resp.json();
            chaosHints = normalizeHints(payload.hints || []);
            chaosKeyBlacklist = parseListLines(payload.keyBlacklist || []);
            chaosFirstScreenHint = normalizeScreenHint(payload.firstScreenHint || {});
            renderHints(chaosHints);
            renderChaosKeyBlacklist(chaosKeyBlacklist);
            renderFirstScreenHint(chaosFirstScreenHint);
            setHintsStatus(`Saved ${chaosHints.length} hint(s).`);
            hintsDirty = false;
            return true;
        } catch (err) {
            setHintsStatus((err && err.message) || 'Failed to save hints.', true);
            return false;
        }
    };

    const discardHintEdits = () => {
        renderHints(chaosHints);
        renderChaosKeyBlacklist(chaosKeyBlacklist);
        renderFirstScreenHint(chaosFirstScreenHint);
        hintsDirty = false;
        setHintsStatus('');
    };

    // Closing used to throw unsaved edits away without a word, Blocked Keys
    // among them, and the next run then used whatever had last been saved.
    const requestCloseHintsModal = async () => {
        if (!hintsModal || hintsModal.hidden) {
            return;
        }
        if (!hintsDirty) {
            closeChaosModal(hintsModal);
            return;
        }
        const choice = await promptChaosChoice({
            title: 'Save your changes to the hints?',
            message: 'You have changed the hints or the blocked keys and not saved them. Explorations use the saved version.',
            confirmLabel: 'Save',
            altLabel: 'Discard changes',
            cancelLabel: 'Keep editing',
            danger: false,
            altDanger: true,
        });
        if (choice === 'confirm') {
            const saved = await saveHints();
            if (saved) {
                closeChaosModal(hintsModal);
                notifyUi('Hints saved.', 'success');
            }
            return;
        }
        if (choice === 'alt') {
            discardHintEdits();
            closeChaosModal(hintsModal);
        }
    };

    const extractHintsFromRecording = async (file) => {
        try {
            const formData = new FormData();
            if (file) {
                formData.append('workflow', file, file.name || 'workflow.json');
            }
            const resp = await fetch('/chaos/hints/extract-recording', {
                method: 'POST',
                body: formData,
            });
            let payload = {};
            try {
                payload = await resp.json();
            } catch (_parseErr) {
                payload = {};
            }
            if (!resp.ok) {
                throw new Error(payload.error || 'request failed');
            }
            const extracted = normalizeHints(payload.hints || []);
            const draft = (hintsModal && !hintsModal.hidden) ? collectHintsFromUI() : chaosHints;
            const merged = mergeHints(draft, extracted);
            renderHints(merged);
            // Shown, not saved: closing now has to ask.
            hintsDirty = extracted.length > 0 || hintsDirty;
            const source = payload.source === 'loaded' ? 'loaded recording' : 'recording';
            if (extracted.length > 0) {
                setHintsStatus(`Added ${extracted.length} hint(s) from the ${source}. Save hints to keep them.`);
            } else {
                setHintsStatus(`No hint candidates found in the ${source}.`);
            }
        } catch (err) {
            setHintsStatus((err && err.message) ? err.message : 'Failed to extract hints from the recording.', true);
        }
    };

    const loadScreenHints = async () => {
        try {
            const resp = await fetch('/chaos/screen-hints');
            if (!resp.ok) {
                throw new Error('request failed');
            }
            const payload = await resp.json();
            screenHintsByHash = normalizeScreenHintsMap(payload.screenHints || {});
            return screenHintsByHash;
        } catch (_err) {
            return screenHintsByHash;
        }
    };

    // ---- the full screen map ----------------------------------------
    // The status poll answers with an outline of the map (hash, label and
    // visit count per screen) so that it stays cheap to ask every few
    // seconds. The cards and the diagram need the whole thing: the captured
    // screen, the fields, the keys pressed and where each one led. That comes
    // from ?verbose=true, fetched when a view that draws it opens, and again
    // while one stays open and the run has moved on. An outline never
    // replaces a full map: that was the bug that left both views hollow.
    const mindMapAreaList = (mm) => (mm && mm.areas && typeof mm.areas === 'object'
        ? Object.values(mm.areas).filter((a) => a && typeof a === 'object')
        : []);

    const isSlimMindMap = (mm) => {
        if (!mm || typeof mm !== 'object') {
            return false;
        }
        if (Array.isArray(mm.areas)) {
            return true;
        }
        const areas = mindMapAreaList(mm);
        if (!areas.length) {
            return false;
        }
        return !areas.some((a) => a.fieldCount != null || a.numericFieldCount != null
            || a.keyPresses || a.previewText || a.fieldMetadata);
    };

    const slimMindMapToKeyed = (mm) => {
        const areas = {};
        mindMapAreaList(mm).forEach((area) => {
            const hash = String(area.hash || '').trim();
            if (hash) {
                areas[hash] = area;
            }
        });
        return { areas };
    };

    const chaosMapSignatureFor = (status, mm) => {
        const areas = mindMapAreaList(mm);
        let visits = 0;
        areas.forEach((a) => {
            visits += Number(a.visits) || 0;
        });
        return [
            areas.length,
            visits,
            Number(status && status.stepsRun) || 0,
            String((status && status.loadedRunID) || ''),
        ].join(':');
    };

    const chaosMapViewOpen = () => !!((mapModal && !mapModal.hidden) || (flowModal && !flowModal.hidden));

    const refreshOpenChaosMapViews = () => {
        if (mapModal && !mapModal.hidden) {
            renderChaosMap();
            setChaosMapStatus(chaosMapStatusText());
        }
        if (flowModal && !flowModal.hidden) {
            renderChaosFlowModal({ force: false });
            setFlowStatus(chaosFlowStatusText());
        }
    };

    const fetchFullChaosMap = (options = {}) => {
        if (fullMapFetchInFlight) {
            return fullMapFetchInFlight;
        }
        if (!options.force && (Date.now() - fullMapFetchedAt) < fullMapMinIntervalMs) {
            return Promise.resolve(false);
        }
        fullMapFetchInFlight = (async () => {
            try {
                const resp = await fetch('/chaos/status?verbose=true', { headers: { Accept: 'application/json' } });
                if (handleChaosAuthFailure(resp)) {
                    return false;
                }
                if (!resp.ok) {
                    throw new Error(await chaosErrorMessage(resp, `The server answered ${resp.status}.`));
                }
                const status = await resp.json();
                fullMapFetchedAt = Date.now();
                fullMapUnavailable = !!(status && status.mindMap && isSlimMindMap(status.mindMap));
                updateUI(status);
                return !fullMapUnavailable;
            } catch (err) {
                fullMapFetchedAt = Date.now();
                if (options.onError) {
                    options.onError((err && err.message) ? err.message : 'The screen map could not be loaded.');
                }
                return false;
            } finally {
                fullMapFetchInFlight = null;
            }
        })();
        return fullMapFetchInFlight;
    };

    // After a status poll: is the full map behind what the poll just said?
    const refreshFullChaosMapIfStale = () => {
        if (fullMapUnavailable || latestAreaCount <= 0) {
            return;
        }
        if (chaosMapViewOpen()) {
            if (fullMapSlimSignature !== latestSlimSignature) {
                fetchFullChaosMap();
            }
            return;
        }
        // Nothing is drawing the map, so all it is wanted for is the count of
        // distinct screens the run strip quotes. That only changes when a new
        // screen turns up, which is rare enough to ask about at leisure.
        if (groupedScreenCount.areaCount !== latestAreaCount && (Date.now() - fullMapFetchedAt) > 10000) {
            fetchFullChaosMap();
        }
    };

    // "4 screens (7 variants)" when the layouts have been counted for the map
    // as it now stands, "7 screen variants" when all that is known is how many
    // distinct screens the server has seen.
    const screenCountLabel = (variants) => {
        const v = Number(variants) || 0;
        if (v <= 0) {
            return '';
        }
        if (groupedScreenCount.screens > 0 && groupedScreenCount.areaCount === latestAreaCount) {
            return formatChaosScreenCount(groupedScreenCount.screens, Math.max(v, groupedScreenCount.areaCount));
        }
        return formatChaosScreenCount(0, v);
    };

    const refreshChaosMapView = async () => {
        await Promise.all([loadScreenHints(), pollStatus()]);
        let failure = '';
        await fetchFullChaosMap({ force: true, onError: (message) => { failure = message; } });
        renderChaosMap({ reset: true });
        return failure;
    };

    const openChaosMapModal = async () => {
        if (!mapModal) {
            return false;
        }
        openChaosModal(mapModal, chaosMapTopFocusSelector);
        setChaosMapStatus('Loading the screen map…');
        if (mapList && !latestMindMapIsFull) {
            mapList.innerHTML = '<p class="subtle" data-chaos-map-empty>Loading the screen map…</p>';
        }
        const failure = await refreshChaosMapView();
        if (mapList) {
            mapList.scrollTop = 0;
        }
        const modalPanel = mapModal.querySelector('.modal');
        if (modalPanel) {
            modalPanel.scrollTop = 0;
        }
        const modalStack = mapModal.querySelector('.stack');
        if (modalStack) {
            modalStack.scrollTop = 0;
        }
        if (failure) {
            setChaosMapStatus(failure, true);
        } else {
            setChaosMapStatus(chaosMapStatusText());
        }
        return true;
    };

    const openChaosFlowFromMap = () => {
        if (!flowModal) {
            return;
        }
        openChaosModal(flowModal, '[data-chaos-flow-refresh], [data-chaos-flow-close]', { keepPrevious: true });
        setFlowStatus('Loading the flow diagram…');
        const draw = (failure) => {
            if (!flowModal || flowModal.hidden) {
                return;
            }
            renderChaosFlowModal();
            if (flowContent) {
                flowContent.scrollTop = 0;
            }
            if (failure) {
                setFlowStatus(failure, true);
            } else {
                setFlowStatus(chaosFlowStatusText());
            }
        };
        if (latestMindMapIsFull && fullMapSlimSignature === latestSlimSignature) {
            window.requestAnimationFrame(() => draw(''));
            return;
        }
        let failure = '';
        fetchFullChaosMap({ force: true, onError: (message) => { failure = message; } })
            .then(() => draw(failure));
    };

    const isRecordingActive = () => {
        if (body && body.dataset.recordingActive === 'true') {
            return true;
        }
        if (recordingIndicator && !recordingIndicator.hidden) {
            return true;
        }
        const recordingStop = document.querySelector('[data-recording-stop]');
        return !!(recordingStop && !recordingStop.hidden);
    };
    const isPlaybackActive = () => !!(body && body.dataset.playbackActive === 'true');

    const sectionHasVisibleChild = (section) => {
        if (!section) {
            return false;
        }
        return Array.from(section.children).some((child) => !child.hidden);
    };

    const syncChaosSectionLayout = () => {
        if (chaosSections.length === 0) {
            return;
        }
        chaosSections.forEach((section) => {
            const alwaysVisible = section.dataset.chaosSection === 'run' || section.dataset.chaosSection === 'load';
            section.hidden = !(alwaysVisible || sectionHasVisibleChild(section));
        });
        chaosDividers.forEach((divider, index) => {
            const hasVisibleBefore = chaosSections.slice(0, index + 1).some((section) => !section.hidden);
            const hasVisibleAfter = chaosSections.slice(index + 1).some((section) => !section.hidden);
            divider.hidden = !(hasVisibleBefore && hasVisibleAfter);
        });
    };

    const applyChaosInterlocks = () => {
        const running = !!(lastStatus && lastStatus.active);
        const blockedByWorkflow = isRecordingActive() || isPlaybackActive();

        setButtonDisabledState(
            startBtn,
            !running && blockedByWorkflow,
            'Stop recording/playback before starting an exploration'
        );
        setButtonDisabledState(
            loadRecordingBtn,
            running || blockedByWorkflow,
            running
                ? 'Stop the exploration before loading a recording into it'
                : 'Stop recording/playback before loading a recording into exploration'
        );
        setButtonDisabledState(
            resumeBtn,
            !running && blockedByWorkflow,
            'Stop recording/playback before continuing an exploration'
        );
        [runPanelContinueBtn, activeRunContinueBtn].forEach((btn) => {
            if (btn) {
                btn.disabled = running || blockedByWorkflow || !loadedRunID;
            }
        });
        setButtonDisabledState(
            mapImportBtn,
            running,
            'Stop the exploration before importing a map'
        );
        setButtonDisabledState(
            recordingStartButton,
            running,
            'Stop the exploration before starting a recording'
        );
        setButtonDisabledState(
            workflowLoadButton,
            running,
            'Stop the exploration before loading a recording'
        );
        syncRunsLoadButtons();
    };

    const updateChaosMapFromStatus = (status) => {
        const cleared = !!(status && !status.active && !(status.loadedRunID) && (Number(status.stepsRun) || 0) === 0);
        const incoming = status && status.mindMap && typeof status.mindMap === 'object' ? status.mindMap : null;
        let fullArrived = false;
        if (incoming) {
            const signature = chaosMapSignatureFor(status, incoming);
            latestSlimSignature = signature;
            latestAreaCount = mindMapAreaList(incoming).length;
            if (!isSlimMindMap(incoming)) {
                latestMindMap = incoming;
                latestMindMapIsFull = true;
                fullMapSlimSignature = signature;
                fullArrived = true;
            } else if (!latestMindMapIsFull) {
                // Nothing better is held yet, so keep the outline: it still
                // carries the labels and the count.
                latestMindMap = slimMindMapToKeyed(incoming);
            }
        } else if (cleared) {
            latestMindMap = null;
            latestMindMapIsFull = false;
            latestSlimSignature = '';
            fullMapSlimSignature = '';
            latestAreaCount = 0;
            groupedScreenCount.areaCount = -1;
            groupedScreenCount.screens = 0;
            fullArrived = true;
        }
        if (status && status.screenHints && typeof status.screenHints === 'object') {
            screenHintsByHash = normalizeScreenHintsMap(status.screenHints);
        } else if (cleared) {
            screenHintsByHash = {};
        }
        return fullArrived;
    };

    const updateLoadedChaosActionTooltips = (status, visibleLoadedRunID, visibleHasData) => {
        const steps = Number((status && status.stepsRun) || 0);
        const screens = Number((status && status.uniqueScreens) || 0);
        const inputs = Number((status && status.uniqueInputs) || 0);
        const keys = Number((status && (status.uniqueKeys || (status.aidKeyCounts ? Object.keys(status.aidKeyCounts).length : 0))) || 0);
        const runID = String(visibleLoadedRunID || '').trim();
        const suffixParts = [];
        if (runID) {
            suffixParts.push(`run ${runID}`);
        }
        const summary = chaosCountsSummary({ steps, screens, inputs, keys });
        if (summary) {
            suffixParts.push(summary);
        }
        const suffix = suffixParts.length ? ` (${suffixParts.join(', ')})` : '';
        const applyBaseWithSuffix = (btn, enabled) => {
            if (!btn || btn.disabled || enabled === false) {
                return;
            }
            const base = defaultTooltip(btn);
            if (base) {
                setTooltipContent(btn, `${base}${suffix}`);
            }
        };
        applyBaseWithSuffix(resumeBtn, !!runID);
        applyBaseWithSuffix(reportBtn, !!visibleHasData);
        applyBaseWithSuffix(exportBtn, !!visibleHasData);
        applyBaseWithSuffix(removeBtn, !!visibleHasData);
    };

    // ---- how a run ended, in words ------------------------------------
    const pluralize = (count, singular, plural) => `${count} ${count === 1 ? singular : (plural || `${singular}s`)}`;

    const formatChaosDuration = (seconds) => {
        const s = Math.max(0, Math.round(Number(seconds) || 0));
        if (s < 60) {
            return pluralize(s, 'second');
        }
        const minutes = Math.floor(s / 60);
        const rest = s % 60;
        return rest ? `${pluralize(minutes, 'minute')} ${pluralize(rest, 'second')}` : pluralize(minutes, 'minute');
    };

    // tone is the notification type: an error is not a success, and a run
    // somebody stopped is neither.
    const chaosOutcomeFor = (status) => {
        const st = status || {};
        const steps = Number(st.stepsRun) || 0;
        const errorText = String(st.error || '').trim();
        let reason = String(st.terminationReason || '').trim().toLowerCase();
        const cov = st.coverageStats && typeof st.coverageStats === 'object' ? st.coverageStats : null;
        const streak = cov ? Number(cov.saturationStreak) || 0 : 0;
        if (!reason) {
            // The server did not say. Work it out from what is known, and
            // only when st is the run this page has been watching.
            const threshold = cov ? Number(cov.saturationThresholdSteps) || 0 : 0;
            const started = Date.parse(knownRunLimits.startedAt || '');
            const stopped = Date.parse(knownRunLimits.stoppedAt || '');
            if (errorText) {
                reason = 'error';
            } else if (userRequestedStop) {
                reason = 'stopped';
            } else if (st === lastStatus && !st.active) {
                if (knownRunLimits.maxSteps > 0 && steps >= knownRunLimits.maxSteps) {
                    reason = 'max_steps';
                } else if (threshold > 0 && streak >= threshold) {
                    reason = 'saturated';
                } else if (knownRunLimits.timeBudgetMs > 0 && started && stopped
                    && (stopped - started) >= knownRunLimits.timeBudgetMs - 1500) {
                    reason = 'time_budget';
                }
            }
        }
        const stepWord = pluralize(steps, 'step');
        switch (reason) {
        case 'max_steps':
            return {
                reason,
                tone: 'success',
                label: 'step limit reached',
                title: 'Exploration finished',
                sentence: `It reached its step limit after ${stepWord}.`,
                toast: `Exploration finished: it reached its step limit after ${stepWord}.`,
            };
        case 'time_budget':
            return {
                reason,
                tone: 'success',
                label: 'time limit reached',
                title: 'Exploration finished',
                sentence: `It reached its time limit after ${stepWord}.`,
                toast: `Exploration finished: it reached its time limit after ${stepWord}.`,
            };
        case 'saturated':
            return {
                reason,
                tone: 'success',
                label: 'nothing new left to find',
                title: 'Exploration finished',
                sentence: streak > 0
                    ? `It stopped by itself after ${stepWord}: the last ${pluralize(streak, 'step')} found no new screen or transition.`
                    : `It stopped by itself after ${stepWord} because it was no longer finding anything new.`,
                toast: `Exploration finished after ${stepWord}: it was no longer finding anything new.`,
            };
        case 'stopped':
            return {
                reason,
                tone: 'info',
                label: 'stopped',
                title: 'Exploration stopped',
                sentence: `It was stopped after ${stepWord}, before it reached a limit.`,
                toast: `Exploration stopped after ${stepWord}.`,
            };
        case 'error':
            return {
                reason,
                tone: 'error',
                label: 'stopped by an error',
                title: 'Exploration stopped by an error',
                sentence: errorText
                    ? `It failed after ${stepWord}: ${errorText}`
                    : `It failed after ${stepWord}. Check that the host session is still connected.`,
                toast: errorText
                    ? `Exploration stopped by an error after ${stepWord}: ${errorText}`
                    : `Exploration stopped by an error after ${stepWord}.`,
            };
        case 'blocked':
            return {
                reason,
                tone: 'warning',
                label: 'no key left to press',
                title: 'Exploration could not go further',
                sentence: `After ${stepWord} every key it could press on the screen it reached is blocked. Unblock a key under Exploration hints, or move to another screen and continue.`,
                toast: `Exploration could not go further after ${stepWord}: every key it could press is blocked.`,
            };
        default:
            return {
                reason: '',
                tone: 'success',
                label: steps > 0 ? 'finished' : 'loaded',
                title: steps > 0 ? 'Exploration results' : 'Saved run loaded',
                sentence: '',
                toast: `Exploration finished${steps > 0 ? ` after ${stepWord}` : ''}.`,
            };
        }
    };

    // The server writes its suggestions for a model as well as for a person.
    const tidyChaosSuggestion = (text) => String(text || '')
        .replace(/\s*\(chaos_[a-z_]+[^)]*\)/gi, '')
        .replace(/\bchaos hints\b/gi, 'exploration hints')
        .replace(/\bchaos exploration\b/gi, 'exploration')
        .replace(/\s{2,}/g, ' ')
        .trim();

    const renderChaosSuggestions = (items) => {
        if (!runPanelNext || !runPanelNextList) {
            return;
        }
        const list = (Array.isArray(items) ? items : [])
            .map(tidyChaosSuggestion)
            .filter(Boolean)
            .slice(0, 3);
        runPanelNextList.innerHTML = '';
        list.forEach((text) => {
            const li = document.createElement('li');
            li.textContent = text;
            runPanelNextList.appendChild(li);
        });
        runPanelNext.hidden = list.length === 0;
    };

    const loadChaosSuggestions = async (status) => {
        const key = `${String((status && status.loadedRunID) || loadedRunID || '')}:${Number(status && status.stepsRun) || 0}`;
        if (key === lastInsightsKey) {
            return;
        }
        lastInsightsKey = key;
        renderChaosSuggestions([]);
        try {
            const resp = await fetch('/chaos/insights', { headers: { Accept: 'application/json' } });
            if (!resp.ok) {
                // No insights for this run, or a server without the endpoint:
                // the panel simply goes without them.
                return;
            }
            const data = await resp.json();
            if (key !== lastInsightsKey) {
                return;
            }
            renderChaosSuggestions(data && data.suggestedExperiments);
        } catch (_err) {
            // optional
        }
    };

    const chaosRunPanelIcons = {
        success: '<svg viewBox="0 0 24 24" focusable="false"><path d="M9 16.2 4.8 12l-1.4 1.4L9 19 21 7l-1.4-1.4L9 16.2z"/></svg>',
        info: '<svg viewBox="0 0 24 24" focusable="false"><path d="M6 6h12v12H6z"/></svg>',
        warning: '<svg viewBox="0 0 24 24" focusable="false"><path d="M1 21h22L12 2 1 21zm12-3h-2v-2h2v2zm0-4h-2v-4h2v4z"/></svg>',
        error: '<svg viewBox="0 0 24 24" focusable="false"><path d="M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20zm1 15h-2v-2h2v2zm0-4h-2V7h2v6z"/></svg>',
    };

    // The state a run leaves on the strip: why it ended, what it found, and
    // the four things somebody does next.
    const renderChaosRunPanel = (status, visible) => {
        if (!runPanel) {
            return;
        }
        if (!visible) {
            runPanel.hidden = true;
            return;
        }
        const st = status || {};
        const outcome = chaosOutcomeFor(st);
        lastOutcome = outcome;
        runPanel.dataset.tone = outcome.tone;
        if (runPanelIcon) {
            runPanelIcon.innerHTML = chaosRunPanelIcons[outcome.tone] || chaosRunPanelIcons.success;
        }
        if (runPanelTitle) {
            runPanelTitle.textContent = outcome.title;
        }
        if (runPanelReason) {
            runPanelReason.textContent = outcome.sentence;
            runPanelReason.hidden = !outcome.sentence;
        }
        if (runPanelCounts) {
            const steps = Number(st.stepsRun) || 0;
            const transitions = Number(st.transitions) || 0;
            const inputs = Number(st.uniqueInputs) || 0;
            const host = String(st.targetHost || '').trim();
            const runName = String(st.label || '').trim() || String(st.loadedRunID || loadedRunID || '').trim();
            runPanelCounts.textContent = [
                steps > 0 ? pluralize(steps, 'step') : '',
                screenCountLabel(st.uniqueScreens),
                transitions > 0 ? pluralize(transitions, 'transition') : '',
                inputs > 0 ? pluralize(inputs, 'input') : '',
                host ? `on ${host}` : '',
                runName ? `run ${runName}` : 'saving…',
            ].filter(Boolean).join(' · ');
        }
        const hasRun = !!String(st.loadedRunID || loadedRunID || '').trim();
        if (runPanelContinueBtn) {
            runPanelContinueBtn.title = hasRun ? '' : 'Available once the run has been saved';
        }
        runPanel.hidden = false;
        loadChaosSuggestions(st);
    };

    // ---- progress while it runs ----------------------------------------
    const chaosMomentumText = (status) => {
        const cov = status && status.coverageStats && typeof status.coverageStats === 'object'
            ? status.coverageStats
            : null;
        if (!cov) {
            return '';
        }
        const streak = Number(cov.saturationStreak) || 0;
        const threshold = Number(cov.saturationThresholdSteps) || 0;
        const windowSteps = Number(cov.windowSteps) || 10;
        const newScreens = Number(cov.newScreensLast10Steps) || 0;
        const newTransitions = Number(cov.newTransitionsLast10Steps) || 0;
        if (streak > 0) {
            return threshold > 0
                ? `nothing new for ${streak} of ${pluralize(threshold, 'step')}`
                : `nothing new for ${pluralize(streak, 'step')}`;
        }
        const lastSteps = windowSteps === 1 ? 'the last step' : `the last ${windowSteps} steps`;
        if (newScreens > 0) {
            return `${pluralize(newScreens, 'new screen')} in ${lastSteps}`;
        }
        if (newTransitions > 0) {
            return `${pluralize(newTransitions, 'new transition')} in ${lastSteps}`;
        }
        return '';
    };

    const announceChaos = (message) => {
        if (!chaosLiveRegion || !message) {
            return;
        }
        chaosLiveRegion.textContent = message;
    };

    const updateChaosMomentum = (status, running) => {
        const text = running ? chaosMomentumText(status) : '';
        if (momentumEl) {
            momentumEl.textContent = text;
            momentumEl.hidden = !text;
            const cov = status && status.coverageStats;
            const threshold = cov ? Number(cov.saturationThresholdSteps) || 0 : 0;
            momentumEl.title = text && threshold > 0
                ? `The exploration stops by itself after ${threshold} steps in a row that find nothing new.`
                : '';
        }
        if (!running) {
            lastMomentumText = '';
            return;
        }
        // Spoken at a fraction of the rate it is drawn: a counter that ticks
        // every half second is not something to read out as it ticks.
        const now = Date.now();
        if (text && text !== lastMomentumText && (now - lastMomentumAnnouncedAt) >= momentumAnnounceIntervalMs) {
            lastMomentumAnnouncedAt = now;
            lastMomentumText = text;
            const steps = Number(status && status.stepsRun) || 0;
            announceChaos(`Exploring: ${pluralize(steps, 'step')} so far, ${text}.`);
        }
    };

    const updateUI = (status) => {
        const wasRunning = lastChaosRunning;
        const previous = lastStatus || {};
        const next = status || { active: false, stepsRun: 0, transitions: 0 };
        // A partial update (one the page makes for itself, between polls) must
        // not forget what the server last said about the run.
        // The same run, that is: one that has just been saved gains its run
        // ID between two polls and is still the same run.
        const sameRun = !!next.active === !!previous.active
            && (String(next.loadedRunID || '') === String(previous.loadedRunID || '')
                || (!String(previous.loadedRunID || '') && (Number(previous.stepsRun) || 0) > 0
                    && (Number(next.stepsRun) || 0) === (Number(previous.stepsRun) || 0)));
        ['terminationReason', 'targetHost', 'label', 'coverageStats', 'error'].forEach((key) => {
            if ((next[key] == null || next[key] === '') && previous[key] != null && sameRun) {
                next[key] = previous[key];
            }
        });
        lastStatus = next;
        const fullMapArrived = updateChaosMapFromStatus(next);
        if (fullMapArrived && latestMindMapIsFull) {
            const all = getChaosFlowAreas();
            groupedScreenCount.areaCount = all.length;
            groupedScreenCount.screens = all.length ? buildChaosMapTemplateGroups(all).groups.length : 0;
        }
        const running = !!next.active;
        lastChaosRunning = running;
        hasData = !!(next.stepsRun > 0 || next.loadedRunID);
        if (next.loadedRunID) {
            loadedRunID = next.loadedRunID;
        }
        if (typeof next.firstScreenHash === 'string') {
            latestFirstScreenHash = next.firstScreenHash.trim();
        } else if (running && (Number(next.stepsRun) || 0) === 0) {
            latestFirstScreenHash = '';
        } else if (!running && !(next.loadedRunID) && (Number(next.stepsRun) || 0) === 0) {
            latestFirstScreenHash = '';
        }
        const suppressStoredChaosDisplay = !running && isPlaybackActive();
        const visibleLoadedRunID = suppressStoredChaosDisplay ? null : loadedRunID;
        const visibleHasData = suppressStoredChaosDisplay ? false : hasData;

        if (startBtn) {
            startBtn.hidden = running;
        }
        if (stopBtn) {
            stopBtn.hidden = !running;
        }
        // One "Continue exploring", offered whenever there is a run to carry on.
        if (resumeBtn) {
            resumeBtn.hidden = running || !visibleLoadedRunID;
        }
        if (exportBtn) {
            exportBtn.hidden = running || !visibleHasData;
        }
        if (reportBtn) {
            reportBtn.hidden = running || !visibleHasData;
        }
        if (taskOpenBtn) {
            taskOpenBtn.hidden = running || !visibleHasData;
        }
        if (removeBtn) {
            removeBtn.hidden = running || !visibleHasData;
        }
        if (indicator) {
            indicator.hidden = !running;
        }
        if (completeIndicator) {
            completeIndicator.hidden = !visibleHasData || running;
        }
        if (statsIndicator) {
            const hasStats = !suppressStoredChaosDisplay && !!(next.stepsRun > 0 || next.transitions > 0);
            statsIndicator.hidden = !hasStats;
            if (statsText && hasStats) {
                statsText.textContent = [
                    pluralize(Number(next.stepsRun) || 0, 'step'),
                    next.transitions > 0 ? pluralize(Number(next.transitions), 'transition') : '',
                    screenCountLabel(next.uniqueScreens),
                    next.uniqueInputs > 0 ? pluralize(Number(next.uniqueInputs), 'input') : '',
                    next.error ? 'error' : '',
                    !running && visibleLoadedRunID ? `run ${visibleLoadedRunID}` : '',
                ].filter(Boolean).join(' | ');
            }
        }
        syncChaosSectionLayout();
        applyChaosInterlocks();
        updateLoadedChaosActionTooltips(next, visibleLoadedRunID, visibleHasData);
        updateChaosMomentum(next, running);
        renderChaosRunPanel(next, !running && visibleHasData);
        if (fullMapArrived) {
            refreshOpenChaosMapViews();
        }
        if (running) {
            userRequestedStop = userRequestedStop && wasRunning;
        }
        // One notification when a run ends, and it says how it ended: an
        // error is not announced as a success, and stopping a run yourself
        // does not get you two toasts.
        if (wasRunning && !running) {
            if (hasData) {
                const outcome = chaosOutcomeFor(next);
                lastOutcome = outcome;
                if (!next.terminationReason && outcome.reason) {
                    // Worked out here because the server did not say; keep
                    // it, so the panel goes on saying what the toast said.
                    next.terminationReason = outcome.reason;
                }
                renderChaosRunPanel(next, runPanel ? !runPanel.hidden : false);
                notifyUi(outcome.toast, outcome.tone);
                // The panel quotes the number of distinct screens, which
                // needs the full map as it stood at the end.
                fetchFullChaosMap({ force: true });
            }
            userRequestedStop = false;
            if (typeof window.refreshWorkflowStatus === 'function') {
                window.refreshWorkflowStatus();
            }
        }
    };

    // 401/403 means the session behind this page has gone. Say so once and
    // stop asking: the poll used to carry on at full rate against a server
    // that had already answered. It starts again when the window is next
    // focused, or when anything else calls pollStatus().
    const handleChaosAuthFailure = (resp) => {
        if (!resp || (resp.status !== 401 && resp.status !== 403)) {
            return false;
        }
        clearTimeout(pollTimer);
        pollTimer = null;
        if (!chaosSessionExpired) {
            chaosSessionExpired = true;
            const ui = window.ThreeSeventyWeb || {};
            const status = resp.status;
            if (typeof ui.handleAccessFailure === 'function') {
                resp.clone().json().catch(() => ({})).then((payload) => {
                    ui.handleAccessFailure(status, payload || {});
                });
            } else if (typeof ui.notifySessionExpired === 'function') {
                ui.notifySessionExpired();
            } else {
                notifyUi('Your session has ended. Reload the page to carry on.', 'warning');
            }
        }
        return true;
    };

    const scheduleChaosPoll = (delay) => {
        clearTimeout(pollTimer);
        pollTimer = setTimeout(pollStatus, delay);
    };

    const pollStatus = async () => {
        clearTimeout(pollTimer);
        pollTimer = null;
        try {
            const resp = await fetch('/chaos/status', { headers: { Accept: 'application/json' } });
            if (handleChaosAuthFailure(resp)) {
                return null;
            }
            if (!resp.ok) {
                throw new Error('exploration status request failed: ' + resp.status);
            }
            const status = await resp.json();
            pollFailureCount = 0;
            chaosSessionExpired = false;
            updateUI(status);
            refreshFullChaosMapIfStale();
            const finalizingCompletedRun = !status.active
                && (Number(status.stepsRun) || 0) > 0
                && !String(status.loadedRunID || '').trim();
            if (status.active) {
                pollFinalizeTries = 0;
                scheduleChaosPoll(document.visibilityState === 'visible' ? pollActiveDelayMs : pollHiddenDelayMs);
            } else if (finalizingCompletedRun) {
                if (pollFinalizeTries < pollFinalizeMaxTries) {
                    pollFinalizeTries += 1;
                    scheduleChaosPoll(pollBaseDelayMs);
                }
            } else {
                pollFinalizeTries = 0;
            }
            return status;
        } catch (_err) {
            // A transient failure (network blip, momentary 5xx) shouldn't
            // freeze the displayed status until the user happens to trigger
            // another action. Back off and keep retrying while a run is
            // believed to be going; with nothing running, a handful of tries
            // is enough, and the next action polls again anyway.
            pollFailureCount += 1;
            if ((lastStatus && lastStatus.active) || pollFailureCount <= 5) {
                scheduleChaosPoll(Math.min(pollBaseDelayMs * Math.pow(2, pollFailureCount), pollMaxDelayMs));
            }
            return null;
        }
    };

    // ---- saved runs ------------------------------------------------------
    const setRunsStatus = (message, isError = false) => setStatusLine(runsStatus, message, isError);

    const chaosRunDisplayName = (run) => {
        const id = String((run && run.id) || '').trim();
        const label = String((run && run.label) || '').trim();
        return label ? `${label} (${id})` : id;
    };

    const chaosRunEndText = (run) => {
        if (!run) {
            return '';
        }
        const outcome = chaosOutcomeFor({
            terminationReason: run.terminationReason,
            error: run.error,
            stepsRun: run.stepsRun,
        });
        if (outcome.reason) {
            return `Ended: ${outcome.label}`;
        }
        return '';
    };

    const syncRunsLoadButtons = () => {
        if (!runsList) {
            return;
        }
        const running = !!(lastStatus && lastStatus.active);
        runsList.querySelectorAll('[data-load-run-id]').forEach((btn) => {
            btn.disabled = running;
            btn.title = running ? 'Stop the exploration before loading a saved run' : '';
        });
    };

    const chaosRunRowMarkup = (r) => {
        const id = String(r.id || '');
        const name = chaosRunDisplayName(r);
        const date = r.startedAt ? new Date(r.startedAt).toLocaleString() : '';
        const host = String(r.targetHost || '').trim();
        const stats = [
            r.stepsRun > 0 ? pluralize(Number(r.stepsRun), 'step') : null,
            r.uniqueScreens > 0 ? formatChaosScreenCount(0, r.uniqueScreens) : null,
            r.transitions > 0 ? pluralize(Number(r.transitions), 'transition') : null,
        ].filter(Boolean).join(' · ');
        const end = chaosRunEndText(r);
        const isLoaded = !!loadedRunID && String(loadedRunID) === id;
        const safeID = escapeHtml(id);
        const safeName = escapeHtml(name);
        return `<div class="chaos-run-item" data-run-id="${safeID}">
                    <div class="chaos-run-item-top">
                        <input type="checkbox" class="chaos-run-select" data-chaos-run-select="${safeID}" aria-label="Select run ${safeName}">
                        <div class="chaos-run-item-body">
                            <div class="chaos-run-meta">
                                <strong class="chaos-run-id">${safeID}</strong>
                                ${isLoaded ? '<span class="chaos-run-loaded-chip">Loaded</span>' : ''}
                                <span class="subtle">${escapeHtml(date)}</span>
                            </div>
                            <div class="chaos-run-stats subtle">${escapeHtml([host, stats].filter(Boolean).join(' · '))}</div>
                            ${end ? `<div class="chaos-run-end${r.error || r.terminationReason === 'error' ? ' is-error' : ''}">${escapeHtml(end)}${r.error ? `: ${escapeHtml(String(r.error))}` : ''}</div>` : ''}
                            <div class="chaos-run-label-row">
                                <label class="chaos-hint-field-label" for="chaos-run-label-${safeID}">Name</label>
                                <input type="text" id="chaos-run-label-${safeID}" class="chaos-run-label-input" data-chaos-run-label="${safeID}" value="${escapeHtml(String(r.label || ''))}" maxlength="80" placeholder="Give this run a name" autocomplete="off">
                            </div>
                        </div>
                    </div>
                    <div class="chaos-run-actions">
                        <button type="button" class="chaos-run-load-btn" data-load-run-id="${safeID}" aria-label="Load run ${safeName}">Load</button>
                        <button type="button" class="chaos-run-delete-btn" data-delete-run-id="${safeID}" aria-label="Delete run ${safeName}">Delete</button>
                    </div>
                </div>`;
    };

    const openRunsModal = async () => {
        if (!runsModal || !runsList) {
            return;
        }
        currentRuns = [];
        selectedChaosRunIDs = new Set();
        syncRunsSelectionUI();
        setRunsStatus('');
        runsList.innerHTML = '<p class="subtle">Loading…</p>';
        openChaosModal(runsModal, '[data-chaos-runs-close]');
        try {
            const resp = await fetch('/chaos/runs', { headers: { Accept: 'application/json' } });
            if (!resp.ok) {
                const message = await chaosErrorMessage(resp, 'The saved runs could not be loaded.');
                runsList.innerHTML = '';
                setRunsStatus(message, true);
                return;
            }
            const runs = await resp.json();
            if (!Array.isArray(runs) || runs.length === 0) {
                runsList.innerHTML = '<p class="subtle">No saved runs yet. A run is saved here when an exploration ends.</p>';
                return;
            }
            currentRuns = runs.slice();
            runsList.innerHTML = runs.map(chaosRunRowMarkup).join('');
            syncRunsSelectionUI();
            syncRunsLoadButtons();
            if (lastStatus && lastStatus.active) {
                setRunsStatus('An exploration is running. Stop it to load a saved run; naming, comparing and deleting still work.');
            }
            focusModalElement(runsModal, '[data-load-run-id]:not([disabled]), [data-chaos-run-select], [data-chaos-runs-close]');
        } catch (_err) {
            currentRuns = [];
            runsList.innerHTML = '';
            setRunsStatus('The saved runs could not be loaded: network error.', true);
        }
    };

    const saveChaosRunLabel = async (input) => {
        const id = String(input.getAttribute('data-chaos-run-label') || '').trim();
        const run = currentRuns.find((r) => String(r && r.id) === id);
        if (!id || !run) {
            return;
        }
        const label = input.value.trim();
        const previousLabel = String(run.label || '');
        if (label === previousLabel) {
            input.value = previousLabel;
            return;
        }
        input.disabled = true;
        setRunsStatus('Saving the name…');
        try {
            const resp = await fetch('/chaos/runs/label', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ id, label }),
            });
            if (!resp.ok) {
                const fallback = resp.status === 404 || resp.status === 405
                    ? 'This server cannot name saved runs yet.'
                    : 'The name could not be saved.';
                const message = resp.status === 404 || resp.status === 405
                    ? fallback
                    : await chaosErrorMessage(resp, fallback);
                input.value = previousLabel;
                setRunsStatus(message, true);
                return;
            }
            run.label = label;
            setRunsStatus(label ? `Run ${id} is now named “${label}”.` : `Name removed from run ${id}.`);
            const row = input.closest('.chaos-run-item');
            if (row) {
                const name = chaosRunDisplayName(run);
                const select = row.querySelector('[data-chaos-run-select]');
                const load = row.querySelector('[data-load-run-id]');
                const del = row.querySelector('[data-delete-run-id]');
                if (select) {
                    select.setAttribute('aria-label', `Select run ${name}`);
                }
                if (load) {
                    load.setAttribute('aria-label', `Load run ${name}`);
                }
                if (del) {
                    del.setAttribute('aria-label', `Delete run ${name}`);
                }
            }
        } catch (_err) {
            input.value = previousLabel;
            setRunsStatus('The name could not be saved: network error.', true);
        } finally {
            input.disabled = false;
        }
    };

    // ---- comparing two saved runs -----------------------------------------
    const setCompareStatus = (message, isError = false) => setStatusLine(compareStatus, message, isError);

    const chaosCompareRefLabel = (ref) => {
        const label = String((ref && ref.label) || '').trim();
        const hash = String((ref && (ref.hash || ref.candidate_hash || ref.baseline_hash)) || '').trim();
        return label || (hash ? `Screen ${hash.slice(0, 8)}` : 'Unnamed screen');
    };

    const chaosCompareFieldText = (delta) => {
        const where = `row ${Number(delta.row) || 0}, column ${Number(delta.col) || 0}, length ${Number(delta.length) || 0}`;
        const change = String(delta.change || 'changed');
        return `Field at ${where}: ${change}`;
    };

    const chaosCompareTransitionText = (delta) => {
        const key = String(delta.key || 'key');
        const from = String(delta.baseline_target || '').trim();
        const to = String(delta.candidate_target || '').trim();
        const short = (value) => (value.length > 16 ? `${value.slice(0, 12)}…` : value);
        switch (String(delta.change || '')) {
        case 'added':
            return `${key}: now leads to ${short(to) || 'a screen'}`;
        case 'removed':
            return `${key}: no longer leads to ${short(from) || 'a screen'}`;
        case 'redirected':
            return `${key}: led to ${short(from) || 'one screen'}, now leads to ${short(to) || 'another'}`;
        default:
            return `${key}: ${String(delta.change || 'changed')}`;
        }
    };

    const renderChaosCompare = (diff, base, candidate) => {
        if (!compareContent) {
            return;
        }
        const summary = (diff && diff.summary) || {};
        const onlyBase = Array.isArray(diff && diff.only_in_baseline) ? diff.only_in_baseline : [];
        const onlyCandidate = Array.isArray(diff && diff.only_in_candidate) ? diff.only_in_candidate : [];
        const changed = (Array.isArray(diff && diff.common) ? diff.common : [])
            .filter((area) => (Array.isArray(area.field_deltas) && area.field_deltas.length)
                || (Array.isArray(area.transition_deltas) && area.transition_deltas.length));
        const baseName = escapeHtml(chaosRunDisplayName(base));
        const candidateName = escapeHtml(chaosRunDisplayName(candidate));
        const num = (value) => Number(value) || 0;
        const refList = (refs) => (refs.length
            ? `<ul>${refs.map((ref) => `<li>${escapeHtml(chaosCompareRefLabel(ref))} <span class="chaos-run-id">${escapeHtml(String(ref.hash || '').slice(0, 8))}</span></li>`).join('')}</ul>`
            : '<p class="subtle">None.</p>');
        const changedMarkup = changed.length
            ? changed.map((area) => {
                const fields = Array.isArray(area.field_deltas) ? area.field_deltas : [];
                const transitions = Array.isArray(area.transition_deltas) ? area.transition_deltas : [];
                const items = fields.map(chaosCompareFieldText).concat(transitions.map(chaosCompareTransitionText));
                return `<details class="chaos-compare-area">
                        <summary>${escapeHtml(chaosCompareRefLabel(area))} <span class="subtle">${pluralize(items.length, 'difference')}</span></summary>
                        <ul>${items.map((item) => `<li>${escapeHtml(item)}</li>`).join('')}</ul>
                    </details>`;
            }).join('')
            : '<p class="subtle">None: every screen both runs found has the same fields and leads to the same places.</p>';
        compareContent.innerHTML = `
            <table class="chaos-compare-summary">
                <caption class="visually-hidden">Summary of the comparison</caption>
                <thead><tr><th scope="col"></th><th scope="col">Earlier run<br><span class="subtle">${baseName}</span></th><th scope="col">Later run<br><span class="subtle">${candidateName}</span></th></tr></thead>
                <tbody>
                    <tr><th scope="row">Screens found</th><td>${num(summary.baseline_areas)}</td><td>${num(summary.candidate_areas)}</td></tr>
                    <tr><th scope="row">Found by this run only</th><td>${num(summary.areas_only_in_baseline)}</td><td>${num(summary.areas_only_in_candidate)}</td></tr>
                </tbody>
            </table>
            <p class="chaos-compare-line">${pluralize(num(summary.common_areas), 'screen')} found by both · ${pluralize(num(summary.field_changes), 'field change')} · ${pluralize(num(summary.transition_changes), 'transition change')}</p>
            <h4>Only in the earlier run (${onlyBase.length})</h4>
            ${refList(onlyBase)}
            <h4>Only in the later run (${onlyCandidate.length})</h4>
            ${refList(onlyCandidate)}
            <h4>Screens that changed (${changed.length})</h4>
            ${changedMarkup}
        `;
    };

    const compareSelectedChaosRuns = async () => {
        const ids = Array.from(selectedChaosRunIDs);
        if (ids.length !== 2 || !compareModal) {
            return;
        }
        const picked = ids
            .map((id) => currentRuns.find((r) => String(r && r.id) === id))
            .filter(Boolean)
            .sort((a, b) => (Date.parse(a.startedAt || '') || 0) - (Date.parse(b.startedAt || '') || 0));
        if (picked.length !== 2) {
            return;
        }
        const [base, candidate] = picked;
        if (compareTitle) {
            compareTitle.textContent = 'Compare runs';
        }
        if (compareContent) {
            compareContent.innerHTML = '';
        }
        setCompareStatus('Comparing…');
        openChaosModal(compareModal, '[data-chaos-compare-close]', { keepPrevious: true });
        try {
            const resp = await fetch('/chaos/mindmap/compare', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
                body: JSON.stringify({ baseRunId: base.id, candidateRunId: candidate.id }),
            });
            if (!resp.ok) {
                setCompareStatus(await chaosErrorMessage(resp, 'The runs could not be compared.'), true);
                return;
            }
            const diff = await resp.json();
            const summary = (diff && diff.summary) || {};
            const sawNothing = !(Number(summary.baseline_areas) || 0) && !(Number(summary.candidate_areas) || 0);
            if (sawNothing && ((Number(base.uniqueScreens) || 0) > 0 || (Number(candidate.uniqueScreens) || 0) > 0)) {
                // A server that does not yet take run IDs here compares two
                // empty maps and reports, truthfully and uselessly, no
                // difference. Do not pass that off as a result.
                setCompareStatus('This server cannot compare saved runs by name yet.', true);
                return;
            }
            setCompareStatus(`Earlier run ${chaosRunDisplayName(base)} against later run ${chaosRunDisplayName(candidate)}.`);
            renderChaosCompare(diff, base, candidate);
        } catch (_err) {
            setCompareStatus('The runs could not be compared: network error.', true);
        }
    };

    const clearLoadedChaosIfDeletedRun = async (rid) => {
        if (loadedRunID !== rid) {
            return;
        }
        loadedRunID = null;
        updateUI({
            active: false,
            stepsRun: 0,
            transitions: 0,
            uniqueScreens: 0,
            uniqueInputs: 0,
        });
        if (typeof window.refreshWorkflowStatus === 'function') {
            await window.refreshWorkflowStatus();
        }
    };

    const removeRunRowsFromModal = (runIDs) => {
        const ids = new Set((runIDs || []).map((id) => String(id || '').trim()).filter(Boolean));
        if (!ids.size || !runsList) {
            return;
        }
        runsList.querySelectorAll('.chaos-run-item').forEach((row) => {
            const rid = String(row.getAttribute('data-run-id') || '').trim();
            if (ids.has(rid)) {
                row.remove();
            }
        });
        currentRuns = currentRuns.filter((r) => !ids.has(String(r && r.id || '').trim()));
        ids.forEach((id) => selectedChaosRunIDs.delete(id));
        syncRunsSelectionUI();
        if (!runsList.querySelector('.chaos-run-item')) {
            runsList.innerHTML = '<p class="subtle">No saved runs left.</p>';
        }
    };

    const deleteChaosRunsByID = async (runIDs) => {
        const ids = Array.from(new Set((runIDs || []).map((id) => String(id || '').trim()).filter(Boolean)));
        if (!ids.length) {
            return { deleted: [], failed: [], errors: [] };
        }
        const deleted = [];
        const failed = [];
        const errors = [];
        for (const rid of ids) {
            try {
                const respDelete = await fetch('/chaos/runs/delete', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ runID: rid }),
                });
                if (!respDelete.ok) {
                    failed.push(rid);
                    errors.push(await chaosErrorMessage(respDelete, `Run ${rid} could not be deleted.`));
                    continue;
                }
                deleted.push(rid);
                await clearLoadedChaosIfDeletedRun(rid);
            } catch (_e) {
                failed.push(rid);
                errors.push(`Run ${rid} could not be deleted: network error.`);
            }
        }
        removeRunRowsFromModal(deleted);
        return { deleted, failed, errors };
    };

    if (runsModal) {
        runsModalClose.forEach((btn) => {
            btn.addEventListener('click', () => {
                closeChaosModal(runsModal);
            });
        });
        runsModal.addEventListener('click', (e) => {
            if (e.target === runsModal) {
                closeChaosModal(runsModal);
            }
        });
    }
    if (runsList) {
        runsList.addEventListener('change', (event) => {
            const target = event.target;
            if (!(target instanceof HTMLInputElement)) {
                return;
            }
            if (target.hasAttribute('data-chaos-run-label')) {
                saveChaosRunLabel(target);
                return;
            }
            const rid = target.getAttribute('data-chaos-run-select');
            if (!rid) {
                return;
            }
            setRunSelected(rid, target.checked);
        });
        runsList.addEventListener('keydown', (event) => {
            const target = event.target;
            if (event.key === 'Enter' && target instanceof HTMLInputElement && target.hasAttribute('data-chaos-run-label')) {
                // Enter commits the name; blur() fires the change that saves it.
                event.preventDefault();
                target.blur();
                target.focus();
            }
        });
        runsList.addEventListener('click', async (event) => {
            const target = event.target instanceof Element ? event.target.closest('button') : null;
            if (!target) {
                return;
            }
            const loadID = target.getAttribute('data-load-run-id');
            if (loadID) {
                setButtonBusy(target, true);
                setRunsStatus(`Loading run ${loadID}…`);
                try {
                    const r2 = await fetch('/chaos/load', {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify({ runID: loadID }),
                    });
                    if (!r2.ok) {
                        // Stays in the dialog, with the reason: a run that
                        // would not load used to close it as if it had.
                        setRunsStatus(await chaosErrorMessage(r2, `Run ${loadID} could not be loaded.`), true);
                        return;
                    }
                    const data = await r2.json();
                    const meta = currentRuns.find((r) => String(r && r.id) === String(data.runID || loadID)) || {};
                    loadedRunID = data.runID || loadID;
                    updateUI({
                        active: false,
                        stepsRun: data.stepsRun || 0,
                        transitions: data.transitions || 0,
                        uniqueScreens: data.uniqueScreens || 0,
                        uniqueInputs: data.uniqueInputs || 0,
                        loadedRunID,
                        mindMap: data.mindMap || null,
                        terminationReason: data.terminationReason || meta.terminationReason || '',
                        targetHost: data.targetHost || meta.targetHost || '',
                        label: data.label || meta.label || '',
                        error: meta.error || '',
                    });
                    closeChaosModal(runsModal);
                    pollStatus();
                    if (typeof window.refreshWorkflowStatus === 'function') {
                        await window.refreshWorkflowStatus();
                    }
                    const summary = chaosCountsSummary({
                        steps: data.stepsRun || 0,
                        screens: data.uniqueScreens || 0,
                        inputs: data.uniqueInputs || 0,
                        keys: data.uniqueKeys || 0,
                    });
                    notifyUi(
                        `Loaded run ${chaosRunDisplayName({ id: loadedRunID, label: meta.label })}${summary ? ` (${summary})` : ''}.`,
                        'success'
                    );
                } catch (_e) {
                    setRunsStatus(`Run ${loadID} could not be loaded: network error.`, true);
                } finally {
                    setButtonBusy(target, false);
                    syncRunsLoadButtons();
                }
                return;
            }
            const deleteID = target.getAttribute('data-delete-run-id');
            if (!deleteID) {
                return;
            }
            const run = currentRuns.find((r) => String(r && r.id) === deleteID) || { id: deleteID };
            const confirmed = await promptChaosConfirm({
                title: 'Delete this saved run?',
                message: `Run ${chaosRunDisplayName(run)} will be deleted from disk. This cannot be undone.`,
                confirmLabel: 'Delete run',
            });
            if (!confirmed) {
                return;
            }
            target.disabled = true;
            const result = await deleteChaosRunsByID([deleteID]);
            if (result.deleted.length) {
                setRunsStatus(`Deleted run ${deleteID}.`);
                focusModalElement(runsModal, '[data-chaos-run-select], [data-chaos-runs-close]');
            } else {
                target.disabled = false;
                setRunsStatus(result.errors[0] || `Run ${deleteID} could not be deleted.`, true);
            }
        });
    }
    if (runsSelectAllBtn) {
        runsSelectAllBtn.addEventListener('click', () => {
            currentRuns.forEach((r) => {
                const rid = String(r && r.id || '').trim();
                if (rid) {
                    selectedChaosRunIDs.add(rid);
                }
            });
            syncRunsSelectionUI();
        });
    }
    if (runsClearSelectionBtn) {
        runsClearSelectionBtn.addEventListener('click', () => {
            selectedChaosRunIDs.clear();
            syncRunsSelectionUI();
        });
    }
    if (runsDeleteSelectedBtn) {
        runsDeleteSelectedBtn.addEventListener('click', async () => {
            const ids = Array.from(selectedChaosRunIDs);
            if (!ids.length) {
                return;
            }
            const confirmed = await promptChaosConfirm({
                title: ids.length === 1 ? 'Delete the selected run?' : `Delete ${ids.length} selected runs?`,
                message: `${ids.length === 1 ? 'The selected run' : `The ${ids.length} selected runs`} will be deleted from disk. This cannot be undone.`,
                confirmLabel: ids.length === 1 ? 'Delete run' : `Delete ${ids.length} runs`,
            });
            if (!confirmed) {
                return;
            }
            runsDeleteSelectedBtn.disabled = true;
            const result = await deleteChaosRunsByID(ids);
            if (result.failed.length) {
                setRunsStatus(
                    `${result.deleted.length ? `Deleted ${pluralize(result.deleted.length, 'run')}. ` : ''}${pluralize(result.failed.length, 'run')} could not be deleted${result.errors[0] ? `: ${result.errors[0]}` : '.'}`,
                    true
                );
            } else if (result.deleted.length) {
                setRunsStatus(`Deleted ${pluralize(result.deleted.length, 'run')}.`);
            }
            syncRunsSelectionUI();
            focusModalElement(runsModal, '[data-chaos-run-select], [data-chaos-runs-close]');
        });
    }
    if (runsCompareBtn) {
        runsCompareBtn.addEventListener('click', () => {
            compareSelectedChaosRuns();
        });
    }
    if (compareModal) {
        compareModalClose.forEach((btn) => {
            btn.addEventListener('click', () => {
                closeChaosModal(compareModal);
            });
        });
        compareModal.addEventListener('click', (event) => {
            if (event.target === compareModal) {
                closeChaosModal(compareModal);
            }
        });
    }

    if (hintsOpenBtn) {
        hintsOpenBtn.addEventListener('click', async () => {
            openChaosModal(hintsModal, '[data-chaos-key-blacklist]');
            hintsDirty = false;
            if (hintsList && hintsList.children.length === 0) {
                renderHints(chaosHints);
            }
            renderChaosKeyBlacklist(chaosKeyBlacklist);
            renderFirstScreenHint(chaosFirstScreenHint);
            await loadHints({ quiet: true });
        });
    }
    if (hintsMaximizeBtn) {
        hintsMaximizeBtn.addEventListener('click', () => {
            setHintsModalMaximized(true);
            focusModalElement(hintsModal, '[data-chaos-hint-transaction], [data-chaos-hints-add]');
        });
    }
    if (hintsMinimizeBtn) {
        hintsMinimizeBtn.addEventListener('click', () => {
            setHintsModalMaximized(false);
            focusModalElement(hintsModal, '[data-chaos-hint-transaction], [data-chaos-hints-add]');
        });
    }
    if (mapOpenBtn) {
        mapOpenBtn.addEventListener('click', () => {
            openChaosMapModal();
        });
    }
    if (flowMenuOpenBtn) {
        flowMenuOpenBtn.addEventListener('click', () => {
            openChaosFlowFromMap();
        });
    }
    if (mapViewBtn) {
        mapViewBtn.addEventListener('click', (event) => {
            event.preventDefault();
            if (mapViewBtn.disabled) {
                return;
            }
            openChaosFlowFromMap();
        });
    }
    if (mapList) {
        mapList.addEventListener('click', (event) => {
            const fieldToggleBtn = event.target && event.target.closest ? event.target.closest('[data-chaos-map-field-toggle]') : null;
            if (!fieldToggleBtn) {
                return;
            }
            event.preventDefault();
            const container = fieldToggleBtn.closest('[data-chaos-map-field-discovery]');
            if (!container) {
                return;
            }
            const topCount = Math.max(1, Number(fieldToggleBtn.dataset.topCount) || 8);
            const totalCount = Math.max(0, Number(fieldToggleBtn.dataset.totalCount) || 0);
            const showAll = container.dataset.fieldMode !== 'all';
            container.dataset.fieldMode = showAll ? 'all' : 'top';
            fieldToggleBtn.textContent = showAll ? 'Top fields' : 'All fields';
            fieldToggleBtn.setAttribute('aria-pressed', showAll ? 'true' : 'false');

            container.querySelectorAll('.chaos-map-field-row-extra').forEach((row) => {
                row.hidden = !showAll;
            });

            const countEl = container.querySelector('[data-chaos-map-field-count]');
            if (countEl) {
                const shownCount = showAll ? totalCount : Math.min(topCount, totalCount);
                countEl.textContent = `${shownCount}${totalCount > shownCount ? ` / ${totalCount}` : ''} field${totalCount === 1 ? '' : 's'} shown`;
            }
            const noteEl = container.querySelector('[data-chaos-map-field-note]');
            if (noteEl) {
                noteEl.textContent = showAll
                    ? 'Showing all discovered fields for this screen.'
                    : `Showing top ${topCount} fields by progression/writes.`;
            }
        });
    }
    if (hintsModal) {
        hintsModalClose.forEach((btn) => {
            btn.addEventListener('click', () => {
                requestCloseHintsModal();
            });
        });
        hintsModal.addEventListener('click', (event) => {
            if (event.target === hintsModal) {
                requestCloseHintsModal();
            }
        });
    }
    if (startLogConfirmModal) {
        startLogConfirmCancelButtons.forEach((btn) => {
            btn.addEventListener('click', () => {
                closeChaosModal(startLogConfirmModal);
            });
        });
        if (startLogConfirmContinueBtn) {
            startLogConfirmContinueBtn.addEventListener('click', () => {
                settleStartLogConfirm('continue');
            });
        }
        if (startLogConfirmDisableBtn) {
            startLogConfirmDisableBtn.addEventListener('click', () => {
                if (startLogConfirmDisableBtn.disabled) {
                    return;
                }
                settleStartLogConfirm('disable');
            });
        }
        startLogConfirmModal.addEventListener('click', (event) => {
            if (event.target === startLogConfirmModal) {
                closeChaosModal(startLogConfirmModal);
            }
        });
    }
    if (mapModal) {
        mapModalClose.forEach((btn) => {
            btn.addEventListener('click', () => {
                closeChaosModal(mapModal);
            });
        });
        mapModal.addEventListener('click', (event) => {
            if (event.target === mapModal) {
                closeChaosModal(mapModal);
            }
        });
    }
    if (flowModal) {
        flowModalClose.forEach((btn) => {
            btn.addEventListener('click', () => {
                closeChaosModal(flowModal);
            });
        });
        flowModal.addEventListener('click', (event) => {
            if (event.target === flowModal) {
                closeChaosModal(flowModal);
            }
        });
    }
    if (reportModal) {
        reportModalClose.forEach((btn) => {
            btn.addEventListener('click', () => {
                closeChaosModal(reportModal);
            });
        });
        reportModal.addEventListener('click', (event) => {
            if (event.target === reportModal) {
                closeChaosModal(reportModal);
            }
        });
    }
    if (mapRefreshBtn) {
        mapRefreshBtn.addEventListener('click', async () => {
            setChaosMapStatus('Refreshing the screen map…');
            setButtonBusy(mapRefreshBtn, true);
            try {
                const failure = await refreshChaosMapView();
                if (failure) {
                    setChaosMapStatus(failure, true);
                } else {
                    setChaosMapStatus(chaosMapStatusText());
                }
            } finally {
                setButtonBusy(mapRefreshBtn, false);
            }
        });
    }
    if (mapList) {
        // A card left alone while it was being edited is brought up to date
        // as soon as focus leaves it.
        mapList.addEventListener('focusout', () => {
            if (!mapRenderPending) {
                return;
            }
            window.setTimeout(() => {
                if (mapRenderPending && mapModal && !mapModal.hidden) {
                    renderChaosMap();
                    setChaosMapStatus(chaosMapStatusText());
                }
            }, 0);
        });
    }
    if (mapExportBtn) {
        mapExportBtn.addEventListener('click', async () => {
            setButtonBusy(mapExportBtn, true);
            try {
                const resp = await fetch('/chaos/mindmap/export', { headers: { Accept: 'application/json' } });
                if (!resp.ok) {
                    setChaosMapStatus(await chaosErrorMessage(resp, 'The screen map could not be exported.'), true);
                    return;
                }
                const text = await resp.text();
                const name = loadedRunID ? `screen-map-${loadedRunID}.json` : 'screen-map.json';
                downloadChaosFile(name, text, 'application/json');
                setChaosMapStatus(`Screen map exported as ${name}.`);
            } catch (_err) {
                setChaosMapStatus('The screen map could not be exported: network error.', true);
            } finally {
                setButtonBusy(mapExportBtn, false);
            }
        });
    }
    if (mapImportBtn && mapImportInput) {
        mapImportBtn.addEventListener('click', () => {
            mapImportInput.click();
        });
        mapImportInput.addEventListener('change', async () => {
            const file = mapImportInput.files && mapImportInput.files.length ? mapImportInput.files[0] : null;
            mapImportInput.value = '';
            if (!file) {
                return;
            }
            setButtonBusy(mapImportBtn, true);
            setChaosMapStatus(`Importing ${file.name}…`);
            try {
                const text = await file.text();
                try {
                    JSON.parse(text);
                } catch (_parseErr) {
                    setChaosMapStatus(`${file.name} is not a screen map: it is not valid JSON.`, true);
                    return;
                }
                const resp = await fetch('/chaos/mindmap/import', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: text,
                });
                if (!resp.ok) {
                    setChaosMapStatus(await chaosErrorMessage(resp, 'The screen map could not be imported.'), true);
                    return;
                }
                let imported = 0;
                try {
                    imported = Number((await resp.json()).areasImported) || 0;
                } catch (_err) {
                    imported = 0;
                }
                const failure = await refreshChaosMapView();
                if (typeof window.refreshWorkflowStatus === 'function') {
                    window.refreshWorkflowStatus();
                }
                setChaosMapStatus(failure || `Imported ${pluralize(imported, 'screen variant')} from ${file.name}. ${chaosMapStatusText()}`, !!failure);
            } catch (_err) {
                setChaosMapStatus('The screen map could not be imported: network error.', true);
            } finally {
                setButtonBusy(mapImportBtn, false);
                applyChaosInterlocks();
            }
        });
    }
    [mapTaskBtn, taskOpenBtn].forEach((btn) => {
        if (!btn) {
            return;
        }
        btn.addEventListener('click', () => {
            const wizard = window.ThreeSeventyWeb && window.ThreeSeventyWeb.taskWizard;
            if (!wizard || typeof wizard.openFromExploration !== 'function') {
                notifyUi('The task wizard is not available on this page.', 'error');
                return;
            }
            // The wizard is a dialog on the shared stack; the screen map is
            // on this module's. Close ours so the two do not fight over focus.
            if (mapModal && !mapModal.hidden) {
                closeChaosModal(mapModal, { restoreFocus: false });
            }
            wizard.openFromExploration();
        });
    });
    if (mapMaximizeBtn) {
        mapMaximizeBtn.addEventListener('click', () => {
            setMapModalMaximized(true);
            focusModalElement(mapModal, chaosMapTopFocusSelector);
        });
    }
    if (mapMinimizeBtn) {
        mapMinimizeBtn.addEventListener('click', () => {
            setMapModalMaximized(false);
            focusModalElement(mapModal, chaosMapTopFocusSelector);
        });
    }
    flowRefreshButtons.forEach((btn) => {
        btn.addEventListener('click', async () => {
            setFlowStatus('Refreshing the flow diagram…');
            await Promise.all([loadScreenHints(), pollStatus()]);
            let failure = '';
            await fetchFullChaosMap({ force: true, onError: (message) => { failure = message; } });
            renderChaosFlowModal();
            if (flowContent) {
                flowContent.scrollTop = 0;
            }
            if (failure) {
                setFlowStatus(failure, true);
            } else {
                setFlowStatus(chaosFlowStatusText());
            }
        });
    });
    if (flowContent) {
        // The diagram is not redrawn under a drag; it catches up on release.
        flowContent.addEventListener('pointerdown', () => {
            flowPointerActive = true;
        });
        const releaseFlowPointer = () => {
            if (!flowPointerActive) {
                return;
            }
            flowPointerActive = false;
            if (flowModal && !flowModal.hidden) {
                renderChaosFlowModal({ force: false });
            }
        };
        window.addEventListener('pointerup', releaseFlowPointer);
        window.addEventListener('pointercancel', releaseFlowPointer);
    }
    if (reportContent) {
        reportContent.addEventListener('click', (event) => {
            const opener = event.target instanceof Element ? event.target.closest('[data-chaos-report-open-flow]') : null;
            if (opener) {
                openChaosFlowFromMap();
            }
        });
    }
    if (flowMaximizeBtn) {
        flowMaximizeBtn.addEventListener('click', () => {
            setFlowModalMaximized(true);
            focusModalElement(flowModal, '[data-chaos-flow-refresh], [data-chaos-flow-close]');
        });
    }
    if (flowMinimizeBtn) {
        flowMinimizeBtn.addEventListener('click', () => {
            setFlowModalMaximized(false);
            focusModalElement(flowModal, '[data-chaos-flow-refresh], [data-chaos-flow-close]');
        });
    }
    if (reportRefreshBtn) {
        reportRefreshBtn.addEventListener('click', async () => {
            setButtonBusy(reportRefreshBtn, true);
            try {
                await refreshChaosReportView({ statusMessage: 'Refreshing the discovery report\u2026' });
                focusModalElement(reportModal, chaosReportTopFocusSelector);
            } catch (_err) {
                // handled in refreshChaosReportView
            } finally {
                setButtonBusy(reportRefreshBtn, false);
            }
        });
    }
    if (reportRawToggleBtn) {
        reportRawToggleBtn.addEventListener('click', () => {
            setReportRawToggleState(!reportRawMarkdownMode);
            renderChaosReportCurrentView();
            setReportStatus(reportRawMarkdownMode ? 'Showing the raw Markdown, for copying.' : 'Showing the formatted report.');
            if (!reportRawMarkdownMode) {
                focusModalElement(reportModal, chaosReportTopFocusSelector);
            }
        });
    }
    if (reportDownloadBtn) {
        reportDownloadBtn.addEventListener('click', async () => {
            setButtonBusy(reportDownloadBtn, true);
            try {
                await downloadChaosReportMarkdown();
                setReportStatus(`Report downloaded as ${chaosReportFileName()}.`);
            } catch (err) {
                setReportStatus((err && err.message) ? err.message : 'Failed to download the report.', true);
            } finally {
                setButtonBusy(reportDownloadBtn, false);
            }
        });
    }
    if (confirmModal) {
        confirmCancelButtons.forEach((btn) => {
            btn.addEventListener('click', () => settleChaosConfirm(false));
        });
        if (confirmOkButton) {
            confirmOkButton.addEventListener('click', () => settleChaosConfirm('confirm'));
        }
        if (confirmAltButton) {
            confirmAltButton.addEventListener('click', () => settleChaosConfirm('alt'));
        }
        confirmModal.addEventListener('click', (e) => {
            if (e.target === confirmModal) {
                settleChaosConfirm(false);
            }
        });
    }
    if (flowScreenHintsModal) {
        flowScreenHintsCloseButtons.forEach((btn) => {
            btn.addEventListener('click', () => {
                closeChaosModal(flowScreenHintsModal);
            });
        });
        flowScreenHintsModal.addEventListener('click', (e) => {
            if (e.target === flowScreenHintsModal) {
                closeChaosModal(flowScreenHintsModal);
            }
        });
    }
    if (flowScreenHintsMaximizeBtn) {
        flowScreenHintsMaximizeBtn.addEventListener('click', () => {
            setFlowScreenHintsModalMaximized(true);
            focusModalElement(flowScreenHintsModal, '[data-chaos-flow-screen-known-data]');
        });
    }
    if (flowScreenHintsMinimizeBtn) {
        flowScreenHintsMinimizeBtn.addEventListener('click', () => {
            setFlowScreenHintsModalMaximized(false);
            focusModalElement(flowScreenHintsModal, '[data-chaos-flow-screen-known-data]');
        });
    }
    if (flowScreenHintsSaveBtn) {
        flowScreenHintsSaveBtn.addEventListener('click', async () => {
            const target = activeFlowScreenHintsTarget;
            const memberHashes = target && Array.isArray(target.memberHashes) ? target.memberHashes : [];
            if (!memberHashes.length) {
                setFlowScreenHintsStatus('No screen selected.', true);
                return;
            }
            const draft = collectFlowScreenHintsDraft();
            setFlowScreenHintsStatus('Saving...');
            markScreenHintDraftDirtyForHashes(memberHashes, draft);
            flowScreenHintsSaveBtn.disabled = true;
            try {
                const saved = await saveScreenHintsForHashes(memberHashes, draft);
                setFlowScreenHintsStatus(saved ? 'Saved' : 'Save failed', !saved);
                if (saved) {
                    renderFlowScreenHintsDraft(mergeScreenHintsForHashes(memberHashes));
                }
            } finally {
                flowScreenHintsSaveBtn.disabled = false;
            }
        });
    }
    if (reportMaximizeBtn) {
        reportMaximizeBtn.addEventListener('click', () => {
            setReportModalMaximized(true);
            focusModalElement(reportModal, chaosReportTopFocusSelector);
        });
    }
    if (reportMinimizeBtn) {
        reportMinimizeBtn.addEventListener('click', () => {
            setReportModalMaximized(false);
            focusModalElement(reportModal, chaosReportTopFocusSelector);
        });
    }
    if (hintsAddBtn) {
        hintsAddBtn.addEventListener('click', () => {
            markHintsDirty();
            createHintRow();
            const rows = hintsList ? hintsList.querySelectorAll('.chaos-hint-row') : [];
            const lastRow = rows.length ? rows[rows.length - 1] : null;
            const focusField = lastRow ? lastRow.querySelector('[data-chaos-hint-transaction]') : null;
            if (focusField && typeof focusField.focus === 'function') {
                focusField.focus();
            }
        });
    }
    if (hintsLoadRecordingBtn && hintsRecordingInput) {
        hintsLoadRecordingBtn.addEventListener('click', () => {
            hintsRecordingInput.click();
        });
        hintsRecordingInput.addEventListener('change', () => {
            const file = hintsRecordingInput.files && hintsRecordingInput.files.length
                ? hintsRecordingInput.files[0]
                : null;
            hintsRecordingInput.value = '';
            if (!file) {
                return;
            }
            extractHintsFromRecording(file);
        });
    }
    if (hintsReloadBtn) {
        hintsReloadBtn.addEventListener('click', () => {
            loadHints();
        });
    }
    if (hintsSaveBtn) {
        hintsSaveBtn.addEventListener('click', () => {
            saveHints();
        });
    }
    if (hintsKeyBlacklistInput) {
        hintsKeyBlacklistInput.addEventListener('input', markHintsDirty);
    }
    if (firstScreenKnownDataInput) {
        firstScreenKnownDataInput.addEventListener('input', markHintsDirty);
    }
    if (firstScreenKnownKeysInput) {
        firstScreenKnownKeysInput.addEventListener('input', markHintsDirty);
    }
    if (firstScreenBlockedKeysInput) {
        firstScreenBlockedKeysInput.addEventListener('input', markHintsDirty);
    }
    if (firstScreenKeyAssignmentsInput) {
        firstScreenKeyAssignmentsInput.addEventListener('input', markHintsDirty);
    }

    // Read chaos config from the settings modal fields (populated from CHAOS_* settings).
    const readChaosConfig = () => {
        const getVal = (key) => {
            const el = document.querySelector(`[data-setting-key="${key}"]`);
            if (!el) {
                return '';
            }
            if (el.dataset.kind === 'checkbox') {
                return el.checked ? 'true' : 'false';
            }
            return el.value.trim();
        };

        const getBool = (key, fallback) => {
            const raw = getVal(key).toLowerCase();
            if (raw === 'true') {
                return true;
            }
            if (raw === 'false') {
                return false;
            }
            return !!fallback;
        };

        const cfg = {};

        const maxSteps = parseInt(getVal('CHAOS_MAX_STEPS'), 10);
        if (!isNaN(maxSteps) && maxSteps >= 0) {
            cfg.maxSteps = maxSteps;
        }

        const timeBudgetSec = parseFloat(getVal('CHAOS_TIME_BUDGET_SEC'));
        if (!isNaN(timeBudgetSec) && timeBudgetSec >= 0) {
            cfg.timeBudgetSec = timeBudgetSec;
        }

        const stepDelaySec = parseFloat(getVal('CHAOS_STEP_DELAY_SEC'));
        if (!isNaN(stepDelaySec) && stepDelaySec >= 0) {
            cfg.stepDelaySec = stepDelaySec;
        }

        const seed = parseInt(getVal('CHAOS_SEED'), 10);
        if (!isNaN(seed)) {
            cfg.seed = seed;
        }

        const maxFieldLength = parseInt(getVal('CHAOS_MAX_FIELD_LENGTH'), 10);
        if (!isNaN(maxFieldLength) && maxFieldLength > 0) {
            cfg.maxFieldLength = maxFieldLength;
        }
        const screenDedupSimilarity = parseFloat(getVal('CHAOS_SCREEN_DEDUP_SIMILARITY'));
        if (!isNaN(screenDedupSimilarity) && screenDedupSimilarity > 0 && screenDedupSimilarity <= 1) {
            cfg.screenDedupSimilarity = screenDedupSimilarity;
        }
        const learnedInputReuseBias = parseFloat(getVal('CHAOS_LEARNED_INPUT_REUSE_BIAS'));
        if (!isNaN(learnedInputReuseBias) && learnedInputReuseBias >= 0 && learnedInputReuseBias <= 1) {
            cfg.learnedInputReuseBias = learnedInputReuseBias;
        }
        const learnedKeyReuseBias = parseFloat(getVal('CHAOS_LEARNED_KEY_REUSE_BIAS'));
        if (!isNaN(learnedKeyReuseBias) && learnedKeyReuseBias >= 0 && learnedKeyReuseBias <= 1) {
            cfg.learnedKeyReuseBias = learnedKeyReuseBias;
        }

        const outputFile = getVal('CHAOS_OUTPUT_FILE');
        if (outputFile) {
            cfg.outputFile = outputFile;
        }

        cfg.forceOverrideExistingInputs = getBool('CHAOS_FORCE_OVERRIDE_EXISTING_INPUTS', true);
        cfg.excludeNoProgressEvents = getBool('CHAOS_EXCLUDE_NO_PROGRESS_EVENTS', true);

        const draftHints = (hintsModal && !hintsModal.hidden) ? collectHintsFromUI() : chaosHints;
        if (Array.isArray(draftHints) && draftHints.length > 0) {
            cfg.hints = draftHints;
        }
        const draftKeyBlacklist = (hintsModal && !hintsModal.hidden)
            ? collectChaosKeyBlacklistFromUI()
            : chaosKeyBlacklist;
        if (Array.isArray(draftKeyBlacklist) && draftKeyBlacklist.length > 0) {
            cfg.keyBlacklist = draftKeyBlacklist;
        }
        const draftFirstScreenHint = (hintsModal && !hintsModal.hidden)
            ? collectFirstScreenHintFromUI()
            : chaosFirstScreenHint;
        if (draftFirstScreenHint && (
            (Array.isArray(draftFirstScreenHint.knownData) && draftFirstScreenHint.knownData.length > 0) ||
            (Array.isArray(draftFirstScreenHint.knownKeys) && draftFirstScreenHint.knownKeys.length > 0) ||
            (Array.isArray(draftFirstScreenHint.blockedKeys) && draftFirstScreenHint.blockedKeys.length > 0) ||
            (draftFirstScreenHint.keyAssignments && Object.keys(draftFirstScreenHint.keyAssignments).length > 0)
        )) {
            cfg.firstScreenHint = {
                knownData: draftFirstScreenHint.knownData || [],
                knownKeys: draftFirstScreenHint.knownKeys || [],
                blockedKeys: draftFirstScreenHint.blockedKeys || [],
                keyAssignments: draftFirstScreenHint.keyAssignments || {},
            };
        }

        return cfg;
    };

    // The server's own explanation of a failed request, or the fallback when
    // it gave none. Never the raw body: a JSON object in a toast is not a
    // message.
    const chaosErrorMessage = async (resp, fallbackMessage) => {
        if (!resp) {
            return fallbackMessage;
        }
        handleChaosAuthFailure(resp);
        let text = '';
        try {
            text = await resp.text();
        } catch (_err) {
            text = '';
        }
        if (text) {
            try {
                const parsed = JSON.parse(text);
                const detail = parsed && typeof parsed.error === 'string' ? parsed.error.trim() : '';
                if (detail) {
                    return detail.charAt(0).toUpperCase() + detail.slice(1);
                }
            } catch (_err) {
                // Not JSON: an HTML error page or a proxy's text. Use the fallback.
            }
        }
        return fallbackMessage;
    };

    const notifyChaosError = async (resp, fallbackMessage) => {
        const message = await chaosErrorMessage(resp, fallbackMessage);
        if (resp && (resp.status === 401 || resp.status === 403)) {
            // handleChaosAuthFailure has already put up the sign-in banner.
            return message;
        }
        notifyUi(message, 'error');
        return message;
    };

    const downloadChaosFile = (name, text, mime) => {
        const blob = new Blob([text], { type: mime || 'application/octet-stream' });
        const url = URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = name;
        document.body.appendChild(a);
        a.click();
        document.body.removeChild(a);
        URL.revokeObjectURL(url);
    };

    // ---- the start dialog ----------------------------------------------
    const chaosStartKeyChoices = [
        'Enter', 'PF1', 'PF2', 'PF3', 'PF4', 'PF5', 'PF6', 'PF7', 'PF8', 'PF9', 'PF10', 'PF11', 'PF12',
        'PA1', 'PA2', 'Clear',
    ];
    // What an exploration tries when nothing says otherwise.
    const chaosStartUsualKeys = ['Enter', 'PF1', 'PF2', 'PF4', 'PF12'];
    let startDialogKeys = new Set(chaosStartUsualKeys);

    const chaosKeyWireName = (key) => {
        const match = /^(PF|PA)(\d+)$/.exec(key);
        return match ? `${match[1]}(${match[2]})` : key;
    };

    const setStartStatus = (message, isError = false) => setStatusLine(startModalStatus, message, isError);

    const buildStartDialogKeys = () => {
        if (!startModalKeys || startModalKeys.children.length) {
            return;
        }
        chaosStartKeyChoices.forEach((key) => {
            const id = `chaos-start-key-${key.toLowerCase()}`;
            const label = document.createElement('label');
            label.className = 'chaos-start-key';
            label.htmlFor = id;
            const input = document.createElement('input');
            input.type = 'checkbox';
            input.id = id;
            input.value = key;
            input.dataset.chaosStartKey = key;
            const text = document.createElement('span');
            text.textContent = key;
            label.appendChild(input);
            label.appendChild(text);
            startModalKeys.appendChild(label);
        });
    };

    const setStartDialogKeys = (keys) => {
        if (!startModalKeys) {
            return;
        }
        const wanted = new Set(keys);
        startModalKeys.querySelectorAll('[data-chaos-start-key]').forEach((input) => {
            input.checked = wanted.has(input.value);
        });
    };

    const readStartDialogKeys = () => (startModalKeys
        ? Array.from(startModalKeys.querySelectorAll('[data-chaos-start-key]'))
            .filter((input) => input.checked)
            .map((input) => input.value)
        : chaosStartUsualKeys.slice());

    const readStartBlockedKeys = () => {
        const seen = new Set();
        return parseListLines(startModalBlockedKeys ? startModalBlockedKeys.value : '').filter((key) => {
            const normalized = String(key || '').trim();
            if (!normalized || seen.has(normalized.toUpperCase())) {
                return false;
            }
            seen.add(normalized.toUpperCase());
            return true;
        });
    };

    const syncStartBlockedWarning = () => {
        if (startModalBlockedWarning) {
            startModalBlockedWarning.hidden = readStartBlockedKeys().length > 0;
        }
    };

    const chaosTargetHost = () => {
        const fromStatus = String((lastStatus && lastStatus.targetHost) || '').trim();
        if (fromStatus) {
            return fromStatus;
        }
        return String((body && body.dataset.targetHost) || '').trim();
    };

    // The limits a run gets when the dialog leaves them empty: whatever
    // Settings says. Read from the server so the dialog shows the real
    // figures even when Settings has never been opened on this page.
    const loadChaosLimitDefaults = async () => {
        const fromDom = (key) => {
            const el = document.querySelector(`[data-setting-key="${key}"]`);
            return el && typeof el.value === 'string' ? el.value.trim() : '';
        };
        let settings = null;
        try {
            const resp = await fetch('/api/settings', { headers: { Accept: 'application/json' } });
            if (resp.ok) {
                const data = await resp.json();
                settings = (data && data.settings) || null;
            }
        } catch (_err) {
            settings = null;
        }
        const read = (key) => String((settings && settings[key] != null ? settings[key] : fromDom(key)) || '').trim();
        return {
            maxSteps: read('CHAOS_MAX_STEPS'),
            timeBudgetSec: read('CHAOS_TIME_BUDGET_SEC'),
        };
    };

    const openStartDialog = async (requestedMode = 'start') => {
        if (!startModal) {
            return false;
        }
        if (lastStatus && lastStatus.active) {
            notifyUi('An exploration is already running.', 'info');
            return false;
        }
        if (isRecordingActive() || isPlaybackActive()) {
            notifyUi('Stop recording or playback before starting an exploration.', 'warning');
            return false;
        }
        const mode = requestedMode === 'continue' && loadedRunID ? 'continue' : 'start';
        startDialogMode = mode;
        const continuing = mode === 'continue';
        const stepsSoFar = Number(lastStatus && lastStatus.stepsRun) || 0;

        if (startModalTitle) {
            startModalTitle.textContent = continuing ? 'Continue exploring' : 'Start exploration';
        }
        if (startModalConfirmBtn) {
            startModalConfirmBtn.textContent = continuing ? 'Continue exploring' : 'Start exploration';
        }
        if (startModalHost) {
            startModalHost.textContent = chaosTargetHost() || 'The host this session is connected to';
        }
        if (startModalRunRow && startModalRun) {
            startModalRunRow.hidden = !continuing;
            const runName = String((lastStatus && lastStatus.label) || '').trim();
            startModalRun.textContent = continuing
                ? `Run ${runName ? `${runName} (${loadedRunID})` : loadedRunID}, ${pluralize(stepsSoFar, 'step')} so far. It carries on from the screen you are on now, with everything it has already learned.`
                : '';
        }
        if (startModalStepsLabel) {
            startModalStepsLabel.textContent = continuing ? 'Extra steps' : 'Step limit';
        }
        if (startModalStepsHelp) {
            startModalStepsHelp.textContent = continuing
                ? `How many more times it may press a key, on top of the ${stepsSoFar} it has already used. Leave empty for 100 more.`
                : 'How many times it may press a key. Leave empty to use the limit from Settings.';
        }
        const timeHelp = document.getElementById('chaos-start-time-budget-help');
        if (timeHelp) {
            timeHelp.textContent = continuing
                ? 'It stops when either limit is reached. Leave empty for 5 minutes.'
                : 'It stops when either limit is reached. Leave empty to use the limit from Settings.';
        }
        if (startModalMaxSteps) {
            startModalMaxSteps.value = '';
            startModalMaxSteps.placeholder = '';
        }
        if (startModalTimeBudget) {
            startModalTimeBudget.value = '';
            startModalTimeBudget.placeholder = '';
        }
        if (startModalBlockedKeys) {
            startModalBlockedKeys.value = formatListLines(chaosKeyBlacklist);
        }
        buildStartDialogKeys();
        setStartDialogKeys(Array.from(startDialogKeys));
        syncStartBlockedWarning();
        setStartStatus('');
        openChaosModal(startModal, '[data-chaos-start-max-steps]');

        // Fill in the limits once known. A value somebody has already typed
        // is left alone.
        const [limits] = await Promise.all([loadChaosLimitDefaults(), loadHints({ quiet: true })]);
        if (startModal.hidden || startDialogMode !== mode) {
            return true;
        }
        const fill = (input, raw, fallback) => {
            if (!input || input.value !== '') {
                return;
            }
            const n = Number(raw);
            if (raw !== '' && Number.isFinite(n) && n > 0) {
                input.value = String(n);
            } else if (continuing) {
                input.value = String(fallback);
            } else {
                input.placeholder = raw === '0' ? 'No limit' : 'From Settings';
            }
        };
        fill(startModalMaxSteps, limits.maxSteps, 100);
        fill(startModalTimeBudget, limits.timeBudgetSec, 300);
        if (startModalBlockedKeys && document.activeElement !== startModalBlockedKeys
            && startModalBlockedKeys.value === '') {
            startModalBlockedKeys.value = formatListLines(chaosKeyBlacklist);
        }
        syncStartBlockedWarning();
        return true;
    };

    // Blocked keys changed in the start dialog become the saved default: the
    // server falls back to the saved list whenever a request carries none, so
    // an emptied list sent only with the request would be quietly ignored.
    const saveStartBlockedKeys = async (keys) => {
        const resp = await fetch('/chaos/hints', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                hints: chaosHints,
                keyBlacklist: keys,
                firstScreenHint: chaosFirstScreenHint,
            }),
        });
        if (!resp.ok) {
            throw new Error(await chaosErrorMessage(resp, 'The blocked keys could not be saved.'));
        }
        const payload = await resp.json();
        chaosHints = normalizeHints(payload.hints || []);
        chaosKeyBlacklist = parseListLines(payload.keyBlacklist || []);
        chaosFirstScreenHint = normalizeScreenHint(payload.firstScreenHint || {});
        if (!(hintsModal && !hintsModal.hidden)) {
            renderChaosKeyBlacklist(chaosKeyBlacklist);
        }
    };

    const readStartLimit = (input, name) => {
        const raw = input ? input.value.trim() : '';
        if (raw === '') {
            return { value: null };
        }
        const n = Number(raw);
        if (!Number.isFinite(n) || n < 1 || Math.floor(n) !== n) {
            return { error: `${name} must be a whole number of 1 or more, or left empty.`, input };
        }
        return { value: n };
    };

    const submitStartDialog = async () => {
        if (startDialogBusy || !startModal || startModal.hidden) {
            return;
        }
        const mode = startDialogMode;
        const continuing = mode === 'continue';
        const steps = readStartLimit(startModalMaxSteps, continuing ? 'Extra steps' : 'Step limit');
        const time = readStartLimit(startModalTimeBudget, 'Time limit');
        const invalid = steps.error ? steps : (time.error ? time : null);
        if (invalid) {
            setStartStatus(invalid.error, true);
            invalid.input.setAttribute('aria-invalid', 'true');
            invalid.input.focus();
            return;
        }
        [startModalMaxSteps, startModalTimeBudget].forEach((input) => {
            if (input) {
                input.removeAttribute('aria-invalid');
            }
        });
        const keys = readStartDialogKeys();
        if (!keys.length) {
            setStartStatus('Tick at least one key for the exploration to try.', true);
            const first = startModalKeys ? startModalKeys.querySelector('[data-chaos-start-key]') : null;
            if (first) {
                first.focus();
            }
            return;
        }
        const blocked = readStartBlockedKeys();

        startDialogBusy = true;
        setButtonBusy(startModalConfirmBtn, true);
        setStartStatus(continuing ? 'Continuing…' : 'Starting…');
        try {
            if (blocked.join('\n') !== chaosKeyBlacklist.join('\n')) {
                try {
                    await saveStartBlockedKeys(blocked);
                } catch (err) {
                    setStartStatus((err && err.message) || 'The blocked keys could not be saved.', true);
                    return;
                }
            }
            const proceed = await confirmLoggingBeforeChaosStart();
            if (!proceed) {
                setStartStatus('');
                return;
            }
            const cfg = readChaosConfig();
            if (steps.value !== null) {
                cfg.maxSteps = steps.value;
            }
            if (time.value !== null) {
                cfg.timeBudgetSec = time.value;
            }
            if (blocked.length) {
                cfg.keyBlacklist = blocked;
            } else {
                delete cfg.keyBlacklist;
            }
            const usual = chaosStartUsualKeys.slice().sort().join(',');
            if (keys.slice().sort().join(',') !== usual) {
                // Enter carries most of the weight, as it does by default:
                // it is the key that submits a screen.
                const weights = {};
                keys.forEach((key) => {
                    weights[chaosKeyWireName(key)] = key === 'Enter' ? 70 : 5;
                });
                cfg.aidKeyWeights = weights;
            }
            if (continuing) {
                cfg.extendLimits = true;
            }
            const resp = await fetch(continuing ? '/chaos/resume' : '/chaos/start', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(cfg),
            });
            if (!resp.ok) {
                setStartStatus(await chaosErrorMessage(
                    resp,
                    continuing ? 'The exploration could not be continued.' : 'The exploration could not be started.'
                ), true);
                return;
            }
            startDialogKeys = new Set(keys);
            startDialogBusy = false;
            closeChaosModal(startModal, { restoreFocus: false });
            if (!continuing) {
                // A new run starts a new map.
                loadedRunID = null;
                latestMindMap = null;
                latestMindMapIsFull = false;
                latestSlimSignature = '';
                fullMapSlimSignature = '';
                latestAreaCount = 0;
                groupedScreenCount.areaCount = -1;
                groupedScreenCount.screens = 0;
            }
            lastInsightsKey = '';
            userRequestedStop = false;
            updateUI(continuing
                ? { active: true, stepsRun: stepsSoFarForContinue(), transitions: 0, loadedRunID }
                : { active: true, stepsRun: 0, transitions: 0 });
            pollStatus();
            const message = continuing ? 'Exploration continuing.' : 'Exploration started.';
            notifyUi(message, 'info');
            if (typeof window.refreshWorkflowStatus === 'function') {
                await window.refreshWorkflowStatus();
            }
            // The strip has just appeared with the one control that matters
            // now; put focus on it rather than leave it on the page body.
            if (activeRunStopBtn && activeRunStopBtn.getClientRects().length) {
                activeRunStopBtn.focus();
            } else {
                restoreChaosFocus(null);
            }
        } catch (_err) {
            setStartStatus(continuing
                ? 'The exploration could not be continued: network error.'
                : 'The exploration could not be started: network error.', true);
        } finally {
            startDialogBusy = false;
            setButtonBusy(startModalConfirmBtn, false);
            applyChaosInterlocks();
        }
    };

    const stepsSoFarForContinue = () => Number(lastStatus && lastStatus.stepsRun) || 0;

    if (startModal) {
        startModalCancelButtons.forEach((btn) => {
            btn.addEventListener('click', () => requestCloseChaosModal(startModal));
        });
        startModal.addEventListener('click', (event) => {
            if (event.target === startModal) {
                requestCloseChaosModal(startModal);
            }
        });
        if (startForm) {
            startForm.addEventListener('submit', (event) => {
                event.preventDefault();
                submitStartDialog();
            });
        }
        if (startModalBlockedKeys) {
            startModalBlockedKeys.addEventListener('input', syncStartBlockedWarning);
        }
        if (startModalKeysDefaultBtn) {
            startModalKeysDefaultBtn.addEventListener('click', () => setStartDialogKeys(chaosStartUsualKeys));
        }
        if (startModalKeysAllBtn) {
            startModalKeysAllBtn.addEventListener('click', () => setStartDialogKeys(chaosStartKeyChoices));
        }
    }

    const stopChaosRun = async () => {
        const buttons = [stopBtn, activeRunStopBtn].filter(Boolean);
        buttons.forEach((btn) => setButtonBusy(btn, true));
        userRequestedStop = true;
        try {
            const resp = await fetch('/chaos/stop', { method: 'POST' });
            if (!resp.ok) {
                userRequestedStop = false;
                await notifyChaosError(resp, 'The exploration could not be stopped.');
            }
            // No toast from here: the run ending is announced once, by the
            // status change, with how many steps it took.
            await pollStatus();
            if (typeof window.refreshWorkflowStatus === 'function') {
                window.refreshWorkflowStatus();
            }
        } catch (_err) {
            userRequestedStop = false;
            await notifyChaosError(null, 'The exploration could not be stopped: network error.');
        } finally {
            buttons.forEach((btn) => setButtonBusy(btn, false));
            applyChaosInterlocks();
        }
    };

    const exportChaosWorkflow = async (button) => {
        setButtonBusy(button, true);
        try {
            const resp = await fetch('/chaos/export', { method: 'POST' });
            if (!resp.ok) {
                await notifyChaosError(resp, 'The workflow could not be exported.');
                return;
            }
            const data = await resp.text();
            const name = loadedRunID ? `chaos-workflow-${loadedRunID}.json` : 'chaos-workflow.json';
            if (typeof window.openWorkflowJsonEditor === 'function') {
                window.openWorkflowJsonEditor({
                    title: 'Exploration workflow JSON',
                    name,
                    downloadName: name,
                    content: data,
                    editable: true,
                    saveMode: 'chaos-workflow',
                    showSave: false,
                    showLoad: true,
                    showDownload: true,
                });
            } else {
                downloadChaosFile(name, data, 'application/json');
                notifyUi('Workflow JSON downloaded.', 'success');
            }
        } catch (_err) {
            await notifyChaosError(null, 'The workflow could not be exported: network error.');
        } finally {
            setButtonBusy(button, false);
            applyChaosInterlocks();
        }
    };

    const openChaosReportFrom = async (button) => {
        setButtonBusy(button, true);
        try {
            await openChaosReportModal();
        } catch (_err) {
            // The dialog is open and says what went wrong.
        } finally {
            setButtonBusy(button, false);
            applyChaosInterlocks();
        }
    };

    // Initial status check on page load.
    try {
        setHintsModalMaximized(window.localStorage.getItem('h3270ChaosHintsModalMaximized') === '1');
    } catch (_err) {
        setHintsModalMaximized(false);
    }
    try {
        setMapModalMaximized(window.localStorage.getItem('h3270ChaosMapModalMaximized') === '1');
    } catch (_err) {
        setMapModalMaximized(false);
    }
    try {
        setFlowModalMaximized(window.localStorage.getItem('h3270ChaosFlowModalMaximized') === '1');
    } catch (_err) {
        setFlowModalMaximized(false);
    }
    try {
        setFlowScreenHintsModalMaximized(window.localStorage.getItem('h3270ChaosFlowScreenHintsModalMaximized') === '1');
    } catch (_err) {
        setFlowScreenHintsModalMaximized(false);
    }
    try {
        setReportModalMaximized(window.localStorage.getItem('h3270ChaosReportModalMaximized') === '1');
    } catch (_err) {
        setReportModalMaximized(false);
    }
    setReportRawToggleState(false);
    syncChaosSectionLayout();
    loadHints({ quiet: true });
    pollStatus();

    if (startBtn) {
        startBtn.addEventListener('click', () => {
            if (startBtn.disabled || startBtn.getAttribute('aria-busy') === 'true') {
                return;
            }
            openStartDialog('start');
        });
    }

    if (stopBtn) {
        stopBtn.addEventListener('click', () => {
            stopChaosRun();
        });
    }

    if (loadBtn) {
        loadBtn.addEventListener('click', () => {
            openRunsModal();
        });
    }

    if (loadRecordingBtn) {
        loadRecordingBtn.addEventListener('click', async () => {
            setButtonBusy(loadRecordingBtn, true);
            try {
                const resp = await fetch('/chaos/load-recording', { method: 'POST' });
                if (!resp.ok) {
                    await notifyChaosError(resp, 'The recording could not be loaded into exploration.');
                    return;
                }
                const data = await resp.json();
                loadedRunID = data.runID || loadedRunID;
                updateUI({
                    active: false,
                    stepsRun: data.stepsRun || 0,
                    transitions: data.transitions || 0,
                    uniqueScreens: data.uniqueScreens || 0,
                    uniqueInputs: data.uniqueInputs || 0,
                    loadedRunID,
                    mindMap: data.mindMap || null,
                });
                if (typeof window.refreshWorkflowStatus === 'function') {
                    await window.refreshWorkflowStatus();
                }
                const summary = chaosCountsSummary({
                    steps: data.stepsRun || data.stepsSeeded || 0,
                    screens: data.uniqueScreens || 0,
                    inputs: data.uniqueInputs || 0,
                    keys: data.uniqueKeys || 0,
                });
                notifyUi(
                    `Recording loaded into exploration${summary ? ` (${summary})` : ''}.`,
                    'success'
                );
            } catch (_err) {
                await notifyChaosError(null, 'The recording could not be loaded into exploration: network error.');
            } finally {
                setButtonBusy(loadRecordingBtn, false);
                applyChaosInterlocks();
            }
        });
    }

    // "Continue exploring", from the menu, the strip and the results panel.
    [resumeBtn, runPanelContinueBtn].forEach((btn) => {
        if (!btn) {
            return;
        }
        btn.addEventListener('click', () => {
            if (btn.disabled || btn.getAttribute('aria-busy') === 'true') {
                return;
            }
            if (!loadedRunID) {
                notifyUi('There is no saved run to continue yet. Wait for the run to be saved, or load one from Saved runs.', 'info');
                return;
            }
            openStartDialog('continue');
        });
    });

    [exportBtn, runPanelExportBtn].forEach((btn) => {
        if (btn) {
            btn.addEventListener('click', () => {
                exportChaosWorkflow(btn);
            });
        }
    });

    [reportBtn, runPanelReportBtn].forEach((btn) => {
        if (btn) {
            btn.addEventListener('click', () => {
                openChaosReportFrom(btn);
            });
        }
    });

    if (runPanelMapBtn) {
        runPanelMapBtn.addEventListener('click', () => {
            openChaosMapModal();
        });
    }

    if (removeBtn) {
        removeBtn.addEventListener('click', async () => {
            const saved = String(loadedRunID || '').trim();
            const confirmed = await promptChaosConfirm({
                title: 'Remove this run from the session?',
                message: saved
                    ? `The screen map, report and workflow for run ${saved} will be closed here. The saved copy stays under Saved runs and can be loaded again.`
                    : 'This run has not been saved yet. Its screen map, report and workflow will be lost.',
                confirmLabel: 'Remove run',
            });
            if (!confirmed) {
                return;
            }
            setButtonBusy(removeBtn, true);
            try {
                const resp = await fetch('/chaos/remove', { method: 'POST' });
                if (!resp.ok) {
                    await notifyChaosError(resp, 'The run could not be removed.');
                    return;
                }
                loadedRunID = null;
                updateUI({
                    active: false,
                    stepsRun: 0,
                    transitions: 0,
                    uniqueScreens: 0,
                    uniqueInputs: 0,
                });
                await pollStatus();
                if (typeof window.refreshWorkflowStatus === 'function') {
                    await window.refreshWorkflowStatus();
                }
                notifyUi('Run removed from this session.', 'info');
            } catch (_err) {
                await notifyChaosError(null, 'The run could not be removed: network error.');
            } finally {
                setButtonBusy(removeBtn, false);
                applyChaosInterlocks();
            }
        });
    }

    // A session that expired stops the status poll; coming back to the
    // window is the natural moment to find out whether it is back.
    window.addEventListener('focus', () => {
        if (chaosSessionExpired) {
            chaosSessionExpired = false;
            pollStatus();
        }
    });

    if (body && typeof MutationObserver !== 'undefined') {
        const obs = new MutationObserver(() => {
            applyChaosInterlocks();
        });
        obs.observe(body, { attributes: true, attributeFilter: ['data-playback-active'] });
        if (recordingIndicator) {
            obs.observe(recordingIndicator, { attributes: true, attributeFilter: ['hidden'] });
        }
    }

    // Expose integration surface for the AI chat panel so tool results can
    // drive UI updates without duplicating state or bypassing existing logic.
    window.ChaosUI = {
        // A status object somebody else fetched (the assistant's chaos_status
        // tool). It goes through the same door as the poll, so the menu, the
        // strip and any open view all follow.
        updateFromStatus: function (status) {
            if (!status || typeof status !== 'object') return;
            if (typeof status.active === 'boolean' || status.stepsRun != null) {
                updateUI(status);
                if (status.active && !pollTimer) {
                    scheduleChaosPoll(pollActiveDelayMs);
                }
                return;
            }
            if (updateChaosMapFromStatus(status)) {
                refreshOpenChaosMapViews();
            }
        },
        // Ask the server now. For anything that starts, stops or loads a run
        // without going through this module: the assistant, another tab.
        refresh: function () {
            return pollStatus();
        },
        // Called by the workflow-status poll with each payload. That poll
        // already knows whether an exploration is running; when its answer
        // changes, this module catches up instead of waiting to be asked.
        syncFromWorkflowStatus: function (payload) {
            if (!payload || typeof payload !== 'object') return;
            const active = !!payload.chaosActive;
            const limitsBefore = `${knownRunLimits.maxSteps}|${knownRunLimits.stoppedAt}`;
            knownRunLimits.maxSteps = Number(payload.chaosMaxSteps || 0);
            knownRunLimits.timeBudgetMs = Number(payload.chaosTimeBudgetMs || 0);
            knownRunLimits.startedAt = String(payload.chaosStartedAt || '');
            knownRunLimits.stoppedAt = String(payload.chaosStoppedAt || '');
            if (!active && lastStatus && !lastStatus.active && hasData
                && limitsBefore !== `${knownRunLimits.maxSteps}|${knownRunLimits.stoppedAt}`) {
                renderChaosRunPanel(lastStatus, runPanel ? !runPanel.hidden : false);
            }
            const key = [active, String(payload.chaosLoadedRunID || ''), active ? '' : Number(payload.chaosStepsRun || 0)].join('|');
            if (key === lastWorkflowSyncKey) return;
            lastWorkflowSyncKey = key;
            const known = lastStatus || {};
            if (active === !!known.active
                && String(payload.chaosLoadedRunID || '') === String(loadedRunID || '')
                && (active || Number(payload.chaosStepsRun || 0) === Number(known.stepsRun || 0))) {
                return;
            }
            pollStatus();
        },
        openStartDialog: function (mode) {
            return openStartDialog(mode === 'continue' ? 'continue' : 'start');
        },
        stop: function () {
            return stopChaosRun();
        },
        // "4 screens (7 variants)" for a count of distinct screens the server
        // reported, so the strip and the map say the same thing.
        screenCountLabel: function (variants) {
            return screenCountLabel(variants);
        },
        // How the last run ended: { reason, tone, label, title, sentence }.
        outcome: function () {
            if (lastStatus && lastStatus.active) return null;
            return lastOutcome;
        },
        canContinue: function () {
            return !!loadedRunID && !(lastStatus && lastStatus.active) && !isPlaybackActive() && !isRecordingActive();
        },
        setChaosReport: function (markdown) {
            setChaosReportContent(String(markdown || ''));
        },
        openChaosMap: function () {
            if (!mapModal) return false;
            openChaosMapModal();
            return true;
        },
        openChaosFlow: function () {
            if (!flowModal) return false;
            openChaosFlowFromMap();
            return true;
        },
        openChaosReport: function () {
            if (!reportModal) return false;
            openChaosModal(reportModal, chaosReportTopFocusSelector);
            return true;
        },
        isMapVisible: function () { return !!(mapModal && !mapModal.hidden); },
        isFlowVisible: function () { return !!(flowModal && !flowModal.hidden); },
        isReportVisible: function () { return !!(reportModal && !reportModal.hidden); },
        downloadWorkflow: function (jsonText, runID) {
            const name = runID ? `chaos-workflow-${runID}.json` : 'chaos-workflow.json';
            if (typeof window.openWorkflowJsonEditor === 'function') {
                window.openWorkflowJsonEditor({
                    title: 'Exploration workflow JSON',
                    name,
                    downloadName: name,
                    content: jsonText,
                    editable: true,
                    saveMode: 'chaos-workflow',
                    showSave: false,
                    showLoad: true,
                    showDownload: true,
                });
            } else {
                const blob = new Blob([jsonText], { type: 'application/json' });
                const url = URL.createObjectURL(blob);
                const a = document.createElement('a');
                a.href = url;
                a.download = name;
                document.body.appendChild(a);
                a.click();
                document.body.removeChild(a);
                URL.revokeObjectURL(url);
                notifyUi('Workflow JSON downloaded.', 'success');
            }
        },
    };
})();
