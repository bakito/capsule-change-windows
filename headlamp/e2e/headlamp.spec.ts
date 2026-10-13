import { expect, test } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { actionFixture, deploymentFixture, namespaceFixture } from '../src/testFixtures';

const bundle = readFileSync('dist/main.js', 'utf8');
const packageJSON = readFileSync('package.json', 'utf8');
const name = 'capsule-change-windows';
const action = actionFixture();
action.spec.schedules = [
  { name: 'continuous', cron: '* * * * *', duration: '1h', timeZone: 'UTC' },
];
const deployment = { ...deploymentFixture, spec: { replicas: 3 } };
const replicaSet = {
  apiVersion: 'apps/v1',
  kind: 'ReplicaSet',
  metadata: {
    name: 'web-rs',
    namespace: 'development',
    uid: 'rs-a',
    ownerReferences: [
      {
        apiVersion: 'apps/v1',
        kind: 'Deployment',
        name: 'web',
        uid: 'deployment-a',
        controller: true,
      },
    ],
  },
};
const pod = {
  apiVersion: 'v1',
  kind: 'Pod',
  metadata: {
    name: 'web-pod',
    namespace: 'development',
    uid: 'pod-a',
    resourceVersion: '1',
    creationTimestamp: '2026-10-01T00:00:00Z',
    ownerReferences: [
      { apiVersion: 'apps/v1', kind: 'ReplicaSet', name: 'web-rs', uid: 'rs-a', controller: true },
    ],
  },
  spec: { containers: [{ name: 'web', image: 'example.invalid/web:dev' }] },
  status: {
    phase: 'Running',
    conditions: [],
    containerStatuses: [
      {
        name: 'web',
        ready: true,
        restartCount: 0,
        state: { running: { startedAt: '2026-10-01T00:00:00Z' } },
      },
    ],
  },
};

// Use the real Headlamp frontend/plugin loader, but intercept *every* Kubernetes
// and plugin request in this browser. No tokens, policies, or cluster writes.
test.beforeEach(async ({ page }) => {
  await page.route('**/*', async route => {
    const path = new URL(route.request().url()).pathname;
    const json = (body: unknown, status = 200) =>
      route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) });
    if (path === '/plugins') return json([{ path: `plugins/${name}`, type: 'user', name }]);
    if (path === `/plugins/${name}/package.json`)
      return route.fulfill({ contentType: 'application/json', body: packageJSON });
    if (path === `/plugins/${name}/main.js`)
      return route.fulfill({ contentType: 'application/javascript', body: bundle });
    if (path.startsWith('/clusters/') || path.startsWith('/api/') || path.startsWith('/apis/')) {
      if (path.endsWith('/me')) return json({ username: 'fixture-viewer' });
      if (path.includes('selfsubjectreviews'))
        return json({ status: { userInfo: { username: 'fixture-viewer' } } });
      if (path.includes('selfsubjectaccessreviews')) return json({ status: { allowed: true } });
      if (path.endsWith('/version'))
        return json({ major: '1', minor: '36', gitVersion: 'v1.36.0' });
      if (path.endsWith('/changewindows'))
        return json({
          apiVersion: action.apiVersion,
          kind: 'ChangeWindowList',
          metadata: { resourceVersion: '1' },
          items: [action],
        });
      if (path.endsWith(`/changewindows/${action.metadata.name}`)) return json(action);
      if (path.endsWith('/namespaces'))
        return json({
          apiVersion: 'v1',
          kind: 'NamespaceList',
          metadata: { resourceVersion: '1' },
          items: [
            namespaceFixture,
            {
              ...namespaceFixture,
              metadata: { name: 'kube-system', labels: namespaceFixture.metadata.labels },
            },
            {
              ...namespaceFixture,
              metadata: { name: 'production', labels: { environment: 'production' } },
            },
          ],
        });
      if (path.endsWith('/events'))
        return json({
          apiVersion: 'v1',
          kind: 'EventList',
          metadata: { resourceVersion: '1' },
          items: [
            {
              apiVersion: 'v1',
              kind: 'Event',
              metadata: {
                name: 'observed-window',
                uid: 'event-a',
                namespace: 'default',
                creationTimestamp: '2026-10-01T10:00:00Z',
              },
              involvedObject: { kind: 'ChangeWindow', name: action.metadata.name, uid: 'action-a' },
              type: 'Normal',
              reason: 'Observed',
              message: 'Observed this window',
              source: { component: 'fixture' },
              lastTimestamp: '2026-10-01T10:00:00Z',
            },
            {
              apiVersion: 'v1',
              kind: 'Event',
              metadata: { name: 'old-window', uid: 'event-old' },
              involvedObject: { kind: 'ChangeWindow', name: action.metadata.name, uid: 'old-uid' },
              message: 'Previous resource event',
            },
          ],
        });
      if (path.endsWith('/namespaces/development')) return json(namespaceFixture);
      if (path.endsWith('/pods/web-pod')) return json(pod);
      if (path.endsWith('/replicasets/web-rs')) return json(replicaSet);
      if (path.endsWith('/deployments/web')) return json(deployment);
      return json({
        kind: 'List',
        metadata: { resourceVersion: '1' },
        items: [],
        resources: [],
        groups: [],
        status: { resourceRules: [], nonResourceRules: [] },
      });
    }
    if (!['GET', 'HEAD'].includes(route.request().method())) return route.abort();
    return route.continue();
  });
  await page.routeWebSocket(/\/clusters\//, () => {});
});

