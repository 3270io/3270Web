/**
 * Capture the screenshots used by the MkDocs site (docs/images).
 *
 * Usage:
 *   go run ./cmd/3270Web           # in another terminal
 *   node scripts/capture-doc-screenshots.mjs
 *
 * Options (environment variables):
 *   DOCS_BASE_URL   base URL of a running 3270Web (default http://127.0.0.1:3270)
 *   DOCS_OUT_DIR    output directory       (default <repo>/docs/images)
 *   DOCS_SAMPLE_APP sample app id          (default app1)
 *   DOCS_SAMPLE_PORT sample app port       (default 3271)
 *
 * Notes
 *   - Paths are resolved from this file's location, so the script runs from
 *     any working directory and on any OS.
 *   - Callout badges are drawn *outside* the control they label, joined by a
 *     connector line. The previous version placed them directly on top of the
 *     buttons, which hid the very icons the docs were pointing at.
 *   - Screenshots are clipped to the region of interest rather than captured
 *     full-page, so the images stay legible when scaled down in the docs.
 */
import { chromium } from 'playwright';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { mkdir } from 'node:fs/promises';

const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(here, '..');

const baseUrl = process.env.DOCS_BASE_URL || 'http://127.0.0.1:3270';
const outDir = process.env.DOCS_OUT_DIR || path.join(repoRoot, 'docs', 'images');
const sampleApp = process.env.DOCS_SAMPLE_APP || 'app1';
// 3270 is the web server's own port and is not in the sample-app allow list,
// so the default used to make every run fail its connect with a 503.
const samplePort = process.env.DOCS_SAMPLE_PORT || '3271';
const workflowFile = path.join(repoRoot, 'workflow_app1.json');

const VIEWPORT = { width: 1500, height: 940 };
const SCALE = 2;

/* ------------------------------------------------------------------ */
/* Helpers                                                             */
/* ------------------------------------------------------------------ */

const settle = (page, ms = 500) => page.waitForTimeout(ms);

/**
 * Tippy leaves a tooltip on screen after a programmatic click, and the
 * animated "code rain" background is random, so both would otherwise show up
 * as noise in tightly-cropped callout images.
 */
async function dismissTooltips(page) {
  await page.mouse.move(4, 4);
  await page.evaluate(() => {
    document.querySelectorAll('[data-tippy-root]').forEach((el) => el.remove());
  });
  await settle(page, 150);
}

/** Hide chrome that would otherwise overlap a tightly-cropped region. */
async function setDistractionsHidden(page, hidden) {
  await page.evaluate((hide) => {
    const ids = ['.bg-overlay', '[data-status-widget]'];
    ids.forEach((sel) => {
      document.querySelectorAll(sel).forEach((el) => {
        el.style.visibility = hide ? 'hidden' : '';
      });
    });
  }, hidden);
  await settle(page, 200);
}

/**
 * Toasts fire on their own schedule ("Recording loaded…", "No saved hints
 * yet.") and would land in whatever image happened to be capturing at the
 * time. Keep them out of every screenshot.
 */
async function suppressToasts(page) {
  await page.addStyleTag({ content: '#h3270-notification-container{display:none !important}' });
}

/** Wait for webfonts so terminal glyph metrics are final before capturing. */
async function ready(page) {
  await page.evaluate(() => document.fonts && document.fonts.ready);
  await settle(page, 400);
}

/**
 * Business mode is the default surface and hides the Automation menu, so the
 * automation screenshots have to ask for Engineering mode first — otherwise
 * every callout inside that menu resolves to a zero-size node. The connect
 * and session shots are deliberately left in Business mode: that is what a
 * new user actually opens.
 */
async function setWorkspaceMode(page, mode) {
  await page.evaluate((next) => {
    const ws = window.ThreeSeventyWeb && window.ThreeSeventyWeb.workspace;
    if (ws && typeof ws.setMode === 'function') ws.setMode(next);
  }, mode);
  await settle(page, 500);
}

