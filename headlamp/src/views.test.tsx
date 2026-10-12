import type { KubeObject } from '@kinvolk/headlamp-plugin/lib/k8s/cluster';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { Details } from './Details';
import {
  addMaintenanceSection,
  MAINTENANCE_SECTION,
  MaintenanceSection,
} from './MaintenanceSection';
import { Overview } from './Overview';
import { actionFixture, deploymentFixture, namespaceFixture } from './testFixtures';
import type { ChangeWindowData, Resource } from './types';

const api = vi.hoisted(() => ({
  cluster: 'cluster-a' as string | null,
  list: vi.fn(),
  items: [] as Array<{ jsonData: ChangeWindowData }> | null,
  error: null as { status: number; message?: string } | null,
  namespace: { jsonData: {} as Resource },
  namespaceError: null as Error | null,
  now: Date.parse('2026-10-01T11:00:00Z'),
  request: vi.fn(),
}));
vi.mock('@kinvolk/headlamp-plugin/lib', () => ({
  ApiProxy: { request: api.request },
  K8s: { ResourceClasses: { Namespace: { useGet: () => [api.namespace, api.namespaceError] } } },
}));
vi.mock('@kinvolk/headlamp-plugin/lib/Utils', () => ({ getCluster: () => api.cluster }));
vi.mock('@kinvolk/headlamp-plugin/lib/CommonComponents', () => ({
  SectionBox: ({ title, children }: { title: string; children: React.ReactNode }) => (
    <section>
      <h2>{title}</h2>
      {children}
    </section>
  ),
  Link: ({
    params,
    children,
    activeCluster,
  }: {
    params?: { name: string };
    children: React.ReactNode;
    activeCluster: string;
  }) => <a href={`/c/${activeCluster}/change-windows/${params?.name || ''}`}>{children}</a>,
}));
vi.mock('./resources', () => ({ ChangeWindow: { useList: api.list } }));
vi.mock('./settings', () => ({ useSettings: () => ({}) }));
vi.mock('./hooks', async importOriginal => ({
  ...(await importOriginal<object>()),
  useNow: () => api.now,
}));

beforeEach(() => {
  api.cluster = 'cluster-a';
  api.list.mockReset().mockImplementation(() => [api.items, api.error]);
  api.items = [{ jsonData: actionFixture() }];
  api.error = null;
  api.namespace = { jsonData: namespaceFixture };
  api.namespaceError = null;
  api.now = Date.parse('2026-10-01T11:00:00Z');
  api.request.mockReset();
});
afterEach(cleanup);

describe('Change Windows overview', () => {
  it.each([Overview, Details])(
    'requires a single cluster before reading policies (%s)',
    Component => {
      api.cluster = null;
      render(<Component />);
      expect(screen.getByRole('alert').textContent).toContain('Select a single cluster');
      expect(api.list).not.toHaveBeenCalled();
    }
  );
  it('paginates all actions and resets the page when filtering', () => {
    api.items = Array.from({ length: 51 }, (_, index) => {
      const action = actionFixture();
      action.metadata.name = `action-${String(index).padStart(3, '0')}`;
      action.metadata.uid = action.metadata.name;
      return { jsonData: action };
    });
    render(<Overview />);
    const table = screen.getByRole('table', { name: 'Change windows' });
    expect(within(table).getAllByRole('row')).toHaveLength(51);
    fireEvent.click(screen.getByRole('button', { name: 'Go to next page' }));
    expect(within(table).getAllByRole('row')).toHaveLength(2);
    expect(within(table).getByRole('link').textContent).toBe('action-050');
    fireEvent.change(screen.getByLabelText('Filter windows by name or resource kind'), {
      target: { value: 'action-000' },
    });
    expect(within(table).getByRole('link').textContent).toBe('action-000');
    expect(api.list).toHaveBeenCalledWith({ cluster: 'cluster-a' });
  });
  it('renders a calendar, action table, status, and cluster-aware links', () => {
    render(<Overview />);
    expect(screen.getByRole('region', { name: 'Weekly action calendar' })).toBeTruthy();
    const table = screen.getByRole('table', { name: 'Change windows' });
    expect(within(table).getByText('Active')).toBeTruthy();
    expect(within(table).getByText('Ready')).toBeTruthy();
    expect(
      within(table).getByRole('link', { name: 'development-downtime' }).getAttribute('href')
    ).toBe('/c/cluster-a/change-windows/development-downtime');
    fireEvent.click(screen.getByRole('button', { name: 'Next week' }));
    expect(screen.getByRole('region', { name: 'Weekly action calendar' })).toBeTruthy();
    fireEvent.change(screen.getByLabelText('Filter windows by name or resource kind'), {
      target: { value: 'unknown' },
    });
    expect(screen.getByText('No actions match this filter.')).toBeTruthy();
  });
  it.each([403, 404, 500])('shows API errors instead of an empty-success state (%s)', status => {
    api.error = { status, message: 'test error' };
    render(<Overview />);
    expect(screen.getByRole('alert')).toBeTruthy();
    expect(screen.queryByText('No ChangeWindows found in this cluster.')).toBeNull();
  });
  it('distinguishes loading from an empty list', () => {
    api.items = null;
    const view = render(<Overview />);
    expect(screen.getByLabelText('Loading windows')).toBeTruthy();
    api.items = [];
    view.rerender(<Overview />);
    expect(screen.getByText('No ChangeWindows found in this cluster.')).toBeTruthy();
  });
  it('updates state at the exclusive end even with stale status.active', () => {
    api.items![0].jsonData.status!.active = true;
    const view = render(<Overview />);
    api.now = Date.parse('2026-10-01T12:00:00Z');
    view.rerender(<Overview />);
    expect(within(screen.getByRole('table')).getByText('Inactive')).toBeTruthy();
  });
  it('keeps malformed schedules visible with an unknown state', () => {
    api.items![0].jsonData.spec.schedules[0].cron = 'broken';
    render(<Overview />);
    expect(within(screen.getByRole('table')).getByText('Unknown')).toBeTruthy();
    expect(screen.getByRole('alert').textContent).toContain('Unsupported cron');
  });
});

