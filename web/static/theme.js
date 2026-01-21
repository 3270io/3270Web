(function () {
  "use strict";

  var themes = [
    { id: "theme-classic", name: "Classic 3270" },
    { id: "theme-dark", name: "Dark Mode" },
    { id: "theme-light", name: "Light Mode" },
    { id: "theme-modern", name: "Super Modern 3270" },
    { id: "theme-minimal", name: "Minimal 3270" },
    { id: "theme-slick", name: "Slick 3270" },
    { id: "theme-not3270", name: "Not 3270" }
  ];

  function applyTheme(themeId) {
    var body = document.body;
    themes.forEach(function (theme) {
      body.classList.remove(theme.id);
    });
    body.classList.add(themeId);
    localStorage.setItem("3270Web.theme", themeId);
    if (
      window.ThreeSeventyWeb &&
      typeof window.ThreeSeventyWeb.updateBackgroundTheme === "function"
    ) {
      window.ThreeSeventyWeb.updateBackgroundTheme(themeId);
    }
    document.dispatchEvent(new CustomEvent("themechange", { detail: themeId }));
  }

  function getStoredTheme() {
    return localStorage.getItem("3270Web.theme") || "theme-classic";
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
  }

  document.addEventListener("DOMContentLoaded", initThemeSelector);
})();
