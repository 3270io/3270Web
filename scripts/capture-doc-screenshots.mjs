/**
 * Capture the screenshots used by the MkDocs site (docs/images).
 *
 * Usage:
 *   go run ./cmd/3270Web           # in another terminal
 *   node scripts/capture-doc-screenshots.mjs
 *
 * Options (environment variables):
 *   DOCS_BASE_URL   base URL of a running 3270Web (default http://127.0.0.1:8080)
 *   DOCS_OUT_DIR    output directory       (default <repo>/docs/images)
 *   DOCS_SAMPLE_APP sample app id          (default app1)
 *   DOCS_SAMPLE_PORT sample app port       (default 3270)
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

const baseUrl = process.env.DOCS_BASE_URL || 'http://127.0.0.1:8080';
const outDir = process.env.DOCS_OUT_DIR || path.join(repoRoot, 'docs', 'images');
const sampleApp = process.env.DOCS_SAMPLE_APP || 'app1';
const samplePort = process.env.DOCS_SAMPLE_PORT || '3270';
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
    const ids = ['.bg-overlay', '[data-status-widget]', '.terminal-tools-widget'];
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

async function ensureScreen(page) {
  await page.goto(baseUrl, { waitUntil: 'domcontentloaded' });
  await settle(page, 800);
  if (/\/screen(?:$|\?)/.test(page.url())) {
    await page.waitForSelector('[data-settings-open]', { timeout: 15000 });
    return;
  }
  await page.fill('#hostname-input', `sampleapp:${sampleApp}:${samplePort}`);
  await page.click('#connect-btn');
  await page.waitForURL(/\/screen/, { timeout: 40000 });
  await page.waitForSelector('[data-settings-open]', { timeout: 15000 });
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
  await shotRegion(page, 'yorkshire_image.png', '.card', 24);
  await shotRegion(page, 'sampleapp1_image.png', '.terminal-shell', 24);

  /* ---- Terminal status bar (OIA) --------------------------------- */
  await shotRegion(page, 'terminal-status-bar.png', '.screen-status-line', 14);

  /* ---- Command palette ------------------------------------------- */
  await page.keyboard.press('Control+k');
  await page.waitForSelector('.cmdk-panel', { timeout: 5000 });
  await page.fill('.cmdk-input', 'chaos');
  await settle(page, 500);
  await shotRegion(page, 'command-palette.png', '.cmdk-panel', 20);
  await page.keyboard.press('Escape');
  await settle(page, 400);

  /* ---- Toolbar callouts ------------------------------------------ */
  // Expand both collapsible groups so every control is visible.
  for (const sel of ['[data-recording-toggle]', '[data-chaos-toggle]']) {
    const t = page.locator(sel).first();
    if ((await t.count()) && (await t.getAttribute('aria-expanded')) === 'false') {
      await t.click();
    }
  }
  await settle(page, 600);
  await dismissTooltips(page);
  await setDistractionsHidden(page, true);
  await annotate(page, [
    { selector: '[data-disconnect-open]', label: 1, place: 'above' },
    { selector: '[data-logs-open]', label: 2 },
    { selector: '[data-print-screen]', label: 3, place: 'above' },
    { selector: '[data-recording-toggle]', label: 4 },
    { selector: '[data-chaos-toggle]', label: 5, place: 'above' },
    { selector: '[data-command-palette-open]', label: 6 },
    { selector: '[data-copilot-toggle]', label: 7, place: 'above' },
    { selector: '[data-settings-open]', label: 8 },
  ]);
  await shotRegion(page, 'toolbar-real.png', ['.card-header', '[data-main-toolbar]'], 24);
  await clearAnnotations(page);
  await setDistractionsHidden(page, false);

  /* ---- Recording + playback controls ----------------------------- */
  const fileInput = page.locator('input[name="workflow"]');
  if (await fileInput.count()) {
    await fileInput.setInputFiles(workflowFile);
    await page.waitForURL(/\/screen/, { timeout: 20000 });
    await page.waitForSelector('[data-modal-open]', { timeout: 15000 });
    await ready(page);
    // Loading a recording navigates, which drops the injected style tag.
    await suppressToasts(page);
    const rec = page.locator('[data-recording-toggle]').first();
    if ((await rec.count()) && (await rec.getAttribute('aria-expanded')) === 'false') {
      await rec.click();
      await settle(page, 500);
    }
  }
  await dismissTooltips(page);
  await annotate(page, [
    { selector: '[data-recording-start] button[type=submit]', label: 1, place: 'above' },
    { selector: 'form[action="/workflow/play"] button', label: 2 },
    { selector: 'form[action="/workflow/debug"] button', label: 3, place: 'above' },
    { selector: '.workflow-controls [data-modal-open]', label: 4 },
    { selector: 'form[action="/workflow/remove"] button', label: 5, place: 'above' },
    { selector: '[data-status-widget]', label: 6, place: 'left' },
  ]);
  await shotRegion(page, 'workflow-controls-real.png', ['[data-main-toolbar]', '[data-status-widget]'], 24);
  await clearAnnotations(page);

  /* ---- Settings modal -------------------------------------------- */
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
  await page.locator('[data-copilot-close]').click();
  await settle(page, 400);
  await page.setViewportSize(VIEWPORT);
  await settle(page, 400);

  /* ---- Virtual keypad --------------------------------------------- */
  await page.evaluate(() => {
    const keypad = document.getElementById('keypad');
    if (keypad) keypad.hidden = false;
    if (typeof window.renderKeypad === 'function') window.renderKeypad('keypad');
  });
  await page.waitForSelector('.h3270-keypad:not([hidden])');
  // The keypad is taller than the default viewport; grow it so the whole
  // control (and its callouts) fits in one clip.
  await page.setViewportSize({ width: VIEWPORT.width, height: 1250 });
  await settle(page, 500);
  // "Full" shows the PF/PA/action groups the callouts describe. The stored
  // preference may be "max", whose full QWERTY layout is far too wide and
  // tall to read once scaled into the docs page.
  const fullMode = page.locator('.h3270-keypad-mode-btn[data-mode="full"]').first();
  if (await fullMode.count()) {
    await fullMode.click();
    await settle(page, 500);
  }
  await page.locator('.h3270-keypad').first().scrollIntoViewIfNeeded();
  await dismissTooltips(page);
  await setDistractionsHidden(page, true);
  await settle(page, 400);
  await annotate(page, [
    { selector: '.h3270-keypad-title', label: 1, place: 'right', gap: 14 },
    { selector: '.h3270-keypad-mode-btn[data-mode="compact"]', label: 2, place: 'above', gap: 12 },
    { selector: '.h3270-keypad-hide-btn', label: 3, place: 'above', gap: 12 },
    { selector: '.h3270-key[data-key="PF1"]', label: 4, place: 'left', gap: 12 },
    { selector: '.h3270-key[data-key="PA1"]', label: 5, place: 'above', gap: 12 },
    { selector: '.h3270-key[data-key="Enter"]', label: 6, place: 'below', gap: 12 },
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