describe('resource maintenance section', () => {
  const resource = (data = deploymentFixture) =>
    ({ jsonData: data, cluster: 'cluster-a' } as KubeObject);
  it('shows the active rule on a directly selected resource', async () => {
    render(<MaintenanceSection resource={resource()} />);
    expect(await screen.findByText('Direct target: Deployment/web')).toBeTruthy();
    expect(screen.getByText('Deny UPDATE on Deployment (parent, scale).')).toBeTruthy();
  });
  it('does not leak matches across namespaces', async () => {
    api.namespace = {
      jsonData: {
        ...namespaceFixture,
        metadata: { name: 'production', labels: { environment: 'production' } },
      },
    };
    render(
      <MaintenanceSection
        resource={resource({
          ...deploymentFixture,
          metadata: { ...deploymentFixture.metadata, namespace: 'production' },
        })}
      />
    );
    expect(
      await screen.findByText(
        'No active windows select this resource or its supported workload owners.'
      )
    ).toBeTruthy();
    expect(screen.queryByText('Active')).toBeNull();
  });
  it('reports inaccessible namespace labels rather than inferring selection', async () => {
    api.namespaceError = new Error('Forbidden');
    render(<MaintenanceSection resource={resource()} />);
    expect(
      await screen.findByText(
        'Namespace labels cannot be read; maintenance selection is unavailable.'
      )
    ).toBeTruthy();
    expect(screen.queryByText('Active')).toBeNull();
  });
  it('connects a Pod to its verified controlling Deployment', async () => {
    const pod: Resource = {
      apiVersion: 'v1',
      kind: 'Pod',
      metadata: {
        name: 'web-pod',
        namespace: 'development',
        uid: 'pod-a',
        ownerReferences: [
          {
            apiVersion: 'apps/v1',
            kind: 'ReplicaSet',
            name: 'web-rs',
            uid: 'rs-a',
            controller: true,
          },
        ],
      },
    };
    api.request
      .mockResolvedValueOnce({
        apiVersion: 'apps/v1',
        kind: 'ReplicaSet',
        metadata: {
          name: 'web-rs',
          uid: 'rs-a',
          namespace: 'development',
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
      })
      .mockResolvedValueOnce(deploymentFixture);
    render(<MaintenanceSection resource={resource(pod)} />);
    expect(await screen.findByText('Through workload owner: Deployment/web')).toBeTruthy();
    expect(
      screen.getByText(/Restrictions on this resource require a matching Pod target/)
    ).toBeTruthy();
    expect(api.request).toHaveBeenCalledTimes(2);
    expect(api.request.mock.calls[0][1].cluster).toBe('cluster-a');
  });
  it('inserts its section once, preserves opaque sections, and skips cluster resources', () => {
    const sections = [{ id: 'METADATA', section: <div /> }, 'opaque'];
    const added = addMaintenanceSection(resource(), sections);
    expect(added).toHaveLength(3);
    expect(added[1]).toHaveProperty('id', MAINTENANCE_SECTION);
    expect(addMaintenanceSection(resource(), added)).toBe(added);
    expect(sections).toHaveLength(2);
    expect(addMaintenanceSection(resource(namespaceFixture), sections)).toBe(sections);
    expect(addMaintenanceSection(null, sections)).toBe(sections);
  });
});