/** Open a drop-down in the menu bar by its label, and leave it open. */
async function openMenu(page, label) {
  const trigger = page.locator(`.appmenu-trigger:has-text("${label}")`).first();
  if (!(await trigger.count())) return;
  if ((await trigger.getAttribute('aria-expanded')) !== 'true') {
    await trigger.click();
    await settle(page, 400);
  }
}

/** Open the account and settings menu at the right-hand end of the bar. */
async function openAccountMenu(page) {
  const trigger = page.locator('.appmenu-end .appmenu-trigger').first();
  if ((await trigger.getAttribute('aria-expanded')) !== 'true') {
    await trigger.click();
    await settle(page, 350);
  }
}

/** Close whichever menu is open. */
async function closeMenus(page) {
  await page.evaluate(() => {
    const api = window.ThreeSeventyWeb;
    if (api && api.menuBar) api.menuBar.close();
  });
  await settle(page, 250);
}

async function ensureScreen(page) {
  await page.goto(baseUrl, { waitUntil: 'domcontentloaded' });
  await settle(page, 800);
  if (/\/screen(?:$|\?)/.test(page.url())) {
    await page.waitForSelector('[data-appbar]', { timeout: 15000 });
    return;
  }
  await page.fill('#hostname-input', `sampleapp:${sampleApp}:${samplePort}`);
  await page.click('#connect-btn');
  await page.waitForURL(/\/screen/, { timeout: 40000 });
  await page.waitForSelector('[data-appbar]', { timeout: 15000 });
  await ready(page);
}

/**
 * Draw numbered callouts next to (never over) the given selectors.
 *
 * item = { selector, label, place?: 'below'|'above'|'left'|'right', gap?: number }
 */
async function annotate(page, items) {
  await page.evaluate(() => {
    document.querySelectorAll('.doc-badge, .doc-badge-line').forEach((el) => el.remove());
  });
  for (const item of items) {
    const target = page.locator(item.selector).first();
    if (!(await target.count())) continue;
    const box = await target.boundingBox();
    if (!box) continue;
    await page.evaluate(
      ({ box, label, place, gap }) => {
        const SIZE = 26;
        const cx = box.x + box.width / 2;
        const cy = box.y + box.height / 2;
        let bx;
        let by;
        let line;
        if (place === 'above') {
          bx = cx - SIZE / 2;
          by = box.y - gap - SIZE;
          line = { x: cx - 1, y: by + SIZE, w: 2, h: gap };
        } else if (place === 'left') {
          bx = box.x - gap - SIZE;
          by = cy - SIZE / 2;
          line = { x: bx + SIZE, y: cy - 1, w: gap, h: 2 };
        } else if (place === 'right') {
          bx = box.x + box.width + gap;
          by = cy - SIZE / 2;
          line = { x: box.x + box.width, y: cy - 1, w: gap, h: 2 };
        } else {
          bx = cx - SIZE / 2;
          by = box.y + box.height + gap;
          line = { x: cx - 1, y: box.y + box.height, w: 2, h: gap };
        }

        const rule = document.createElement('div');
        rule.className = 'doc-badge-line';
        Object.assign(rule.style, {
          position: 'fixed',
          left: `${line.x}px`,
          top: `${line.y}px`,
          width: `${line.w}px`,
          height: `${line.h}px`,
          background: '#ef4444',
          zIndex: '999998',
          pointerEvents: 'none',
        });
        document.body.appendChild(rule);

        const el = document.createElement('div');
        el.className = 'doc-badge';
        el.textContent = String(label);
        Object.assign(el.style, {
          position: 'fixed',
          left: `${Math.max(4, bx)}px`,
          top: `${Math.max(4, by)}px`,
          width: `${SIZE}px`,
          height: `${SIZE}px`,
          borderRadius: '999px',
          background: '#ef4444',
          color: '#fff',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          fontWeight: '700',
          fontSize: '14px',
          fontFamily: 'Arial, Helvetica, sans-serif',
          zIndex: '999999',
          border: '2px solid #fff',
          boxShadow: '0 2px 8px rgba(0,0,0,0.45)',
          pointerEvents: 'none',
        });
        document.body.appendChild(el);
      },
      { box, label: item.label, place: item.place || 'below', gap: item.gap ?? 18 }
    );
  }
}

