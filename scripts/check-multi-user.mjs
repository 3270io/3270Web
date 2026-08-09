/**
 * Walk a running instance through the whole multi-user flow, in a browser.
 *
 * Every part of this is covered by Go tests, which is why they are not
 * repeated here. What they cannot cover is whether the parts compose in a
 * real browser against a real server: first-run setup handing straight to a
 * signed-in administrator, that administrator creating somebody, the new
 * account being forced to choose its own password, opening a terminal, being
 * refused administration, and the trail showing all of it afterwards.
 *
 * Usage:
 *   AUTH_MODE=local ALLOW_SAMPLE_APPS=1 go run ./cmd/3270Web    # another terminal
 *   node scripts/check-multi-user.mjs --code EJWQ-RUYN-7XL3-PT3O
 *
 * The setup code is printed to the server log on first start:
 *   auth: setup code: EJWQ-RUYN-7XL3-PT3O
 *
 * Options (environment variables):
 *   CHECK_BASE_URL  base URL of a running 3270Web (default http://127.0.0.1:8080)
 *   CHECK_OUT_DIR   where screenshots are written (default <repo>/output)
 *   CHECK_SETUP_CODE  the code, instead of --code
 *
 * Exits non-zero on the first failed expectation, and names it. Run it
 * against a *fresh* data directory: it expects an instance with no accounts.
 */
import { chromium } from 'playwright';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { mkdir } from 'node:fs/promises';

const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(here, '..');

const BASE = process.env.CHECK_BASE_URL || 'http://127.0.0.1:8080';
const OUT = process.env.CHECK_OUT_DIR || path.join(repoRoot, 'output');
const codeFlag = process.argv.indexOf('--code');
const CODE = (codeFlag !== -1 ? process.argv[codeFlag + 1] : process.env.CHECK_SETUP_CODE) || '';

if (!CODE) {
  console.error('A setup code is required: --code <code>, or CHECK_SETUP_CODE.');
  console.error('It is printed to the server log the first time an instance starts');
  console.error('with AUTH_MODE=local and no accounts.');
  process.exit(2);
}

// Long enough to pass the 12-character floor, and obviously not real.
const PW_ADMIN = 'check-admin-password';
const PW_USER_TEMP = 'check-temp-password';
const PW_USER = 'check-chosen-password';

const failures = [];
function expect(label, condition, detail = '') {
  const line = `${condition ? 'ok  ' : 'FAIL'}  ${label}${detail ? ` — ${detail}` : ''}`;
  console.log(line);
  if (!condition) failures.push(label);
}

const host = new URL(BASE).hostname;
await mkdir(OUT, { recursive: true });
const browser = await chromium.launch();

async function session() {
  const context = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const page = await context.newPage();
  page.on('pageerror', (err) => failures.push(`uncaught page error: ${err.message}`));
  return { context, page };
}

const shot = (page, name, opts = {}) =>
  page.screenshot({ path: path.join(OUT, `multi-user-${name}.png`), ...opts });

// ---------------------------------------------------------------- setup

const admin = await session();
await admin.page.goto(`${BASE}/`, { waitUntil: 'networkidle' });
expect('an instance with no accounts sends you to setup',
  admin.page.url().endsWith('/setup'), admin.page.url());
await shot(admin.page, '1-setup');

await admin.page.fill('input[name="setupCode"]', CODE);
await admin.page.fill('input[name="username"]', 'checkadmin');
await admin.page.fill('input[name="password"]', PW_ADMIN);
const confirmSetup = await admin.page.$('input[name="confirmPassword"]');
if (confirmSetup) await confirmSetup.fill(PW_ADMIN);
await Promise.all([
  admin.page.waitForNavigation({ waitUntil: 'networkidle' }),
  admin.page.click('button[type="submit"]'),
]);
expect('completing setup signs the first administrator straight in',
  !/\/(setup|login)$/.test(admin.page.url()), admin.page.url());

// ------------------------------------------------------- create an account

