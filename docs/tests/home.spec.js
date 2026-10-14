import { test, expect } from '@playwright/test';
import { readFile } from 'node:fs/promises';

const source = await readFile(new URL('../../playground/manifests/examples/quickstart.yaml', import.meta.url), 'utf8');

const guides = {
  namespaceSelectors: 'schedules/',
  schedules: 'schedules/#cron-and-time-zones',
  targets: 'schedules/#selection-over-time',
  serviceAccount: 'security/#executor-permissions',
  actions: 'actions/#downscale-and-restore',
  admission: 'admission/#match-conditions',
  config: 'admission/#warning-periods',
};

test('YAML blocks open matching guides on mobile and in new tabs', async ({ page, context, isMobile, baseURL }) => {
  for (const [key, guide] of Object.entries(guides)) {
    await page.goto('');
    const block = page.locator(`[data-window-part="${key}"]`);
    await expect(block).toHaveAttribute('href', `/schedule-actions/docs/guides/${guide}`);
    let destination = page;
    if (isMobile) {
      await block.tap();
    } else {
      const opened = context.waitForEvent('page');
      await block.click({ modifiers: ['ControlOrMeta'] });
      destination = await opened;
    }
    await expect(destination).toHaveURL(new URL(`docs/guides/${guide}`, baseURL).href);
    const fragment = guide.split('#')[1];
    if (fragment) await expect(destination.locator(`#${fragment}`)).toBeVisible();
    if (!isMobile) await destination.close();
  }
});

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

test('explore YAML, copy the complete manifest, and follow matching references', async ({ page, context, isMobile }) => {
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  await context.grantPermissions(['clipboard-read', 'clipboard-write']);
  await page.goto('');
  const example = page.locator('[data-window-example]');
  await expect(example.locator('[data-window-source]')).toHaveText(source);
  const parts = example.locator('[data-window-part]');
  await expect(parts).toHaveCount(7);
  for (const key of isMobile ? [] : Object.keys(guides)) {
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
  const reference = isMobile ? 'changewindowspecnamespaceselectorsindex' : 'changewindowspecconfigadmissionwarningsindex';
  await example.locator('[data-window-help]:visible').getByRole('link', { name: 'API reference' }).click();
  await expect(page).toHaveURL(new RegExp(`/schedule-actions/docs/reference/api/#${reference}$`));
  await expect(page.locator(`#${reference}`)).toBeVisible();
  expect(errors).toEqual([]);
});

test('pointer selection, copy failure, and dark mode stay usable', async ({ page, isMobile }) => {
  await page.addInitScript(() => Object.defineProperty(navigator, 'clipboard', {
    value: { writeText: async () => { throw new Error('Permission denied'); } },
  }));
  await page.emulateMedia({ colorScheme: 'dark' });
  await page.goto('');
  const example = page.locator('[data-window-example]');
  if (!isMobile) {
    await example.locator('[data-window-part="admission"]').click();
    await expect(example.locator('[data-window-help="admission"]')).toBeVisible();
  }
  await example.getByRole('button', { name: 'Copy YAML', exact: true }).click();
  await expect(example.getByRole('status')).toContainText('Use Download YAML');
  await expect(example.getByRole('link', { name: 'Download YAML' })).toBeVisible();
});

test('without JavaScript the YAML links and download remain available', async ({ browser, baseURL, viewport }) => {
  const context = await browser.newContext({ javaScriptEnabled: false, viewport });
  const page = await context.newPage();
  await page.goto(baseURL);
  const example = page.locator('[data-window-example]');
  await expect(example.locator('[data-window-source]')).toHaveText(source);
  await expect(example.getByRole('button', { name: 'Copy YAML', exact: true })).toBeHidden();
  await example.locator('[data-window-part="schedules"]').click();
  await expect(page).toHaveURL(/\/schedule-actions\/docs\/guides\/schedules\/#cron-and-time-zones$/);
  await context.close();
});

test('search resolves examples beneath the project base URL', async ({ page, isMobile }) => {
  const index = page.waitForResponse(response => response.url().endsWith('/schedule-actions/search-index.json'));
  await page.goto('');
  expect((await index).ok()).toBeTruthy();
  const search = page.getByRole('button', { name: 'Search website', exact: true });
  if (isMobile) await search.tap();
  else await search.click();
  await page.locator('.search-text').fill('node pool');
  await page.locator('.search-results').getByRole('link', { name: /Freeze a node pool/ }).click();
  await expect(page).toHaveURL(/\/schedule-actions\/docs\/examples\/node-pool\/$/);
});

for (const searchLoads of [true, false]) {
  test(`main and section navigation work (search loaded: ${searchLoads})`, async ({ page, isMobile }) => {
    if (!searchLoads) await page.route('**/js/search-modal.*.js', route => route.abort());
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    await page.goto('');
    const main = page.locator('#offcanvasNavMain');
    const toggle = page.getByRole('button', { name: 'Open main navigation menu', exact: true });
    if (isMobile) {
      await expect(main).toBeHidden();
      await toggle.tap();
      await expect(main).toBeVisible();
      await expect(page.locator('.offcanvas-backdrop')).toHaveCount(1);
      await main.getByRole('button', { name: 'Close', exact: true }).tap();
      await expect(main).toBeHidden();
      await expect(page.locator('.offcanvas-backdrop')).toHaveCount(0);
      await toggle.tap();
    } else {
      await expect(toggle).toBeHidden();
      await expect(main).toBeVisible();
    }
    await main.getByRole('link', { name: 'Documentation', exact: true }).click();
    await expect(page).toHaveURL(/\/schedule-actions\/docs\/getting-started\/overview\/$/);
    if (isMobile) {
      const section = page.locator('#offcanvasNavSection');
      await expect(section).toBeHidden();
      await page.getByRole('button', { name: 'Open section navigation menu', exact: true }).tap();
      await expect(section).toBeVisible();
      await section.getByRole('button', { name: 'Close', exact: true }).tap();
      await expect(section).toBeHidden();
      await page.getByRole('button', { name: 'Open section navigation menu', exact: true }).tap();
      await section.getByRole('link', { name: 'Installation', exact: true }).click();
      await expect(page).toHaveURL(/\/schedule-actions\/docs\/getting-started\/installation\/$/);
    }
    expect(errors).toEqual([]);
  });
}
