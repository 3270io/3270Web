/**
 * Check that a recorded flow can be turned into a business task, in a browser.
 *
 * The authoring wizard is the one part of Guided Business Tasks that cannot be
 * checked from Go: it is a drag over a rendered screen, and everything that
 * makes it right or wrong — which cell is under the pointer, whether a marked
 * region is drawn where the value is, whether the password that was typed
 * during the recording is still anywhere — only exists once a browser has laid
 * the screen out. Each of those was wrong at least once: a <pre> as wide as its
 * panel rather than its text put every cell half a column out, and overlays
 * placed before the terminal font loaded landed nowhere at all.
 *
 * It records a real flow against the bundled Pet Store sample, walks the five
 * stages, marks an answer by clicking the value on screen, saves, and reopens
 * the saved task for editing.
 *
 * Usage:
 *   ALLOW_SAMPLE_APPS=true go run ./cmd/3270Web      # another terminal
 *   node scripts/check-task-wizard.mjs
 *
 * Options (environment variables):
 *   CHECK_BASE_URL     base URL of a running 3270Web (default http://127.0.0.1:3270)
 *   CHECK_SAMPLE_APP   sample app id   (default petstore)
 *   CHECK_SAMPLE_PORT  sample app port (default 3271)
 *   CHECK_SHOTS        directory to write two screenshots into (optional)
 *
 * Exits non-zero on the first failed expectation, and names it.
 */
import { chromium } from 'playwright';

const BASE = process.env.CHECK_BASE_URL || 'http://127.0.0.1:3270';
const PORT = process.env.CHECK_SAMPLE_PORT || '3271';
const APP = process.env.CHECK_SAMPLE_APP || 'petstore';
const SHOTS = process.env.CHECK_SHOTS || '';
const TASK_NAME = 'Stock valuation enquiry';

const failures = [];
function expect(label, condition, detail = '') {
  console.log(`${condition ? 'ok  ' : 'FAIL'}  ${label}${detail ? ` — ${detail}` : ''}`);
  if (!condition) failures.push(label);
}
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const FIELDS = '.terminal-shell input[name^="field_"]';

async function waitForScreen(page) {
  await page.waitForURL(/\/screen/, { timeout: 40000 });
  await page.waitForSelector('[data-appbar]', { timeout: 15000 });
  await sleep(300);
}
async function waitForText(page, pattern, timeout = 20000) {
  await page.waitForFunction(
    (src) => new RegExp(src).test(document.querySelector('.terminal-shell')?.innerText || ''),
    pattern,
    { timeout }
  );
}

