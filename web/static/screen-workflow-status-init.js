(function () {
    try {
        var root = document.documentElement;
        // The Workflow status widget always starts minimised when the
        // connected screen loads. Reflect that before first paint so the
        // expanded widget never flashes in while scripts initialise.
        root.classList.add('workflow-status-minimized');
        root.classList.remove('workflow-status-maximized');
    } catch (err) {
        // ignore
    }
})();
