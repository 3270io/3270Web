/**
 * Check that every dialog on the terminal screen behaves like a dialog.
 *
 * Five properties, all of which a modal needs and none of which any single
 * module can be trusted to arrange for itself:
 *
 *   1. Opening moves keyboard focus into the dialog.
 *   2. Tab stays inside it.
 *   3. The page behind the backdrop does not scroll.
 *   4. Escape closes the topmost dialog, and only that one.
 *   5. Closing everything releases the scroll lock again.
 *
 * These are properties of the shared helper (web/static/modal-utils.js), and
 * the reason to check them in a browser is that they were each got wrong at
 * least once by a module that hand-rolled its own version: a focus trap
 * listening on a dialog focus never entered, a scroll lock on <body> when
 * <html> is the scroller, and six private Escape listeners that all fired on
 * one keypress.
 *
 * Usage:
 *   ALLOW_SAMPLE_APPS=true go run ./cmd/3270Web      # another terminal
 *   node scripts/check-modals.mjs
 *
 * Options (environment variables):
 *   CHECK_BASE_URL    base URL of a running 3270Web (default http://127.0.0.1:3270)
 *   CHECK_SAMPLE_PORT port for the sample app it connects to (default 3271)
 *
 * Exits non-zero on the first failed expectation, and names it.
 */
import { chromium } from 'playwright';

const BASE = process.env.CHECK_BASE_URL || 'http://127.0.0.1:3270';
const PORT = process.env.CHECK_SAMPLE_PORT || '3271';

/* Every dialog reachable from the terminal screen, by the hook that opens it. */
const DIALOGS = [
  ['Settings', '[data-settings-open]'],
  ['About', '[data-about-open]'],
  ['Logs', '[data-logs-open]'],
  ['Connection details', '[data-host-details-open]'],
  ['Screen history', '[data-history-open]'],
  ['File transfer', '[data-transfer-open]'],
  ['Keyboard mapping', '[data-keymap-open]'],
  ['Tasks', '[data-tasks-open]'],
  ['Connection profiles', '[data-profiles-open]'],
  ['Disconnect', '[data-disconnect-open]'],
];

const failures = [];
function expect(label, condition, detail = '') {
  console.log(`${condition ? 'ok  ' : 'FAIL'}  ${label}${detail ? ` — ${detail}` : ''}`);
  if (!condition) failures.push(label);
}

const openDialogCount = (page) =>
  page.evaluate(
    () =>
      [...document.querySelectorAll('[role=dialog]')].filter(
        (d) => !!(d.offsetWidth || d.offsetHeight || d.getClientRects().length)
      ).length
  );

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1400, height: 900 } });

/* ---- reach a live terminal ------------------------------------------- */
await page.goto(BASE, { waitUntil: 'networkidle' });
if (!page.url().includes('/screen')) {
  try {
    await page.click('button:has-text("Start sample app")');
    await page.waitForTimeout(400);
    await page.selectOption('#sample-app-port', PORT);
    await page.click('[data-sample-start]');
    await page.waitForTimeout(3000);
  } catch (_) {
    /* already running is fine — connect to it directly below */
  }
}
if (!page.url().includes('/screen')) {
  await page.evaluate(() => document.querySelectorAll('[data-sample-close]').forEach((b) => b.click()));
  await page.fill('#hostname-input', `localhost:${PORT}`);
  await page.click('button:has-text("Connect")');
  await page.waitForTimeout(4000);
}
if (!page.url().includes('/screen')) {
  console.error(`Could not reach a terminal session (stopped at ${page.url()}).`);
  console.error('Start the server with ALLOW_SAMPLE_APPS=true and try again.');
  await browser.close();
  process.exit(2);
}

/* ---- one dialog at a time -------------------------------------------- */
for (const [name, sel] of DIALOGS) {
  const trigger = await page.$(sel);
  if (!trigger) {
    expect(`${name}: trigger present`, false, sel);
    continue;
  }
  await page.evaluate(() => window.scrollTo(0, 0));
  await trigger.click({ force: true });
  await page.waitForTimeout(900);

  expect(
    `${name}: focus moves into the dialog`,
    await page.evaluate(() => {
      const d = [...document.querySelectorAll('[role=dialog]')]
        .filter((x) => !!(x.offsetWidth || x.offsetHeight || x.getClientRects().length))
        .pop();
      return d ? d.contains(document.activeElement) : false;
    })
  );

  let escaped = false;
  for (let i = 0; i < 14; i++) {
    await page.keyboard.press('Tab');
    escaped = await page.evaluate(() => {
      const d = [...document.querySelectorAll('[role=dialog]')]
        .filter((x) => !!(x.offsetWidth || x.offsetHeight || x.getClientRects().length))
        .pop();
      return d ? !d.contains(document.activeElement) : true;
    });
    if (escaped) break;
  }
  expect(`${name}: Tab stays inside the dialog`, !escaped);

  // A gesture, not window.scrollTo: overflow:hidden stops the gesture, and
  // the programmatic API scrolls straight past it.
  await page.mouse.move(30, 400);
  await page.mouse.wheel(0, 600);
  await page.waitForTimeout(200);
  const scrolled = await page.evaluate(() => window.scrollY);
  expect(`${name}: the page behind does not scroll`, scrolled === 0, `scrollY=${scrolled}`);
  await page.evaluate(() => window.scrollTo(0, 0));

  await page.keyboard.press('Escape');
  await page.waitForTimeout(500);
  expect(`${name}: Escape closes it`, (await openDialogCount(page)) === 0);

  // Leave a clean screen for the next one even if something above failed.
  await page.evaluate(() =>
    document.querySelectorAll('.modal-close,[data-modal-close]').forEach((b) => b.offsetParent && b.click())
  );
  await page.waitForTimeout(300);
}

/* ---- stacked dialogs -------------------------------------------------- */
await page.evaluate(() => document.querySelector('[data-tasks-open]').click());
await page.waitForTimeout(500);
await page.evaluate(() => document.querySelector('[data-transfer-open]').click());
await page.waitForTimeout(500);
const stacked = await openDialogCount(page);
await page.keyboard.press('Escape');
await page.waitForTimeout(500);
const left = await openDialogCount(page);
expect(
  'Escape closes only the topmost of two stacked dialogs',
  stacked === 2 && left === 1,
  `open ${stacked} -> ${left}`
);
await page.keyboard.press('Escape');
await page.waitForTimeout(500);

/* ---- the lock is released -------------------------------------------- */
expect('every dialog is closed again', (await openDialogCount(page)) === 0);
await page.mouse.wheel(0, 600);
await page.waitForTimeout(250);
expect(
  'the page scrolls again once nothing is open',
  await page.evaluate(() => window.scrollY > 0)
);
expect(
  'no inline overflow is left on <html> or <body>',
  await page.evaluate(
    () => !document.documentElement.style.overflow && !document.body.style.overflow
  )
);

await browser.close();

if (failures.length) {
  console.error(`\n${failures.length} failed:`);
  failures.forEach((f) => console.error(`  - ${f}`));
  process.exit(1);
}
console.log('\nAll modal checks passed.');
