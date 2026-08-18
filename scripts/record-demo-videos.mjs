/**
 * Record the demonstration videos used by the MkDocs site (docs/videos).
 *
 * Each scenario drives a live 3270Web session against a bundled sample app
 * and records it as a .webm, with overlay text (title cards, lower-third
 * captions, a visible cursor, highlight rings) injected into the page so the
 * narration is baked into the recording — no editing pass required.
 *
 * Usage:
 *   go run ./cmd/3270Web                     # in another terminal
 *   node scripts/record-demo-videos.mjs      # record every scenario
 *   node scripts/record-demo-videos.mjs howto-connect showcase-tour
 *
 * Options (environment variables):
 *   DEMO_BASE_URL     base URL of a running 3270Web (default http://127.0.0.1:3270)
 *   DEMO_OUT_DIR      output directory       (default <repo>/docs/videos)
 *   DEMO_SAMPLE_APP   sample app id          (default app1)
 *   DEMO_SAMPLE_PORT  sample app port        (default 3271 — 3270 is the web
 *                     server's own port and is not in the sample-app allow list)
 *   DEMO_CHROMIUM     path to a Chromium executable, for environments where
 *                     the installed Playwright package and the pre-installed
 *                     browser revisions do not match
 *
 * Notes
 *   - The viewport is 16:9 and the video is recorded at the same size, so
 *     frames are pixel-for-pixel with no scaling blur.
 *   - Overlays live in a single #demo-overlay-root element. Navigation (every
 *     Enter key in a 3270 session navigates) wipes it, so every helper
 *     re-installs the overlay before touching it.
 *   - The synthetic cursor exists because a screen recording of a headless
 *     browser has no pointer: clicks would read as the UI acting by itself.
 *     Its last position is kept on the Node side and restored after each
 *     navigation so it never appears to teleport.
 */
import { chromium } from 'playwright';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { mkdir, rm } from 'node:fs/promises';

const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(here, '..');

const baseUrl = process.env.DEMO_BASE_URL || 'http://127.0.0.1:3270';
const outDir = process.env.DEMO_OUT_DIR || path.join(repoRoot, 'docs', 'videos');
const sampleApp = process.env.DEMO_SAMPLE_APP || 'petstore';
const samplePort = process.env.DEMO_SAMPLE_PORT || '3271';

const VIEWPORT = { width: 1600, height: 900 };

/* ------------------------------------------------------------------ */
/* Overlay + pacing helpers                                            */
/* ------------------------------------------------------------------ */

/**
 * Injected verbatim into the page. Builds (or rebuilds, after navigation)
 * the overlay root: a style tag, the lower-third caption, the title card and
 * the synthetic cursor. Everything is pointer-events:none so overlays can
 * never swallow the clicks they are narrating.
 */
const OVERLAY_BOOTSTRAP = `(() => {
  if (document.getElementById('demo-overlay-root')) return;
  const style = document.createElement('style');
  style.id = 'demo-overlay-style';
  style.textContent = \`
    #demo-overlay-root, #demo-overlay-root * { box-sizing: border-box; margin: 0; }
    #demo-overlay-root { position: fixed; inset: 0; z-index: 2147483000; pointer-events: none;
      font-family: -apple-system, "Segoe UI", Roboto, Helvetica, Arial, sans-serif; }
    .demo-caption { position: absolute; left: 50%; bottom: 36px; transform: translateX(-50%) translateY(16px);
      max-width: 72%; padding: 14px 26px; border-radius: 10px;
      background: rgba(6, 14, 9, 0.88); border: 1px solid rgba(63, 185, 80, 0.45);
      box-shadow: 0 8px 32px rgba(0, 0, 0, 0.55); opacity: 0;
      transition: opacity 320ms ease, transform 320ms ease; text-align: center; }
    .demo-caption.is-on { opacity: 1; transform: translateX(-50%) translateY(0); }
    .demo-caption.is-top { bottom: auto; top: 84px; transform: translateX(-50%) translateY(-16px); }
    .demo-caption.is-top.is-on { transform: translateX(-50%) translateY(0); }
    .demo-caption .demo-kicker { display: block; font-size: 12px; font-weight: 700;
      letter-spacing: 0.18em; text-transform: uppercase; color: #3fb950; margin-bottom: 5px; }
    .demo-caption .demo-text { display: block; font-size: 21px; font-weight: 600;
      line-height: 1.35; color: #f0f6f0; }
    .demo-title { position: absolute; inset: 0; display: flex; flex-direction: column;
      align-items: center; justify-content: center; gap: 14px;
      background: radial-gradient(ellipse at center, rgba(8, 20, 12, 0.94), rgba(2, 6, 4, 0.97));
      opacity: 0; transition: opacity 420ms ease; }
    .demo-title.is-on { opacity: 1; }
    .demo-title .demo-kicker { font-size: 15px; font-weight: 700; letter-spacing: 0.28em;
      text-transform: uppercase; color: #3fb950; }
    .demo-title .demo-big { font-size: 52px; font-weight: 800; color: #f0f6f0;
      letter-spacing: 0.01em; text-align: center; max-width: 80%; line-height: 1.15; }
    .demo-title .demo-sub { font-size: 21px; font-weight: 500; color: #9fb8a6;
      text-align: center; max-width: 70%; line-height: 1.4; }
    .demo-cursor { position: absolute; width: 26px; height: 26px; border-radius: 50%;
      border: 2.5px solid rgba(255, 255, 255, 0.95); background: rgba(63, 185, 80, 0.55);
      box-shadow: 0 2px 10px rgba(0, 0, 0, 0.6), 0 0 0 3px rgba(6, 14, 9, 0.35);
      transform: translate(-50%, -50%);
      transition: left 560ms cubic-bezier(0.22, 0.61, 0.36, 1), top 560ms cubic-bezier(0.22, 0.61, 0.36, 1); }
    .demo-cursor.is-clicking { animation: demo-click 360ms ease; }
    @keyframes demo-click {
      0% { box-shadow: 0 0 0 0 rgba(63, 185, 80, 0.75), 0 2px 10px rgba(0,0,0,0.6); }
      100% { box-shadow: 0 0 0 26px rgba(63, 185, 80, 0), 0 2px 10px rgba(0,0,0,0.6); }
    }
    .demo-ring { position: absolute; border: 3px solid #3fb950; border-radius: 10px;
      box-shadow: 0 0 0 4px rgba(63, 185, 80, 0.25), 0 0 24px rgba(63, 185, 80, 0.35);
      animation: demo-ring-pulse 1.4s ease-in-out infinite; }
    @keyframes demo-ring-pulse {
      0%, 100% { box-shadow: 0 0 0 4px rgba(63, 185, 80, 0.25), 0 0 24px rgba(63, 185, 80, 0.35); }
      50% { box-shadow: 0 0 0 8px rgba(63, 185, 80, 0.12), 0 0 32px rgba(63, 185, 80, 0.5); }
    }
  \`;
  document.head.appendChild(style);

  const root = document.createElement('div');
  root.id = 'demo-overlay-root';
  root.innerHTML =
    '<div class="demo-title" data-demo-title>' +
    '  <span class="demo-kicker" data-demo-title-kicker></span>' +
    '  <span class="demo-big" data-demo-title-big></span>' +
    '  <span class="demo-sub" data-demo-title-sub></span>' +
    '</div>' +
    '<div class="demo-caption" data-demo-caption>' +
    '  <span class="demo-kicker" data-demo-caption-kicker></span>' +
    '  <span class="demo-text" data-demo-caption-text></span>' +
    '</div>' +
    '<div class="demo-cursor" data-demo-cursor style="left:-60px;top:-60px"></div>';
  document.body.appendChild(root);
})();`;

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

