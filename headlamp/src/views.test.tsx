import type { KubeObject } from '@kinvolk/headlamp-plugin/lib/k8s/cluster';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { Details } from './Details';
import {
  addMaintenanceSection,
  MAINTENANCE_SECTION,
  MaintenanceSection,
} from './MaintenanceSection';
import { NamespaceSelection } from './NamespaceSelection';
import { Overview } from './Overview';
import type { ChangeWindow } from './resources';
import { actionFixture, deploymentFixture, namespaceFixture } from './testFixtures';
import type { ChangeWindowData, Resource } from './types';
import { WindowEvents } from './WindowEvents';

const api = vi.hoisted(() => ({
  cluster: 'cluster-a' as string | null,
  list: vi.fn(),
  items: [] as Array<{ jsonData: ChangeWindowData }> | null,
  error: null as { status: number; message?: string } | null,
  namespace: { jsonData: {} as Resource },
  namespaceError: null as Error | null,
  namespaceList: vi.fn(),
  events: vi.fn(),
  config: {} as { systemNamespaces?: Record<string, string> },
  now: Date.parse('2026-10-01T11:00:00Z'),
  request: vi.fn(),
}));
vi.mock('@kinvolk/headlamp-plugin/lib', () => ({
  ApiProxy: { request: api.request },
  K8s: {
    event: { default: { useList: api.events } },
    ResourceClasses: {
      Namespace: { useGet: () => [api.namespace, api.namespaceError], useList: api.namespaceList },
    },
  },
}));
vi.mock('@kinvolk/headlamp-plugin/lib/Utils', () => ({ getCluster: () => api.cluster }));
vi.mock('@kinvolk/headlamp-plugin/lib/CommonComponents', () => ({
  SectionBox: ({
    title,
    children,
    ...props
  }: {
    title: string;
    children: React.ReactNode;
    role?: string;
    'aria-label'?: string;
  }) => (
    <section {...props}>
      <h2>{title}</h2>
      {children}
    </section>
  ),
  Link: ({
    params,
    children,
    activeCluster,
    routeName,
  }: {
    params?: { name: string };
    children: React.ReactNode;
    activeCluster: string;
    routeName: string;
  }) => (
    <a
      href={`/c/${activeCluster}/${routeName === 'namespace' ? 'namespaces' : 'change-windows'}/${
        params?.name || ''
      }`}
    >
      {children}
    </a>
  ),
  DateLabel: ({ date }: { date: string }) => <span>{date}</span>,
  SimpleTable: ({
    columns,
    data,
    emptyMessage,
  }: {
    columns: Array<{ label: string; getter: (value: unknown) => React.ReactNode }>;
    data: unknown[];
    emptyMessage: string;
  }) =>
    data?.length ? (
      <table>
        <thead>
          <tr>
            {columns.map(column => (
              <th key={column.label}>{column.label}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {data.map((row, index) => (
            <tr key={index}>
              {columns.map(column => (
                <td key={column.label}>{column.getter(row)}</td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    ) : (
      <span>{emptyMessage}</span>
    ),
  Resource: {
    DetailsGrid: ({
      cluster,
      name,
      extraInfo,
      extraSections,
      actions,
      noDefaultActions,
    }: {
      cluster: string;
      name: string;
      extraInfo: (item: ChangeWindow) => Array<{ name: string; value: React.ReactNode }>;
      extraSections: (item: ChangeWindow) => Array<{ id: string; section: React.ReactNode }>;
      actions: (item: ChangeWindow) => React.ReactNode[];
      noDefaultActions: boolean;
    }) => {
      const data = api.items?.[0]?.jsonData;
      if (api.error) return <div role="alert">{api.error.message}</div>;
      if (!data) return <span>Loading resource data</span>;
      const item = { jsonData: data, metadata: data.metadata, cluster } as ChangeWindow;
      return (
        <>
          <h1>{name || data.metadata.name}</h1>
          {actions(item)}
          {!noDefaultActions && <button>Edit</button>}
          <table aria-label="Metadata">
            <tbody>
              <tr>
                <th>Name</th>
                <td>{data.metadata.name}</td>
              </tr>
              {extraInfo(item).map(row => (
                <tr key={row.name}>
                  <th>{row.name}</th>
                  <td>{row.value}</td>
                </tr>
              ))}
            </tbody>
          </table>
          {extraSections(item).map(section => (
            <div key={section.id}>{section.section}</div>
          ))}
        </>
      );
    },
    ViewButton: () => <button>View YAML</button>,
    MetadataDictGrid: ({ dict }: { dict: Record<string, string> }) => (
      <span>
        {Object.entries(dict)
          .map(([key, value]) => `${key}=${value}`)
          .join(', ')}
      </span>
    ),
    ConditionsTable: ({ resource }: { resource: ChangeWindowData }) => (
      <table>
        <tbody>
          {resource.status?.conditions?.map(condition => (
            <tr key={condition.type}>
              <td>{condition.type}</td>
              <td>{condition.status}</td>
              <td>{condition.reason}</td>
            </tr>
          ))}
        </tbody>
      </table>
    ),
  },
}));
vi.mock('./resources', () => ({
  ChangeWindow: { useList: api.list, useGet: () => [api.items?.[0], api.error] },
}));
vi.mock('./settings', () => ({ useSettings: () => api.config }));
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
  api.namespaceList.mockReset().mockReturnValue([[], null]);
  api.events.mockReset().mockReturnValue([[], null]);
  api.config = {};
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
  it('renders a calendar, action table, status, and cluster-aware links', async () => {
    render(<Overview />);
    expect(await screen.findByRole('region', { name: 'Weekly action calendar' })).toBeTruthy();
    const table = screen.getByRole('table', { name: 'Change windows' });
    expect(within(table).getByText('Active')).toBeTruthy();
    expect(within(table).getByText('Ready')).toBeTruthy();
    expect(
      within(table).getByRole('link', { name: 'development-downtime' }).getAttribute('href')
    ).toBe('/c/cluster-a/change-windows/development-downtime');
    fireEvent.click(screen.getByRole('button', { name: 'Next week' }));
    expect(await screen.findByRole('region', { name: 'Weekly action calendar' })).toBeTruthy();
    fireEvent.change(screen.getByLabelText('Filter windows by name or resource kind'), {
      target: { value: 'unknown' },
    });
    expect(screen.getByText('No actions match this filter.')).toBeTruthy();
  });
  it('switches calendar views, navigates years, drills into months and returns to today', async () => {
    render(<Overview />);
    fireEvent.click(screen.getByRole('button', { name: 'Monthly' }));
    expect(await screen.findByRole('region', { name: 'Monthly action calendar' })).toBeTruthy();
    expect(screen.getByTestId('calendar-period').textContent).toBe('October 2026');
    fireEvent.click(screen.getByRole('button', { name: 'Next month' }));
    expect(screen.getByTestId('calendar-period').textContent).toBe('November 2026');
    fireEvent.click(screen.getByRole('button', { name: 'Yearly' }));
    expect(await screen.findByRole('region', { name: 'Yearly action calendar' })).toBeTruthy();
    expect(screen.getAllByRole('button', { name: /^Open .* 2026$/ })).toHaveLength(12);
    fireEvent.click(screen.getByRole('button', { name: 'Next year' }));
    expect(screen.getByTestId('calendar-period').textContent).toBe('2027');
    fireEvent.click(await screen.findByRole('button', { name: 'Open February 2027' }));
    expect(screen.getByTestId('calendar-period').textContent).toBe('February 2027');
    fireEvent.click(screen.getByRole('button', { name: 'Today' }));
    expect(screen.getByTestId('calendar-period').textContent).toBe('October 2026');
    fireEvent.click(screen.getByRole('button', { name: 'Yearly' }));
    fireEvent.click(
      await screen.findByRole('button', { name: 'Thursday, October 1, 2026: 1 window. Open week' })
    );
    expect(await screen.findByRole('region', { name: 'Weekly action calendar' })).toBeTruthy();
    fireEvent.change(screen.getByLabelText('Filter windows by name or resource kind'), {
      target: { value: 'does-not-exist' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Yearly' }));
    expect(
      await screen.findByRole('button', { name: 'Thursday, October 1, 2026: 0 windows. Open week' })
    ).toBeTruthy();
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
  it('keeps malformed schedules visible with an unknown state', async () => {
    api.items![0].jsonData.spec.schedules[0].cron = 'broken';
    render(<Overview />);
    expect(within(screen.getByRole('table')).getByText('Unknown')).toBeTruthy();
    expect((await screen.findByRole('alert')).textContent).toContain('Unsupported cron');
  });
});

describe('ChangeWindow detail calendar', () => {
  it('previews only the current resource in every view without listing other windows', async () => {
    const other = actionFixture();
    other.metadata.name = 'another-window';
    other.metadata.uid = 'another-uid';
    api.items!.push({ jsonData: other });
    render(
      <MemoryRouter>
        <Details />
      </MemoryRouter>
    );
    const week = await screen.findByRole('region', { name: 'Weekly action calendar' });
    expect(within(week).getAllByRole('group')).toHaveLength(7);
    expect(within(week).getAllByText('development-downtime')).toHaveLength(7);
    expect(within(week).queryByText('another-window')).toBeNull();
    expect(
      within(week)
        .getByRole('group', { name: 'Thursday, October 1, 2026' })
        .querySelector('[aria-current="date"]')
    ).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Monthly' }));
    const month = await screen.findByRole('region', { name: 'Monthly action calendar' });
    expect(within(month).getAllByText('development-downtime')).toHaveLength(35);
    expect(within(month).queryByText('another-window')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Yearly' }));
    expect(
      await screen.findByRole('button', {
        name: 'Thursday, October 1, 2026: 1 window. Open week',
      })
    ).toBeTruthy();
    expect(screen.queryByRole('button', { name: /: 2 windows. Open week/ })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Open November 2026' }));
    expect(screen.getByTestId('calendar-period').textContent).toBe('November 2026');
    fireEvent.click(screen.getByRole('button', { name: 'Today' }));
    expect(screen.getByTestId('calendar-period').textContent).toBe('October 2026');
    expect(api.list).not.toHaveBeenCalled();
  });
  it('shows one empty-period message and refreshes when the resource schedule changes', async () => {
    api.items![0].jsonData.spec.schedules[0].cron = '0 10 1 10 *';
    const view = render(
      <MemoryRouter>
        <Details />
      </MemoryRouter>
    );
    await screen.findByRole('region', { name: 'Weekly action calendar' });
    fireEvent.click(screen.getByRole('button', { name: 'Next week' }));
    expect(await screen.findByText('No scheduled windows in this period.')).toBeTruthy();
    const week = screen.getByRole('region', { name: 'Weekly action calendar' });
    expect(within(week).queryByText('development-downtime')).toBeNull();
    expect(screen.queryByText('No windows')).toBeNull();
    api.items = [{ jsonData: actionFixture() }];
    view.rerender(
      <MemoryRouter>
        <Details />
      </MemoryRouter>
    );
    const refreshedWeek = await screen.findByRole('region', { name: 'Weekly action calendar' });
    expect(within(refreshedWeek).getAllByText('development-downtime')).toHaveLength(7);
    expect(screen.queryByText('No scheduled windows in this period.')).toBeNull();
  });
});

describe('controller lifecycle', () => {
  it.each(['Planned', 'Finished'])(
    'shows %s as normal inactive state in overview and details',
    reason => {
      api.now = Date.parse('2026-10-01T13:00:00Z');
      const action = api.items![0].jsonData;
      action.status!.nextTransition = '2026-10-02T10:00:00Z';
      action.status!.conditions!.push({
        type: 'Active',
        status: 'False',
        reason,
        observedGeneration: 1,
        message: 'Window is inactive.',
      });
      const overview = render(<Overview />);
      expect(
        within(screen.getByRole('table', { name: 'Change windows' })).getByText(reason)
      ).toBeTruthy();
      overview.unmount();
      render(
        <MemoryRouter>
          <Details />
        </MemoryRouter>
      );
      expect(
        within(screen.getByRole('table', { name: 'Metadata' })).getByText(reason)
      ).toBeTruthy();
      expect(
        within(screen.getByRole('region', { name: 'Conditions' })).getByText(reason)
      ).toBeTruthy();
      expect(screen.queryByRole('alert')).toBeNull();
      expect(screen.getByRole('table', { name: 'Metadata' }).textContent).not.toContain(
        'Not reported'
      );
    }
  );
  it('does not claim a stale lifecycle is current', () => {
    const action = api.items![0].jsonData;
    action.status!.conditions!.push({
      type: 'Active',
      status: 'False',
      reason: 'Finished',
      observedGeneration: 0,
    });
    render(
      <MemoryRouter>
        <Details />
      </MemoryRouter>
    );
    expect(
      within(screen.getByRole('table', { name: 'Metadata' })).getByText('Reconciling')
    ).toBeTruthy();
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

describe('standard ChangeWindow details', () => {
  it('starts with metadata, then conditions and events, and offers View YAML', () => {
    render(
      <MemoryRouter>
        <Details />
      </MemoryRouter>
    );
    expect(screen.getByRole('table', { name: 'Metadata' })).toBe(screen.getAllByRole('table')[0]);
    expect(screen.getByRole('button', { name: 'View YAML' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Edit' })).toBeNull();
    expect(
      screen
        .getAllByRole('heading', { level: 2 })
        .slice(0, 3)
        .map(item => item.textContent)
    ).toEqual(['Conditions', 'Events', 'Namespace selection']);
    expect(screen.queryByText('Resource definition')).toBeNull();
  });
});

describe('namespace selection details', () => {
  const namespace = (name: string, labels: Record<string, string> = {}, cluster = 'cluster-a') => ({
    cluster,
    jsonData: { kind: 'Namespace', metadata: { name, labels } },
    metadata: { name, labels },
    status: { phase: 'Active' },
  });
  it('shows only visible matching namespaces, ORs selectors, excludes infrastructure and keeps cluster links', () => {
    const action = actionFixture();
    action.spec.namespaceSelectors = [
      {
        matchLabels: { environment: 'development' },
        matchExpressions: [{ key: 'team', operator: 'In', values: ['a'] }],
      },
      { matchLabels: { environment: 'staging' } },
    ];
    api.config = { systemNamespaces: { 'cluster-a': 'controller-custom' } };
    api.namespaceList.mockReturnValue([
      [
        namespace('selected', { environment: 'development', team: 'a' }),
        namespace('staging', { environment: 'staging' }),
        namespace('other-team', { environment: 'development', team: 'b' }),
        namespace('production', { environment: 'production' }),
        namespace('other-cluster', { environment: 'staging' }, 'cluster-b'),
        ...['kube-system', 'kube-public', 'kube-node-lease', 'controller-custom'].map(name =>
          namespace(name, { environment: 'staging' })
        ),
      ],
      null,
    ]);
    render(<NamespaceSelection action={action} cluster="cluster-a" />);
    expect(screen.getAllByRole('link').map(link => link.textContent)).toEqual([
      'selected',
      'staging',
    ]);
    expect(screen.getByRole('link', { name: 'selected' }).getAttribute('href')).toBe(
      '/c/cluster-a/namespaces/selected'
    );
    expect(api.namespaceList).toHaveBeenCalledWith({ cluster: 'cluster-a' });
    expect(api.request).not.toHaveBeenCalled();
  });
  it('updates with namespace labels, selectors and cluster settings', () => {
    const action = actionFixture();
    api.namespaceList.mockReturnValue([
      [namespace('development', { environment: 'development' })],
      null,
    ]);
    const view = render(<NamespaceSelection action={action} cluster="cluster-a" />);
    expect(screen.getByRole('link', { name: 'development' })).toBeTruthy();
    api.namespaceList.mockReturnValue([
      [namespace('development', { environment: 'production' })],
      null,
    ]);
    view.rerender(<NamespaceSelection action={action} cluster="cluster-a" />);
    expect(screen.queryByRole('link')).toBeNull();
    view.rerender(
      <NamespaceSelection
        action={{ ...action, spec: { ...action.spec, namespaceSelectors: [{}] } }}
        cluster="cluster-a"
      />
    );
    expect(screen.getByRole('link', { name: 'development' })).toBeTruthy();
    api.config = { systemNamespaces: { 'cluster-a': 'development' } };
    view.rerender(
      <NamespaceSelection
        action={{ ...action, spec: { ...action.spec, namespaceSelectors: [{}] } }}
        cluster="cluster-a"
      />
    );
    expect(screen.queryByRole('link')).toBeNull();
  });
  it.each([undefined, []])('does not list namespaces without selectors (%s)', selectors => {
    const action = actionFixture();
    action.spec.namespaceSelectors = selectors;
    render(<NamespaceSelection action={action} cluster="cluster-a" />);
    expect(screen.getByText(/No namespace selectors/)).toBeTruthy();
    expect(api.namespaceList).not.toHaveBeenCalled();
  });
  it('distinguishes loading, no matches and forbidden or failed reads', () => {
    const action = actionFixture();
    api.namespaceList.mockReturnValue([null, null]);
    const view = render(<NamespaceSelection action={action} cluster="cluster-a" />);
    expect(screen.getByLabelText('Loading namespace selection')).toBeTruthy();
    api.namespaceList.mockReturnValue([[], null]);
    view.rerender(<NamespaceSelection action={action} cluster="cluster-a" />);
    expect(screen.getByText(/No visible namespaces match/)).toBeTruthy();
    for (const status of [403, 500]) {
      api.namespaceList.mockReturnValue([
        [namespace('stale', { environment: 'development' })],
        { status, message: 'unavailable' },
      ]);
      view.rerender(<NamespaceSelection action={action} cluster="cluster-a" />);
      expect(screen.getByRole('alert').textContent).toContain(
        status === 403 ? 'permission' : 'Unable to load'
      );
      expect(screen.queryByText(/No visible namespaces match/)).toBeNull();
      expect(screen.queryByRole('link')).toBeNull();
    }
  });
});

describe('ChangeWindow events', () => {
  const item = (uid = 'action-a') => ({ metadata: { uid } } as ChangeWindow);
  const event = (uid: string, cluster = 'cluster-a') => ({
    cluster,
    involvedObject: { uid },
    type: 'Normal',
    reason: uid,
    message: 'Window reconciled',
    source: {},
    jsonData: {},
    lastOccurrence: '2026-10-01T10:00:00Z',
  });
  it('uses a bounded shared UID and cluster query, excluding replaced resources', () => {
    api.events.mockReturnValue([
      [event('action-a'), event('old-uid'), event('action-a', 'cluster-b')],
      null,
    ]);
    const view = render(<WindowEvents item={item()} cluster="cluster-a" />);
    expect(api.events).toHaveBeenCalledWith({
      cluster: 'cluster-a',
      namespace: '',
      fieldSelector: 'involvedObject.uid=action-a',
      limit: 2000,
      refetchInterval: 30000,
    });
    expect(screen.getAllByText('Window reconciled')).toHaveLength(1);
    expect(screen.queryByText('old-uid')).toBeNull();
    view.rerender(<WindowEvents item={item('replacement')} cluster="cluster-b" />);
    expect(api.events).toHaveBeenLastCalledWith(
      expect.objectContaining({
        cluster: 'cluster-b',
        fieldSelector: 'involvedObject.uid=replacement',
      })
    );
    expect(screen.queryByText('Window reconciled')).toBeNull();
  });
  it('distinguishes loading, no events, and errors', () => {
    api.events.mockReturnValue([null, null]);
    const view = render(<WindowEvents item={item()} cluster="cluster-a" />);
    expect(screen.getByLabelText('Loading events')).toBeTruthy();
    api.events.mockReturnValue([[], null]);
    view.rerender(<WindowEvents item={item()} cluster="cluster-a" />);
    expect(screen.getByText('No events for this ChangeWindow.')).toBeTruthy();
    for (const status of [403, 500]) {
      api.events.mockReturnValue([[], { status, message: 'unavailable' }]);
      view.rerender(<WindowEvents item={item()} cluster="cluster-a" />);
      expect(screen.getByRole('alert').textContent).toContain(
        status === 403 ? 'permission' : 'Unable to load'
      );
      expect(screen.queryByText('No events for this ChangeWindow.')).toBeNull();
    }
  });
  it('never fetches an unscoped event list when identity is missing', () => {
    render(<WindowEvents item={item('')} cluster="cluster-a" />);
    expect(api.events).not.toHaveBeenCalled();
    expect(screen.getByText(/until the resource has a UID/)).toBeTruthy();
  });
});
