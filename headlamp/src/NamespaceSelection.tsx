import { K8s } from '@kinvolk/headlamp-plugin/lib';
import {
  Link,
  Resource,
  SectionBox,
  SimpleTable,
} from '@kinvolk/headlamp-plugin/lib/CommonComponents';
import type Namespace from '@kinvolk/headlamp-plugin/lib/k8s/namespace';
import { Alert, CircularProgress, Typography } from '@mui/material';
import { useMemo } from 'react';
import { DEFAULT_SYSTEM_NAMESPACE, matchesNamespace } from './matching';
import { useSettings } from './settings';
import type { ChangeWindowData } from './types';

export function NamespaceSelection({
  action,
  cluster,
}: {
  action: ChangeWindowData;
  cluster: string;
}) {
  return (
    <SectionBox title="Namespace selection" role="region" aria-label="Namespace selection">
      {action.spec.namespaceSelectors?.length ? (
        <SelectedNamespaces action={action} cluster={cluster} />
      ) : (
        <Typography>
          No namespace selectors are configured. Node-only windows select Nodes directly.
        </Typography>
      )}
    </SectionBox>
  );
}

function SelectedNamespaces({ action, cluster }: { action: ChangeWindowData; cluster: string }) {
  const [namespaces, error] = K8s.ResourceClasses.Namespace.useList({ cluster });
  const config = useSettings();
  const systemNamespace = config?.systemNamespaces?.[cluster] || DEFAULT_SYSTEM_NAMESPACE;
  const selected = useMemo(
    () =>
      namespaces?.filter(
        namespace =>
          namespace.cluster === cluster &&
          matchesNamespace(action, namespace.jsonData, systemNamespace)
      ) ?? null,
    [namespaces, action, systemNamespace, cluster]
  );
  if (error)
    return (
      <Alert severity="warning">
        {error.status === 403
          ? 'You do not have permission to list namespaces in this cluster; namespace selection is unavailable.'
          : `Unable to load namespaces: ${error.message || 'request failed'}`}
      </Alert>
    );
  if (!selected) return <CircularProgress aria-label="Loading namespace selection" />;
  return (
    <>
      <Typography sx={{ mb: 2 }}>
        Matching namespaces visible to your account. Protected namespaces are excluded. This is
        current selection; action participants were recorded when the window started.
      </Typography>
      <SimpleTable
        data={selected}
        columns={[
          {
            label: 'Name',
            getter: (namespace: Namespace) => (
              <Link
                routeName="namespace"
                params={{ name: namespace.metadata.name }}
                activeCluster={cluster}
              >
                {namespace.metadata.name}
              </Link>
            ),
          },
          {
            label: 'Status',
            getter: (namespace: Namespace) => namespace.status?.phase || 'Unknown',
          },
          {
            label: 'Labels',
            getter: (namespace: Namespace) => (
              <Resource.MetadataDictGrid dict={namespace.metadata.labels || {}} />
            ),
          },
        ]}
        emptyMessage="No visible namespaces match this window's selectors."
        reflectInURL={false}
      />
    </>
  );
}