test('loads through the real plugin loader and renders calendar, table and details', async ({
  page,
}) => {
  const errors: string[] = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.goto('/c/main/change-windows');
  await expect(page.getByRole('region', { name: 'Weekly action calendar' })).toBeVisible();
  const table = page.getByRole('table', { name: 'Change windows' });
  await expect(table.getByText('Active', { exact: true })).toBeVisible();
  await expect(table.getByText('Ready', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Next week' }).click();
  await table.getByRole('link', { name: action.metadata.name }).click();
  await expect(page.getByRole('table', { name: 'Action schedules' })).toBeVisible();
  await expect(page.getByText('Active window', { exact: true })).toBeVisible();
  await page.screenshot({ path: 'test-results/action-detail.png', fullPage: true });
  expect(errors).toEqual([]);
});

test('adds an active maintenance topic to a Pod through its verified workload owner', async ({
  page,
}) => {
  const errors: string[] = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.goto('/c/main/pods/development/web-pod');
  await expect(page.getByText('Through workload owner: Deployment/web')).toBeVisible();
  await expect(page.getByText('Deny UPDATE on Deployment (parent, scale).')).toBeVisible();
  await page.screenshot({ path: 'test-results/pod-maintenance.png', fullPage: true });
  expect(errors).toEqual([]);
});

for (const status of [403, 404]) {
  test(`explains an unavailable API (${status})`, async ({ page }) => {
    await page.route('**/apis/addons.projectcapsule.dev/v1alpha1/changewindows*', route =>
      route.fulfill({
        status,
        contentType: 'application/json',
        body: JSON.stringify({
          kind: 'Status',
          status: 'Failure',
          code: status,
          message: status === 403 ? 'Forbidden' : 'Not found',
        }),
      })
    );
    await page.goto('/c/main/change-windows');
    // Headlamp's query client retries 404s with 1s/2s/4s backoff before exposing
    // the final error. Wait for that host behavior rather than changing it.
    await expect(page.getByRole('alert')).toContainText(
      status === 403 ? 'permission' : 'not installed',
      { timeout: 12_000 }
    );
    await expect(page.getByRole('table', { name: 'Change windows' })).toHaveCount(0);
  });
}

test('keeps the calendar navigable at a narrow viewport', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/c/main/change-windows');
  await expect(page.getByRole('region', { name: 'Weekly action calendar' })).toBeVisible();
  await page.getByRole('button', { name: 'Next week' }).click();
  await page.getByRole('button', { name: 'Today', exact: true }).click();
  await expect(page.getByRole('table', { name: 'Change windows' })).toBeVisible();
});

for (const mobile of [false, true]) {
  test(`switches calendar periods and drills down (mobile: ${mobile})`, async ({ page }) => {
    if (mobile) await page.setViewportSize({ width: 390, height: 844 });
    const errors: string[] = [];
    page.on('pageerror', error => errors.push(error.message));
    await page.goto('/c/main/change-windows');
    await page.getByRole('button', { name: 'Monthly', exact: true }).click();
    await expect(page.getByRole('region', { name: 'Monthly action calendar' })).toBeVisible();
    const current = await page.getByTestId('calendar-period').innerText();
    await page.getByRole('button', { name: 'Next month' }).click();
    await expect(page.getByTestId('calendar-period')).not.toHaveText(current);
    await page.getByRole('button', { name: 'Previous month' }).click();
    await expect(page.getByTestId('calendar-period')).toHaveText(current);
    await page.getByRole('button', { name: 'Yearly', exact: true }).click();
    const year = await page.getByTestId('calendar-period').innerText();
    await expect(page.getByRole('region', { name: 'Yearly action calendar' })).toBeVisible();
    await page.getByRole('button', { name: 'Next year' }).click();
    await expect(page.getByTestId('calendar-period')).toHaveText(String(Number(year) + 1));
    await page.getByRole('button', { name: `Open February ${Number(year) + 1}` }).click();
    await expect(page.getByRole('region', { name: 'Monthly action calendar' })).toBeVisible();
    await page.getByRole('button', { name: 'Today', exact: true }).click();
    await expect(page.getByTestId('calendar-period')).toHaveText(current);
    await page.getByRole('button', { name: 'Yearly', exact: true }).click();
    await page
      .getByRole('button', { name: /: 1 window. Open week/ })
      .first()
      .click();
    await expect(page.getByRole('region', { name: 'Weekly action calendar' })).toBeVisible();
    const table = page.getByRole('table', { name: 'Change windows' });
    await table.getByRole('link', { name: action.metadata.name }).click();
    await expect(page.getByRole('table', { name: 'Action schedules' })).toBeVisible();
    expect(errors).toEqual([]);
  });
}

test('keeps navigation available while projecting a large year', async ({ page }) => {
  const items = Array.from({ length: 100 }, (_, index) => {
    const item = actionFixture();
    item.metadata.name = `daily-${index}`;
    item.metadata.uid = `daily-${index}`;
    item.spec.schedules = [
      {
        name: 'daily',
        cron: `${index % 60} ${10 + Math.floor(index / 60)} * * *`,
        duration: '2h',
        timeZone: 'Europe/Zurich',
      },
    ];
    return item;
  });
  await page.route('**/apis/addons.projectcapsule.dev/v1alpha1/changewindows*', route =>
    route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        apiVersion: action.apiVersion,
        kind: 'ChangeWindowList',
        metadata: { resourceVersion: '1' },
        items,
      }),
    })
  );
  await page.goto('/c/main/change-windows');
  await expect(page.getByRole('region', { name: 'Weekly action calendar' })).toBeVisible();
  await page.getByRole('button', { name: 'Yearly', exact: true }).click();
  await expect(page.getByText('Calculating calendar…', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Weekly', exact: true }).click();
  await expect(page.getByRole('region', { name: 'Weekly action calendar' })).toBeVisible();
  await expect(page.getByRole('region', { name: 'Yearly action calendar' })).toBeHidden();
});

