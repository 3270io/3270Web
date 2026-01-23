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
  // Divisors keep animation density balanced around a ~1200x700 viewport.
  var PIXEL_AREA_DIVISOR = 20000;
  var CARD_AREA_DIVISOR = 80000;
  var BLOCK_AREA_DIVISOR = 220000;
  var CHAR_AREA_DIVISOR = 12000;
  var MAX_DELTA_SECONDS = 0.05;
  var TOGGLE_KEYS = [" ", "Enter"];
  var MIN_CHAR_SIZE = 12;
  var CHAR_SIZE_RANGE = 18;
  var CHAR_MIN_LIFE = 0.6;
  var CHAR_LIFE_RANGE = 1.8;
  var CHAR_DRIFT_RANGE = 12;
  var MIN_PIXEL_SIZE = 2;
  var PIXEL_SIZE_RANGE = 4;
  var PIXEL_SPEED_RANGE = 18;
  var PIXEL_MIN_ALPHA = 0.15;
  var PIXEL_ALPHA_RANGE = 0.35;
  var CARD_MIN_WIDTH = 120;
  var CARD_WIDTH_RANGE = 140;
  var CARD_MIN_HEIGHT = 60;
  var CARD_HEIGHT_RANGE = 60;
  var CARD_COLUMNS = 8;
  var CARD_ROWS = 3;
  var HOLE_THRESHOLD = 0.4;
  var HOLE_MIN_RADIUS = 4;
  var HOLE_RADIUS_RANGE = 2;
  var CARD_MIN_SPEED = 12;
  var CARD_SPEED_RANGE = 24;
  var CARD_MIN_ALPHA = 0.18;
  var CARD_ALPHA_RANGE = 0.15;
  var CARD_RESET_PADDING = 40;

  var themeConfigs = {
    "theme-classic": { mode: "characters", density: 1.05, speed: 1 },
    "theme-dark": { mode: "characters", density: 0.9, speed: 0.8 },
    "theme-light": { mode: "characters", density: 0.95, speed: 0.9 },
    "theme-modern": { mode: "pixels", density: 1, speed: 1 },
    "theme-minimal": { mode: "characters", density: 0.7, speed: 0.75 },
    "theme-slick": { mode: "pixels", density: 1.15, speed: 1.15 },
    "theme-yorkshire": { mode: "blocks", density: 0.6, speed: 0.6 },
    "theme-not3270": { mode: "characters", density: 1, speed: 0.9 }
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
    if (state.mode === "blocks") {
      return clamp(Math.round(area / BLOCK_AREA_DIVISOR * state.density), 4, 16);
    }
    return clamp(Math.round(area / CHAR_AREA_DIVISOR * state.density), 20, 160);
  }

  function newCharacter() {
    var size = MIN_CHAR_SIZE + Math.random() * CHAR_SIZE_RANGE;
    return {
      x: Math.random() * state.width,
      y: Math.random() * state.height,
      size: size,
      char: chars[Math.floor(Math.random() * chars.length)],
      life: CHAR_MIN_LIFE + Math.random() * CHAR_LIFE_RANGE,
      maxLife: 0,
      drift: (Math.random() - 0.5) * CHAR_DRIFT_RANGE
    };
  }

  function newPixel() {
    var size = MIN_PIXEL_SIZE + Math.random() * PIXEL_SIZE_RANGE;
    return {
      x: Math.random() * state.width,
      y: Math.random() * state.height,
      size: size,
      vx: (Math.random() - 0.5) * PIXEL_SPEED_RANGE,
      vy: (Math.random() - 0.5) * PIXEL_SPEED_RANGE,
      alpha: PIXEL_MIN_ALPHA + Math.random() * PIXEL_ALPHA_RANGE
    };
  }

  function newCard() {
    var width = CARD_MIN_WIDTH + Math.random() * CARD_WIDTH_RANGE;
    var height = CARD_MIN_HEIGHT + Math.random() * CARD_HEIGHT_RANGE;
    var columns = CARD_COLUMNS;
    var rows = CARD_ROWS;
    var holes = [];
    for (var r = 0; r < rows; r++) {
      for (var c = 0; c < columns; c++) {
        if (Math.random() > HOLE_THRESHOLD) {
          holes.push({
            x: ((c + 1) / (columns + 1)) * width,
            y: ((r + 1) / (rows + 1)) * height,
            r: HOLE_MIN_RADIUS + Math.random() * HOLE_RADIUS_RANGE
          });
        }
      }
    }
    return {
      x: Math.random() * state.width,
      y: Math.random() * state.height,
      width: width,
      height: height,
      speed: CARD_MIN_SPEED + Math.random() * CARD_SPEED_RANGE,
      alpha: CARD_MIN_ALPHA + Math.random() * CARD_ALPHA_RANGE,
      holes: holes
    };
  }

  function newLine() {
    var size = 10 + Math.random() * 14;
    return {
      x: Math.random() * state.width,
      y: Math.random() * state.height,
      size: size,
      char: chars[Math.floor(Math.random() * chars.length)],
      life: CHAR_MIN_LIFE + Math.random() * CHAR_LIFE_RANGE,
      maxLife: 0,
      drift: (Math.random() - 0.5) * 8
    };
  }

  var yorkshireBlocks = [
    [
      "000001 //CICSTEST JOB (ACCT),'EY UP',CLASS=A,MSGCLASS=X",
      "000002 //STEP010 EXEC PGM=DFHRM000",
      "000003 //SYSOUT  DD SYSOUT=*",
      "000004 DFHAC2200I CICSTEST CONTROL REGION READY"
    ],
    [
      "SDSF STATUS: ACTIVE JOBS",
      "JOBNAME  STEPNAME  PROC  RC",
      "PAYROLL  STEP020   PAYP  0000",
      "INVOICE  STEP010   INV1  0004",
      "EZYUP001 MESSAGE: EY UP, CHECK THA' RC"
    ],
    [
      "IEF404I JOB LEDGER  ENDED - TIME=09.12.44",
      "IEC130I SYS1.PARMLIB, VOLUME SERIAL MISMATCH",
      "RESP=12  REASON=00000014",
      "EZYUP404I IF WEATHER = 'COLD' THEN PUT JUMPER ON"
    ],
    [
      "IKJ56250I COMMAND COMPLETE",
      "TSO READY",
      "SDSF DA",
      "OWNER  ID     NAME     CL  POS",
      "EYUP001I THA' JOB'S REET, LAD"
    ],
    [
      "COBOL  CICS  DFHCOMMAREA",
      "01  WS-STATUS.",
      "    05 WS-ERROR-CODE   PIC X(08).",
      "    05 WS-ERROR-TEXT   PIC X(40).",
      "IF WS-ERROR-CODE = 'COLD' DISPLAY 'GET THA JUMPER'"
    ]
  ];

  function newBlock() {
    var size = 10 + Math.random() * 4;
    var block = yorkshireBlocks[Math.floor(Math.random() * yorkshireBlocks.length)];
    return {
      x: Math.random() * state.width,
      y: Math.random() * state.height,
      size: size,
      lines: block,
      life: 1.4 + Math.random() * 1.8,
      maxLife: 0,
      drift: (Math.random() - 0.5) * 10
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

  function updateLines(delta) {
    var target = desiredCount(state.width * state.height);
    while (state.items.length < target) {
      var next = newLine();
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
      ctx.globalAlpha = alpha * 0.35;
      ctx.fillStyle = state.colors.accent || state.colors.fg;
      ctx.font = item.size + "px " + textFont;
      ctx.fillText(item.char, item.x, item.y);
    }
    ctx.globalAlpha = 1;
  }

  function updateBlocks(delta) {
    var target = desiredCount(state.width * state.height);
    while (state.items.length < target) {
      var next = newBlock();
      next.maxLife = next.life;
      state.items.push(next);
    }
    ctx.clearRect(0, 0, state.width, state.height);
    ctx.textBaseline = "top";
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
      ctx.globalAlpha = alpha * 0.5;
      ctx.fillStyle = state.colors.accent || state.colors.fg;
      ctx.font = item.size + "px " + textFont;
      var lineHeight = item.size + 3;
      for (var l = 0; l < item.lines.length; l++) {
        ctx.fillText(item.lines[l], item.x, item.y + l * lineHeight);
      }
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
        item.y = -item.height - Math.random() * CARD_RESET_PADDING;
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
      } else if (state.mode === "lines") {
        updateLines(delta);
      } else if (state.mode === "blocks") {
        updateBlocks(delta);
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

  init();
})();
