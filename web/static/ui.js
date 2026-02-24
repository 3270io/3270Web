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
        CHAOS_SCREEN_DEDUP_SIMILARITY: '0.985',
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
            title: 'Chaos Explorer',
            description: 'Configuration for chaos exploration runs. Values are used when starting a chaos run from the toolbar.',
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
                { key: 'CHAOS_SCREEN_DEDUP_SIMILARITY', label: 'Screen dedup similarity', type: 'text', helper: 'Similarity threshold (0-1] for merging near-duplicate screens in the chaos discovery map. Higher = stricter (0.995 keeps more separate screens), lower = more merging (0.97 merges value-echo variants more aggressively). Default 0.985 is a balanced starting point.' },
                { key: 'CHAOS_EXCLUDE_NO_PROGRESS_EVENTS', label: 'Exclude no-progress events', type: 'checkbox', helper: 'Exclude attempts with no screen transition from chaos event history and attempt detail views.' },
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
        {
            id: 'automation',
            title: 'Automation/Startup',
            description: 'Startup commands and automation hooks.',
            sections: [
                { id: 'auto_start', title: 'Startup Actions', fieldKeys: ['S3270_EXEC_COMMAND', 'S3270_LOGIN_MACRO'] },
                { id: 'auto_server', title: 'Built-in Server', fieldKeys: ['S3270_HTTPD'] },
                { id: 'auto_require', title: 'Requirements', fieldKeys: ['S3270_MIN_VERSION'] },
            ],
            fields: [
                { key: 'S3270_EXEC_COMMAND', label: 'Exec command', type: 'text', helper: 'Command to run instead of connecting to a host.' },
                { key: 'S3270_LOGIN_MACRO', label: 'Login macro', type: 'text', helper: 'Actions to run when the host connection is established.' },
                { key: 'S3270_HTTPD', label: 'HTTPD', type: 'text', helper: 'Start HTTP server at the given address.' },
                { key: 'S3270_MIN_VERSION', label: 'Min version', type: 'text', helper: 'Minimum required s3270 version.' },
            ],
        },
        {
            id: 'diagnostics',
            title: 'Diagnostics',
            description: 'Tracing, version checks, and help toggles.',
            sections: [
                { id: 'diag_trace', title: 'Tracing', fieldKeys: ['S3270_TRACE', 'S3270_TRACE_FILE', 'S3270_TRACE_FILE_SIZE'] },
                { id: 'diag_info', title: 'Info & Debug Flags', fieldKeys: ['S3270_HELP', 'S3270_VERSION', 'S3270_UTENV'] },
            ],
            fields: [
                { key: 'S3270_TRACE', label: 'Trace', type: 'checkbox', helper: 'Turn on data stream and action tracing.' },
                { key: 'S3270_TRACE_FILE', label: 'Trace file', type: 'text', helper: 'File for data stream and action tracing.' },
                { key: 'S3270_TRACE_FILE_SIZE', label: 'Trace file size', type: 'text', helper: 'Limit trace file size in bytes.' },
                { key: 'S3270_HELP', label: 'Show help', type: 'checkbox', helper: 'Display command-line help and exit.' },
                { key: 'S3270_VERSION', label: 'Show version', type: 'checkbox', helper: 'Display version information and exit.' },
                { key: 'S3270_UTENV', label: 'UT env', type: 'checkbox', helper: 'Allow unit-test-specific env vars.' },
            ],
        },
    ];

    const fieldMap = new Map();
    const fieldMetaMap = new Map();
    const fieldBaseOptionsMap = new Map();
    const fieldExtraOptionsMap = new Map();
    const fieldExtrasListMap = new Map();
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
        txInput.addEventListener('input', () => setChaosDefaultsStatus('Unsaved chaos default hints changes.'));
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
        dataInput.addEventListener('input', () => setChaosDefaultsStatus('Unsaved chaos default hints changes.'));
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
        keysInput.addEventListener('input', () => setChaosDefaultsStatus('Unsaved chaos default hints changes.'));
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
            setChaosDefaultsStatus('Unsaved chaos default hints changes.');
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
        setChaosDefaultsStatus('Unsaved chaos default hints changes.');
    };

    const loadChaosDefaultsFromServer = async () => {
        if (!chaosDefaultsHintsList) {
            return;
        }
        setChaosDefaultsStatus('Loading chaos defaults...');
        try {
            const response = await fetch('/chaos/hints');
            let payload = {};
            try {
                payload = await response.json();
            } catch (_parseErr) {
                payload = {};
            }
            if (!response.ok) {
                throw new Error(payload.error || 'Failed to load chaos defaults.');
            }
            renderChaosDefaultsEditorPayload(payload);
            setChaosDefaultsStatus('Chaos defaults loaded.');
        } catch (error) {
            setChaosDefaultsStatus((error && error.message) ? error.message : 'Failed to load chaos defaults.', true);
        }
    };

    const saveChaosDefaultsToServer = async () => {
        if (!chaosDefaultsHintsList) {
            return;
        }
        const payload = collectChaosDefaultsEditorPayload();
        setChaosDefaultsStatus('Saving chaos defaults...');
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
                throw new Error(data.error || 'Failed to save chaos defaults.');
            }
            renderChaosDefaultsEditorPayload(data);
            setChaosDefaultsStatus('Chaos defaults saved.');
        } catch (error) {
            setChaosDefaultsStatus((error && error.message) ? error.message : 'Failed to save chaos defaults.', true);
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
        help.textContent = 'Saved separately from .env settings via /chaos/hints. These defaults are used by chaos runs when request-level hints/blacklist are not provided.';
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
        h3.textContent = 'Default Chaos Hints';
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
        modalHelp.textContent = 'Edit saved default chaos hints and blocked keys. These are used when a chaos run starts without request-level overrides.';
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
        blockedInput.placeholder = 'Keys chaos must never press (one per line)\nPF3\nPF12';
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
        loadBtn.textContent = 'Load Chaos Defaults';
        loadBtn.dataset.settingsChaosDefaultsLoad = '1';
        loadBtn.addEventListener('click', () => {
            loadChaosDefaultsFromServer();
        });
        actions.appendChild(loadBtn);

        const saveBtn = document.createElement('button');
        saveBtn.type = 'button';
        saveBtn.textContent = 'Save Chaos Defaults';
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
        ensureSettingsTooltips();
        if (typeof loadSettings === 'function') {
            loadSettings();
        }
    };

    const closeSettingsModal = () => {
        closeChaosDefaultsSubModal();
        closeRestartConfirm(false);
        modal.hidden = true;
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
        restartConfirmButton.focus();
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
                    if (entry) {
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

                fieldMap.set(field.key, { input, error });
                fieldMetaMap.set(field.key, field);
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

    const loadSettings = async () => {
        setStatus('Loading settings...');
        try {
            const response = await fetch('/api/settings?includeSensitive=true');
            if (!response.ok) {
                throw new Error('Failed to load settings.');
            }
            const data = await response.json();
            populateSettings(data.settings || {});
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
                throw new Error(data.error || 'Failed to save settings.');
            }

            populateSettings(data.settings || payload.settings);
            setStatus('Settings saved.');
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

    document.addEventListener('keydown', (event) => {
        if (event.key === 'Escape' && chaosDefaultsSubModal && !chaosDefaultsSubModal.hidden) {
            closeChaosDefaultsSubModal();
            return;
        }
        if (event.key === 'Escape' && !modal.hidden && restartConfirmModal && !restartConfirmModal.hidden) {
            closeRestartConfirm(false);
        }
    });
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
    const extendBtn = document.querySelector('[data-chaos-extend]');
    const indicator = document.querySelector('[data-chaos-indicator]');
    const completeIndicator = document.querySelector('[data-chaos-complete-indicator]');
    const statsIndicator = document.querySelector('[data-chaos-stats-indicator]');
    const statsText = document.querySelector('[data-chaos-stats-text]');
    const runsModal = document.querySelector('[data-chaos-runs-modal]');
    const runsModalClose = document.querySelectorAll('[data-chaos-runs-close]');
    const runsList = document.querySelector('[data-chaos-runs-list]');
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
    const flowModal = document.querySelector('[data-chaos-flow-modal]');
    const flowModalClose = document.querySelectorAll('[data-chaos-flow-close]');
    const flowMaximizeBtn = document.querySelector('[data-chaos-flow-maximize]');
    const flowMinimizeBtn = document.querySelector('[data-chaos-flow-minimize]');
    const flowContent = document.querySelector('[data-chaos-flow-content]');
    const flowStatus = document.querySelector('[data-chaos-flow-status]');
    const flowRefreshButtons = Array.from(document.querySelectorAll('[data-chaos-flow-refresh]'));
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
    const recordingStartButton = document.querySelector('form[data-recording-start] .icon-button');
    const workflowLoadButton = document.querySelector('[data-workflow-trigger]');
    const logsToggleCheckbox = document.querySelector('[data-logs-toggle-checkbox]');

    if (!startBtn && !stopBtn && !exportBtn && !removeBtn) {
        return;
    }

    let pollTimer = null;
    let hasData = false;
    let loadedRunID = null;
    let lastStatus = { active: false, stepsRun: 0, transitions: 0 };
    let chaosHints = [];
    let chaosKeyBlacklist = [];
    let chaosFirstScreenHint = { knownData: [], knownKeys: [], blockedKeys: [], keyAssignments: {} };
    let hintsDirty = false;
    let hintsModalMaximized = false;
    let mapModalMaximized = false;
    let flowModalMaximized = false;
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

    const closeChaosModal = (modal, options = {}) => {
        if (!modal) {
            return;
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
            if (restoreFocus && previousChaosFocus && typeof previousChaosFocus.focus === 'function') {
                previousChaosFocus.focus();
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
            closeChaosModal(modal);
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

    const setHintsStatus = (message, isError = false) => {
        if (!hintsStatus) {
            return;
        }
        hintsStatus.textContent = message || '';
        hintsStatus.style.color = isError ? '#ff9a5a' : '';
    };

    const setFlowStatus = (message, isError = false) => {
        if (!flowStatus) {
            return;
        }
        flowStatus.textContent = message || '';
        flowStatus.style.color = isError ? '#ff9a5a' : '';
    };

    const setReportStatus = (message, isError = false) => {
        if (!reportStatus) {
            return;
        }
        reportStatus.textContent = message || '';
        reportStatus.style.color = isError ? '#ff9a5a' : '';
    };

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
            if (canDisable) {
                const disable = window.confirm('Verbose logging is enabled. Press OK to disable logging and start Chaos mode.');
                if (disable) {
                    return Promise.resolve('disable');
                }
            }
            const proceed = window.confirm('Verbose logging is enabled. Start Chaos mode with logging still enabled?');
            return Promise.resolve(proceed ? 'continue' : 'cancel');
        }

        const canDisable = options.canDisable !== false;
        setStartLogConfirmDisableAvailable(canDisable);
        setStartLogConfirmMessage(
            canDisable
                ? 'Continue with logging enabled, or disable logging and then start Chaos mode?'
                : 'Logging is enabled, but log access is disabled, so it cannot be turned off from here. Start Chaos with logging enabled, or cancel?'
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
                    : '[data-chaos-start-log-confirm-continue]'
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
            window.alert('Unable to disable verbose logging. Chaos mode was not started.');
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

    const setChaosMapStatus = (message, isError = false) => {
        if (!mapStatus) {
            return;
        }
        mapStatus.textContent = message || '';
        mapStatus.style.color = isError ? '#ff9a5a' : '';
    };

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
        return out;
    };

    const parseMarkdownTableRow = (line) => {
        const src = String(line || '').trim();
        if (!src.includes('|')) {
            return [];
        }
        let body = src;
        if (body.startsWith('|')) {
            body = body.slice(1);
        }
        if (body.endsWith('|')) {
            body = body.slice(0, -1);
        }
        return body.split('|').map((cell) => cell.trim());
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
        const out = { knownData: [], knownKeys: [], keyAssignments: {} };
        const dataSeen = new Set();
        const keySeen = new Set();
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
                setChaosMapStatus(options.successMessage || `Saved screen hints for ${screenHash}.`);
            }
            if (!options.skipRender && mapModal && !mapModal.hidden) {
                renderChaosMap();
            }
            return true;
        } catch (err) {
            if (!options.quiet) {
                setChaosMapStatus((err && err.message) ? err.message : 'Failed to save screen hints.', true);
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
                setChaosMapStatus(`Failed to save screen hints for ${hash}.`, true);
                if (mapModal && !mapModal.hidden) {
                    renderChaosMap();
                }
                return false;
            }
            okCount += 1;
        }
        setChaosMapStatus(okCount > 1
            ? `Saved screen hints to ${okCount} merged screen variants.`
            : `Saved screen hints for ${targets[0]}.`);
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
                };
                groupedByTemplate.set(templateId, group);
            }
            group.visits += areaVisits;
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
                'Start or extend chaos again'.slice(0, fallbackCols).padEnd(fallbackCols, ' '),
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
            const nodeTitle = [
                discoveryText,
                String(node.label || ''),
                `${node.visits} visit${node.visits === 1 ? '' : 's'}`,
                `${node.inputs} input${node.inputs === 1 ? '' : 's'}`,
                node.variants > 1 ? `${node.variants} variants merged` : '',
            ].filter(Boolean).join(' | ');
            return `
                <g
                    class="chaos-map-flow-node"
                    transform="translate(${node.x},${node.y})"
                    data-chaos-flow-node
                    data-chaos-node-id="${escapeHtml(node.hash)}"
                    data-chaos-node-x="${node.x}"
                    data-chaos-node-y="${node.y}"
                    data-chaos-node-w="${node.w}"
                    data-chaos-node-h="${node.h}"
                >
                    <rect width="${node.w}" height="${node.h}" rx="10" ry="10"></rect>
                    ${chaosFlowNodeBodyMarkup(node, isModal)}
                    <title>${escapeHtml(nodeTitle)}</title>
                </g>
            `;
        }).join('');

        const summaryBits = [
            `${model.totalAreas} unique screen${model.totalAreas === 1 ? '' : 's'}`,
            model.totalRawAreas > model.totalAreas ? `${model.totalRawAreas - model.totalAreas} variants merged` : '',
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
            <section class="chaos-map-flow-panel${isModal ? ' is-modal' : ''}" aria-label="Chaos discovery screen flow" data-chaos-flow-canvas="${isModal ? 'modal' : 'inline'}">
                <div class="chaos-map-flow-header">
                    <div class="chaos-map-flow-header-top">
                        <strong>${isModal ? 'Discovery Flow (Expanded)' : 'Discovery Flow'}</strong>
                        <div class="chaos-map-flow-header-actions">
                            <div class="chaos-map-flow-zoom-controls" role="group" aria-label="Discovery flow zoom controls">
                                <button type="button" data-chaos-flow-zoom-out title="Zoom out">-</button>
                                <button type="button" data-chaos-flow-zoom-in title="Zoom in">+</button>
                                <button type="button" data-chaos-flow-zoom-fit title="Fit graph to viewport">Fit</button>
                                <button type="button" data-chaos-flow-layout-reset title="Reset moved screen positions">Layout</button>
                                <button type="button" data-chaos-flow-zoom-reset title="Reset zoom to 100%">100%</button>
                                <span class="chaos-map-flow-zoom-value subtle" data-chaos-flow-zoom-value>100%</span>
                            </div>
                            ${showOpenButton ? '<button type="button" data-chaos-flow-open>View Map</button>' : ''}
                        </div>
                    </div>
                    <div class="chaos-map-flow-meta-row">
                        <span class="subtle">${escapeHtml(summaryBits)}</span>
                    </div>
                    <div class="chaos-map-flow-meta-row">
                        <span class="subtle">Screen previews show one captured example for each unique screen. Other visits may differ based on data entered.</span>
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
            `${model.totalAreas} unique screen${model.totalAreas === 1 ? '' : 's'}`,
            model.totalRawAreas > model.totalAreas ? `${model.totalRawAreas - model.totalAreas} variants merged` : '',
            `${(Array.isArray(model.edges) ? model.edges.length : 0)} transition${(Array.isArray(model.edges) ? model.edges.length : 0) === 1 ? '' : 's'} discovered`,
            model.truncatedNodes ? `top ${(Array.isArray(model.nodes) ? model.nodes.length : 0)} screens sampled` : '',
        ].filter(Boolean).join(' | ');

        return `
            <section class="chaos-map-flow-panel chaos-map-flow-panel--launcher" aria-label="Chaos discovery flow launcher">
                <div class="chaos-map-flow-header">
                    <strong>Discovery Flow</strong>
                    <div class="chaos-map-flow-header-actions">
                        <span class="subtle">${escapeHtml(summaryBits || 'No discovery flow data yet.')}</span>
                        <button type="button" data-chaos-flow-open>View Map</button>
                    </div>
                </div>
                <p class="subtle chaos-map-flow-launcher-copy">Open the dedicated Discovery Flow modal for the interactive canvas (pan, drag, zoom) and the full transition list.</p>
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

    const getChaosMapUniqueScreenCount = () => buildChaosMapTemplateGroups(getChaosMapCardAreas()).groups.length;
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
            const stopDrag = () => {
                if (!dragState) {
                    return;
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
        });
    };

    const renderChaosFlowModal = () => {
        if (!flowContent) {
            return;
        }
        const areas = getChaosFlowAreas();
        if (!areas.length) {
            flowContent.innerHTML = '<p class="subtle">No chaos map data yet. Start or load a chaos run.</p>';
            return;
        }
        flowContent.innerHTML = chaosMapFlowMarkup(areas, {
            isModal: true,
            markerId: 'chaos-map-flow-arrow-modal',
        });
        initChaosFlowCanvasInteractions(flowContent);
    };

    const chaosReportFileName = () => (loadedRunID ? `chaos-discovery-report-${loadedRunID}.md` : 'chaos-discovery-report.md');

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
            textarea.setAttribute('aria-label', 'Chaos report raw markdown');
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
            let errText = '';
            try {
                errText = await resp.text();
            } catch (_err) {
                errText = '';
            }
            throw new Error(errText || `HTTP ${resp.status}`);
        }
        return resp.text();
    };

    const refreshChaosReportView = async (options = {}) => {
        if (!reportContent) {
            return '';
        }
        setReportStatus(options.statusMessage || 'Loading chaos report...');
        reportContent.innerHTML = '<p class="subtle">Loading report\u2026</p>';
        try {
            const markdown = await fetchChaosReportMarkdown();
            latestChaosReportMarkdown = String(markdown || '');
            setChaosReportContent(latestChaosReportMarkdown);
            setReportStatus('Chaos discovery report loaded.');
            return latestChaosReportMarkdown;
        } catch (err) {
            const message = (err && err.message) ? err.message : 'Failed to load chaos report.';
            setReportStatus(message, true);
            reportContent.innerHTML = `<p class="subtle">${escapeHtml(message)}</p>`;
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

    const renderChaosMap = () => {
        if (!mapList) {
            return;
        }
        const areas = getChaosMapCardAreas();
        if (!areas.length) {
            if (mapViewBtn) {
                mapViewBtn.disabled = true;
            }
            mapList.innerHTML = '<p class="subtle">No chaos map data yet. Start or load a chaos run.</p>';
            return;
        }
        if (mapViewBtn) {
            mapViewBtn.disabled = false;
        }
        mapList.innerHTML = '';
        const templateGroups = buildChaosMapTemplateGroups(areas).groups;
        templateGroups.forEach((group) => {
            const hash = String(group.representativeHash || '').trim();
            const cardKey = String(group.templateId || hash || 'screen').trim();
            const memberHashes = Array.isArray(group.memberHashes) ? group.memberHashes.slice() : (hash ? [hash] : []);
            const hint = mergeScreenHintsForHashes(memberHashes);
            const mergedKeyAssignments = normalizeKeyAssignments(hint.keyAssignments || {});
            const fieldDiscovery = summarizeChaosGroupFieldDiscovery(group);
            const card = document.createElement('section');
            card.className = 'chaos-map-card';
            card.dataset.chaosMapHash = hash;
            card.dataset.chaosMapTemplate = cardKey;

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
                        return `${escapeHtml(compactLabel.slice(0, 28))} (${count})`;
                    })
                    .join(', ');
                const presses = Number(kp.presses) || 0;
                const progs = Number(kp.progressions) || 0;
                edgeRows.push(`<div class="chaos-map-edge"><strong>${escapeHtml(key)}</strong> | ${presses} press${presses === 1 ? '' : 'es'} | ${progs} progression${progs === 1 ? '' : 's'}${destList ? ` | to ${destList}` : ''}</div>`);
            });
            edgeRows.sort();

            const titleLabel = String(group.label || hash || 'Screen').trim() || 'Screen';
            const hashMeta = group.memberHashes.length > 1
                ? `${group.memberHashes.length} variants merged`
                : (hash || 'unknown-hash');

            card.innerHTML = `
                <div class="chaos-map-card-header">
                    <div class="chaos-map-card-title">
                        <strong>${escapeHtml(titleLabel)}</strong>
                        <span class="chaos-map-card-hash">${escapeHtml(hashMeta)}</span>
                    </div>
                    <span class="chaos-map-chip">${Number(group.visits) || 0} visits</span>
                </div>
                <div class="chaos-map-chip-row">
                    <span class="chaos-map-chip">${Number(group.fieldCount) || 0} fields</span>
                    <span class="chaos-map-chip">${Number(group.inputFieldCount) || 0} inputs</span>
                    <span class="chaos-map-chip">${Number(group.numericFieldCount) || 0} numeric</span>
                    <span class="chaos-map-chip">${Object.keys(keyPresses).length} keys</span>
                    ${group.memberHashes.length > 1 ? `<span class="chaos-map-chip">${group.memberHashes.length} variants</span>` : ''}
                    ${fieldDiscovery.fields > 0 ? `<span class="chaos-map-chip">${fieldDiscovery.fields} discovered fields</span>` : ''}
                    ${fieldDiscovery.triedValues > 0 ? `<span class="chaos-map-chip">${fieldDiscovery.triedValues} tried values</span>` : ''}
                    ${fieldDiscovery.workingValues > 0 ? `<span class="chaos-map-chip">${fieldDiscovery.workingValues} working values</span>` : ''}
                    ${fieldDiscovery.progressions > 0 ? `<span class="chaos-map-chip">${fieldDiscovery.progressions} field progressions</span>` : ''}
                </div>
                <div class="chaos-map-edges">${edgeRows.length ? edgeRows.join('') : '<div class="chaos-map-edge subtle">No key usage yet.</div>'}</div>
                ${chaosMapFieldDiscoveryTableMarkup(group)}
                <div class="chaos-map-hints">
                    <div class="chaos-hint-field">
                        <label class="chaos-hint-field-label" for="chaos-map-data-${cardKey}">Known Data</label>
                        <textarea id="chaos-map-data-${cardKey}" data-chaos-map-known-data placeholder="Known values for this screen (comma or newline separated)">${formatListLines(hint.knownData)}</textarea>
                    </div>
                    <div class="chaos-hint-field">
                        <label class="chaos-hint-field-label" for="chaos-map-keys-${cardKey}">Known Keys</label>
                        <textarea id="chaos-map-keys-${cardKey}" data-chaos-map-known-keys placeholder="Known keys for this screen (e.g. PF3, Enter, Down)">${formatListLines(hint.knownKeys)}</textarea>
                    </div>
                </div>
                <div class="chaos-map-card-actions">
                    <span class="subtle" data-chaos-map-card-status></span>
                    <button type="button" data-chaos-map-save>Save Screen Hints</button>
                </div>
            `;

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
                    if (cardStatus) {
                        cardStatus.textContent = saved ? 'Saved' : 'Save failed';
                    }
                });
            }

            mapList.appendChild(card);
        });
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
        txInput.setAttribute('aria-label', 'Chaos hint transaction');
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
        knownDataInput.setAttribute('aria-label', 'Chaos hint known data');
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
        keyAssignmentsInput.setAttribute('aria-label', 'Chaos hint key assignments');
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

    const loadHints = async () => {
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
            setHintsStatus(chaosHints.length ? `Loaded ${chaosHints.length} hint(s).` : 'No saved hints yet.');
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
                throw new Error('request failed');
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
        } catch (_err) {
            setHintsStatus('Failed to save hints.', true);
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
            chaosHints = mergeHints(draft, extracted);
            renderHints(chaosHints);
            const source = payload.source === 'loaded' ? 'loaded recording' : 'recording';
            if (extracted.length > 0) {
                setHintsStatus(`Extracted ${extracted.length} hint(s) from ${source}. Save hints to persist.`);
            } else {
                setHintsStatus(`No hint candidates found in ${source}.`);
            }
        } catch (err) {
            const msg = (err && err.message) ? err.message : 'Failed to extract hints from recording.';
            setHintsStatus(msg, true);
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

    const refreshChaosMapView = async () => {
        await pollStatus();
        await loadScreenHints();
        renderChaosMap();
    };

    const openChaosFlowFromMap = () => {
        openChaosModal(flowModal, '[data-chaos-flow-refresh], [data-chaos-flow-close]', { keepPrevious: true });
        setFlowStatus('Loading discovery flow...');
        window.requestAnimationFrame(() => {
            if (!flowModal || flowModal.hidden) {
                return;
            }
            renderChaosFlowModal();
            if (flowContent) {
                flowContent.scrollTop = 0;
            }
            const areaCount = getChaosMapUniqueScreenCount();
            setFlowStatus(areaCount ? `Showing discovery flow for ${areaCount} unique screen(s).` : 'No chaos map data yet.');
            focusModalElement(flowModal, '[data-chaos-flow-refresh], [data-chaos-flow-close]');
        });
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
            'Stop recording/playback before starting chaos exploration'
        );
        setButtonDisabledState(
            loadBtn,
            !running && blockedByWorkflow,
            'Stop recording/playback before loading a chaos run'
        );
        setButtonDisabledState(
            loadRecordingBtn,
            !running && blockedByWorkflow,
            'Stop recording/playback before loading a recording into chaos'
        );
        setButtonDisabledState(
            resumeBtn,
            !running && blockedByWorkflow,
            'Stop recording/playback before resuming chaos exploration'
        );
        setButtonDisabledState(
            extendBtn,
            !running && blockedByWorkflow,
            'Stop recording/playback before extending chaos exploration'
        );
        setButtonDisabledState(
            recordingStartButton,
            running,
            'Stop chaos exploration before starting recording'
        );
        setButtonDisabledState(
            workflowLoadButton,
            running,
            'Stop chaos exploration before loading a recording'
        );
    };

    const updateChaosMapFromStatus = (status) => {
        if (status && status.mindMap && typeof status.mindMap === 'object') {
            latestMindMap = status.mindMap;
        } else if (status && !status.active && !(status.loadedRunID) && (Number(status.stepsRun) || 0) === 0) {
            latestMindMap = null;
        }
        if (status && status.screenHints && typeof status.screenHints === 'object') {
            screenHintsByHash = normalizeScreenHintsMap(status.screenHints);
        } else if (status && !status.active && !(status.loadedRunID) && (Number(status.stepsRun) || 0) === 0) {
            screenHintsByHash = {};
        }
        if (mapModal && !mapModal.hidden) {
            renderChaosMap();
        }
    };

    const updateUI = (status) => {
        lastStatus = status || { active: false, stepsRun: 0, transitions: 0 };
        updateChaosMapFromStatus(status || {});
        const running = !!(status && status.active);
        hasData = !!(status && (status.stepsRun > 0 || status.loadedRunID));
        const completed = !running && hasData;
        if (status && status.loadedRunID) {
            loadedRunID = status.loadedRunID;
        }
        if (status && typeof status.firstScreenHash === 'string') {
            latestFirstScreenHash = status.firstScreenHash.trim();
        } else if (running && (Number(status.stepsRun) || 0) === 0) {
            latestFirstScreenHash = '';
        } else if (status && !running && !(status.loadedRunID) && (Number(status.stepsRun) || 0) === 0) {
            latestFirstScreenHash = '';
        }
        const suppressStoredChaosDisplay = !running && isPlaybackActive();
        const visibleLoadedRunID = suppressStoredChaosDisplay ? null : loadedRunID;
        const visibleHasData = suppressStoredChaosDisplay ? false : hasData;
        const visibleCompleted = suppressStoredChaosDisplay ? false : completed;

        if (startBtn) {
            startBtn.hidden = running;
        }
        if (stopBtn) {
            stopBtn.hidden = !running;
        }
        if (resumeBtn) {
            resumeBtn.hidden = running || !visibleLoadedRunID || visibleCompleted;
        }
        if (extendBtn) {
            extendBtn.hidden = running || !visibleCompleted || !visibleLoadedRunID;
        }
        if (exportBtn) {
            exportBtn.hidden = running || !visibleHasData;
        }
        if (reportBtn) {
            reportBtn.hidden = running || !visibleHasData;
        }
        if (removeBtn) {
            removeBtn.hidden = running || !visibleHasData;
        }
        if (indicator) {
            indicator.hidden = !running;
        }
        if (completeIndicator) {
            completeIndicator.hidden = !visibleCompleted;
        }
        if (statsIndicator) {
            const hasStats = !suppressStoredChaosDisplay && !!(status && (status.stepsRun > 0 || status.transitions > 0));
            statsIndicator.hidden = !hasStats;
            if (statsText && hasStats) {
                let txt = visibleCompleted ? `Complete: ${status.stepsRun} attempts` : `${status.stepsRun} attempts`;
                if (status.transitions > 0) {
                    txt += ` | ${status.transitions} transitions`;
                }
                if (status.uniqueScreens > 0) {
                    txt += ` | ${status.uniqueScreens} screens`;
                }
                if (status.uniqueInputs > 0) {
                    txt += ` | ${status.uniqueInputs} inputs`;
                }
                if (status.error) {
                    txt += ' | error';
                }
                if (visibleCompleted && visibleLoadedRunID) {
                    txt += ` | run ${visibleLoadedRunID}`;
                }
                statsText.textContent = txt;
            }
        }
        syncChaosSectionLayout();
        applyChaosInterlocks();
    };

    const pollStatus = async () => {
        clearTimeout(pollTimer);
        try {
            const resp = await fetch('/chaos/status');
            if (!resp.ok) {
                return;
            }
            const status = await resp.json();
            updateUI(status);
            const finalizingCompletedRun = !status.active
                && (Number(status.stepsRun) || 0) > 0
                && !String(status.loadedRunID || '').trim();
            if (status.active || finalizingCompletedRun) {
                pollTimer = setTimeout(pollStatus, 1000);
            }
        } catch (_err) {
            // Network error â€“ stop polling
        }
    };

    // Open/close the runs modal.
    const openRunsModal = async () => {
        if (!runsModal || !runsList) {
            return;
        }
        runsList.innerHTML = '<p class="subtle">Loading\u2026</p>';
        openChaosModal(runsModal, '[data-chaos-runs-close]');
        try {
            const resp = await fetch('/chaos/runs');
            if (!resp.ok) {
                runsList.innerHTML = '<p class="subtle">Failed to load saved runs.</p>';
                return;
            }
            const runs = await resp.json();
            if (!runs || runs.length === 0) {
                runsList.innerHTML = '<p class="subtle">No saved runs found.</p>';
                return;
            }
            const items = runs.map((r) => {
                const date = r.startedAt ? new Date(r.startedAt).toLocaleString() : '';
                const meta = [
                    r.stepsRun > 0 ? `${r.stepsRun} steps` : null,
                    r.transitions > 0 ? `${r.transitions} transitions` : null,
                    r.uniqueScreens > 0 ? `${r.uniqueScreens} screens` : null,
                ].filter(Boolean).join(', ');
                return `<div class="chaos-run-item" data-run-id="${r.id}">
                    <div class="chaos-run-meta">
                        <strong class="chaos-run-id">${r.id}</strong>
                        <span class="subtle">${date}</span>
                    </div>
                    <div class="chaos-run-stats subtle">${meta}</div>
                    <div class="chaos-run-actions">
                        <button type="button" class="chaos-run-load-btn" data-load-run-id="${r.id}">Load</button>
                        <button type="button" class="chaos-run-delete-btn" data-delete-run-id="${r.id}">Delete</button>
                    </div>
                </div>`;
            });
            runsList.innerHTML = items.join('');
            runsList.querySelectorAll('[data-load-run-id]').forEach((btn) => {
                btn.addEventListener('click', async () => {
                    const rid = btn.getAttribute('data-load-run-id');
                    try {
                        const r2 = await fetch('/chaos/load', {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify({ runID: rid }),
                        });
                        if (r2.ok) {
                            const data = await r2.json();
                            loadedRunID = data.runID || rid;
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
                        }
                    } catch (_e) {
                        // Ignore
                    }
                    closeChaosModal(runsModal);
                });
            });
            runsList.querySelectorAll('[data-delete-run-id]').forEach((btn) => {
                btn.addEventListener('click', async () => {
                    const rid = btn.getAttribute('data-delete-run-id');
                    if (!rid) {
                        return;
                    }
                    if (!window.confirm(`Delete saved chaos run ${rid}?`)) {
                        return;
                    }
                    btn.disabled = true;
                    try {
                        const respDelete = await fetch('/chaos/runs/delete', {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify({ runID: rid }),
                        });
                        if (!respDelete.ok) {
                            btn.disabled = false;
                            return;
                        }
                        const row = btn.closest('.chaos-run-item');
                        if (row) {
                            row.remove();
                        }
                        if (loadedRunID === rid) {
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
                        }
                        if (!runsList.querySelector('.chaos-run-item')) {
                            runsList.innerHTML = '<p class="subtle">No saved runs found.</p>';
                        }
                    } catch (_e) {
                        btn.disabled = false;
                    }
                });
            });
            focusModalElement(runsModal, '[data-load-run-id], [data-chaos-runs-close]');
        } catch (_err) {
            runsList.innerHTML = '<p class="subtle">Failed to load saved runs.</p>';
        }
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

    if (hintsOpenBtn) {
        hintsOpenBtn.addEventListener('click', async () => {
            openChaosModal(hintsModal, '[data-chaos-hint-transaction], [data-chaos-hints-add]');
            hintsDirty = false;
            if (hintsList && hintsList.children.length === 0) {
                renderHints(chaosHints);
            }
            renderChaosKeyBlacklist(chaosKeyBlacklist);
            await loadHints();
            focusModalElement(hintsModal, '[data-chaos-hint-transaction], [data-chaos-hints-add]');
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
        mapOpenBtn.addEventListener('click', async () => {
            openChaosModal(mapModal, chaosMapTopFocusSelector);
            setChaosMapStatus('Loading chaos mapâ€¦');
            await loadScreenHints();
            await pollStatus();
            renderChaosMap();
            if (mapList) {
                mapList.scrollTop = 0;
            }
            if (mapModal) {
                const modalPanel = mapModal.querySelector('.modal');
                if (modalPanel) {
                    modalPanel.scrollTop = 0;
                }
                const modalStack = mapModal.querySelector('.stack');
                if (modalStack) {
                    modalStack.scrollTop = 0;
                }
            }
            const areaCount = getChaosMapUniqueScreenCount();
            setChaosMapStatus(areaCount ? `Showing ${areaCount} unique screen(s).` : 'No chaos map data yet.');
            focusModalElement(mapModal, chaosMapTopFocusSelector);
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
                closeChaosModal(hintsModal);
            });
        });
        hintsModal.addEventListener('click', (event) => {
            if (event.target === hintsModal) {
                closeChaosModal(hintsModal);
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
            setChaosMapStatus('Refreshing chaos mapâ€¦');
            await refreshChaosMapView();
            const areaCount = getChaosMapUniqueScreenCount();
            setChaosMapStatus(areaCount ? `Showing ${areaCount} unique screen(s).` : 'No chaos map data yet.');
        });
    }
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
            setFlowStatus('Refreshing discovery flow...');
            await refreshChaosMapView();
            renderChaosFlowModal();
            if (flowContent) {
                flowContent.scrollTop = 0;
            }
            const areaCount = getChaosMapUniqueScreenCount();
            setFlowStatus(areaCount ? `Showing discovery flow for ${areaCount} unique screen(s).` : 'No chaos map data yet.');
            focusModalElement(flowModal, '[data-chaos-flow-refresh], [data-chaos-flow-close]');
        });
    });
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
                await refreshChaosReportView({ statusMessage: 'Refreshing chaos report...' });
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
            setReportStatus(reportRawMarkdownMode ? 'Showing raw markdown for copy/paste.' : 'Showing formatted report view.');
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
                setReportStatus('Report downloaded.');
            } catch (err) {
                setReportStatus((err && err.message) ? err.message : 'Failed to download report.', true);
            } finally {
                setButtonBusy(reportDownloadBtn, false);
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

    const startChaosRun = async () => {
        if (!startBtn) {
            return;
        }
        setButtonBusy(startBtn, true);
        try {
            const cfg = readChaosConfig();
            const resp = await fetch('/chaos/start', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(cfg),
            });
            if (resp.ok) {
                updateUI({ active: true, stepsRun: 0, transitions: 0 });
                pollStatus();
            }
        } catch (_err) {
            // Ignore
        } finally {
            setButtonBusy(startBtn, false);
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
        setReportModalMaximized(window.localStorage.getItem('h3270ChaosReportModalMaximized') === '1');
    } catch (_err) {
        setReportModalMaximized(false);
    }
    setReportRawToggleState(false);
    syncChaosSectionLayout();
    loadHints();
    pollStatus();

    if (startBtn) {
        startBtn.addEventListener('click', async () => {
            if (startBtn.disabled || startBtn.getAttribute('aria-busy') === 'true') {
                return;
            }
            const proceed = await confirmLoggingBeforeChaosStart();
            if (!proceed) {
                applyChaosInterlocks();
                return;
            }
            await startChaosRun();
        });
    }

    if (stopBtn) {
        stopBtn.addEventListener('click', async () => {
            setButtonBusy(stopBtn, true);
            try {
                await fetch('/chaos/stop', { method: 'POST' });
                await pollStatus();
            } catch (_err) {
                // Ignore
            } finally {
                setButtonBusy(stopBtn, false);
                applyChaosInterlocks();
            }
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
                if (resp.ok) {
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
                }
            } catch (_err) {
                // Ignore
            } finally {
                setButtonBusy(loadRecordingBtn, false);
                applyChaosInterlocks();
            }
        });
    }

    if (resumeBtn) {
        resumeBtn.addEventListener('click', async () => {
            if (!loadedRunID) {
                return;
            }
            if (resumeBtn.disabled || resumeBtn.getAttribute('aria-busy') === 'true') {
                return;
            }
            const proceed = await confirmLoggingBeforeChaosStart();
            if (!proceed) {
                applyChaosInterlocks();
                return;
            }
            setButtonBusy(resumeBtn, true);
            try {
                const cfg = readChaosConfig();
                const resp = await fetch('/chaos/resume', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(cfg),
                });
                if (resp.ok) {
                    updateUI({ active: true, stepsRun: 0, transitions: 0, loadedRunID });
                    pollStatus();
                }
            } catch (_err) {
                // Ignore
            } finally {
                setButtonBusy(resumeBtn, false);
                applyChaosInterlocks();
            }
        });
    }

    if (extendBtn) {
        extendBtn.addEventListener('click', async () => {
            if (!loadedRunID) {
                return;
            }
            if (extendBtn.disabled || extendBtn.getAttribute('aria-busy') === 'true') {
                return;
            }
            const proceed = await confirmLoggingBeforeChaosStart();
            if (!proceed) {
                applyChaosInterlocks();
                return;
            }
            setButtonBusy(extendBtn, true);
            try {
                const cfg = readChaosConfig();
                cfg.extendLimits = true;
                const resp = await fetch('/chaos/resume', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(cfg),
                });
                if (resp.ok) {
                    updateUI({ active: true, stepsRun: 0, transitions: 0, loadedRunID });
                    pollStatus();
                }
            } catch (_err) {
                // Ignore
            } finally {
                setButtonBusy(extendBtn, false);
                applyChaosInterlocks();
            }
        });
    }

    if (exportBtn) {
        exportBtn.addEventListener('click', async () => {
            setButtonBusy(exportBtn, true);
            try {
                const resp = await fetch('/chaos/export', { method: 'POST' });
                if (resp.ok) {
                    const data = await resp.text();
                    const blob = new Blob([data], { type: 'application/json' });
                    const url = URL.createObjectURL(blob);
                    const a = document.createElement('a');
                    a.href = url;
                    a.download = loadedRunID ? `chaos-workflow-${loadedRunID}.json` : 'chaos-workflow.json';
                    document.body.appendChild(a);
                    a.click();
                    document.body.removeChild(a);
                    URL.revokeObjectURL(url);
                }
            } catch (_err) {
                // Ignore
            } finally {
                setButtonBusy(exportBtn, false);
                applyChaosInterlocks();
            }
        });
    }

    if (reportBtn) {
        reportBtn.addEventListener('click', async () => {
            setButtonBusy(reportBtn, true);
            try {
                await openChaosReportModal();
            } catch (_err) {
                // Ignore
            } finally {
                setButtonBusy(reportBtn, false);
                applyChaosInterlocks();
            }
        });
    }

    if (removeBtn) {
        removeBtn.addEventListener('click', async () => {
            setButtonBusy(removeBtn, true);
            try {
                const resp = await fetch('/chaos/remove', { method: 'POST' });
                if (resp.ok) {
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
                }
            } catch (_err) {
                // Ignore
            } finally {
                setButtonBusy(removeBtn, false);
                applyChaosInterlocks();
            }
        });
    }

    if (body && typeof MutationObserver !== 'undefined') {
        const obs = new MutationObserver(() => {
            applyChaosInterlocks();
        });
        obs.observe(body, { attributes: true, attributeFilter: ['data-playback-active'] });
        if (recordingIndicator) {
            obs.observe(recordingIndicator, { attributes: true, attributeFilter: ['hidden'] });
        }
    }
})();