await admin.page.goto(`${BASE}/admin/users`, { waitUntil: 'networkidle' });
await admin.page.waitForTimeout(400);
await admin.page.click('[data-admin-add]');
await admin.page.fill('#new-username', 'checkuser');
await admin.page.fill('#new-password', PW_USER_TEMP);
await admin.page.click('[data-add-form] button[type="submit"]');
await admin.page.waitForTimeout(800);
expect('the accounts page creates an account',
  (await admin.page.textContent('[data-admin-rows]')).includes('checkuser'));
await shot(admin.page, '2-accounts');

// ----------------------------------------------- the new account signs in

const user = await session();
await user.page.goto(`${BASE}/login`, { waitUntil: 'networkidle' });
await user.page.fill('input[name="username"]', 'checkuser');
await user.page.fill('input[name="password"]', PW_USER_TEMP);
await Promise.all([
  user.page.waitForNavigation({ waitUntil: 'networkidle' }),
  user.page.click('button[type="submit"]'),
]);
expect('a password an administrator chose must be replaced',
  user.page.url().includes('/account/password'), user.page.url());

await user.page.fill('input[name="currentPassword"]', PW_USER_TEMP);
await user.page.fill('input[name="newPassword"]', PW_USER);
await user.page.fill('input[name="confirmPassword"]', PW_USER);
await Promise.all([
  user.page.waitForNavigation({ waitUntil: 'networkidle' }),
  user.page.click('button[type="submit"]'),
]);
expect('changing it lands them in the application',
  !user.page.url().includes('/account/password'), user.page.url());

const adminStatus = await user.page.evaluate(async () => {
  const res = await fetch('/api/admin/users', { headers: { 'X-Requested-With': 'XMLHttpRequest' } });
  return res.status;
});
expect('a regular account is refused instance administration',
  adminStatus === 403, `status ${adminStatus}`);

// ------------------------------------------------------- open a terminal

await user.page.goto(`${BASE}/`, { waitUntil: 'networkidle' });
await user.page.fill('#hostname-input', 'mock');
// #connect-btn, not the first button[type=submit]: the header's sign-out form
// comes earlier in the DOM, and clicking that instead signs the user out and
// makes every later check fail as "not signed in".
await Promise.all([
  user.page.waitForNavigation({ waitUntil: 'networkidle' }).catch(() => {}),
  user.page.click('#connect-btn'),
]);
await user.page.waitForTimeout(1500);
expect('they can open a terminal session', user.page.url().includes('/screen'), user.page.url());
await shot(user.page, '3-terminal');

// -------------------------------------------- somebody else's session id

const theirSession = (await user.context.cookies()).find((c) => c.name === '3270Web_session')?.value;
expect('the terminal session has an id', Boolean(theirSession));
if (theirSession) {
  await admin.context.addCookies([
    { name: '3270Web_session', value: theirSession, domain: host, path: '/' },
  ]);
  const stolen = await admin.page.evaluate(async () => {
    const res = await fetch('/screen.json', { headers: { 'X-Requested-With': 'XMLHttpRequest' } });
    return res.status;
  });
  expect("an administrator holding another account's session id cannot use it",
    stolen !== 200, `status ${stolen}`);
}

// ------------------------------------------------------------ the trail

await admin.page.goto(`${BASE}/admin/audit`, { waitUntil: 'networkidle' });
await admin.page.waitForTimeout(700);
const trail = await admin.page.textContent('[data-audit-rows]');
for (const event of [
  'First administrator created', 'Account created', 'Signed in',
  'Password changed', 'Session opened',
]) {
  expect(`the trail records: ${event}`, trail.includes(event));
}
await shot(admin.page, '4-audit', { fullPage: true });

await browser.close();

if (failures.length) {
  console.error(`\n${failures.length} check(s) failed:\n  ${failures.join('\n  ')}`);
  process.exit(1);
}
console.log(`\nAll checks passed. Screenshots in ${OUT}`);
