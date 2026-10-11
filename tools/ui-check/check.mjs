// Drives the dashboard in headless Chromium and checks what the Go tests cannot:
// the page renders in both themes at desktop and phone width, makes no request
// to another origin, logs no console errors, never scrolls sideways, labels
// every control, and shows a focus ring on keyboard focus.
//
//   make demo                       # in one terminal (made-up data)
//   cd tools/ui-check && npm ci && node check.mjs
//
// Environment: BASE (default http://127.0.0.1:8787), PT_USER / PT_PASSWORD
// (default parent / demo-password), CHROMIUM (path to a Chromium binary; the
// default is the one Playwright finds), OUT (a directory for screenshots).
import { chromium } from 'playwright-core';
import { mkdirSync } from 'node:fs';

const base = process.env.BASE || 'http://127.0.0.1:8787';
const user = process.env.PT_USER || 'parent';
const password = process.env.PT_PASSWORD || 'demo-password';
const out = process.env.OUT || '';
if (out) mkdirSync(out, { recursive: true });

const browser = await chromium.launch({ executablePath: process.env.CHROMIUM || undefined, args: ['--no-sandbox'] });
const failures = [];
const fail = (ctx, msg) => failures.push(`${ctx}: ${msg}`);
let checks = 0;

for (const scheme of ['light', 'dark']) {
  for (const width of [1280, 360]) {
    const ctx = `${scheme} ${width}px`;
    const context = await browser.newContext({ viewport: { width, height: 900 }, colorScheme: scheme });
    const page = await context.newPage();
    const origins = new Set();
    let signedIn = false; // the sign-in screen's own 401s (session probe, wrong password) are expected
    page.on('console', (m) => {
      if (m.type() !== 'error' && m.type() !== 'warning') return;
      if (!signedIn && /status of 401/.test(m.text())) return;
      fail(ctx, `console ${m.type()}: ${m.text()}`);
    });
    page.on('pageerror', (e) => fail(ctx, `page error: ${e.message}`));
    page.on('request', (r) => origins.add(new URL(r.url()).origin));

    await page.goto(base);
    await page.waitForSelector('input[name=username]');
    // A wrong password must say so in words.
    await page.fill('input[name=username]', user);
    await page.fill('input[name=password]', password + '-wrong');
    await page.click('button:has-text("Sign in")');
    await page.waitForSelector('text=/incorrect/i', { timeout: 5000 }).catch(() => fail(ctx, 'a wrong password shows no message'));
    await page.fill('input[name=password]', password);
    await page.click('button:has-text("Sign in")');
    await page.waitForSelector('role=tab[name="Hourly history"]');
    signedIn = true;
    await page.waitForTimeout(1200);

    const theme = await page.evaluate(() => document.documentElement.getAttribute('data-theme'));
    if (theme !== scheme) fail(ctx, `data-theme is ${theme}, expected ${scheme} (follow the OS)`);

    for (const tab of ['Today', 'Hourly history', 'Trends', 'Apps', 'Alerts']) {
      await page.click(`role=tab[name="${tab}"]`);
      await page.waitForTimeout(700);
      const m = await page.evaluate(() => ({
        overflow: document.documentElement.scrollWidth - window.innerWidth,
        unlabeled: [...document.querySelectorAll('button, a[href], input, select, textarea, [role=button], [role=tab]')]
          .filter((e) => e.type !== 'hidden' && !(e.labels && e.labels.length)
            && !(e.getAttribute('aria-label') || e.getAttribute('aria-labelledby') || e.innerText || '').trim())
          .map((e) => e.outerHTML.slice(0, 100)),
        headings: document.querySelectorAll('h1').length,
        landmarks: document.querySelectorAll('main, [role=main]').length,
      }));
      checks++;
      if (m.overflow > 0) fail(`${ctx} ${tab}`, `scrolls sideways by ${m.overflow}px`);
      for (const u of m.unlabeled) fail(`${ctx} ${tab}`, `control without a name: ${u}`);
      if (m.headings !== 1) fail(`${ctx} ${tab}`, `${m.headings} h1 headings, expected 1`);
      if (m.landmarks !== 1) fail(`${ctx} ${tab}`, `${m.landmarks} main landmarks, expected 1`);
      if (out) await page.screenshot({ path: `${out}/${scheme}-${width}-${tab.toLowerCase().replace(/\W+/g, '-')}.png`, fullPage: true });
    }

    // Keyboard: the first stops after the page start must show a visible ring.
    await page.click('role=tab[name="Today"]');
    await page.evaluate(() => document.activeElement && document.activeElement.blur());
    for (let i = 0; i < 6; i++) {
      await page.keyboard.press('Tab');
      const ring = await page.evaluate(() => {
        const e = document.activeElement;
        if (!e || e === document.body) return 'body';
        // Lumen marks focus in a text or date field by darkening its 2px border, not with a ring.
        if (e.tagName === 'INPUT' && ['text', 'date', 'password', 'search', 'email', 'number', 'time'].includes(e.type)) {
          return 'ok';
        }
        const s = getComputedStyle(e);
        const box = e.closest('label') || e;
        const sb = getComputedStyle(box);
        const sib = e.nextElementSibling ? getComputedStyle(e.nextElementSibling) : null;
        const shown = (st) => st && ((st.outlineStyle !== 'none' && parseFloat(st.outlineWidth) >= 2) || st.boxShadow !== 'none');
        return shown(s) || shown(sb) || shown(sib) ? 'ok' : `no ring on <${e.tagName.toLowerCase()}> ${(e.getAttribute('aria-label') || e.innerText || '').slice(0, 30)}`;
      });
      checks++;
      if (ring !== 'ok' && ring !== 'body') fail(ctx, ring);
    }

    // Nothing may leave the origin.
    const own = new URL(base).origin;
    for (const o of origins) if (o !== own && !o.startsWith('data:')) fail(ctx, `request to another origin: ${o}`);
    await context.close();
  }
}
await browser.close();

if (failures.length) {
  console.error(`${failures.length} problem(s):\n- ` + [...new Set(failures)].join('\n- '));
  process.exit(1);
}
console.log(`ok: ${checks} checks across light/dark at 1280 and 360 px`);
