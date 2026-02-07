(function () {
  "use strict";

  var themes = [
    { id: "theme-yorkshire", name: "Yorkshire Mainframe Terminal" },
    { id: "theme-authentic", name: "Authentic 3270" },
    { id: "theme-classic", name: "Classic 3270" },
    { id: "theme-dark", name: "Dark Mode" },
    { id: "theme-light", name: "Light Mode" },
    { id: "theme-modern", name: "Super Modern 3270" },
    { id: "theme-minimal", name: "Minimal 3270" },
    { id: "theme-slick", name: "Slick 3270" },
    { id: "theme-not3270", name: "Not 3270" }
  ];
  var authenticThemeId = "theme-authentic";
  var authenticChromeKey = "3270Web.authenticChromeHidden";

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
    var body = document.body;
    themes.forEach(function (theme) {
      body.classList.remove(theme.id);
    });
    body.classList.add(themeId);
    localStorage.setItem("3270Web.theme", themeId);
    applyAuthenticChromeState(themeId === authenticThemeId);
    if (
      window.ThreeSeventyWeb &&
      typeof window.ThreeSeventyWeb.updateBackgroundTheme === "function"
    ) {
      window.ThreeSeventyWeb.updateBackgroundTheme(themeId);
    }
    document.dispatchEvent(new CustomEvent("themechange", { detail: themeId }));
  }

  function getStoredTheme() {
    return localStorage.getItem("3270Web.theme") || "theme-yorkshire";
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
