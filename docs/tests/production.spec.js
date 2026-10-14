import { test, expect } from '@playwright/test';
import { execFileSync } from 'node:child_process';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { extname, join, resolve, sep } from 'node:path';
import { fileURLToPath } from 'node:url';

const origin = 'https://change-windows.addons.projectcapsule.dev';
const types = {
  '.html': 'text/html', '.js': 'application/javascript', '.css': 'text/css',
  '.json': 'application/json', '.svg': 'image/svg+xml', '.woff2': 'font/woff2',
  '.png': 'image/png', '.ico': 'image/x-icon',
};
let output;

test.beforeAll(async () => {
  output = await mkdtemp(join(tmpdir(), 'change-windows-docs-https-'));
  // Build production assets, including minification and script integrity hashes.
  execFileSync('npm', ['run', 'build', '--', '--destination', output], {
    cwd: fileURLToPath(new URL('..', import.meta.url)), stdio: 'pipe',
  });
});
test.afterAll(async () => { if (output) await rm(output, { recursive: true, force: true }); });

test('production HTTPS assets support menu and search interactions', async ({ page, isMobile }) => {
  const errors = [];
  const failures = [];
  page.on('pageerror', error => errors.push(error.message));
  page.on('requestfailed', request => failures.push(request.url()));
  page.on('console', message => {
    if (message.type() === 'error') errors.push(message.text());
  });
  // Serve the generated site under its real HTTPS origin without external network access.
  await page.route('**/*', async route => {
    const url = new URL(route.request().url());
    if (url.origin !== origin) return route.abort();
    const pathname = decodeURIComponent(url.pathname);
    const file = resolve(output, `.${pathname.endsWith('/') ? pathname + 'index.html' : pathname}`);
    if (!file.startsWith(output + sep)) return route.abort();
    try {
      await route.fulfill({ body: await readFile(file), contentType: types[extname(file)] || 'application/octet-stream' });
    } catch {
      await route.fulfill({ status: 404, body: 'Not found' });
    }
  });
  const activate = locator => isMobile ? locator.tap() : locator.click();
  await page.goto(origin + '/');
  await expect(page.locator('base')).toHaveAttribute('href', origin + '/');
  const main = page.locator('#offcanvasNavMain');
  if (isMobile) {
    await expect(main).toBeHidden();
    await activate(page.getByRole('button', { name: 'Open main navigation menu', exact: true }));
    await expect(main).toBeVisible();
    await activate(main.getByRole('button', { name: 'Close', exact: true }));
    await expect(main).toBeHidden();
    await activate(page.getByRole('button', { name: 'Open main navigation menu', exact: true }));
  }
  await activate(main.getByRole('link', { name: 'Documentation', exact: true }));
  await expect(page).toHaveURL(origin + '/docs/getting-started/overview/');
  if (isMobile) {
    const section = page.locator('#offcanvasNavSection');
    await activate(page.getByRole('button', { name: 'Open section navigation menu', exact: true }));
    await expect(section).toBeVisible();
    await activate(section.getByRole('link', { name: 'Installation', exact: true }));
    await expect(page).toHaveURL(origin + '/docs/getting-started/installation/');
  }
  await activate(page.getByRole('button', { name: 'Search website', exact: true }));
  await page.locator('.search-text').fill('node pool');
  await activate(page.locator('.search-results').getByRole('link', { name: /Freeze a node pool/ }));
  await expect(page).toHaveURL(origin + '/docs/examples/node-pool/');
  expect(failures).toEqual([]);
  expect(errors).toEqual([]);
});
