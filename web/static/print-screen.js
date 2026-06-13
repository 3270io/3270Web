(function () {
    "use strict";

    function notify(message, type) {
        if (window.ThreeSeventyWeb && typeof window.ThreeSeventyWeb.notify === "function") {
            window.ThreeSeventyWeb.notify(message, type || "error");
        } else {
            // Fallback for the unlikely case the toast module failed to load.
            window.alert(message);
        }
    }

    function openPrintWindow(html) {
        // NOTE: we deliberately do not pass "noopener" here. With noopener the
        // browser returns null instead of a window handle, which would make it
        // impossible to write the print document and would trigger the
        // pop-up-blocked alert even when pop-ups are allowed. The target is a
        // same-origin about:blank window we fully control, so noopener adds no
        // security benefit.
        const win = window.open("", "_blank");
        if (!win) {
            notify("Unable to open print window. Allow pop-ups for this site and try again.", "warning");
            return;
        }
        win.document.open();
        win.document.write(html);
        win.document.close();
        const triggerPrint = function () {
            try {
                win.focus();
                win.print();
            } catch (err) {
                // Some browsers throw when print() is called too early on a cross-document
                // window. The user can still print manually via Ctrl+P.
                console.warn("print-screen: window.print() failed", err);
            }
        };
        if (win.document.readyState === "complete") {
            triggerPrint();
        } else {
            win.addEventListener("load", triggerPrint, { once: true });
        }
    }

    async function handlePrintClick(event) {
        event.preventDefault();
        const btn = event.currentTarget;
        if (!btn || btn.disabled) {
            return;
        }
        // Show a loading state while the network request is in flight, matching
        // the icon-button spinner treatment used by the other async toolbar
        // actions. The button is icon-only, so swap the icon for a centered
        // spinner and mark it aria-busy.
        const originalHtml = btn.innerHTML;
        const originalLabel = btn.getAttribute("aria-label");
        btn.disabled = true;
        btn.setAttribute("aria-busy", "true");
        btn.setAttribute("aria-label", "Printing...");
        btn.innerHTML = '<span class="spinner" aria-hidden="true" style="margin-right: 0"></span>';
        try {
            const resp = await fetch("/screen/print?format=html", {
                credentials: "same-origin",
                headers: { Accept: "application/json" },
            });
            if (!resp.ok) {
                let detail = "";
                try {
                    const body = await resp.json();
                    detail = body && body.error ? ": " + body.error : "";
                } catch (_) { /* ignore */ }
                throw new Error("HTTP " + resp.status + detail);
            }
            const data = await resp.json();
            const content = (data && typeof data.content === "string") ? data.content : "";
            if (!content) {
                throw new Error("empty print payload");
            }
            openPrintWindow(content);
        } catch (err) {
            console.error("print-screen: request failed", err);
            notify("Failed to print screen: " + err.message, "error");
        } finally {
            btn.innerHTML = originalHtml;
            btn.removeAttribute("aria-busy");
            if (originalLabel) {
                btn.setAttribute("aria-label", originalLabel);
            } else {
                btn.removeAttribute("aria-label");
            }
            btn.disabled = false;
        }
    }

    function init() {
        const buttons = document.querySelectorAll("[data-print-screen]");
        buttons.forEach(function (btn) {
            btn.addEventListener("click", handlePrintClick);
        });
    }

    if (document.readyState === "loading") {
        document.addEventListener("DOMContentLoaded", init);
    } else {
        init();
    }
})();
