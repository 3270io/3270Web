(function () {
    function init() {
        var marker = document.querySelector("[data-initial-focus]");
        if (!marker) {
            return;
        }
        var formName = marker.getAttribute("data-form-name");
        if (!formName) {
            return;
        }
        if (typeof window.installKeyHandler === "function") {
            window.installKeyHandler(formName);
        }
        var form = document.forms[formName];
        if (!form) {
            return;
        }
        if (marker.hasAttribute("data-unformatted")) {
            if (form.field && typeof form.field.focus === "function") {
                form.field.focus();
            }
            return;
        }
        var fieldName = marker.getAttribute("data-field-name");
        if (!fieldName) {
            return;
        }
        var el = form.elements[fieldName];
        if (!el) {
            return;
        }
        el.focus();
        if (typeof el.setSelectionRange === "function") {
            var pos = parseInt(marker.getAttribute("data-caret"), 10);
            if (isNaN(pos) || pos < 0) {
                pos = 0;
            }
            if (pos > el.value.length) {
                pos = el.value.length;
            }
            el.setSelectionRange(pos, pos);
        }
    }

    if (document.readyState === "loading") {
        document.addEventListener("DOMContentLoaded", init);
    } else {
        init();
    }
})();