test('shows completed lifecycle and conditions without treating inactivity as a failure', async ({
  page,
}) => {
  await page.clock.install({ time: new Date('2026-10-01T13:00:00Z') });
  const completed = actionFixture();
  completed.status = {
    observedGeneration: 1,
    active: false,
    nextTransition: '2026-10-02T10:00:00Z',
    conditions: [
      {
        type: 'Ready',
        status: 'True',
        reason: 'Succeeded',
        observedGeneration: 1,
        lastTransitionTime: '2026-10-01T10:00:00Z',
      },
      {
        type: 'Active',
        status: 'False',
        reason: 'Finished',
        observedGeneration: 1,
        lastTransitionTime: '2026-10-01T12:00:00Z',
        message: 'The last observed window has finished.',
      },
    ],
  };
  await page.route(
    /\/apis\/addons\.projectcapsule\.dev\/v1alpha1\/changewindows(?:\/|\?|$)/,
    route =>
      route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify(
          new URL(route.request().url()).pathname.endsWith('/changewindows')
            ? {
                apiVersion: completed.apiVersion,
                kind: 'ChangeWindowList',
                metadata: { resourceVersion: '1' },
                items: [completed],
              }
            : completed
        ),
      })
  );
  await page.goto('/c/main/change-windows');
  const table = page.getByRole('table', { name: 'Change windows' });
  await expect(table.getByRole('cell', { name: 'Finished', exact: true })).toBeVisible();
  await expect(table.getByText('Inactive', { exact: true })).toBeVisible();
  await table.getByRole('link', { name: completed.metadata.name }).click();
  await expect(
    page.getByRole('region', { name: 'Conditions' }).getByText('Finished', { exact: true })
  ).toBeVisible();
  await expect(
    page.getByRole('alert').filter({ hasText: 'The last observed window has finished.' })
  ).toHaveCount(0);
  await expect(page.getByRole('main').locator('dl').first()).toContainText('Next transition');
  await expect(page.getByRole('main').locator('dl').first()).not.toContainText('Not reported');
});

