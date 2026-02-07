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
    const form = modal.querySelector('[data-settings-form]');
    const groupsContainer = modal.querySelector('[data-settings-groups]');
    const status = modal.querySelector('[data-settings-status]');

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
        'bracket',
        'cp037',
        'cp273',
        'cp277',
        'cp278',
        'cp280',
        'cp284',
        'cp285',
        'cp297',
        'cp500',
        'cp870',
        'cp871',
        'cp875',
        'cp1026',
        'cp1047',
        'cp1140',
        'cp1141',
        'cp1142',
        'cp1143',
        'cp1144',
        'cp1145',
        'cp1146',
        'cp1147',
        'cp1148',
        'cp1149',
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
                    helperLink: 'https://x3270.miraheze.org/wiki/Code_page'
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
    ];

    const fieldMap = new Map();

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
        groupsContainer.innerHTML = '';
        groups.forEach((group) => {
            const fieldset = document.createElement('fieldset');
            fieldset.className = 'settings-group';

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
                    if (field.allowEmpty) {
                        const optionEl = document.createElement('option');
                        optionEl.value = '';
                        optionEl.textContent = 'Default';
                        input.appendChild(optionEl);
                    }
                    field.options.forEach((option) => {
                        const optionEl = document.createElement('option');
                        optionEl.value = option;
                        optionEl.textContent = option;
                        input.appendChild(optionEl);
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
                }

                const helper = buildHelper(field);
                if (helper) {
                    wrapper.appendChild(helper);
                }

                const error = document.createElement('div');
                error.className = 'settings-error';
                wrapper.appendChild(error);

                fieldMap.set(field.key, { input, error });
                fieldsWrap.appendChild(wrapper);
            });

            fieldset.appendChild(header);
            fieldset.appendChild(description);
            fieldset.appendChild(fieldsWrap);
            groupsContainer.appendChild(fieldset);
        });
    };

    const populateSettings = (settings) => {
        fieldMap.forEach((entry, key) => {
            entry.error.textContent = '';
            if (Object.prototype.hasOwnProperty.call(settings, key)) {
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
        } catch (error) {
            setStatus(error.message || 'Failed to save settings.', true);
        }
    };

    buildGroups();

    if (openButton) {
        openButton.addEventListener('click', () => {
            modal.hidden = false;
            loadSettings();
        });
    }

    closeButtons.forEach((button) => {
        button.addEventListener('click', () => {
            modal.hidden = true;
            setStatus('');
        });
    });

    if (refreshButton) {
        refreshButton.addEventListener('click', () => {
            loadSettings();
        });
    }

    if (form) {
        form.addEventListener('submit', (event) => {
            event.preventDefault();
            saveSettings();
        });
    }
})();
