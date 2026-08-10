/**
 * Capture the 3270Web screenshots used by the 3270.io landing page.
 *
 * The MkDocs captures (capture-doc-screenshots.mjs) are annotated with
 * numbered callouts and cropped to individual controls, which is wrong for a
 * marketing gallery. These are the same UI, clean: no badges, no connectors,
 * framed on the surface the caption talks about.
 *
 * Usage:
 *   go run ./cmd/3270Web            # in another terminal
 *   node scripts/capture-landing-screenshots.mjs --out ../3270io/src/assets/shots/web
 *
 * Options (environment variables):
 *   SITE_BASE_URL    base URL of a running 3270Web (default http://127.0.0.1:3270)
 *   SITE_OUT_DIR     output directory (default <repo>/../3270io/src/assets/shots/web)
 *   SITE_SAMPLE_APP  sample app id    (default app1)
 *   SITE_SAMPLE_PORT sample app port  (default 3271 — 3270 is the web server's
 *                    own port, and the sample-app listener may not use it)
 */
import { chromium } from 'playwright';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { mkdir } from 'node:fs/promises';

const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(here, '..');

const argOut = (() => {
  const i = process.argv.indexOf('--out');
  return i >= 0 ? process.argv[i + 1] : null;
})();

const baseUrl = process.env.SITE_BASE_URL || 'http://127.0.0.1:3270';
const outDir = path.resolve(
  argOut || process.env.SITE_OUT_DIR || path.join(repoRoot, '..', '3270io', 'src', 'assets', 'shots', 'web')
);
const sampleApp = process.env.SITE_SAMPLE_APP || 'app1';
const samplePort = process.env.SITE_SAMPLE_PORT || '3271';

const VIEWPORT = { width: 1500, height: 940 };
const SCALE = 2;

const settle = (page, ms = 500) => page.waitForTimeout(ms);

/** Tippy leaves a tooltip behind after a programmatic click. */
async function dismissTooltips(page) {
  await page.mouse.move(4, 4);
  await page.evaluate(() => {
    document.querySelectorAll('[data-tippy-root]').forEach((el) => el.remove());
  });
  await settle(page, 150);
}

/** Toasts fire on their own schedule and would land in whatever is capturing. */
async function suppressToasts(page) {
  await page.addStyleTag({ content: '#h3270-notification-container{display:none !important}' });
}

/** Wait for webfonts so terminal glyph metrics are final before capturing. */
async function ready(page) {
  await page.evaluate(() => document.fonts && document.fonts.ready);
  await settle(page, 400);
}

async function setWorkspaceMode(page, mode) {
  await page.evaluate((next) => {
    const ws = window.ThreeSeventyWeb && window.ThreeSeventyWeb.workspace;
    if (ws && typeof ws.setMode === 'function') ws.setMode(next);
  }, mode);
  await settle(page, 500);
}

async function closeMenus(page) {
  await page.evaluate(() => {
    const api = window.ThreeSeventyWeb;
    if (api && api.menuBar) api.menuBar.close();
  });
  await settle(page, 250);
}

/**
 * Screenshot the union of the given selectors, plus padding.
 *
 * `padTop` overrides the padding above the region. The session surface stacks
 * its chrome tightly, so a symmetric pad that frames the sides comfortably
 * will also drag a sliver of the row above into the shot.
 */
async function shotRegion(page, name, selectors, pad = 24, padTop = pad) {
  const list = Array.isArray(selectors) ? selectors : [selectors];
  const boxes = [];
  for (const sel of list) {
    const loc = page.locator(sel).first();
    if (!(await loc.count())) continue;
    const b = await loc.boundingBox();
    if (b) boxes.push(b);
  }
  if (!boxes.length) {
    // A renamed class silently turning a tight crop into a whole browser
    // window is exactly the failure that puts a bad image on the landing
    // page, so fail the run rather than write one.
    throw new Error(`${name}: no selector matched (${list.join(', ')})`);
  }
  const vp = page.viewportSize() || VIEWPORT;
  const x = Math.max(0, Math.min(...boxes.map((b) => b.x)) - pad);
  const y = Math.max(0, Math.min(...boxes.map((b) => b.y)) - padTop);
  const right = Math.min(vp.width, Math.max(...boxes.map((b) => b.x + b.width)) + pad);
  const bottom = Math.min(vp.height, Math.max(...boxes.map((b) => b.y + b.height)) + pad);
  await page.screenshot({
    path: path.join(outDir, name),
    clip: { x, y, width: Math.max(1, right - x), height: Math.max(1, bottom - y) },
  });
  console.log('  wrote', name);
}

async function main() {
  await mkdir(outDir, { recursive: true });

  const browser = await chromium.launch();
  const context = await browser.newContext({ viewport: VIEWPORT, deviceScaleFactor: SCALE });
  const page = await context.newPage();
  page.on('pageerror', (e) => console.warn('  page error:', e.message));

  /* ---- Connect page ---------------------------------------------- */
  await page.goto(baseUrl, { waitUntil: 'domcontentloaded' });
  await settle(page, 900);
  await ready(page);
  await shotRegion(page, 'connect.png', '.card', 36);

  /* ---- Connect and land on the session ---------------------------- */
  await page.fill('#hostname-input', `sampleapp:${sampleApp}:${samplePort}`);
  await page.click('#connect-btn');
  await page.waitForURL(/\/screen/, { timeout: 40000 });
  await page.waitForSelector('[data-appbar]', { timeout: 15000 });
  await ready(page);
  await suppressToasts(page);

  /* ---- Session screen ---------------------------------------------- */
  // Engineering mode: the gallery caption points at the automation controls,
  // and the Business surface hides that menu entirely.
  await setWorkspaceMode(page, 'engineering');
  await closeMenus(page);
  await dismissTooltips(page);
  await shotRegion(page, 'session.png', '.card', 24);

  /* ---- The sample application, terminal only ----------------------- */
  // The sample-app chip sits just above the terminal, close enough that a
  // plain crop of .terminal-shell slices it in half. Frame it deliberately
  // instead: it names the app the caption is talking about.
  await shotRegion(page, 'sample-app.png', ['[data-sample-status]', '.terminal-shell'], 24, 10);

  /* ---- AI Chat side panel ------------------------------------------ */
  // The panel is 100vh. Signed out it holds a header, the example prompts and
  // a sign-in call to action, so a tall viewport captures mostly empty space.
  await page.setViewportSize({ width: VIEWPORT.width, height: 720 });
  await settle(page, 400);
  await page.locator('[data-copilot-toggle]').click();
  await page.waitForSelector('#copilot-panel:not([hidden])', { timeout: 5000 });
  await settle(page, 900);
  await shotRegion(page, 'ai-chat.png', '#copilot-panel', 0);

  /* ---- AI provider dialog ------------------------------------------- */
  // Shown on Claude rather than the Copilot default: Copilot's row is just a
  // sign-in button, whereas a key-based provider exercises every field the
  // dialog can show (endpoint, key, model).
  await page.locator('[data-copilot-settings]').click();
  await page.waitForSelector('[data-ai-modal]:not([hidden])', { timeout: 5000 });
  await page.selectOption('[data-ai-provider]', 'anthropic');
  await dismissTooltips(page);
  await settle(page, 400);
  await shotRegion(page, 'ai-provider.png', '.ai-provider-modal', 16);

  await browser.close();
  console.log('\nScreenshots written to', outDir);
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
