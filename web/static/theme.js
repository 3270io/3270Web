(function () {
  "use strict";

  var themes = [
    { id: "theme-yorkshire", name: "Yorkshire Mainframe Terminal" },
    { id: "theme-authentic", name: "Authentic 3270" },
    { id: "theme-classic", name: "Amber Phosphor" },
    { id: "theme-dark", name: "Midnight Cyan" },
    { id: "theme-light", name: "Paper Terminal" },
    { id: "theme-modern", name: "Neon Grid" },
    { id: "theme-slick", name: "Ocean Ops" }
  ];
  var themeAliases = {
    "theme-minimal": "theme-dark",
    "theme-not3270": "theme-light"
  };
  var authenticThemeId = "theme-authentic";
  var authenticChromeKey = "3270Web.authenticChromeHidden";

  function hasTheme(themeId) {
    for (var i = 0; i < themes.length; i++) {
      if (themes[i].id === themeId) {
        return true;
      }
    }
    return false;
  }

  function normalizeThemeId(themeId) {
    var candidate = themeAliases[themeId] || themeId;
    if (hasTheme(candidate)) {
      return candidate;
    }
    return "theme-yorkshire";
  }

  function applyAuthenticChromeState(isAuthentic) {
    var body = document.body;
    var control = document.getElementById("authentic-controls");
    var toggle = document.getElementById("authentic-chrome-toggle");
    if (!control || !toggle) {
      body.classList.remove("authentic-chrome-hidden");
      return;
    }
    if (isAuthentic) {
      control.hidden = false;
      var stored = localStorage.getItem(authenticChromeKey) === "1";
      toggle.checked = stored;
      body.classList.toggle("authentic-chrome-hidden", stored);
    } else {
      control.hidden = true;
      body.classList.remove("authentic-chrome-hidden");
    }
  }

  function applyTheme(themeId) {
    var normalized = normalizeThemeId(themeId);
    var body = document.body;
    themes.forEach(function (theme) {
      body.classList.remove(theme.id);
    });
    body.classList.add(normalized);
    localStorage.setItem("3270Web.theme", normalized);
    applyAuthenticChromeState(normalized === authenticThemeId);
    if (
      window.ThreeSeventyWeb &&
      typeof window.ThreeSeventyWeb.updateBackgroundTheme === "function"
    ) {
      window.ThreeSeventyWeb.updateBackgroundTheme(normalized);
    }
    document.dispatchEvent(new CustomEvent("themechange", { detail: normalized }));
  }

  function getStoredTheme() {
    return normalizeThemeId(localStorage.getItem("3270Web.theme") || "theme-yorkshire");
  }

  function initThemeSelector() {
    var select = document.getElementById("theme-select");
    if (!select) {
      applyTheme(getStoredTheme());
      return;
    }

    select.innerHTML = "";
    themes.forEach(function (theme) {
      var opt = document.createElement("option");
      opt.value = theme.id;
      opt.textContent = theme.name;
      select.appendChild(opt);
    });

    var current = getStoredTheme();
    select.value = current;
    applyTheme(current);

    select.addEventListener("change", function () {
      applyTheme(select.value);
    });

    var chromeToggle = document.getElementById("authentic-chrome-toggle");
    if (chromeToggle) {
      chromeToggle.addEventListener("change", function () {
        var hidden = chromeToggle.checked;
        document.body.classList.toggle("authentic-chrome-hidden", hidden);
        localStorage.setItem(authenticChromeKey, hidden ? "1" : "0");
      });
    }
  }

  document.addEventListener("DOMContentLoaded", initThemeSelector);
})();
