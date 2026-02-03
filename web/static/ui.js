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