async function clearAnnotations(page) {
  await page.evaluate(() => {
    document.querySelectorAll('.doc-badge, .doc-badge-line').forEach((el) => el.remove());
  });
}

/** Screenshot the union of the given selectors, plus padding. */
async function shotRegion(page, name, selectors, pad = 28) {
  const list = Array.isArray(selectors) ? selectors : [selectors];
  const boxes = [];
  for (const sel of list) {
    const loc = page.locator(sel).first();
    if (!(await loc.count())) continue;
    const b = await loc.boundingBox();
    if (b) boxes.push(b);
  }
  // Include the callout badges so connectors are never cut off.
  const badges = await page.$$('.doc-badge');
  for (const badge of badges) {
    const b = await badge.boundingBox();
    if (b) boxes.push(b);
  }
  if (!boxes.length) {
    // Every selector missed. Silently writing a full-page shot here is how a
    // renamed class quietly replaces a tight crop with a whole browser window,
    // so say so loudly instead — the image still gets written, but the run
    // makes it obvious which selector needs updating.
    console.warn(`  ! ${name}: no selector matched (${list.join(', ')}) — writing full-page fallback`);
    await page.screenshot({ path: path.join(outDir, name) });
    return;
  }
  const vp = page.viewportSize() || VIEWPORT;
  const x = Math.max(0, Math.min(...boxes.map((b) => b.x)) - pad);
  const y = Math.max(0, Math.min(...boxes.map((b) => b.y)) - pad);
  const right = Math.min(vp.width, Math.max(...boxes.map((b) => b.x + b.width)) + pad);
  const bottom = Math.min(vp.height, Math.max(...boxes.map((b) => b.y + b.height)) + pad);
  await page.screenshot({
    path: path.join(outDir, name),
    clip: { x, y, width: Math.max(1, right - x), height: Math.max(1, bottom - y) },
  });
  console.log('  wrote', name);
}

/* ------------------------------------------------------------------ */