test('uses native metadata, conditions, events and matching visible namespaces', async ({
  page,
}) => {
  const errors: string[] = [];
  const reads: string[] = [];
  page.on('pageerror', error => errors.push(error.message));
  page.on('request', request => {
    if (/\/api\/v1\/(events|namespaces)(?:\?|$)/.test(request.url())) reads.push(request.url());
  });
  await page.goto(`/c/main/change-windows/${action.metadata.name}`);
  const main = page.getByRole('main');
  const metadata = main.locator('dl').first();
  await expect(metadata).toContainText('Name');
  await expect(metadata).toContainText(action.metadata.name);
  await expect(metadata).toContainText('Creation');
  await expect(metadata).toContainText('Labels');
  await expect(metadata).toContainText('platform');
  await expect(metadata).toContainText('Annotations');
  await expect(metadata).toContainText('Workload maintenance');
  await expect(page.getByRole('button', { name: 'View YAML', exact: true })).toBeVisible();
  await expect(
    page
      .getByRole('region', { name: 'Conditions', exact: true })
      .getByText('Ready', { exact: true })
  ).toBeVisible();
  const events = page.getByRole('region', { name: 'Events', exact: true });
  await expect(events).toContainText('Observed this window');
  await expect(events).not.toContainText('Previous resource event');
  const namespaces = page.getByRole('region', { name: 'Namespace selection', exact: true });
  await expect(namespaces.getByRole('link', { name: 'development', exact: true })).toHaveAttribute(
    'href',
    '/c/main/namespaces/development'
  );
  await expect(namespaces.getByRole('link')).toHaveCount(1);
  await expect(namespaces).not.toContainText('kube-system');
  await expect(namespaces).not.toContainText('production');
  expect(
    reads
      .filter(url => /\/events(?:\?|$)/.test(url))
      .every(
        url => new URL(url).searchParams.get('fieldSelector') === 'involvedObject.uid=action-a'
      )
  ).toBe(true);
  expect(reads.filter(url => /\/namespaces(?:\?|$)/.test(url))).toHaveLength(1);
  expect(errors).toEqual([]);
  await page.screenshot({
    path: 'test-results/standard-change-window-details.png',
    fullPage: true,
  });
});

