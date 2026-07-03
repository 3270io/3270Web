(function () {
    try {
        var toggle = document.querySelector('[data-status-tracking-toggle]');
        if (!toggle) {
            return;
        }
        var stored = localStorage.getItem('workflowStatusTrackingEnabled');
        toggle.checked = stored === null ? true : stored === '1';
    } catch (err) {
        // ignore
    }
})();
