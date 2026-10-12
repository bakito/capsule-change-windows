import { test, expect } from '@playwright/test';
import { readFile } from 'node:fs/promises';

const source = await readFile(new URL('../../examples/quickstart.yaml', import.meta.url), 'utf8');

test('business overview explains the project before the configuration example', async ({ page }) => {
  await page.goto('');
  const overview = page.getByRole('region', { name: 'What is Change Windows?' });
  await expect(overview.getByRole('heading', { name: 'What is Change Windows?' })).toBeVisible();
  await expect(overview).toBeVisible();
  await expect(overview).toContainText('less manual coordination');
  await expect(overview).toContainText('control infrastructure costs');
  expect(await overview.evaluate(element => Boolean(
    element.compareDocumentPosition(document.querySelector('[data-window-example]')) & Node.DOCUMENT_POSITION_FOLLOWING
  ))).toBeTruthy();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy();
});

test('explore YAML, copy the complete manifest, and follow matching references', async ({ page, context }) => {
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  await context.grantPermissions(['clipboard-read', 'clipboard-write']);
  await page.goto('');
  const example = page.locator('[data-window-example]');
  await expect(example.locator('[data-window-source]')).toHaveText(source);
  const parts = example.locator('[data-window-part]');
  await expect(parts).toHaveCount(7);
  for (const key of ['namespaceSelectors', 'schedules', 'targets', 'serviceAccount', 'actions', 'admission', 'config']) {
    const block = example.locator(`[data-window-part="${key}"]`);
    // Keyboard activation follows the same selection behavior as a pointer click.
    await block.focus();
    await block.press('Enter');
    await expect(block).toHaveAttribute('aria-current', 'true');
    await expect(example.locator(`[data-window-help="${key}"]`)).toBeVisible();
    await expect(example.locator('[data-window-help]:visible')).toHaveCount(1);
  }
  await example.getByRole('button', { name: 'Copy YAML', exact: true }).click();
  await expect(example.getByRole('status')).toHaveText('Complete ChangeWindow YAML copied.');
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(source);
  const download = await page.request.get(await example.getByRole('link', { name: 'Download YAML' }).getAttribute('href'));
  expect(download.ok()).toBeTruthy();
  expect(await download.text()).toBe(source);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy();
  await example.locator('[data-window-help="config"]').getByRole('link', { name: 'API reference' }).click();
  await expect(page).toHaveURL(/\/schedule-actions\/docs\/reference\/api\/#admissionwarning$/);
  await expect(page.locator('#admissionwarning')).toBeVisible();
  expect(errors).toEqual([]);
});

test('pointer selection, copy failure, and dark mode stay usable', async ({ page }) => {
  await page.addInitScript(() => Object.defineProperty(navigator, 'clipboard', {
    value: { writeText: async () => { throw new Error('Permission denied'); } },
  }));
  await page.emulateMedia({ colorScheme: 'dark' });
  await page.goto('');
  const example = page.locator('[data-window-example]');
  await example.locator('[data-window-part="admission"]').click();
  await expect(example.locator('[data-window-help="admission"]')).toBeVisible();
  await example.getByRole('button', { name: 'Copy YAML', exact: true }).click();
  await expect(example.getByRole('status')).toContainText('Use Download YAML');
  await expect(example.getByRole('link', { name: 'Download YAML' })).toBeVisible();
});

test('without JavaScript the YAML links and download remain available', async ({ browser, baseURL }) => {
  const context = await browser.newContext({ javaScriptEnabled: false });
  const page = await context.newPage();
  await page.goto(baseURL);
  const example = page.locator('[data-window-example]');
  await expect(example.locator('[data-window-source]')).toHaveText(source);
  await expect(example.getByRole('button', { name: 'Copy YAML', exact: true })).toBeHidden();
  await example.locator('[data-window-part="schedules"]').click();
  await expect(page).toHaveURL(/\/schedule-actions\/docs\/reference\/api\/#schedule$/);
  await context.close();
});

test('search resolves examples beneath the project base URL', async ({ page }) => {
  const index = page.waitForResponse(response => response.url().endsWith('/schedule-actions/search-index.json'));
  await page.goto('');
  expect((await index).ok()).toBeTruthy();
  await page.getByRole('button', { name: 'Search website', exact: true }).click();
  await page.locator('.search-text').fill('node pool');
  await page.locator('.search-results').getByRole('link', { name: /Freeze a node pool/ }).click();
  await expect(page).toHaveURL(/\/schedule-actions\/docs\/examples\/node-pool\/$/);
});