const run = async () => {
  const browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: 1500, height: 950 } });
  page.on('console', (m) => {
    if (m.type() === 'error') console.log('   [console error] ' + m.text());
  });
  page.on('pageerror', (e) => console.log('   [page error] ' + e.message));

  await page.goto(BASE, { waitUntil: 'domcontentloaded' });
  if (/\/screen/.test(page.url())) {
    await page.goto(BASE + '/disconnect', { waitUntil: 'domcontentloaded' }).catch(() => {});
    await page.goto(BASE, { waitUntil: 'domcontentloaded' });
  }
  await page.fill('#hostname-input', `sampleapp:${APP}:${PORT}`);
  await page.click('#connect-btn');
  await waitForScreen(page);

  // Engineering surface, so the Automation menu is there.
  await page.evaluate(() => {
    const ws = window.ThreeSeventyWeb && window.ThreeSeventyWeb.workspace;
    if (ws && typeof ws.setMode === 'function') ws.setMode('engineering');
  });
  await sleep(400);

  // Record: sign on, open the stock catalogue.
  await page.evaluate(() => document.querySelector('[data-recording-start] button')?.click());
  await sleep(1200);
  if (!/\/screen/.test(page.url())) await page.goto(BASE + '/screen', { waitUntil: 'domcontentloaded' });
  await page.waitForSelector('[data-appbar]', { timeout: 15000 });

  await page.fill(`${FIELDS} >> nth=0`, 'PETOP1');
  await page.keyboard.press('Tab');
  await page.keyboard.type('PAWS4CLAWS');
  await page.keyboard.press('Enter');
  await waitForScreen(page);
  await waitForText(page, 'MAIN MENU');
  await page.fill(`${FIELDS} >> nth=-1`, 'STOCK');
  await page.keyboard.press('Enter');
  await waitForScreen(page);
  await waitForText(page, 'STOCK CATALOGUE');

  await page.evaluate(() => document.querySelector('[data-recording-stop] button')?.click());
  await sleep(1400);
  if (!/\/screen/.test(page.url())) await page.goto(BASE + '/screen', { waitUntil: 'domcontentloaded' });
  await page.waitForSelector('[data-appbar]', { timeout: 15000 });

  /* ---- the wizard ---- */
  await page.evaluate(() => window.ThreeSeventyWeb.taskWizard.open());
  await page.waitForSelector('.wizard-modal-content', { timeout: 8000 });
  await page.waitForSelector('[data-wizard-name]', { timeout: 8000 });
  expect('stage 1 is Details', await page.locator('[data-wizard-body][data-wizard-stage="details"]').count() === 1);
  expect('the stepper shows five stages', await page.locator('.wizard-step').count() === 5);

  await page.fill('[data-wizard-name]', TASK_NAME);
  await page.fill('[data-wizard-description]', 'Signs on and reads the stock valuation total.');

  // Inputs: the password field must have arrived as a secret with no value.
  await page.click('.wizard-step >> nth=1');
  await page.waitForSelector('[data-wizard-body][data-wizard-stage="inputs"]');
  const modes = await page.locator('[data-param-mode]').evaluateAll((els) => els.map((e) => e.value));
  console.log('   input modes:', JSON.stringify(modes));
  expect('a recorded password became a secret', modes.includes('secret'));
  const secretValues = await page.locator('[data-param-mode]').evaluateAll((els) =>
    els.map((e, i) => ({
      mode: e.value,
      value: e.closest('.wizard-card').querySelector('[data-param-value]').value,
    }))
  );
  expect(
    'no secret carries a stored value',
    secretValues.every((p) => p.mode !== 'secret' || p.value === ''),
    JSON.stringify(secretValues)
  );

  // Fix the last input (the STOCK command) to a literal.
  const count = await page.locator('[data-param-mode]').count();
  await page.locator('[data-param-mode]').nth(count - 1).selectOption('fixed');

  // Steps: guards, and the shortlist of alternatives.
  await page.click('.wizard-step >> nth=2');
  await page.waitForSelector('[data-wizard-body][data-wizard-stage="steps"]');
  const guardCount = await page.locator('[data-wizard-guards] .wizard-guard').count();
  const candidates = await page.locator('.wizard-candidate').count();
  expect('every step shows a guard row', guardCount > 0, `${guardCount} guard(s)`);
  expect('alternative guards are offered', candidates > 0, `${candidates} candidate(s)`);
  const screensOnSteps = await page.locator('[data-wizard-stage="steps"] .wizard-screen').count();
  expect('each step shows the screen it runs on', screensOnSteps > 0, `${screensOnSteps} screen(s)`);
  const regionsDrawn = await page.locator('[data-wizard-stage="steps"] .wizard-region').count();
  expect('the guard is drawn on the screen', regionsDrawn > 0, `${regionsDrawn} region(s)`);

  // The answer: click a value on the final screen.
  await page.click('.wizard-step >> nth=3');
  await page.waitForSelector('[data-wizard-body][data-wizard-stage="answer"]');
  // Click where the glyph actually is on screen, measured from the rendered
  // text — the mapping a person's click goes through, not the wizard's own
  // arithmetic checked against itself.
  const target = await page.evaluate(() => {
    const screen = document.querySelector('[data-wizard-stage="answer"] .wizard-screen');
    const pre = screen && screen.querySelector('pre');
    if (!pre) return null;
    const text = pre.textContent;
    const lines = text.split('\n');
    let row = -1, col = -1, value = '';
    for (let r = 0; r < lines.length; r++) {
      const m = /([\d,]+\.\d{2})\s*$/.exec(lines[r]);
      if (m && /LABRADOR/.test(lines[r])) { row = r; col = m.index; value = m[1]; break; }
    }
    if (row < 0) return { missing: lines.slice(0, 6) };
    const idx = lines.slice(0, row).reduce((n, l) => n + l.length + 1, 0) + col;
    const range = document.createRange();
    range.setStart(pre.firstChild, idx + 2);
    range.setEnd(pre.firstChild, idx + 3);
    const box = screen.getBoundingClientRect();
    let r = range.getBoundingClientRect();
    if (r.top < box.top || r.bottom > box.bottom) {
      screen.scrollTop += r.top - box.top - 60;
      r = range.getBoundingClientRect();
    }
    return { x: r.x + r.width / 2, y: r.y + r.height / 2, value: value };
  });
  expect('the final screen carries the valuation', !!(target && target.x), JSON.stringify(target));
  if (target && target.x) {
    // A single click, not a drag: it should take the whole value.
    await page.mouse.move(target.x, target.y);
    await page.mouse.down();
    await page.mouse.up();
    await sleep(400);
    const marked = await page.locator('[data-wizard-stage="answer"] .wizard-card [data-wizard-sample]').allTextContents();
    expect('a click marks the whole value', marked.length === 1 && marked[0].includes(target.value),
      JSON.stringify(marked) + ' want ' + target.value);
    const overlay = await page.locator('[data-wizard-stage="answer"] .wizard-region').count();
    expect('the marked region is drawn on the screen', overlay === 1, `${overlay}`);
  }

  // Check what the regions read, through the server.
  await page.click('button:has-text("Check what these read")');
  await sleep(700);
  const readBack = await page.locator('.wizard-output-sample').allTextContents();
  console.log('   read back:', JSON.stringify(readBack));

  // Review.
  await page.click('.wizard-step >> nth=4');
  await page.waitForSelector('[data-wizard-body][data-wizard-stage="review"]');
  await sleep(900);
  const review = await page.locator('[data-wizard-review]').innerText();
  console.log('   review:\n     ' + review.split('\n').join('\n     '));
  expect('review reports the server preview', /Read from the screen/.test(review), review.slice(0, 200));

  await page.click('[data-wizard-save]');
  await sleep(1200);
  expect('the wizard closed after saving', await page.locator('.wizard-modal-content').isVisible() === false);

  const saved = await page.evaluate(async () => {
    const r = await fetch('/tasks', { credentials: 'same-origin' });
    return r.json();
  });
  const task = (saved.tasks || []).find((t) => t.name === TASK_NAME);
  expect('the task is in the catalogue', !!task);
  if (task) {
    expect('the fixed input left the form', (task.parameters || []).length < 3,
      `${(task.parameters || []).length} parameter(s)`);
    expect('a secret parameter is marked sensitive',
      (task.parameters || []).some((p) => p.sensitive));
    expect('no recorded password is stored', !JSON.stringify(task).includes('PAWS4CLAWS'));
    expect('the answer was saved', (task.outputs || []).length === 1);
  }

  /* ---- reopening it for editing ---- */
  await page.evaluate((name) => window.ThreeSeventyWeb.taskWizard.openTask(name), TASK_NAME);
  await page.waitForSelector('[data-wizard-name]', { timeout: 8000 });
  const reopened = await page.inputValue('[data-wizard-name]');
  expect('the saved task reopens with its name', reopened === TASK_NAME, reopened);
  await page.click('.wizard-step >> nth=3');
  await page.waitForSelector('[data-wizard-body][data-wizard-stage="answer"]');
  const regionCards = await page.locator('[data-wizard-stage="answer"] .wizard-card').count();
  expect('the saved answer region came back', regionCards === 1, `${regionCards}`);

  // The dialog contract: focus lands inside, Escape closes it.
  expect('focus moves into the dialog', await page.evaluate(() => {
    const d = document.querySelector('.wizard-modal-content');
    return !!d && d.contains(document.activeElement);
  }));
  if (SHOTS) await page.screenshot({ path: `${SHOTS}/wizard-answer.png` });
  await page.click('.wizard-step >> nth=2');
  await sleep(400);
  if (SHOTS) await page.screenshot({ path: `${SHOTS}/wizard-steps.png` });

  await page.keyboard.press('Escape');
  await sleep(400);
  expect('Escape closes the wizard', await page.locator('.wizard-modal-content').isVisible() === false);

  await browser.close();
  console.log(failures.length ? `\n${failures.length} failure(s): ${failures.join(', ')}` : '\nall checks passed');
  process.exit(failures.length ? 1 : 0);
};

run().catch((e) => {
  console.error(e);
  process.exit(2);
});
