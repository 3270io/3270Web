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
        CHAOS_OUTPUT_FILE: '',
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
            id: 'connectivity',
            title: 'Connectivity',
            description: 'Network address and connection options.',
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
            fields: [
                { key: 'S3270_TRACE', label: 'Trace', type: 'checkbox', helper: 'Turn on data stream and action tracing.' },
                { key: 'S3270_TRACE_FILE', label: 'Trace file', type: 'text', helper: 'File for data stream and action tracing.' },
                { key: 'S3270_TRACE_FILE_SIZE', label: 'Trace file size', type: 'text', helper: 'Limit trace file size in bytes.' },
                { key: 'S3270_HELP', label: 'Show help', type: 'checkbox', helper: 'Display command-line help and exit.' },
                { key: 'S3270_VERSION', label: 'Show version', type: 'checkbox', helper: 'Display version information and exit.' },
                { key: 'S3270_UTENV', label: 'UT env', type: 'checkbox', helper: 'Allow unit-test-specific env vars.' },
            ],
        },
        {
            id: 'app',
            title: 'App',
            description: 'Application-level behaviors.',
            fields: [
                { key: 'ALLOW_LOG_ACCESS', label: 'Allow log access', type: 'checkbox', helper: 'Enable viewing log output in the UI.' },
                { key: 'APP_USE_KEYPAD', label: 'Use keypad', type: 'checkbox', helper: 'Show the virtual keypad by default.' },
            ],
        },
        {
            id: 'chaos',
            title: 'Chaos Explorer',
            description: 'Configuration for chaos exploration runs. Values are used when starting a chaos run from the toolbar.',
            fields: [
                { key: 'CHAOS_MAX_STEPS', label: 'Max steps', type: 'text', helper: 'Maximum number of AID key submissions before stopping (0 = unlimited).' },
                { key: 'CHAOS_TIME_BUDGET_SEC', label: 'Time budget (seconds)', type: 'text', helper: 'Maximum wall-clock seconds before stopping (0 = unlimited).' },
                { key: 'CHAOS_STEP_DELAY_SEC', label: 'Step delay (seconds)', type: 'text', helper: 'Pause between submissions in seconds (e.g. 0.5).' },
                { key: 'CHAOS_SEED', label: 'Seed', type: 'text', helper: 'Random seed (0 = use current time).' },
                { key: 'CHAOS_MAX_FIELD_LENGTH', label: 'Max field length', type: 'text', helper: 'Maximum characters generated per input field.' },
                { key: 'CHAOS_OUTPUT_FILE', label: 'Output file', type: 'text', helper: 'Path to save the learned workflow JSON on stop (leave empty to skip).' },
                { key: 'CHAOS_EXCLUDE_NO_PROGRESS_EVENTS', label: 'Exclude no-progress events', type: 'checkbox', helper: 'Exclude attempts with no screen transition from chaos event history and attempt detail views.' },
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

    const setMaximizeUi = (enabled) => {
        maximized = enabled;
        modal.classList.toggle('is-maximized', enabled);
        if (!maximizeButton) {
            return;
        }
        maximizeButton.textContent = enabled ? 'Restore' : 'Maximize';
        maximizeButton.setAttribute('aria-pressed', enabled ? 'true' : 'false');
        maximizeButton.setAttribute('title', enabled ? 'Restore modal size' : 'Maximize modal');
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
        if (typeof loadSettings === 'function') {
            loadSettings();
        }
    };

    const closeSettingsModal = () => {
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

    const buildGroups = () => {
        if (!groupsContainer || !tabsContainer) {
            return;
        }
        const themeSlot = form ? form.querySelector('[data-settings-theme-slot]') : null;
        const builtGroupIds = [];
        const previousActive = activeGroupId;
        tabsContainer.innerHTML = '';
        groupsContainer.innerHTML = '';

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
                fieldsWrap.appendChild(wrapper);
            });

            fieldset.appendChild(header);
            fieldset.appendChild(description);
            fieldset.appendChild(fieldsWrap);
            groupsContainer.appendChild(fieldset);
            builtGroupIds.push(group.id);
        });

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
        } catch (error) {
            setStatus(error.message || 'Failed to load settings.', true);
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

    restartCancelButtons.forEach((button) => {
        button.addEventListener('click', () => closeRestartConfirm(false));
    });

    if (restartConfirmButton) {
        restartConfirmButton.addEventListener('click', () => closeRestartConfirm(true));
    }

    document.addEventListener('keydown', (event) => {
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
    const removeBtn = document.querySelector('[data-chaos-remove]');
    const loadBtn = document.querySelector('[data-chaos-load]');
    const loadRecordingBtn = document.querySelector('[data-chaos-load-recording]');
    const resumeBtn = document.querySelector('[data-chaos-resume]');
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
    const hintsList = document.querySelector('[data-chaos-hints-list]');
    const hintsAddBtn = document.querySelector('[data-chaos-hints-add]');
    const hintsLoadRecordingBtn = document.querySelector('[data-chaos-hints-load-recording]');
    const hintsRecordingInput = document.querySelector('[data-chaos-hints-recording-input]');
    const hintsReloadBtn = document.querySelector('[data-chaos-hints-reload]');
    const hintsSaveBtn = document.querySelector('[data-chaos-hints-save]');
    const hintsStatus = document.querySelector('[data-chaos-hints-status]');
    const chaosSections = chaosControls ? Array.from(chaosControls.querySelectorAll('[data-chaos-section]')) : [];
    const chaosDividers = chaosControls ? Array.from(chaosControls.querySelectorAll('[data-chaos-divider]')) : [];
    const recordingIndicator = document.querySelector('[data-recording-indicator]');
    const recordingStartButton = document.querySelector('form[data-recording-start] .icon-button');
    const workflowLoadButton = document.querySelector('[data-workflow-trigger]');

    if (!startBtn && !stopBtn && !exportBtn && !removeBtn) {
        return;
    }

    let pollTimer = null;
    let hasData = false;
    let loadedRunID = null;
    let lastStatus = { active: false, stepsRun: 0, transitions: 0 };
    let chaosHints = [];
    let hintsDirty = false;
    let hintRowSequence = 0;
    let activeChaosModal = null;
    let previousChaosFocus = null;

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

    const closeChaosModal = (modal, options = {}) => {
        if (!modal) {
            return;
        }
        const restoreFocus = options.restoreFocus !== false;
        modal.hidden = true;
        if (activeChaosModal === modal) {
            activeChaosModal = null;
            if (restoreFocus && previousChaosFocus && typeof previousChaosFocus.focus === 'function') {
                previousChaosFocus.focus();
            }
            previousChaosFocus = null;
        }
    };

    const openChaosModal = (modal, preferredSelector = '') => {
        if (!modal) {
            return;
        }
        if (activeChaosModal && activeChaosModal !== modal) {
            closeChaosModal(activeChaosModal, { restoreFocus: false });
        }
        const activeElement = document.activeElement;
        previousChaosFocus = activeElement instanceof HTMLElement ? activeElement : null;
        modal.hidden = false;
        activeChaosModal = modal;
        if (activeElement && typeof activeElement.blur === 'function') {
            activeElement.blur();
        }
        window.requestAnimationFrame(() => focusModalElement(modal, preferredSelector));
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

    const markHintsDirty = () => {
        if (hintsModal && !hintsModal.hidden) {
            hintsDirty = true;
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
            if (!transaction && knownData.length === 0) {
                return;
            }
            normalized.push({ transaction, knownData });
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
            const key = `${tx.toUpperCase()}|${knownData.join('\u001f')}`;
            if (seen.has(key)) {
                return;
            }
            seen.add(key);
            out.push({ transaction: tx, knownData });
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
            return {
                transaction: tx ? tx.value : '',
                knownData: knownData ? parseKnownData(knownData.value) : [],
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
            chaosHints = loadedHints;
            if (hintsModal && !hintsModal.hidden && hintsDirty) {
                setHintsStatus('Loaded saved hints; unsaved edits were kept.');
                return chaosHints;
            }
            renderHints(chaosHints);
            setHintsStatus(chaosHints.length ? `Loaded ${chaosHints.length} hint(s).` : 'No saved hints yet.');
            return chaosHints;
        } catch (_err) {
            setHintsStatus('Failed to load hints.', true);
            return chaosHints;
        }
    };

    const saveHints = async () => {
        const draftHints = collectHintsFromUI();
        try {
            const resp = await fetch('/chaos/hints', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ hints: draftHints }),
            });
            if (!resp.ok) {
                throw new Error('request failed');
            }
            const payload = await resp.json();
            chaosHints = normalizeHints(payload.hints || []);
            renderHints(chaosHints);
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

    const updateUI = (status) => {
        lastStatus = status || { active: false, stepsRun: 0, transitions: 0 };
        const running = !!(status && status.active);
        hasData = !!(status && (status.stepsRun > 0 || status.loadedRunID));
        const completed = !running && hasData;
        if (status && status.loadedRunID) {
            loadedRunID = status.loadedRunID;
        }

        if (startBtn) {
            startBtn.hidden = running;
        }
        if (stopBtn) {
            stopBtn.hidden = !running;
        }
        if (resumeBtn) {
            resumeBtn.hidden = running || !loadedRunID;
        }
        if (exportBtn) {
            exportBtn.hidden = running || !hasData;
        }
        if (removeBtn) {
            removeBtn.hidden = running || !hasData;
        }
        if (indicator) {
            indicator.hidden = !running;
        }
        if (completeIndicator) {
            completeIndicator.hidden = !completed;
        }
        if (statsIndicator) {
            const hasStats = !!(status && (status.stepsRun > 0 || status.transitions > 0));
            statsIndicator.hidden = !hasStats;
            if (statsText && hasStats) {
                let txt = completed ? `Complete: ${status.stepsRun} attempts` : `${status.stepsRun} attempts`;
                if (status.transitions > 0) {
                    txt += ` · ${status.transitions} transitions`;
                }
                if (status.uniqueScreens > 0) {
                    txt += ` · ${status.uniqueScreens} screens`;
                }
                if (status.uniqueInputs > 0) {
                    txt += ` · ${status.uniqueInputs} inputs`;
                }
                if (status.error) {
                    txt += ' · error';
                }
                if (completed && loadedRunID) {
                    txt += ` · run ${loadedRunID}`;
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
            if (status.active) {
                pollTimer = setTimeout(pollStatus, 1000);
            }
        } catch (_err) {
            // Network error – stop polling
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
                    <button type="button" class="chaos-run-load-btn" data-load-run-id="${r.id}">Load</button>
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
                            });
                        }
                    } catch (_e) {
                        // Ignore
                    }
                    closeChaosModal(runsModal);
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
            await loadHints();
            focusModalElement(hintsModal, '[data-chaos-hint-transaction], [data-chaos-hints-add]');
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

        const outputFile = getVal('CHAOS_OUTPUT_FILE');
        if (outputFile) {
            cfg.outputFile = outputFile;
        }

        cfg.excludeNoProgressEvents = getBool('CHAOS_EXCLUDE_NO_PROGRESS_EVENTS', true);

        const draftHints = (hintsModal && !hintsModal.hidden) ? collectHintsFromUI() : chaosHints;
        if (Array.isArray(draftHints) && draftHints.length > 0) {
            cfg.hints = draftHints;
        }

        return cfg;
    };

    // Initial status check on page load.
    syncChaosSectionLayout();
    loadHints();
    pollStatus();

    if (startBtn) {
        startBtn.addEventListener('click', async () => {
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
                    });
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
