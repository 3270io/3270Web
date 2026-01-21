(function () {
  "use strict";

  var canvas = document.getElementById("bg-canvas");
  if (!canvas) {
    return;
  }

  var ctx = canvas.getContext("2d");
  if (!ctx) {
    return;
  }

  var overlay = canvas.parentElement;
  if (overlay && overlay.classList && !overlay.classList.contains("bg-overlay")) {
    overlay = null;
  }
  var storageKey = "3270Web.bgAnimation";
  var themeKey = "3270Web.theme";
  var textFont = "'Cascadia Mono', 'Consolas', 'Courier New', monospace";
  var chars = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ#$*+-";
  // Divisors keep animation density balanced across common viewport sizes.
  var PIXEL_AREA_DIVISOR = 20000;
  var CARD_AREA_DIVISOR = 80000;
  var CHAR_AREA_DIVISOR = 12000;
  var MAX_DELTA_SECONDS = 0.05;
  var TOGGLE_KEYS = [" ", "Enter"];

  var themeConfigs = {
    "theme-classic": { mode: "characters", density: 1.05, speed: 1 },
    "theme-dark": { mode: "characters", density: 0.9, speed: 0.8 },
    "theme-light": { mode: "punchcards", density: 1, speed: 1 },
    "theme-modern": { mode: "pixels", density: 1, speed: 1 },
    "theme-minimal": { mode: "characters", density: 0.7, speed: 0.75 },
    "theme-slick": { mode: "pixels", density: 1.15, speed: 1.15 },
    "theme-not3270": { mode: "punchcards", density: 1.1, speed: 0.95 }
  };

  var state = {
    width: 0,
    height: 0,
    ratio: 1,
    items: [],
    mode: "characters",
    density: 1,
    speed: 1,
    running: true,
    lastTime: 0,
    colors: {}
  };

  function clamp(value, min, max) {
    return Math.max(min, Math.min(max, value));
  }

  function readColors() {
    var styles = getComputedStyle(document.body);
    return {
      fg: styles.getPropertyValue("--fg").trim() || "#39ff14",
      accent: styles.getPropertyValue("--accent").trim() || "#39ff14",
      muted: styles.getPropertyValue("--fg-muted").trim() || "#8bdc79",
      panel: styles.getPropertyValue("--panel").trim() || "#0f260f",
      bg: styles.getPropertyValue("--bg").trim() || "#0b1a0b"
    };
  }

  function setCanvasSize() {
    var ratio = window.devicePixelRatio || 1;
    var width = overlay ? overlay.clientWidth : window.innerWidth;
    var height = overlay ? overlay.clientHeight : window.innerHeight;
    if (!width || !height) {
      return;
    }
    state.width = width;
    state.height = height;
    state.ratio = ratio;
    canvas.width = width * ratio;
    canvas.height = height * ratio;
    ctx.setTransform(ratio, 0, 0, ratio, 0, 0);
  }

  function getCurrentTheme() {
    var themeId = "";
    Object.keys(themeConfigs).some(function (key) {
      if (document.body.classList.contains(key)) {
        themeId = key;
        return true;
      }
      return false;
    });
    if (!themeId) {
      themeId = localStorage.getItem(themeKey) || "theme-classic";
    }
    return themeId;
  }

  function applyThemeConfig(themeId) {
    var config = themeConfigs[themeId] || themeConfigs["theme-classic"];
    state.mode = config.mode;
    state.density = config.density;
    state.speed = config.speed;
    state.items = [];
    state.colors = readColors();
  }

  function toggleBackgroundAnimation() {
    state.running = !state.running;
    document.body.classList.toggle("bg-paused", !state.running);
    localStorage.setItem(storageKey, state.running ? "on" : "off");
    if (overlay) {
      overlay.setAttribute("aria-pressed", state.running ? "true" : "false");
    }
  }

  function desiredCount(area) {
    if (state.mode === "pixels") {
      return clamp(Math.round(area / PIXEL_AREA_DIVISOR * state.density), 25, 140);
    }
    if (state.mode === "punchcards") {
      return clamp(Math.round(area / CARD_AREA_DIVISOR * state.density), 6, 26);
    }
    return clamp(Math.round(area / CHAR_AREA_DIVISOR * state.density), 20, 160);
  }

  function newCharacter() {
    var size = 12 + Math.random() * 18;
    return {
      x: Math.random() * state.width,
      y: Math.random() * state.height,
      size: size,
      char: chars[Math.floor(Math.random() * chars.length)],
      life: 0.6 + Math.random() * 1.8,
      maxLife: 0,
      drift: (Math.random() - 0.5) * 12
    };
  }

  function newPixel() {
    var size = 2 + Math.random() * 4;
    return {
      x: Math.random() * state.width,
      y: Math.random() * state.height,
      size: size,
      vx: (Math.random() - 0.5) * 18,
      vy: (Math.random() - 0.5) * 18,
      alpha: 0.15 + Math.random() * 0.35
    };
  }

  function newCard() {
    var width = 120 + Math.random() * 140;
    var height = 60 + Math.random() * 60;
    var columns = 8;
    var rows = 3;
    var holes = [];
    for (var r = 0; r < rows; r++) {
      for (var c = 0; c < columns; c++) {
        if (Math.random() > 0.4) {
          holes.push({
            x: ((c + 1) / (columns + 1)) * width,
            y: ((r + 1) / (rows + 1)) * height,
            r: 4 + Math.random() * 2
          });
        }
      }
    }
    return {
      x: Math.random() * state.width,
      y: Math.random() * state.height,
      width: width,
      height: height,
      speed: 12 + Math.random() * 24,
      alpha: 0.18 + Math.random() * 0.15,
      holes: holes
    };
  }

  function updateCharacters(delta) {
    var target = desiredCount(state.width * state.height);
    while (state.items.length < target) {
      var next = newCharacter();
      next.maxLife = next.life;
      state.items.push(next);
    }
    ctx.clearRect(0, 0, state.width, state.height);
    ctx.textAlign = "center";
    ctx.textBaseline = "middle";
    for (var i = state.items.length - 1; i >= 0; i--) {
      var item = state.items[i];
      item.life -= delta * state.speed;
      item.y += item.drift * delta * state.speed;
      if (item.life <= 0) {
        state.items.splice(i, 1);
        continue;
      }
      var progress = 1 - item.life / item.maxLife;
      var alpha = Math.sin(progress * Math.PI);
      ctx.globalAlpha = alpha * 0.65;
      ctx.fillStyle = state.colors.accent || state.colors.fg;
      ctx.font = item.size + "px " + textFont;
      ctx.fillText(item.char, item.x, item.y);
    }
    ctx.globalAlpha = 1;
  }

  function updatePixels(delta) {
    var target = desiredCount(state.width * state.height);
    while (state.items.length < target) {
      state.items.push(newPixel());
    }
    ctx.clearRect(0, 0, state.width, state.height);
    for (var i = state.items.length - 1; i >= 0; i--) {
      var item = state.items[i];
      item.x += item.vx * delta * state.speed;
      item.y += item.vy * delta * state.speed;
      if (
        item.x < -10 ||
        item.x > state.width + 10 ||
        item.y < -10 ||
        item.y > state.height + 10
      ) {
        state.items[i] = newPixel();
        continue;
      }
      ctx.globalAlpha = item.alpha;
      ctx.fillStyle = state.colors.accent || state.colors.fg;
      ctx.fillRect(item.x, item.y, item.size, item.size);
    }
    ctx.globalAlpha = 1;
  }

  function updateCards(delta) {
    var target = desiredCount(state.width * state.height);
    while (state.items.length < target) {
      state.items.push(newCard());
    }
    ctx.clearRect(0, 0, state.width, state.height);
    for (var i = state.items.length - 1; i >= 0; i--) {
      var item = state.items[i];
      item.y += item.speed * delta * state.speed;
      if (item.y > state.height + item.height) {
        item.y = -item.height - Math.random() * 40;
        item.x = Math.random() * state.width;
      }
      ctx.globalAlpha = item.alpha;
      ctx.fillStyle = state.colors.panel || state.colors.muted;
      ctx.strokeStyle = state.colors.accent || state.colors.fg;
      ctx.lineWidth = 1;
      ctx.fillRect(item.x, item.y, item.width, item.height);
      ctx.strokeRect(item.x, item.y, item.width, item.height);
      ctx.globalAlpha = item.alpha + 0.1;
      ctx.fillStyle = state.colors.bg;
      item.holes.forEach(function (hole) {
        ctx.beginPath();
        ctx.arc(item.x + hole.x, item.y + hole.y, hole.r, 0, Math.PI * 2);
        ctx.fill();
      });
    }
    ctx.globalAlpha = 1;
  }

  function renderFrame(timestamp) {
    if (!state.lastTime) {
      state.lastTime = timestamp;
    }
    var delta = Math.min((timestamp - state.lastTime) / 1000, MAX_DELTA_SECONDS);
    state.lastTime = timestamp;
    if (state.running) {
      if (state.mode === "pixels") {
        updatePixels(delta);
      } else if (state.mode === "punchcards") {
        updateCards(delta);
      } else {
        updateCharacters(delta);
      }
    }
    requestAnimationFrame(renderFrame);
  }

  function init() {
    setCanvasSize();
    applyThemeConfig(getCurrentTheme());
    state.running = localStorage.getItem(storageKey) !== "off";
    document.body.classList.toggle("bg-paused", !state.running);
    if (overlay) {
      overlay.setAttribute("aria-pressed", state.running ? "true" : "false");
    }
    requestAnimationFrame(renderFrame);
  }

  window.ThreeSeventyWeb = window.ThreeSeventyWeb || {};
  window.ThreeSeventyWeb.updateBackgroundTheme = applyThemeConfig;

  if (overlay) {
    overlay.addEventListener("click", function (event) {
      if (event.target === overlay || event.target === canvas) {
        toggleBackgroundAnimation();
      }
    });
    overlay.addEventListener("keydown", function (event) {
      if (TOGGLE_KEYS.includes(event.key)) {
        event.preventDefault();
        toggleBackgroundAnimation();
      }
    });
  }

  window.addEventListener("resize", setCanvasSize);
  document.addEventListener("themechange", function (event) {
    if (event && event.detail) {
      applyThemeConfig(event.detail);
    }
  });

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