/**
 * Director wraps a page with the overlay vocabulary the scenarios speak:
 * title cards, captions, a moving cursor, highlight rings, paced typing.
 */
class Director {
  constructor(page) {
    this.page = page;
    this.cursor = { x: VIEWPORT.width / 2, y: VIEWPORT.height - 80 };
  }

  /** (Re)install the overlay and restore the cursor's last position. */
  async ensure() {
    await this.page.evaluate(OVERLAY_BOOTSTRAP);
    await this.page.evaluate(({ x, y }) => {
      const c = document.querySelector('[data-demo-cursor]');
      c.style.transition = 'none';
      c.style.left = x + 'px';
      c.style.top = y + 'px';
      // Force a reflow so the transition removal applies before it returns.
      void c.offsetWidth;
      c.style.transition = '';
    }, this.cursor);
  }

  /** Full-screen card: fade in, hold, fade out. A closing card should pass
   * fadeOut: false so it stays up until the recording stops, rather than
   * flashing the UI for two frames on its way out. */
  async title(kicker, big, sub, holdMs = 2600, { fadeOut = true } = {}) {
    await this.ensure();
    await this.page.evaluate(({ kicker, big, sub }) => {
      document.querySelector('[data-demo-title-kicker]').textContent = kicker || '';
      document.querySelector('[data-demo-title-big]').textContent = big || '';
      document.querySelector('[data-demo-title-sub]').textContent = sub || '';
      document.querySelector('[data-demo-title]').classList.add('is-on');
      // A pointer has no business on a title card.
      document.querySelector('[data-demo-cursor]').style.opacity = '0';
    }, { kicker, big, sub });
    await sleep(holdMs);
    if (!fadeOut) return;
    await this.page.evaluate(() => {
      document.querySelector('[data-demo-title]').classList.remove('is-on');
      document.querySelector('[data-demo-cursor]').style.opacity = '';
    });
    await sleep(520);
  }

  /** Lower-third caption. Stays up until replaced or cleared. Pass
   * pos: 'top' when the caption would sit on the thing it talks about
   * (the OIA lives exactly where the lower third goes). */
  async caption(kicker, text, holdMs = 1500, { pos = 'bottom' } = {}) {
    await this.ensure();
    await this.page.evaluate(({ kicker, text, pos }) => {
      const box = document.querySelector('[data-demo-caption]');
      const kickerEl = document.querySelector('[data-demo-caption-kicker]');
      kickerEl.textContent = kicker || '';
      kickerEl.style.display = kicker ? '' : 'none';
      document.querySelector('[data-demo-caption-text]').textContent = text;
      box.classList.remove('is-on');
      box.classList.toggle('is-top', pos === 'top');
      void box.offsetWidth;
      box.classList.add('is-on');
    }, { kicker, text, pos });
    await sleep(holdMs);
  }

  async clearCaption() {
    await this.ensure();
    await this.page.evaluate(() => {
      document.querySelector('[data-demo-caption]').classList.remove('is-on');
    });
    await sleep(400);
  }

  /** Glide the synthetic cursor to a point, mirroring the real mouse. */
  async cursorToPoint(x, y) {
    await this.ensure();
    await this.page.evaluate(({ x, y }) => {
      const c = document.querySelector('[data-demo-cursor]');
      c.style.left = x + 'px';
      c.style.top = y + 'px';
    }, { x, y });
    await this.page.mouse.move(x, y, { steps: 12 });
    this.cursor = { x, y };
    await sleep(640);
  }

  async cursorTo(selector) {
    const box = await this.#box(selector);
    await this.cursorToPoint(box.x + box.width / 2, box.y + box.height / 2);
  }

  /** Move to the element, pulse, then really click it. The click goes
   * through the locator (not raw mouse coordinates) so Playwright waits for
   * the element to stop moving — a menu item mid-slide swallows a raw
   * mousedown without a trace. */
  async click(selector) {
    await this.cursorTo(selector);
    await this.page.evaluate(() => {
      const c = document.querySelector('[data-demo-cursor]');
      c.classList.remove('is-clicking');
      void c.offsetWidth;
      c.classList.add('is-clicking');
    });
    await sleep(220);
    await this.page.locator(selector).first().click({ timeout: 10000 });
    await sleep(320);
  }

  /** Click a field, replace whatever it holds, type at a human pace. */
  async typeInto(selector, text, delay = 75) {
    await this.click(selector);
    // The connect page pre-fills the hostname with the last host used, so
    // typing without selecting first would append to it.
    await this.page.keyboard.press('Control+a');
    await this.page.keyboard.type(text, { delay });
    await sleep(300);
  }

