import type { registerDetailsViewSectionsProcessor } from '@kinvolk/headlamp-plugin/lib';
import { ApiProxy, K8s } from '@kinvolk/headlamp-plugin/lib';
import { SectionBox } from '@kinvolk/headlamp-plugin/lib/CommonComponents';
import type { KubeObject } from '@kinvolk/headlamp-plugin/lib/k8s/cluster';
import { Alert, Box, Chip, CircularProgress, Stack, Typography } from '@mui/material';
import { useEffect, useMemo, useState } from 'react';
import { readyStatus, useCurrentProjections, useNow } from './hooks';
import { controllingOwners, DEFAULT_SYSTEM_NAMESPACE, matchingTargets } from './matching';
import { ActionLink, formatTime, LoadError } from './Overview';
import { ChangeWindow } from './resources';
import { currentWindow, HORIZON } from './schedule';
import { useSettings } from './settings';
import type { ChangeWindowData, Resource } from './types';

const Namespace = K8s.ResourceClasses.Namespace;

function useOwners(resource: Resource, cluster: string) {
  const key = `${cluster}/${resource.metadata.namespace}/${resource.metadata.uid}/${JSON.stringify(
    resource.metadata.ownerReferences
  )}`;
  const [state, setState] = useState<{ key: string; owners: Resource[]; error?: string }>();
  useEffect(() => {
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    const refresh = async () => {
      try {
        const owners = await controllingOwners(resource, path =>
          ApiProxy.request(path, {
            cluster,
            signal: controller.signal,
            autoLogoutOnAuthError: false,
          })
        );
        if (!controller.signal.aborted) setState({ key, owners });
      } catch {
        if (!controller.signal.aborted)
          setState({
            key,
            owners: [],
            error:
              'Some workload owners could not be read. Their maintenance windows cannot be determined.',
          });
      } finally {
        // Refresh labels/owner changes with bounded, non-overlapping requests.
        if (!controller.signal.aborted) timer = setTimeout(refresh, 30_000);
      }
    };
    void refresh();
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
    // The key contains every relationship input. Resource status changes should
    // not trigger another owner-chain fetch.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key]);
  return state?.key === key ? state : undefined;
}

function SelectedWindows({
  resource,
  cluster,
  actions,
}: {
  resource: Resource;
  cluster: string;
  actions: ChangeWindowData[];
}) {
  const [namespace, namespaceError] = Namespace.useGet(resource.metadata.namespace!, undefined, {
    cluster,
  });
  const owners = useOwners(resource, cluster);
  const config = useSettings();
  const systemNamespace = config?.systemNamespaces?.[cluster] || DEFAULT_SYSTEM_NAMESPACE;
  const now = useNow();
  const relevant = useMemo(
    () =>
      namespace
        ? actions.filter(action =>
            [resource, ...(owners?.owners || [])].some(
              subject =>
                matchingTargets(action, subject, namespace.jsonData, systemNamespace).length
            )
          )
        : [],
    [actions, namespace, resource, owners, systemNamespace]
  );
  const current = useCurrentProjections(relevant, now);
  if (namespaceError)
    return (
      <Alert severity="warning">
        Namespace labels cannot be read; maintenance selection is unavailable.
      </Alert>
    );
  if (!namespace) return <CircularProgress size={20} aria-label="Loading namespace selection" />;
  const matches = current.flatMap(projection =>
    [resource, ...(owners?.owners || [])].flatMap(subject => {
      const targets = matchingTargets(
        projection.action,
        subject,
        namespace.jsonData,
        systemNamespace
      );
      if (!targets.length) return [];
      const window = currentWindow(projection.windows, now);
      if (!window && !projection.error) return [];
      return [{ ...projection, subject, targets, window }];
    })
  );
  return (
    <Stack spacing={2} sx={{ p: 2 }}>
      {!owners && <Typography color="text.secondary">Checking workload owners…</Typography>}
      {owners?.error && <Alert severity="warning">{owners.error}</Alert>}
      {!matches.length && owners && !owners.error && (
        <Typography color="text.secondary">
          No active windows select this resource or its supported workload owners.
        </Typography>
      )}
      {matches.map(({ action, subject, targets, window, error }) => {
        const names = new Set(targets.map(target => target.name));
        const steps =
          action.spec.actions?.filter(step => step.targets.some(target => names.has(target))) || [];
        const rules =
          action.spec.admission?.filter(
            rule =>
              (rule.when || 'Active') === 'Active' && rule.targets.some(target => names.has(target))
          ) || [];
        const direct = subject === resource;
        return (
          <Box
            key={`${action.metadata.uid || action.metadata.name}/${
              subject.metadata.uid || subject.metadata.name
            }`}
          >
            <Stack direction="row" spacing={1} alignItems="center">
              <ActionLink action={action} cluster={cluster} />
              <Chip
                label={error ? 'Unknown' : 'Active'}
                color={error ? 'error' : 'warning'}
                size="small"
              />
            </Stack>
            <Typography>
              {direct ? 'Direct target' : 'Through workload owner'}: {subject.kind}/
              {subject.metadata.name}
            </Typography>
            {error ? (
              <Alert severity="warning">{error}</Alert>
            ) : (
              window && (
                <Typography variant="body2">
                  {window.end <= now + HORIZON
                    ? `Until ${formatTime(window.end)}`
                    : 'End beyond the preview horizon'}{' '}
                  · Controller: {readyStatus(action)}
                </Typography>
              )
            )}
            {!!steps.length && (
              <Typography variant="body2">
                Resource actions: {steps.map(step => step.name).join(', ')}
              </Typography>
            )}
            {rules.map(rule => (
              <Typography variant="body2" key={rule.name}>
                Deny {rule.operations.join(', ')} on {subject.kind} (
                {(rule.subresources?.length ? rule.subresources : [''])
                  .map(value => value || 'parent')
                  .join(', ')}
                ).
              </Typography>
            ))}
            {!direct && (
              <Typography variant="caption" color="text.secondary">
                This window selects the workload owner. Restrictions on this resource require a
                matching {resource.kind} target.
              </Typography>
            )}
          </Box>
        );
      })}
      {!!matches.length && (
        <Typography variant="caption" color="text.secondary">
          Selection uses current labels and scheduled time. Resource actions use a snapshot taken at
          window start; this view does not prove that a specific patch has completed.
        </Typography>
      )}
    </Stack>
  );
}

export function MaintenanceSection({ resource }: { resource: KubeObject }) {
  const [items, error] = ChangeWindow.useList<ChangeWindow>({ cluster: resource.cluster });
  const actions = useMemo(() => (items || []).map(item => item.jsonData), [items]);
  return (
    <SectionBox title="Maintenance windows">
      {error ? (
        <LoadError error={error} />
      ) : !items ? (
        <CircularProgress size={20} aria-label="Loading maintenance windows" />
      ) : !actions.length ? (
        <Typography sx={{ p: 2 }}>No ChangeWindows are configured in this cluster.</Typography>
      ) : (
        <SelectedWindows
          resource={resource.jsonData}
          cluster={resource.cluster}
          actions={actions}
        />
      )}
    </SectionBox>
  );
}

export const MAINTENANCE_SECTION = 'capsule-change-windows-maintenance';
type SectionsProcessor = Exclude<
  Parameters<typeof registerDetailsViewSectionsProcessor>[0],
  { id: string }
>;
export const addMaintenanceSection: SectionsProcessor = (resource, sections) => {
  const sectionID = (section: (typeof sections)[number]) =>
    section && typeof section === 'object' && 'id' in section ? section.id : undefined;
  if (
    !resource?.jsonData?.metadata?.namespace ||
    sections.some(section => sectionID(section) === MAINTENANCE_SECTION)
  )
    return sections;
  const added = { id: MAINTENANCE_SECTION, section: <MaintenanceSection resource={resource} /> };
  const metadata = sections.findIndex(section => sectionID(section) === 'METADATA');
  const index = metadata < 0 ? 0 : metadata + 1;
  return [...sections.slice(0, index), added, ...sections.slice(index)];
};
