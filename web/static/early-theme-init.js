(function () {
    try {
        var themeAliases = { "theme-minimal": "theme-dark", "theme-not3270": "theme-light", "theme-custom": "theme-yorkshire" };
        var builtInThemes = {
            "theme-yorkshire": true,
            "theme-authentic": true,
            "theme-classic": true,
            "theme-dark": true,
            "theme-light": true,
            "theme-modern": true,
            "theme-slick": true
        };
        var themeId = localStorage.getItem("3270Web.theme") || localStorage.getItem("3270Web.defaultTheme") || "theme-yorkshire";
        themeId = themeAliases[themeId] || themeId;
        if (themeId.indexOf("theme-file:") === 0) {
            document.body.classList.add("theme-custom");
        } else {
            document.body.classList.add(builtInThemes[themeId] ? themeId : "theme-yorkshire");
        }
    } catch (err) {
        document.body.classList.add("theme-yorkshire");
    }
})();