  /** Pulsing ring around an element. One ring at a time. */
  async highlight(selector, pad = 8) {
    const box = await this.#box(selector);
    await this.ensure();
    await this.page.evaluate(({ box, pad }) => {
      document.querySelectorAll('.demo-ring').forEach((el) => el.remove());
      const ring = document.createElement('div');
      ring.className = 'demo-ring';
      Object.assign(ring.style, {
        left: box.x - pad + 'px',
        top: box.y - pad + 'px',
        width: box.width + pad * 2 + 'px',
        height: box.height + pad * 2 + 'px',
      });
      document.getElementById('demo-overlay-root').appendChild(ring);
    }, { box, pad });
  }

  async clearHighlight() {
    await this.page.evaluate(() => {
      document.querySelectorAll('.demo-ring').forEach((el) => el.remove());
    });
  }

  async pause(ms) {
    await sleep(ms);
  }

  async #box(selector) {
    const loc = this.page.locator(selector).first();
    await loc.waitFor({ state: 'visible', timeout: 10000 });
    const box = await loc.boundingBox();
    if (!box) throw new Error(`no bounding box for ${selector}`);
    return box;
  }
}

/* ------------------------------------------------------------------ */
/* Shared page plumbing                                                */
/* ------------------------------------------------------------------ */

async function fontsReady(page) {
  await page.evaluate(() => document.fonts && document.fonts.ready);
  await sleep(400);
}

async function waitForScreen(page) {
  await page.waitForURL(/\/screen/, { timeout: 40000 });
  await page.waitForSelector('[data-appbar]', { timeout: 15000 });
  await fontsReady(page);
}

/** Land on the connect page, ready to film (an old session is torn down). */
async function openConnectPage(page) {
  await page.goto(baseUrl, { waitUntil: 'domcontentloaded' });
  await sleep(600);
  if (/\/screen/.test(page.url())) {
    await page.goto(baseUrl + '/disconnect', { waitUntil: 'domcontentloaded' }).catch(() => {});
    await page.goto(baseUrl, { waitUntil: 'domcontentloaded' });
    await sleep(600);
  }
  await fontsReady(page);
}

/** Connect without ceremony, for scenarios that start inside a session. */
async function connectQuietly(page) {
  await openConnectPage(page);
  await page.fill('#hostname-input', `sampleapp:${sampleApp}:${samplePort}`);
  await page.click('#connect-btn');
  await waitForScreen(page);
}

/**
 * Business mode hides the Automation menu; scenarios about recording and
 * chaos have to ask for the Engineering surface first.
 */
async function setWorkspaceMode(page, mode) {
  await page.evaluate((next) => {
    const ws = window.ThreeSeventyWeb && window.ThreeSeventyWeb.workspace;
    if (ws && typeof ws.setMode === 'function') ws.setMode(next);
  }, mode);
  await sleep(600);
}

/** Unprotected 3270 fields render as inputs named field_<row>_<index>. */
const FIELDS = '.terminal-shell input[name^="field_"]';

/**
 * Wait until the terminal shows text matching the pattern. Screen submits
 * are asynchronous — the URL never changes — so waiting on navigation is a
 * no-op and pacing alone is a guess. This is the deterministic wait.
 */
async function waitForTerminalText(page, pattern, timeout = 20000) {
  await page.waitForFunction(
    (src) => new RegExp(src).test(document.querySelector('.terminal-shell')?.innerText || ''),
    pattern,
    { timeout }
  );
}

/**
 * Sign on to the Pet Store sample. Field order on PET010 is user id then
 * password (the password is display-suppressed, exactly as a host would).
 */
async function signOn(page, d) {
  await d.typeInto(`${FIELDS} >> nth=0`, 'PETOP1');
  await page.keyboard.press('Tab');
  await page.keyboard.type('PAWS4CLAWS', { delay: 75 });
  await d.pause(400);
  await page.keyboard.press('Enter');
  await waitForScreen(page);
  await waitForTerminalText(page, 'MAIN MENU');
}

/** Type a command on the command line (the last input on every screen). */
async function command(page, d, text) {
  await d.typeInto(`${FIELDS} >> nth=-1`, text);
  await d.pause(300);
  await page.keyboard.press('Enter');
  await waitForScreen(page);
}

/* ------------------------------------------------------------------ */
/* Scenarios                                                           */
/* ------------------------------------------------------------------ */

