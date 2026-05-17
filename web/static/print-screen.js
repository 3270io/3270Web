(function () {
    "use strict";

    function openPrintWindow(html) {
        const win = window.open("", "_blank", "noopener,noreferrer");
        if (!win) {
            window.alert("Unable to open print window. Allow pop-ups for this site and try again.");
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
        btn.disabled = true;
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
            window.alert("Failed to print screen: " + err.message);
        } finally {
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