async function main() {
  await mkdir(outDir, { recursive: true });

  const browser = await chromium.launch();
  const context = await browser.newContext({ viewport: VIEWPORT, deviceScaleFactor: SCALE });
  const page = await context.newPage();
  page.on('pageerror', (e) => console.warn('  page error:', e.message));

  /* ---- Connect page -------------------------------------------- */
  await page.goto(baseUrl, { waitUntil: 'domcontentloaded' });
  await settle(page, 900);
  if (!/\/screen/.test(page.url())) {
    await ready(page);
    await shotRegion(page, 'connect_image.png', '.card', 36);
  }

  await ensureScreen(page);
  await suppressToasts(page);

  /* ---- Session screen (default Yorkshire theme) ------------------ */
  // Engineering mode for the session shot: README and the 3270.io gallery
  // both caption this image as showing the recording and chaos controls, and
  // Business mode hides those groups entirely.
  await setWorkspaceMode(page, 'engineering');
  await dismissTooltips(page);
  await shotRegion(page, 'yorkshire_image.png', '.card', 24);
  await shotRegion(page, 'sampleapp1_image.png', '.terminal-shell', 24);

  /* ---- Terminal status bar (OIA) --------------------------------- */
  // The OIA sits at the bottom of a card that is already about as tall as the
  // viewport, so at the default height it falls below the fold and the clip
  // gets clamped to a sliver. Give this one shot room instead.
  await page.setViewportSize({ width: VIEWPORT.width, height: 1120 });
  await settle(page, 400);
  await page.locator('[data-screen-oia]').first().scrollIntoViewIfNeeded();
  await settle(page, 300);
  await shotRegion(page, 'terminal-status-bar.png', '[data-screen-oia]', 14);
  await page.setViewportSize(VIEWPORT);
  await settle(page, 400);

  /* ---- Command palette ------------------------------------------- */
  await page.keyboard.press('Control+k');
  await page.waitForSelector('.cmdk-panel', { timeout: 5000 });
  await page.fill('.cmdk-input', 'chaos');
  await settle(page, 500);
  await shotRegion(page, 'command-palette.png', '.cmdk-panel', 20);
  await page.keyboard.press('Escape');
  await settle(page, 400);

  /* ---- Menu bar callouts ------------------------------------------ */
  // The Automation menu only exists on the Engineering surface.
  await setWorkspaceMode(page, 'engineering');
  await settle(page, 600);
  await dismissTooltips(page);
  await setDistractionsHidden(page, true);
  await annotate(page, [
    { selector: '.appmenu-trigger:has-text("Session")', label: 1, place: 'above' },
    { selector: '.appmenu-trigger:has-text("Terminal")', label: 2, place: 'above', gap: 44 },
    { selector: '.appmenu-trigger:has-text("View")', label: 3, place: 'above' },
    { selector: '.appmenu-trigger:has-text("Automation")', label: 4, place: 'above', gap: 44 },
    { selector: '[data-tasks-open]', label: 5 },
    { selector: '[data-copilot-toggle]', label: 6, place: 'above' },
    { selector: '[data-command-palette-open]', label: 7 },
    { selector: '.appmenu-end .appmenu-trigger', label: 8, place: 'above' },
    { selector: '[data-chrome-toggle]', label: 9 },
  ]);
  await shotRegion(page, 'toolbar-real.png', ['.appbar'], 24);
  await clearAnnotations(page);
  await setDistractionsHidden(page, false);

  /* ---- Recording + playback controls ----------------------------- */
  const fileInput = page.locator('input[name="workflow"]');
  if (await fileInput.count()) {
    await fileInput.setInputFiles(workflowFile);
    await page.waitForURL(/\/screen/, { timeout: 20000 });
    await page.waitForSelector('[data-appbar]', { timeout: 15000 });
    await ready(page);
    // Loading a recording navigates, which drops the injected style tag.
    // The workspace mode survives (it is stored per browser), but re-assert
    // it so the shot does not depend on that.
    await suppressToasts(page);
    await setWorkspaceMode(page, 'engineering');
  }
  await settle(page, 400);
  await openMenu(page, 'Automation');
  await dismissTooltips(page);
  await annotate(page, [
    { selector: '[data-recording-start] button[type=submit]', label: 1, place: 'right' },
    { selector: 'form[action="/workflow/play"] button', label: 2, place: 'right' },
    { selector: 'form[action="/workflow/debug"] button', label: 3, place: 'right' },
    { selector: '.workflow-controls [data-modal-open]', label: 4, place: 'right' },
    { selector: 'form[action="/workflow/remove"] button', label: 5, place: 'right' },
    { selector: '[data-wizard-open]', label: 6, place: 'right' },
  ]);
  await shotRegion(
    page,
    'workflow-controls-real.png',
    ['.appbar', '[data-app-menu].is-open [data-app-menu-panel]'],
    24
  );
  await clearAnnotations(page);
  await closeMenus(page);

  /* ---- Settings modal -------------------------------------------- */
  await openAccountMenu(page);
  await page.locator('[data-settings-open]').click();
  await page.waitForSelector('[data-settings-modal]:not([hidden])');
  await page.waitForSelector('.settings-tab');
  await settle(page, 700);
  await dismissTooltips(page);
  await annotate(page, [
    { selector: '[data-settings-refresh]', label: 1, place: 'below' },
    { selector: '[data-settings-maximize]', label: 2, place: 'below' },
    // NB: the first [data-settings-close] in the DOM is the full-screen
    // backdrop div, so scope this to the header button.
    { selector: 'button[data-settings-close]', label: 3, place: 'below' },
    { selector: '.settings-tab', label: 4, place: 'below', gap: 12 },
    { selector: '.settings-group.is-active .settings-group-reset', label: 5, place: 'left' },
    { selector: '.settings-group.is-active .settings-subgroup', label: 6, place: 'left' },
    { selector: '[data-settings-save]', label: 7, place: 'above' },
  ]);
  await shotRegion(page, 'settings-modal-real.png', '.settings-modal-content', 20);
  await clearAnnotations(page);
  await page.locator('.settings-modal-actions [data-settings-close]').click();
  await settle(page, 500);

  /* ---- Logs modal ------------------------------------------------- */
  const logsBtn = page.locator('[data-logs-open]').first();
  if ((await logsBtn.count()) && (await logsBtn.isEnabled())) {
    await openAccountMenu(page);
    await logsBtn.click();
    const shown = await page
      .waitForSelector('.logs-modal:not([hidden]) .logs-modal-content', { timeout: 6000 })
      .catch(() => null);
    if (shown) {
      await settle(page, 700);
      await shotRegion(page, 'logging_image.png', '.logs-modal-content', 20);
      await page.keyboard.press('Escape');
      await settle(page, 400);
    } else {
      console.warn('  logs modal did not open (is "Allow log access" enabled?)');
    }
  }

  /* ---- Copilot side panel ----------------------------------------- */
  // The panel is 100vh. Signed out it holds only a header, the example
  // prompts and the sign-in call to action, so a tall viewport captures
  // mostly empty space — shorten it just for this shot.
  await page.setViewportSize({ width: VIEWPORT.width, height: 720 });
  await settle(page, 400);
  await page.locator('[data-copilot-toggle]').click();
  await page.waitForSelector('#copilot-panel:not([hidden])', { timeout: 5000 });
  await settle(page, 900);
  await shotRegion(page, 'copilot-panel.png', '#copilot-panel', 0);

  /* ---- AI provider dialog ------------------------------------------ */
  // Shown on Claude rather than the Copilot default: Copilot's row is just a
  // sign-in button, whereas a key-based provider exercises every field the
  // dialog can show (endpoint, key, model).
  await page.locator('[data-copilot-settings]').click();
  await page.waitForSelector('[data-ai-modal]:not([hidden])', { timeout: 5000 });
  await page.selectOption('[data-ai-provider]', 'anthropic');
  await dismissTooltips(page);
  await settle(page, 400);
  await shotRegion(page, 'ai-provider-dialog.png', '.ai-provider-modal', 16);
  await page.locator('[data-ai-cancel]').click();
  await settle(page, 300);

  await page.locator('[data-copilot-close]').click();
  await settle(page, 400);
  await page.setViewportSize(VIEWPORT);
  await settle(page, 400);

  /* ---- Virtual keypad --------------------------------------------- */
  await page.evaluate(() => {
    const keypad = document.getElementById('keypad');
    if (keypad) keypad.hidden = false;
    // "Full" shows the PF/PA/action groups the callouts describe. The stored
    // preference may be "max", whose whole-keyboard layout is far too wide to
    // read once scaled into the docs page. Set before rendering rather than
    // clicked afterwards: the size control steps in one direction, and the
    // step after "max" puts the keypad away.
    try {
      window.localStorage.setItem('h3270KeypadMode', 'full');
    } catch (err) {
      /* private mode; the keypad renders at its default size */
    }
    if (typeof window.renderKeypad === 'function') window.renderKeypad('keypad');
  });
  await page.waitForSelector('.h3270-keypad:not([hidden])');
  // The keypad is taller than the default viewport; grow it so the whole
  // control (and its callouts) fits in one clip.
  await page.setViewportSize({ width: VIEWPORT.width, height: 1250 });
  await settle(page, 500);
  await page.locator('.h3270-keypad').first().scrollIntoViewIfNeeded();
  await dismissTooltips(page);
  await setDistractionsHidden(page, true);
  await settle(page, 400);
  await annotate(page, [
    { selector: '.h3270-keypad-title', label: 1, place: 'right', gap: 14 },
    { selector: '.h3270-keypad-mode-btn', label: 2, place: 'above', gap: 12 },
    { selector: '.h3270-key[data-key="PF1"]', label: 3, place: 'left', gap: 12 },
    { selector: '.h3270-key[data-key="PA1"]', label: 4, place: 'above', gap: 12 },
    { selector: '.h3270-key[data-key="Enter"]', label: 5, place: 'below', gap: 12 },
  ]);
  await shotRegion(page, 'keypad-real.png', '.h3270-keypad', 24);
  await clearAnnotations(page);

  await browser.close();
  console.log('\nScreenshots written to', outDir);
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