const scenarios = {
  /* A ~70-second showcase: connect, sign on, work the screens, palette,
   * automation. The pitch is breadth; each how-to covers depth. */
  'showcase-tour': async (page, d) => {
    await openConnectPage(page);
    await d.title('3270Web', 'A 3270 terminal in your browser', 'Open source · nothing to install', 3000);

    await d.caption('Connect', 'Point it at any TN3270 host — or at the Pet Store sample app that ships in the box.', 900);
    await d.typeInto('#hostname-input', `sampleapp:${sampleApp}:${samplePort}`);
    await d.pause(500);
    await d.click('#connect-btn');
    await waitForScreen(page);

    await d.caption('Live session', 'A real 3270 sign-on screen, rendered as a live web page.', 2000);
    await d.caption('Live session', 'Type straight into the fields — the password field is display-suppressed, like the real thing.', 700);
    await signOn(page, d);

    await d.caption('Main menu', 'Colours, intensity and field attributes all come from the host data stream.', 2600);
    await d.caption('Command line', 'Every screen has a command line — jump to the stock catalogue with one word.', 800);
    await command(page, d, 'STOCK');
    await d.caption('Stock catalogue', 'Scrollable lists, PF keys, protected and unprotected fields — standard terminal behaviour.', 2600);

    await d.highlight('[data-screen-oia]');
    await d.caption('Status bar', 'The operator information area tracks connection, model and cursor, like the hardware did.', 2600, { pos: 'top' });
    await d.clearHighlight();

    await page.keyboard.press('Control+k');
    const palette = await page.waitForSelector('.cmdk-panel', { timeout: 5000 }).catch(() => null);
    if (palette) {
      await d.caption('Command palette', 'Ctrl+K reaches every 3270Web command from the keyboard.', 800);
      await page.keyboard.type('theme', { delay: 90 });
      await d.pause(1700);
      await page.keyboard.press('Escape');
      await d.pause(400);
    }

    await setWorkspaceMode(page, 'engineering');
    await d.caption('Automation', 'An Engineering surface adds recording, replay and chaos exploration.', 800);
    await d.click('.appmenu-trigger:has-text("Automation")');
    await d.pause(2600);
    await page.keyboard.press('Escape');
    await d.pause(300);

    await d.clearCaption();
    await d.title('3270Web', 'github.com/3270io/3270Web', 'Docs, Docker image and Windows build in the repo', 3000, { fadeOut: false });
  },

  /* How-to: the first thing every new user does. */
  'howto-connect': async (page, d) => {
    await openConnectPage(page);
    await d.title('How to', 'Connect to a host', 'Sample apps included — try it with nothing else on the network', 2800);

    await d.caption('Step 1', 'Enter the host address as hostname:port. The bundled sample apps work out of the box.', 1100);
    await d.typeInto('#hostname-input', `sampleapp:${sampleApp}:${samplePort}`);
    await d.pause(600);

    await d.caption('Step 2', 'Click Connect.', 700);
    await d.click('#connect-btn');
    await waitForScreen(page);

    await d.caption('Connected', 'The host’s first screen appears, and the session gets a tab of its own.', 2400);

    await d.highlight('[data-screen-oia]');
    await d.caption('Check the status bar', 'The operator information area confirms the connection, screen model and cursor position.', 2800, { pos: 'top' });
    await d.clearHighlight();

    await d.caption('Sign on', 'From here the session belongs to the host — sign on with any user id.', 800);
    await signOn(page, d);
    await d.caption('Signed on', 'The main menu is yours — options, a command line and live business totals.', 2600);
    await d.clearCaption();
    await d.title('', 'That’s it — you’re on the host', 'Keyboard, themes and printing are covered in the docs', 2600, { fadeOut: false });
  },

  /* How-to: record a workflow, then replay it hands-off. The recorded flow
   * is a closed loop (sign on → stock → sign off), so replay starts from
   * the same screen the recording started on. */
  'howto-workflow': async (page, d) => {
    await connectQuietly(page);
    await setWorkspaceMode(page, 'engineering');
    await d.title('How to', 'Record and replay a workflow', 'Capture a flow once, replay it hands-off', 2800);

    await d.caption('Step 1', 'Open the Automation menu and start recording.', 900);
    await d.click('.appmenu-trigger:has-text("Automation")');
    await d.pause(1100);
    await d.click('[data-recording-start] button[type="submit"]');
    await waitForScreen(page).catch(() => {});
    await d.caption('Recording', 'Recording is on — now just use the application as normal.', 1400);

    await signOn(page, d);
    await d.caption('Recording', 'Sign on, jump to the stock catalogue…', 600);
    await command(page, d, 'STOCK');
    await d.pause(900);
    await d.caption('Recording', '…and sign off again. Every screen and keystroke is captured.', 600);
    await page.keyboard.press('F3'); // back to the menu
    await waitForScreen(page);
    await page.keyboard.press('F3'); // sign off
    await waitForScreen(page);
    await d.pause(600);

    await d.caption('Step 2', 'Stop recording when the flow is complete.', 900);
    await d.click('.appmenu-trigger:has-text("Automation")');
    await d.pause(1100);
    await d.click('[data-recording-stop] button[type="submit"]');
    await sleep(1500);
    if (!/\/screen/.test(page.url())) await page.goto(baseUrl + '/screen', { waitUntil: 'domcontentloaded' });
    await page.waitForSelector('[data-appbar]', { timeout: 15000 });
    await fontsReady(page);

    // The stopped recording is offered as a JSON download. Take the real
    // file and feed it back through "Load a recording…" — the replay in the
    // video is the very flow recorded a moment earlier.
    const downloadPromise = page.waitForEvent('download');
    await d.highlight('.record-download-link');
    await d.caption('Step 3', 'The captured flow is saved as a JSON recording — download it to keep or share it.', 1800);
    await d.click('.record-download-link');
    const download = await downloadPromise;
    const recordingPath = path.join(outDir, '.tmp-howto-workflow-recording.json');
    await download.saveAs(recordingPath);
    await d.clearHighlight();
    await d.pause(600);

    await d.caption('Step 4', 'Load the recording to arm the player…', 900);
    await d.click('.appmenu-trigger:has-text("Automation")');
    await d.pause(1100);
    const chooserPromise = page.waitForEvent('filechooser');
    await d.click('[data-workflow-trigger]');
    await (await chooserPromise).setFiles(recordingPath);
    await waitForScreen(page);
    await rm(recordingPath, { force: true });

    await d.caption('Step 5', '…and play it back against the live host.', 900);
    await d.click('.appmenu-trigger:has-text("Automation")');
    await d.pause(1100);
    await d.click('form[action="/workflow/play"] button');
    await sleep(800);
    await d.caption('Replay', '3270Web replays every step against the host, screen by screen.', 9000);
    await waitForScreen(page).catch(() => {});
    await d.pause(2000);

    await d.clearCaption();
    await d.title('', 'Recorded once, replayed on demand', 'Workflows are JSON — replay them from the REST API too', 2800, { fadeOut: false });
  },

  /* How-to: chaos exploration. Chaos starts the moment it is asked for, so
   * the video is mostly footage of it driving the application, with the map
   * opened mid-run to show what it has learned. */
  'howto-chaos': async (page, d) => {
    await connectQuietly(page);
    await setWorkspaceMode(page, 'engineering');
    await d.title('How to', 'Explore an application with chaos', 'Let 3270Web drive the screens and map what it finds', 2800);

    await d.caption('Setup', 'Sign on first, so the exploration starts inside the application.', 800);
    await signOn(page, d);
    await d.pause(600);

    await d.caption('Step 1', 'Open Automation and start the exploration.', 900);
    await d.click('.appmenu-trigger:has-text("Automation")');
    await d.pause(1100);
    await d.click('[data-chaos-start]');
    await d.pause(1500);

    await d.highlight('[data-active-run-row], [data-active-run-container], [data-active-run-chip]', 6);
    await d.caption('Running', 'Chaos presses keys and fills fields on its own — the banner counts attempts, screens and transitions.', 3200, { pos: 'bottom' });
    await d.clearHighlight();
    await d.caption('Running', 'Every screen it reaches is added to a live map of the application.', 3600);
    await d.caption('Running', 'It favours keys it has not tried yet, and steers back toward screens with untried actions.', 3600);

    await d.caption('Step 2', 'Open the chaos map to see the structure it has discovered so far.', 900);
    await d.click('.appmenu-trigger:has-text("Automation")');
    await d.pause(1100);
    await d.click('[data-chaos-map-open]');
    await page.waitForSelector('[data-chaos-map-modal]:not([hidden]) .modal-chaos-map', { timeout: 8000 });
    await d.caption('Chaos map', 'Each discovered screen, its visit count, and hints you can add to steer the run.', 3400);
    const viewMap = page.locator('[data-chaos-map-modal]:not([hidden]) button:has-text("View map")');
    if (await viewMap.count()) {
      await d.click('[data-chaos-map-modal]:not([hidden]) button:has-text("View map")');
      // "View map" stacks a second modal (the flow graph) on top of the list.
      await page.waitForSelector('[data-chaos-flow-modal]:not([hidden])', { timeout: 8000 });
      await d.caption('Chaos map', 'The same discoveries as a graph — screens and the keys that join them.', 3200);
      await d.click('[data-chaos-flow-modal]:not([hidden]) button:has-text("Close")');
      await d.pause(400);
    }
    // Escape does not dismiss these modals; use their own Close buttons.
    await d.click('[data-chaos-map-modal]:not([hidden]) button:has-text("Close")');
    await d.pause(600);

    await d.caption('Step 3', 'Stop whenever you have seen enough — the run and its map are kept.', 900);
    await d.click('.appmenu-trigger:has-text("Automation")');
    await d.pause(1100);
    await d.click('[data-chaos-stop]');
    await d.pause(2200);

    // The report entry only appears once the run has stopped and its data
    // has synced back to the session — wait for the run to finalise rather
    // than pacing. The "exploration complete" toast that fires on that
    // transition steals focus and closes an open menu, so let it land first
    // and reopen the menu if it still got in the way.
    await page.waitForFunction(async () => {
      const r = await fetch('/chaos/status').then((res) => res.json()).catch(() => null);
      return !!(r && !r.active && String(r.loadedRunID || '').trim());
    }, { timeout: 30000, polling: 1500 });
    await d.pause(2500);
    await d.caption('Step 4', 'Open the discovery report — everything the run learned, as Markdown.', 900);
    for (let attempt = 0; attempt < 3; attempt++) {
      await d.click('.appmenu-trigger:has-text("Automation")');
      await d.pause(1100);
      if (await page.locator('[data-chaos-report]').first().isVisible().catch(() => false)) break;
    }
    await d.click('[data-chaos-report]');
    await page.waitForSelector('[data-chaos-report-modal]:not([hidden]) .chaos-report-markdown', { timeout: 20000 });
    await d.caption('Discovery report', 'Screens, working keys, field discoveries — and which keys are still untried on each screen.', 4200);
    await d.click('[data-chaos-report-modal]:not([hidden]) [data-chaos-report-close]');
    await d.pause(600);

    await d.clearCaption();
    await d.title('', 'An application map, hands-off', 'Resume to push the frontier further — or export the run as a workflow', 2800, { fadeOut: false });
  },

  /* How-to: themes and terminal fonts, via the Settings modal. */
  'howto-themes': async (page, d) => {
    await connectQuietly(page);
    await d.title('How to', 'Themes and terminal fonts', 'From green phosphor to amber to daylight', 2800);

    await d.caption('Setup', 'Sign on, so there is a full screen of colour to look at.', 800);
    await signOn(page, d);
    await d.pause(600);

    await d.caption('Step 1', 'Open Settings from the account menu.', 900);
    await d.click('.appmenu-end .appmenu-trigger');
    await d.pause(900);
    await d.click('[data-settings-open]');
    await page.waitForSelector('[data-settings-modal]:not([hidden])', { timeout: 8000 });
    await d.pause(700);

    await d.caption('Step 2', 'The Theme tab holds the terminal look: theme and glyph font.', 900);
    await d.click('.settings-tab:has-text("Theme")');
    await d.pause(900);
    await d.click('#theme-select');
    await page.selectOption('#theme-select', 'theme-io-amber');
    await d.pause(900);
    await page.selectOption('#font-select', 'ibm-3270');
    await d.pause(700);
    await d.caption('Step 3', 'Save, and the terminal changes in place.', 800);
    await d.click('[data-settings-save]');
    await d.pause(1600);
    const closeBtn = page.locator('button[data-settings-close]');
    if (await closeBtn.isVisible().catch(() => false)) {
      await d.click('button[data-settings-close]');
    }
    await d.pause(800);
    await d.caption('Amber phosphor', 'The same session, now on an amber tube with authentic 3270 glyphs.', 3200);

    await d.caption('And back', 'Any theme is one Save away — including the default.', 800);
    await d.click('.appmenu-end .appmenu-trigger');
    await d.pause(900);
    await d.click('[data-settings-open]');
    await page.waitForSelector('[data-settings-modal]:not([hidden])', { timeout: 8000 });
    await d.pause(500);
    await d.click('.settings-tab:has-text("Theme")');
    await d.pause(600);
    await page.selectOption('#theme-select', 'theme-yorkshire');
    await d.pause(500);
    await page.selectOption('#font-select', 'default');
    await d.pause(400);
    await d.click('[data-settings-save]');
    await d.pause(1400);
    if (await closeBtn.isVisible().catch(() => false)) {
      await d.click('button[data-settings-close]');
    }
    await d.pause(1000);

    await d.clearCaption();
    await d.title('', 'Eleven themes, four terminal fonts', 'Every combination is in Settings → Theme', 2800, { fadeOut: false });
  },

  /* How-to: PF keys from the physical keyboard, the virtual keypad, and the
   * command palette. */
  'howto-keyboard': async (page, d) => {
    // "Full" is the readable keypad size on camera; the default "max"
    // whole-keyboard layout is far too dense at 900px. It must be stored
    // before /screen loads: submits are asynchronous, so that first load is
    // the one and only time the keypad is rendered.
    await openConnectPage(page);
    await page.evaluate(() => {
      try { window.localStorage.setItem('h3270KeypadMode', 'full'); } catch { /* private mode */ }
    });
    await page.fill('#hostname-input', `sampleapp:${sampleApp}:${samplePort}`);
    await page.click('#connect-btn');
    await waitForScreen(page);
    // Keypad visibility is a server-side preference that survives across
    // sessions. The scenario narrates switching it on, so make sure it
    // starts off.
    const keypadAlreadyOn = await page
      .locator('.h3270-keypad:not([hidden])')
      .isVisible()
      .catch(() => false);
    if (keypadAlreadyOn) {
      await page.click('.appmenu-trigger:has-text("View")');
      await page.click('[data-keypad-toggle]');
      await page.keyboard.press('Escape');
      await sleep(500);
    }
    await d.title('How to', 'Keyboard, PF keys and the virtual keypad', 'Function keys, on-screen AID keys and the command palette', 2800);

    await d.caption('Setup', 'Sign on to the sample application.', 800);
    await signOn(page, d);
    await d.pause(600);

    await d.caption('PF keys', 'Function keys map straight to PF keys — F1 sends PF1, which is Help here.', 1400);
    await page.keyboard.press('F1');
    await waitForScreen(page);
    await d.caption('PF keys', 'The host answers with its command reference. PF3 goes back.', 2600);
    await page.keyboard.press('F3');
    await waitForScreen(page);
    await d.pause(600);

    await d.caption('Virtual keypad', 'No function keys — a touch screen, say? Turn on the virtual keypad in View.', 1100);
    await d.click('.appmenu-trigger:has-text("View")');
    await d.pause(900);
    await d.click('[data-keypad-toggle]');
    await page.waitForSelector('.h3270-keypad:not([hidden])', { timeout: 8000 });
    await page.locator('.h3270-keypad').scrollIntoViewIfNeeded();
    await d.pause(800);
    await d.highlight('.h3270-keypad', 6);
    await d.caption('Virtual keypad', 'Every PF, PA and AID key, clickable.', 2400, { pos: 'top' });
    await d.clearHighlight();
    await d.click('.h3270-key[data-key="PF1"]:not(.h3270-max-key)');
    await waitForScreen(page);
    await d.caption('Virtual keypad', 'Clicking PF1 does exactly what the F1 key did.', 2600);
    await page.keyboard.press('F3');
    await waitForScreen(page);
    await d.pause(400);

    await page.keyboard.press('Control+k');
    const palette = await page.waitForSelector('.cmdk-panel', { timeout: 5000 }).catch(() => null);
    if (palette) {
      await d.caption('Command palette', 'And everything else is one Ctrl+K away.', 900);
      await page.keyboard.type('keypad', { delay: 90 });
      await d.pause(2000);
      await page.keyboard.press('Escape');
      await d.pause(400);
    }

    await d.clearCaption();
    await d.title('', 'Years of muscle memory, kept', 'Keymaps are editable — import your existing .KMP layout too', 2800, { fadeOut: false });
  },

  /* How-to: several host sessions in one browser tab. */
  'howto-sessions': async (page, d) => {
    await connectQuietly(page);
    await d.title('How to', 'Run several hosts at once', 'Each session is a tab; switching is a click', 2800);

    await d.caption('Setup', 'Sign on to the first host — the Pet Store sample.', 800);
    await signOn(page, d);
    await d.pause(600);

    await d.caption('Step 1', 'Open another session with the + tab.', 1000);
    await d.click('[data-session-new]');
    await page.waitForSelector('[data-profiles-modal]:not([hidden])', { timeout: 8000 });
    await d.caption('Step 2', 'Pick any bundled sample app — or type a host address.', 1800);
    await d.click('[data-profiles-modal] button:has-text("Game - Word Guess")');
    await waitForScreen(page);
    await page.waitForFunction(
      () => document.querySelectorAll('.session-tab').length >= 2,
      { timeout: 15000 }
    );
    await d.pause(1500);

    await d.highlight('.session-tabs, [data-session-tabs], .session-tab', 6);
    await d.caption('Two live hosts', 'The tab strip shows every session. The word game runs beside the store.', 2800, { pos: 'bottom' });
    await d.clearHighlight();

    await d.caption('Step 3', 'Switch back with a click — each session keeps its own place.', 1000);
    await d.click('.session-tab >> nth=0');
    await waitForScreen(page).catch(() => {});
    await waitForTerminalText(page, 'MAIN MENU');
    await d.caption('No state lost', 'The Pet Store is exactly where it was left: signed on, at the menu.', 2800);

    await d.clearCaption();
    await d.title('', 'As many hosts as the job needs', 'The per-user session cap is set by the administrator', 2800, { fadeOut: false });
  },

  /* How-to: turn a recorded flow into a guided business task and run it. */
  'howto-business-tasks': async (page, d) => {
    await connectQuietly(page);
    await setWorkspaceMode(page, 'engineering');
    await d.title('How to', 'Turn a flow into a business task', 'Record once — then anyone can run it and read the answer', 2800);

    await d.caption('Step 1', 'Record the flow: start recording, then work as normal.', 1000);
    await d.click('.appmenu-trigger:has-text("Automation")');
    await d.pause(1100);
    await d.click('[data-recording-start] button[type="submit"]');
    await waitForScreen(page).catch(() => {});
    await d.pause(1400);
    await signOn(page, d);
    await d.caption('Recording', 'Sign on, open the stock catalogue, and PF3 back to the menu.', 600);
    await command(page, d, 'STOCK');
    await waitForTerminalText(page, 'STOCK CATALOGUE');
    await d.pause(800);
    await page.keyboard.press('F3');
    await waitForTerminalText(page, 'MAIN MENU');
    await d.pause(600);
    await d.click('.appmenu-trigger:has-text("Automation")');
    await d.pause(1100);
    await d.click('[data-recording-stop] button[type="submit"]');
    await sleep(1500);
    if (!/\/screen/.test(page.url())) await page.goto(baseUrl + '/screen', { waitUntil: 'domcontentloaded' });
    await page.waitForSelector('[data-appbar]', { timeout: 15000 });
    await fontsReady(page);

    await d.caption('Step 2', 'Save the recording as a task — the wizard turns typed values into named inputs.', 1000);
    await d.click('.appmenu-trigger:has-text("Automation")');
    await d.pause(1100);
    await d.click('[data-wizard-open]');
    await page.waitForSelector('.wizard-modal-content', { timeout: 8000 });
    await d.pause(800);
    await d.typeInto('[data-wizard-name]', 'Stock valuation enquiry');
    await d.typeInto('[data-wizard-description]', 'Signs on, opens the catalogue and returns for the valuation total.');

    // The wizard is five stages; the stage bar is how it is navigated.
    await d.caption('Inputs', 'Everything typed became an input. The STOCK command never changes — fix it.', 1600);
    await d.click('.wizard-step >> nth=1');
    await page.waitForSelector('[data-wizard-stage="inputs"]', { timeout: 8000 });
    await d.pause(700);
    const modeSelects = page.locator('.wizard-modal-content [data-param-mode]');
    if ((await modeSelects.count()) > 1) {
      await d.cursorTo('.wizard-modal-content [data-param-mode] >> nth=-1');
      await modeSelects.last().selectOption({ label: 'Always this value' }).catch(() => {});
      await d.pause(800);
    }

    await d.caption('Guards', 'Each step checks the screen it expects before it types anything.', 1600);
    await d.click('.wizard-step >> nth=2');
    await page.waitForSelector('[data-wizard-stage="steps"]', { timeout: 8000 });
    await d.pause(1400);

    await d.caption('The answer', 'Click the value on the final screen to mark what the task reports back.', 1400);
    await d.click('.wizard-step >> nth=3');
    await page.waitForSelector('[data-wizard-stage="answer"]', { timeout: 8000 });
    await d.pause(600);
    // The modal is taller than the viewport, so the answer panel can sit
    // mostly below the fold; a click aimed at clipped text lands on the
    // backdrop — which closes the wizard. Bring the value into view inside the
    // screen block, and clamp to its visible box before trusting the point.
    const rect = await page.evaluate(() => {
      const screen = document.querySelector('[data-wizard-stage="answer"] .wizard-screen');
      const pre = screen && screen.querySelector('pre');
      if (!pre) return null;
      screen.scrollIntoView({ block: 'center' });
      const text = pre.textContent;
      // The figure itself, not the line it is on: a click has to land on the
      // value for the wizard to mark the value.
      const line = text.indexOf('STOCK AT RETAIL');
      if (line < 0) return null;
      const eol = text.indexOf('\n', line);
      const m = /[\d,]+\.\d{2}/.exec(text.slice(line, eol > line ? eol : line + 60));
      if (!m) return null;
      const start = line + m.index;
      const range = document.createRange();
      range.setStart(pre.firstChild, start);
      range.setEnd(pre.firstChild, start + m[0].length);
      screen.scrollTop += range.getBoundingClientRect().top - screen.getBoundingClientRect().top - 120;
      const r = range.getBoundingClientRect();
      const sr = screen.getBoundingClientRect();
      const y = r.y + r.height / 2;
      if (y < sr.top + 4 || y > sr.bottom - 4) return { clipped: true };
      return { x: r.x + r.width / 2, y };
    });
    if (!rect || rect.clipped) throw new Error('answer text not visible in wizard screen');
    await d.cursorToPoint(rect.x, rect.y);
    await page.mouse.down();
    await page.mouse.up();
    await d.pause(1200);
    if (!(await page.locator('.wizard-modal-content').isVisible().catch(() => false))) {
      throw new Error('wizard closed while the answer was being marked');
    }
    if (!(await page.locator('[data-wizard-stage="answer"] .wizard-card').count())) {
      throw new Error('the click marked nothing on the final screen');
    }

    await d.caption('Review', 'The server checks it and reads the answer back before anything is saved.', 1800);
    await d.click('.wizard-step >> nth=4');
    await page.waitForSelector('[data-wizard-stage="review"]', { timeout: 8000 });
    await d.pause(2200);

    await d.caption('Step 3', 'Save it. The task now lives in the Tasks menu, for anyone.', 900);
    await d.click('.wizard-modal-content button:has-text("Save task")');
    await sleep(1500);

    // The recorded flow starts at the sign-on screen, and the task guards
    // its first step against the screen it expects — so put the terminal
    // back where the flow begins before running it.
    await d.caption('Step 4', 'Sign off, back to where the flow starts — then run it from Tasks.', 1200);
    await page.keyboard.press('F3');
    await waitForTerminalText(page, 'Sign on to continue|Signed off');
    await d.pause(800);
    await d.caption('Step 4', 'Fill in the named inputs, and read the answer.', 800);
    await d.click('[data-tasks-open]');
    await page.waitForSelector('.tasks-modal-backdrop', { timeout: 8000 });
    await d.pause(1200);
    // The catalogue entry itself is the control. Its container nests oddly
    // enough that selector-engine visibility gets it wrong, so glide the
    // cursor over the card and activate it straight in the DOM.
    const cardBox = await page.evaluate(() => {
      const leaf = [...document.querySelectorAll('*')].find(
        (e) => e.children.length === 0 && /Stock valuation enquiry/.test(e.textContent || '') && e.offsetParent
      );
      if (!leaf) return null;
      const r = leaf.getBoundingClientRect();
      return { x: r.x + r.width / 2, y: r.y + r.height / 2 };
    });
    if (!cardBox) throw new Error('saved task not listed in the Tasks modal');
    await d.cursorToPoint(cardBox.x, cardBox.y);
    await page.evaluate(() => {
      const leaf = [...document.querySelectorAll('*')].find(
        (e) => e.children.length === 0 && /Stock valuation enquiry/.test(e.textContent || '') && e.offsetParent
      );
      const target = leaf.closest('button, [role="button"], [class*="task-item"], [class*="task-card"], li') || leaf;
      target.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true, view: window }));
    });
    await d.pause(1500);
    // The example values shown in the form are placeholders, not values —
    // the inputs are empty and required. This modal's nesting defeats the
    // selector engine's visibility logic, so measure the visible inputs in
    // the DOM and drive them by coordinates with real clicks.
    const inputPoints = await page.evaluate(() =>
      [...document.querySelectorAll('input')]
        .filter((i) => i.offsetParent && i.type !== 'hidden' && !i.closest('.terminal-shell'))
        .filter((i) => (i.closest('[class*="task"], [class*="modal"]') || null) !== null)
        .map((i) => {
          const r = i.getBoundingClientRect();
          return { x: r.x + r.width / 2, y: r.y + r.height / 2 };
        })
    );
    const runValues = ['PETOP1', 'PAWS4CLAWS'];
    for (let i = 0; i < Math.min(inputPoints.length, runValues.length); i++) {
      await d.cursorToPoint(inputPoints[i].x, inputPoints[i].y);
      await page.mouse.click(inputPoints[i].x, inputPoints[i].y);
      await page.keyboard.press('Control+a');
      await page.keyboard.type(runValues[i], { delay: 60 });
      await d.pause(300);
    }
    const runBox = await page.evaluate(() => {
      const btn = [...document.querySelectorAll('button')].find(
        (b) => b.textContent.trim() === 'Run' && b.offsetParent
      );
      if (!btn) return null;
      window.__taskModal = btn.closest('[class*="modal"]') || document.body;
      const r = btn.getBoundingClientRect();
      return { x: r.x + r.width / 2, y: r.y + r.height / 2 };
    });
    if (!runBox) throw new Error('Run button not found in the task form');
    await d.cursorToPoint(runBox.x, runBox.y);
    await page.mouse.click(runBox.x, runBox.y);
    await d.caption('Running', 'The task replays the recorded flow against the live host…', 2000);
    await page.waitForFunction(
      () => /STOCK AT RETAIL GBP/i.test((window.__taskModal || document.body).innerText || ''),
      { timeout: 60000 }
    ).catch(() => {});
    await d.pause(1200);
    await d.caption('Done', 'It reports back just the marked answer — no terminal knowledge needed.', 4500);

    await d.clearCaption();
    await d.title('', 'A terminal flow, packaged as a question', 'Tasks run from the browser or the REST API', 2800, { fadeOut: false });
  },

  /* Showcase: the bundled sample apps, from the connect-page picker to a
   * game, so a viewer knows they can try everything with no mainframe. */
  'howto-sample-apps': async (page, d) => {
    await openConnectPage(page);
    await d.title('In the box', 'Eight TN3270 sample hosts', 'A retail system, test apps and games — no mainframe needed', 2800);

    await d.caption('Pick one', '3270Web starts these itself: pick one from the connect page.', 1000);
    await d.click('button:has-text("Start sample app")');
    await d.pause(1500);
    await d.caption('The line-up', 'A full retail back office, three focused test apps, and four games.', 2600);
    await page.keyboard.press('Escape');
    await d.pause(400);

    await d.caption('The Pet Store', 'The default: a complete retail system with counter, back office and reports.', 900);
    await d.typeInto('#hostname-input', `sampleapp:${sampleApp}:${samplePort}`);
    await d.pause(400);
    await d.click('#connect-btn');
    await waitForScreen(page);
    await signOn(page, d);
    await command(page, d, 'STOCK');
    await waitForTerminalText(page, 'STOCK CATALOGUE');
    await d.caption('The Pet Store', 'Menus, lists, guarded screens — enough application for chaos and tasks to chew on.', 3000);

    await d.caption('And the games', 'Open a second session and pick a game — Snake, on a 3270.', 1200);
    await d.click('[data-session-new]');
    await page.waitForSelector('[data-profiles-modal]:not([hidden])', { timeout: 8000 });
    await d.pause(600);
    await d.click('[data-profiles-modal] button:has-text("Game - Snake")');
    await waitForScreen(page);
    await page.waitForFunction(
      () => document.querySelectorAll('.session-tab').length >= 2,
      { timeout: 15000 }
    );
    await d.pause(2500);
    await d.caption('Why games?', 'Because the way to learn a terminal is to want to press its keys.', 3000);

    await d.clearCaption();
    await d.title('', 'sampleapp:<name> in any connect box', 'Or run them standalone: 3270Web sampleapp --app petstore', 2800, { fadeOut: false });
  },
};

