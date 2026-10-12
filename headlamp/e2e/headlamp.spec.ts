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