for (const status of [403, 500]) {
  test(`keeps details usable when namespace and event lists fail (${status})`, async ({ page }) => {
    await page.route(/\/api\/v1\/(namespaces|events)(?:\?|$)/, route =>
      route.fulfill({
        status,
        contentType: 'application/json',
        body: JSON.stringify({
          kind: 'Status',
          status: 'Failure',
          reason: status === 403 ? 'Forbidden' : 'InternalError',
          message: 'Fixture list unavailable',
          code: status,
        }),
      })
    );
    await page.goto(`/c/main/change-windows/${action.metadata.name}`);
    await expect(page.getByRole('table', { name: 'Action schedules' })).toBeVisible();
    for (const name of ['Namespace selection', 'Events']) {
      const section = page.getByRole('region', { name, exact: true });
      // Shared Headlamp queries retry 5xx responses after 1, 2 and 4 seconds.
      await expect(section.getByRole('alert')).toContainText(
        status === 403 ? 'permission' : 'Unable to load',
        { timeout: status === 500 ? 10000 : 5000 }
      );
      await expect(section.getByRole('table')).toHaveCount(0);
    }
    await expect(
      page
        .getByRole('region', { name: 'Conditions', exact: true })
        .getByText('Ready', { exact: true })
    ).toBeVisible();
  });
}

for (const mobile of [false, true]) {
  test(
    'shows a compact detail calendar with all views (mobile: ' + mobile + ')',
    async ({ page }) => {
      await page.clock.install({ time: new Date('2026-10-01T11:00:00Z') });
      if (mobile) await page.setViewportSize({ width: 390, height: 844 });
      const errors: string[] = [];
      const listReads: string[] = [];
      page.on('pageerror', error => errors.push(error.message));
      page.on('request', request => {
        if (/\/changewindows(?:\?|$)/.test(request.url())) listReads.push(request.url());
      });
      await page.goto('/c/main/change-windows/' + action.metadata.name);
      const week = page.getByRole('region', { name: 'Weekly action calendar' });
      await expect(week).toBeVisible();
      await week.scrollIntoViewIfNeeded();
      await expect(week.getByRole('group')).toHaveCount(7);
      await expect(week.getByText(action.metadata.name, { exact: true })).toHaveCount(7);
      const weekSize = (await week.boundingBox())!;
      expect(weekSize.height).toBeLessThan(mobile ? 480 : 180);
      if (mobile) {
        expect(weekSize.x).toBeGreaterThanOrEqual(0);
        expect(weekSize.x + weekSize.width).toBeLessThanOrEqual(390);
      }
      await week.screenshot({ path: 'test-results/compact-detail-week-' + mobile + '.png' });
      await page.getByRole('button', { name: 'Monthly', exact: true }).click();
      const month = page.getByRole('region', { name: 'Monthly action calendar' });
      await expect(month).toBeVisible();
      await expect(month.getByRole('group')).toHaveCount(35);
      expect((await month.boundingBox())!.height).toBeLessThan(650);
      await month.scrollIntoViewIfNeeded();
      if (mobile) {
        // Month overflow stays inside its scroll area; the whole page must still fit.
        const scroll = await month.evaluate(element => {
          const parent = element.parentElement!;
          parent.scrollLeft = parent.scrollWidth;
          return { offset: parent.scrollLeft, overflow: getComputedStyle(parent).overflowX };
        });
        expect(scroll.offset).toBeGreaterThan(0);
        expect(scroll.overflow).toBe('auto');
      } else {
        await month.screenshot({ path: 'test-results/compact-detail-month.png' });
      }
      await page.getByRole('button', { name: 'Yearly', exact: true }).click();
      await expect(page.getByRole('region', { name: 'Yearly action calendar' })).toBeVisible();
      await page.getByRole('button', { name: 'Open November 2026' }).click();
      await expect(page.getByTestId('calendar-period')).toHaveText('November 2026');
      await page.getByRole('button', { name: 'Today', exact: true }).click();
      await expect(page.getByTestId('calendar-period')).toHaveText('October 2026');
      await page.getByRole('button', { name: 'Weekly', exact: true }).click();
      await expect(week).toBeVisible();
      expect(listReads).toEqual([]);
      expect(errors).toEqual([]);
    }
  );
}