/* ------------------------------------------------------------------ */

async function recordScenario(browser, name, fn) {
  const videoDir = path.join(outDir, '.tmp-' + name);
  const context = await browser.newContext({
    viewport: VIEWPORT,
    recordVideo: { dir: videoDir, size: VIEWPORT },
  });
  const page = await context.newPage();
  page.on('pageerror', (e) => console.warn(`  [${name}] page error:`, e.message));
  const director = new Director(page);

  console.log(`\nRecording ${name} …`);
  let failed;
  try {
    await fn(page, director);
  } catch (err) {
    failed = err;
  }
  // Give the sessions back — all of them; a multi-session scenario leaves
  // more than one open, and the survivor becomes the screen the next
  // scenario's connect lands on. The server caps concurrent sessions per
  // user, and every scenario is a fresh browser context, so a run that
  // leaves sessions behind locks the next run out with "maximum sessions
  // open". /disconnect alone is not enough — it hangs up the host but keeps
  // the session (and its slot); /sessions/close is what actually frees one.
  await page.evaluate(async () => {
    for (let i = 0; i < 8; i++) {
      const res = await fetch('/sessions/close', {
        method: 'POST',
        headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
        body: '',
      }).then((r) => (r.ok ? r.json() : null)).catch(() => null);
      if (!res || !res.remaining) break;
    }
  }).catch(() => {});
  await sleep(400);
  const video = page.video();
  await context.close();
  if (video) {
    const dest = path.join(outDir, `${name}.webm`);
    await video.saveAs(dest);
    console.log(`  wrote ${path.relative(repoRoot, dest)}`);
  }
  await rm(videoDir, { recursive: true, force: true });
  if (failed) throw new Error(`[${name}] ${failed.message}`);
}

async function main() {
  await mkdir(outDir, { recursive: true });
  const wanted = process.argv.slice(2);
  const names = wanted.length ? wanted : Object.keys(scenarios);
  for (const name of names) {
    if (!scenarios[name]) {
      console.error(`Unknown scenario "${name}". Available: ${Object.keys(scenarios).join(', ')}`);
      process.exit(1);
    }
  }

  const browser = await chromium.launch({
    executablePath: process.env.DEMO_CHROMIUM || undefined,
  });
  const failures = [];
  for (const name of names) {
    try {
      await recordScenario(browser, name, scenarios[name]);
    } catch (err) {
      console.error(' ', err.message);
      failures.push(name);
    }
  }
  await browser.close();
  if (failures.length) {
    console.error(`\nFailed: ${failures.join(', ')}`);
    process.exit(1);
  }
  console.log('\nVideos written to', outDir);
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
