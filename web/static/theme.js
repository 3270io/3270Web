(function () {
  "use strict";

  var builtInThemes = [
    { id: "theme-yorkshire", name: "Yorkshire Mainframe Terminal" },
    { id: "theme-authentic", name: "Authentic 3270" },
    { id: "theme-classic", name: "Amber Phosphor" },
    { id: "theme-dark", name: "Midnight Cyan" },
    { id: "theme-light", name: "Paper Terminal" },
    { id: "theme-modern", name: "Neon Grid" },
    { id: "theme-slick", name: "Ocean Ops" },
    { id: "theme-custom", name: "Custom Theme (Editor)" }
  ];

  var fileThemes = [];

  var themeAliases = {
    "theme-minimal": "theme-dark",
    "theme-not3270": "theme-light"
  };

  var authenticThemeId = "theme-authentic";
  var authenticChromeKey = "3270Web.authenticChromeHidden";
  var customThemeStorageKey = "3270Web.customThemeV1";
  var themeStorageKey = "3270Web.theme";
  var defaultThemeStorageKey = "3270Web.defaultTheme";

  var customFields = [
    { key: "bg", label: "Background", cssVar: "--bg" },
    { key: "panel", label: "Panel", cssVar: "--panel" },
    { key: "panel2", label: "Panel 2", cssVar: "--panel-2" },
    { key: "border", label: "Border", cssVar: "--border" },
    { key: "fg", label: "Text", cssVar: "--fg" },
    { key: "fgMuted", label: "Muted Text", cssVar: "--fg-muted" },
    { key: "accent", label: "Accent", cssVar: "--accent" },
    { key: "accent2", label: "Accent 2", cssVar: "--accent-2" }
  ];

  var defaultCustomTheme = {
    bg: "#0b1a0b",
    panel: "#0f260f",
    panel2: "#123012",
    border: "#1f3b1f",
    fg: "#39ff14",
    fgMuted: "#8bdc79",
    accent: "#39ff14",
    accent2: "#7cff3b"
  };

  var palettes = [
    {
      id: "green-core",
      name: "Green Core",
      values: {
        bg: "#0b1a0b",
        panel: "#0f260f",
        panel2: "#123012",
        border: "#1f3b1f",
        fg: "#39ff14",
        fgMuted: "#8bdc79",
        accent: "#39ff14",
        accent2: "#7cff3b"
      }
    },
    {
      id: "amber",
      name: "Amber",
      values: {
        bg: "#1a1407",
        panel: "#231b0d",
        panel2: "#2e2412",
        border: "#4b3a1c",
        fg: "#ffd36b",
        fgMuted: "#c6a45f",
        accent: "#ffbe3b",
        accent2: "#fff3bf"
      }
    },
    {
      id: "ice",
      name: "Ice",
      values: {
        bg: "#050b14",
        panel: "#0b1222",
        panel2: "#121c33",
        border: "#233251",
        fg: "#b7f5ff",
        fgMuted: "#7ea2c6",
        accent: "#43d9ff",
        accent2: "#78ffe0"
      }
    },
    {
      id: "paper",
      name: "Paper",
      values: {
        bg: "#f7f2e7",
        panel: "#fffaf0",
        panel2: "#f3ead6",
        border: "#d8c8a9",
        fg: "#2f2415",
        fgMuted: "#72604a",
        accent: "#0f6d8f",
        accent2: "#b6621f"
      }
    },
    {
      id: "violet-grid",
      name: "Violet Grid",
      values: {
        bg: "#12071e",
        panel: "#1b0f2d",
        panel2: "#25163d",
        border: "#3a2562",
        fg: "#ffb8f9",
        fgMuted: "#c594c4",
        accent: "#4bffcf",
        accent2: "#ffd166"
      }
    }
  ];

  function getAllThemes() {
    return builtInThemes.concat(fileThemes);
  }

  function hasTheme(themeId) {
    var all = getAllThemes();
    for (var i = 0; i < all.length; i++) {
      if (all[i].id === themeId) {
        return true;
      }
    }
    return false;
  }

  function findFileTheme(themeId) {
    for (var i = 0; i < fileThemes.length; i++) {
      if (fileThemes[i].id === themeId) {
        return fileThemes[i];
      }
    }
    return null;
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

  function isValidCssColor(value) {
    if (typeof value !== "string") {
      return false;
    }
    var trimmed = value.trim();
    if (!trimmed) {
      return false;
    }
    var probe = document.createElement("span");
    probe.style.color = "";
    probe.style.color = trimmed;
    return probe.style.color !== "";
  }

  function normalizeCssColor(value) {
    if (!isValidCssColor(value)) {
      return "";
    }
    var probe = document.createElement("span");
    probe.style.color = "";
    probe.style.color = value.trim();
    return probe.style.color;
  }

  function colorToHex(value) {
    var normalized = normalizeCssColor(value);
    if (!normalized) {
      return "";
    }
    var canvas = document.createElement("canvas");
    canvas.width = 1;
    canvas.height = 1;
    var ctx = canvas.getContext("2d");
    if (!ctx) {
      return "";
    }
    ctx.fillStyle = "#000000";
    ctx.fillStyle = normalized;
    return String(ctx.fillStyle || "");
  }

  function normalizeCustomTheme(candidate) {
    var result = {};
    for (var i = 0; i < customFields.length; i++) {
      var field = customFields[i];
      var next = candidate && candidate[field.key];
      if (isValidCssColor(next)) {
        result[field.key] = normalizeCssColor(next);
      } else {
        result[field.key] = normalizeCssColor(defaultCustomTheme[field.key]);
      }
    }
    return result;
  }

  function readCustomTheme() {
    try {
      var raw = localStorage.getItem(customThemeStorageKey);
      if (!raw) {
        return normalizeCustomTheme(defaultCustomTheme);
      }
      return normalizeCustomTheme(JSON.parse(raw));
    } catch (err) {
      return normalizeCustomTheme(defaultCustomTheme);
    }
  }

  function writeCustomTheme(theme) {
    try {
      localStorage.setItem(customThemeStorageKey, JSON.stringify(theme));
    } catch (err) {
      // ignore
    }
  }

  function applyCustomThemeCss(theme) {
    var root = document.documentElement;
    for (var i = 0; i < customFields.length; i++) {
      var field = customFields[i];
      root.style.setProperty(field.cssVar, theme[field.key]);
    }
  }

  function clearCustomThemeCss() {
    var root = document.documentElement;
    for (var i = 0; i < customFields.length; i++) {
      root.style.removeProperty(customFields[i].cssVar);
    }
  }

  function applyTheme(themeId) {
    var normalized = normalizeThemeId(themeId);
    var body = document.body;

    for (var i = 0; i < builtInThemes.length; i++) {
      body.classList.remove(builtInThemes[i].id);
    }

    var fileTheme = findFileTheme(normalized);
    if (fileTheme) {
      body.classList.add("theme-custom");
      applyCustomThemeCss(normalizeCustomTheme(fileTheme.customTheme || {}));
    } else {
      body.classList.add(normalized);
      if (normalized === "theme-custom") {
        applyCustomThemeCss(readCustomTheme());
      } else {
        clearCustomThemeCss();
      }
    }

    localStorage.setItem(themeStorageKey, normalized);
    applyAuthenticChromeState(normalized === authenticThemeId);
    if (window.ThreeSeventyWeb && typeof window.ThreeSeventyWeb.updateBackgroundTheme === "function") {
      window.ThreeSeventyWeb.updateBackgroundTheme(normalized);
    }
    document.dispatchEvent(new CustomEvent("themechange", { detail: normalized }));
  }

  function getStoredTheme() {
    var startup = localStorage.getItem(defaultThemeStorageKey);
    if (startup) {
      return startup;
    }
    return localStorage.getItem(themeStorageKey) || "theme-yorkshire";
  }

  function loadFileThemes() {
    return fetch("/api/themes", {
      headers: { Accept: "application/json", "Cache-Control": "no-cache" }
    })
      .then(function (res) {
        if (!res.ok) {
          return { themes: [] };
        }
        return res.json();
      })
      .then(function (payload) {
        var incoming = payload && Array.isArray(payload.themes) ? payload.themes : [];
        fileThemes = incoming
          .map(function (entry) {
            if (!entry || typeof entry !== "object") {
              return null;
            }
            if (!entry.id || !entry.name || !entry.customTheme) {
              return null;
            }
            return {
              id: String(entry.id),
              name: String(entry.name),
              fileName: String(entry.fileName || ""),
              customTheme: normalizeCustomTheme(entry.customTheme)
            };
          })
          .filter(Boolean);
      })
      .catch(function () {
        fileThemes = [];
      });
  }

  function renderThemeSelectOptions(select) {
    select.innerHTML = "";
    var all = getAllThemes();
    for (var i = 0; i < all.length; i++) {
      var opt = document.createElement("option");
      opt.value = all[i].id;
      opt.textContent = all[i].name;
      select.appendChild(opt);
    }
  }

  function initCustomThemeEditor(select, refreshThemes) {
    var hostRow = select.closest(".settings-theme-row");
    if (!hostRow) {
      return;
    }

    var editor = document.createElement("div");
    editor.className = "settings-custom-theme-editor";

    var hint = document.createElement("p");
    hint.className = "settings-custom-theme-hint subtle";
    hint.textContent = "Create a theme, then save it. Saved themes appear automatically in Theme list.";
    editor.appendChild(hint);

    var topRow = document.createElement("div");
    topRow.className = "settings-custom-theme-top";
    editor.appendChild(topRow);

    var paletteSection = document.createElement("div");
    paletteSection.className = "settings-custom-theme-section settings-custom-theme-palettes-section";
    topRow.appendChild(paletteSection);

    var paletteTitle = document.createElement("div");
    paletteTitle.className = "settings-custom-theme-title";
    paletteTitle.textContent = "Palettes";
    paletteSection.appendChild(paletteTitle);

    var paletteRow = document.createElement("div");
    paletteRow.className = "settings-theme-palettes";
    paletteSection.appendChild(paletteRow);

    var actionsSection = document.createElement("div");
    actionsSection.className = "settings-custom-theme-section settings-custom-theme-actions-section";
    topRow.appendChild(actionsSection);

    var actionsTitle = document.createElement("div");
    actionsTitle.className = "settings-custom-theme-title";
    actionsTitle.textContent = "Actions";
    actionsSection.appendChild(actionsTitle);

    var fieldsSection = document.createElement("div");
    fieldsSection.className = "settings-custom-theme-section settings-custom-theme-colors-section";
    editor.appendChild(fieldsSection);

    var fieldsTitle = document.createElement("div");
    fieldsTitle.className = "settings-custom-theme-title";
    fieldsTitle.textContent = "Colors";
    fieldsSection.appendChild(fieldsTitle);

    var grid = document.createElement("div");
    grid.className = "settings-custom-theme-grid";
    fieldsSection.appendChild(grid);

    var currentCustomTheme = readCustomTheme();
    var fieldRefs = {};
    var paletteButtons = [];
    var editorStatus = document.createElement("p");
    editorStatus.className = "settings-custom-theme-hint subtle";
    editorStatus.hidden = true;

    function showStatus(message) {
      editorStatus.textContent = message;
      editorStatus.hidden = !message;
    }

    function paletteMatches(themeValues, paletteValues) {
      for (var i = 0; i < customFields.length; i++) {
        var key = customFields[i].key;
        if (normalizeCssColor(themeValues[key]) !== normalizeCssColor(paletteValues[key])) {
          return false;
        }
      }
      return true;
    }

    function updatePaletteActiveState(themeValues) {
      for (var i = 0; i < paletteButtons.length; i++) {
        var entry = paletteButtons[i];
        var active = paletteMatches(themeValues, entry.palette.values);
        entry.button.classList.toggle("is-active", active);
        entry.button.setAttribute("aria-pressed", active ? "true" : "false");
      }
    }

    function syncEditorInputs(themeValues) {
      for (var i = 0; i < customFields.length; i++) {
        var field = customFields[i];
        var ref = fieldRefs[field.key];
        if (!ref) {
          continue;
        }
        var value = themeValues[field.key];
        ref.text.value = value;
        var hex = colorToHex(value);
        if (hex) {
          ref.color.value = hex;
        }
      }
      updatePaletteActiveState(themeValues);
    }

    function saveAndApplyCustomTheme(nextTheme, forceApply) {
      currentCustomTheme = normalizeCustomTheme(nextTheme);
      writeCustomTheme(currentCustomTheme);
      if (forceApply || getStoredTheme() === "theme-custom") {
        applyCustomThemeCss(currentCustomTheme);
        if (getStoredTheme() !== "theme-custom") {
          applyTheme("theme-custom");
          select.value = "theme-custom";
        }
      }
      syncEditorInputs(currentCustomTheme);
    }

    for (var p = 0; p < palettes.length; p++) {
      (function (palette) {
        var btn = document.createElement("button");
        btn.type = "button";
        btn.className = "settings-theme-palette";
        btn.textContent = palette.name;
        btn.setAttribute("aria-pressed", "false");
        btn.addEventListener("click", function () {
          saveAndApplyCustomTheme(palette.values, true);
        });
        paletteRow.appendChild(btn);
        paletteButtons.push({ button: btn, palette: palette });
      })(palettes[p]);
    }

    var resetBtn = document.createElement("button");
    resetBtn.type = "button";
    resetBtn.className = "settings-theme-reset";
    resetBtn.textContent = "Reset Custom";
    resetBtn.addEventListener("click", function () {
      saveAndApplyCustomTheme(defaultCustomTheme, true);
      showStatus("Custom theme reset.");
    });
    actionsSection.appendChild(resetBtn);

    var actionRow = document.createElement("div");
    actionRow.className = "settings-theme-actions";

    var defaultButton = document.createElement("button");
    defaultButton.type = "button";
    defaultButton.className = "settings-theme-action";
    defaultButton.textContent = "Set as startup default";

    var saveButton = document.createElement("button");
    saveButton.type = "button";
    saveButton.className = "settings-theme-action";
    saveButton.textContent = "Save Custom";

    var importButton = document.createElement("button");
    importButton.type = "button";
    importButton.className = "settings-theme-action";
    importButton.textContent = "Load Custom";

    var importInput = document.createElement("input");
    importInput.type = "file";
    importInput.accept = "application/json,.json";
    importInput.hidden = true;

    var saveMetaRow = document.createElement("div");
    saveMetaRow.className = "settings-theme-save-meta";
    var saveNameInput = document.createElement("input");
    saveNameInput.type = "text";
    saveNameInput.className = "settings-theme-save-name";
    saveNameInput.placeholder = "Theme name";
    saveNameInput.value = "Custom Theme";
    saveNameInput.setAttribute("aria-label", "Custom theme name");
    saveMetaRow.appendChild(saveNameInput);

    actionRow.appendChild(defaultButton);
    actionRow.appendChild(saveButton);
    actionRow.appendChild(importButton);
    actionRow.appendChild(importInput);
    actionsSection.appendChild(actionRow);
    actionsSection.appendChild(saveMetaRow);
    editor.appendChild(editorStatus);

    function updateDefaultButtonState() {
      var startupTheme = normalizeThemeId(localStorage.getItem(defaultThemeStorageKey) || "");
      var selected = normalizeThemeId(select.value || "");
      var isDefault = startupTheme && startupTheme === selected;
      defaultButton.textContent = isDefault ? "Startup default set" : "Set as startup default";
      defaultButton.setAttribute("aria-pressed", isDefault ? "true" : "false");
      defaultButton.classList.toggle("is-active", !!isDefault);
    }

    defaultButton.addEventListener("click", function () {
      var selected = normalizeThemeId(select.value || "theme-yorkshire");
      localStorage.setItem(defaultThemeStorageKey, selected);
      updateDefaultButtonState();
      showStatus("Startup default saved.");
    });

    saveButton.addEventListener("click", async function () {
      var name = String(saveNameInput.value || "").trim();
      if (!name) {
        showStatus("Save failed: theme name is required.");
        saveNameInput.focus();
        return;
      }
      try {
        var response = await fetch("/api/themes/save", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ name: name, customTheme: readCustomTheme() })
        });
        var result = await response.json().catch(function () { return {}; });
        if (!response.ok) {
          throw new Error(result.error || "Failed to save theme.");
        }
        await refreshThemes();
        if (result && result.id && hasTheme(result.id)) {
          select.value = result.id;
          applyTheme(result.id);
        }
        showStatus("Saved to app themes folder.");
      } catch (err) {
        showStatus("Save failed: " + (err && err.message ? err.message : "unknown error"));
      }
    });

    importButton.addEventListener("click", function () {
      importInput.click();
    });

    importInput.addEventListener("change", function () {
      var file = importInput.files && importInput.files[0];
      if (!file) {
        return;
      }
      var reader = new FileReader();
      reader.onload = function () {
        try {
          var parsed = JSON.parse(String(reader.result || ""));
          var source = parsed && parsed.customTheme ? parsed.customTheme : parsed;
          var normalized = normalizeCustomTheme(source || {});
          saveAndApplyCustomTheme(normalized, true);
          applyTheme("theme-custom");
          select.value = "theme-custom";
          if (parsed && parsed.name && typeof parsed.name === "string") {
            saveNameInput.value = parsed.name;
          }
          updateDefaultButtonState();
          showStatus("Custom theme loaded into editor. Save to add it to themes folder.");
        } catch (err) {
          showStatus("Load failed: invalid JSON theme file.");
        }
      };
      reader.onerror = function () {
        showStatus("Load failed: unable to read file.");
      };
      reader.readAsText(file);
      importInput.value = "";
    });

    for (var i = 0; i < customFields.length; i++) {
      (function (field) {
        var item = document.createElement("div");
        item.className = "settings-custom-theme-field";

        var label = document.createElement("label");
        label.textContent = field.label;
        item.appendChild(label);

        var controls = document.createElement("div");
        controls.className = "settings-custom-theme-inputs";

        var colorInput = document.createElement("input");
        colorInput.type = "color";
        colorInput.className = "settings-custom-theme-color";

        var textInput = document.createElement("input");
        textInput.type = "text";
        textInput.className = "settings-custom-theme-text";
        textInput.placeholder = "#39ff14 or rgb(57,255,20)";
        textInput.setAttribute("aria-label", field.label + " color value");

        controls.appendChild(colorInput);
        controls.appendChild(textInput);
        item.appendChild(controls);
        grid.appendChild(item);
        fieldRefs[field.key] = { color: colorInput, text: textInput };

        colorInput.addEventListener("input", function () {
          var next = {};
          next[field.key] = colorInput.value;
          textInput.value = colorInput.value;
          textInput.setCustomValidity("");
          saveAndApplyCustomTheme(Object.assign({}, currentCustomTheme, next), true);
        });

        textInput.addEventListener("input", function () {
          var value = textInput.value.trim();
          if (!value) {
            textInput.setCustomValidity("Color value is required.");
            return;
          }
          if (!isValidCssColor(value)) {
            textInput.setCustomValidity("Use #hex or rgb(...) format.");
            return;
          }
          textInput.setCustomValidity("");
          var normalized = normalizeCssColor(value);
          var hex = colorToHex(normalized);
          if (hex) {
            colorInput.value = hex;
          }
          var next = {};
          next[field.key] = normalized;
          saveAndApplyCustomTheme(Object.assign({}, currentCustomTheme, next), true);
        });
      })(customFields[i]);
    }

    hostRow.appendChild(editor);

    function syncEditorVisibility() {
      var customActive = select.value === "theme-custom";
      editor.hidden = !customActive;
      updateDefaultButtonState();
      if (!customActive) {
        return;
      }
      currentCustomTheme = readCustomTheme();
      syncEditorInputs(currentCustomTheme);
    }

    select.addEventListener("change", syncEditorVisibility);
    syncEditorInputs(currentCustomTheme);
    syncEditorVisibility();
  }

  async function initThemeSelector() {
    var select = document.getElementById("theme-select");
    if (!select) {
      var slot = document.querySelector("[data-settings-theme-slot]");
      if (slot) {
        var row = document.createElement("div");
        row.className = "settings-theme-row";
        var label = document.createElement("label");
        label.setAttribute("for", "theme-select");
        label.textContent = "Theme";
        select = document.createElement("select");
        select.id = "theme-select";
        row.appendChild(label);
        row.appendChild(select);
        slot.appendChild(row);
      }
    }

    var refreshThemes = async function () {
      await loadFileThemes();
      if (select) {
        var previous = select.value;
        renderThemeSelectOptions(select);
        if (previous && hasTheme(previous)) {
          select.value = previous;
        }
      }
    };

    await refreshThemes();

    if (!select) {
      applyTheme(normalizeThemeId(getStoredTheme()));
      return;
    }

    var current = normalizeThemeId(getStoredTheme());
    if (!hasTheme(current)) {
      current = "theme-yorkshire";
    }
    select.value = current;
    applyTheme(current);
    initCustomThemeEditor(select, refreshThemes);

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

  document.addEventListener("DOMContentLoaded", function () {
    initThemeSelector();
  });
})();
