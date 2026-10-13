import { Resource, SectionBox } from '@kinvolk/headlamp-plugin/lib/CommonComponents';
import { getCluster } from '@kinvolk/headlamp-plugin/lib/Utils';
import {
  Alert,
  Box,
  Chip,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  Typography,
} from '@mui/material';
import { useMemo } from 'react';
import { useParams } from 'react-router-dom';
import { lifecycleStatus, readyStatus, useCurrentProjections, useNow } from './hooks';
import { NamespaceSelection } from './NamespaceSelection';
import { formatTime } from './Overview';
import { ChangeWindow } from './resources';
import { currentWindow, HORIZON } from './schedule';
import { WindowCalendar } from './WindowCalendar';
import { WindowEvents } from './WindowEvents';

export function Details() {
  const cluster = getCluster();
  if (!cluster)
    return <Alert severity="info">Select a single cluster to view Change Windows.</Alert>;
  return <ClusterDetails key={cluster} cluster={cluster} />;
}

function ClusterDetails({ cluster }: { cluster: string }) {
  const { name } = useParams<{ name: string }>();
  return (
    <Resource.DetailsGrid
      resourceType={ChangeWindow}
      name={name}
      cluster={cluster}
      backLink={`/c/${encodeURIComponent(cluster)}/change-windows`}
      noDefaultActions
      actions={item => (item ? [<Resource.ViewButton key="yaml" item={item} />] : [])}
      extraInfo={item =>
        item
          ? [
              { name: 'Controller', value: readyStatus(item.jsonData) },
              { name: 'Lifecycle', value: lifecycleStatus(item.jsonData) },
              {
                name: 'Next transition',
                value: item.jsonData.status?.nextTransition
                  ? formatTime(Date.parse(item.jsonData.status.nextTransition))
                  : 'Not reported',
              },
              { name: 'Pending resources', value: item.jsonData.status?.pendingResources || 0 },
            ]
          : []
      }
      extraSections={item =>
        item
          ? [
              {
                id: 'capsule-change-windows-details',
                section: (
                  <WindowSections
                    key={`${cluster}/${item.metadata.uid}`}
                    item={item}
                    cluster={cluster}
                  />
                ),
              },
            ]
          : []
      }
    />
  );
}

function WindowSections({ item, cluster }: { item: ChangeWindow; cluster: string }) {
  const actions = useMemo(() => (item ? [item.jsonData] : []), [item]);
  const now = useNow();
  const projections = useCurrentProjections(actions, now);
  const action = item.jsonData;
  const projection = projections[0];
  const active = currentWindow(projection.windows, now);
  return (
    <Box sx={{ minWidth: 0, width: '100%' }}>
      <Stack direction="row" spacing={1} sx={{ mb: 2 }}>
        <Chip
          label={projection.error ? 'Window unknown' : active ? 'Active window' : 'Outside window'}
          color={active ? 'warning' : 'default'}
        />
      </Stack>
      {projection.error && <Alert severity="warning">{projection.error}</Alert>}
      {active && (
        <Alert severity="info" sx={{ mb: 2 }}>
          Maintenance is active
          {active.end <= now + HORIZON
            ? ` until ${formatTime(active.end)}`
            : '; the end is beyond the preview horizon'}
          .
        </Alert>
      )}
      {action.status?.conditions
        ?.filter(condition =>
          condition.type === 'Active' ? condition.status === 'Unknown' : condition.status !== 'True'
        )
        .map(condition => (
          <Alert key={condition.type} severity="warning" sx={{ mb: 1 }}>
            {condition.type}: {condition.message || condition.reason}
          </Alert>
        ))}
      <SectionBox title="Conditions" role="region" aria-label="Conditions">
        <Resource.ConditionsTable resource={item.jsonData} showLastUpdate={false} />
      </SectionBox>
      <WindowEvents item={item} cluster={cluster} />
      <NamespaceSelection action={action} cluster={cluster} />
      <WindowCalendar actions={actions} now={now} renderAction={action => action.metadata.name} />
      <SectionBox title="Schedules">
        <Table aria-label="Action schedules">
          <TableHead>
            <TableRow>
              <TableCell>Name</TableCell>
              <TableCell>Cron</TableCell>
              <TableCell>Time zone</TableCell>
              <TableCell>Duration</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {action.spec.schedules.map(schedule => (
              <TableRow key={schedule.name}>
                <TableCell>{schedule.name}</TableCell>
                <TableCell>
                  <code>{schedule.cron}</code>
                </TableCell>
                <TableCell>{schedule.timeZone || 'UTC'}</TableCell>
                <TableCell>{schedule.duration}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </SectionBox>
      <SectionBox title="Targets">
        <Table aria-label="Resource targets">
          <TableHead>
            <TableRow>
              <TableCell>Name</TableCell>
              <TableCell>API / kind</TableCell>
              <TableCell>Selector</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {action.spec.targets.map(target => (
              <TableRow key={target.name}>
                <TableCell>{target.name}</TableCell>
                <TableCell>
                  {target.apiVersion} / {target.kind}
                </TableCell>
                <TableCell>
                  <code>{JSON.stringify(target.selector || {})}</code>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </SectionBox>
      <SectionBox title="Resource actions">
        <Stack spacing={1} sx={{ p: 2 }}>
          <Typography>
            Executor:{' '}
            {action.spec.serviceAccount
              ? `${action.spec.serviceAccount.namespace}/${action.spec.serviceAccount.name}`
              : 'None (admission only)'}
          </Typography>
          {action.spec.actions?.map(step => (
            <Typography key={step.name}>
              {step.name} · targets {step.targets.join(', ')} ·{' '}
              {[step.onStart && 'on start', step.onEnd && 'on end'].filter(Boolean).join(' and ')}
            </Typography>
          ))}
        </Stack>
      </SectionBox>
      <SectionBox title="Admission rules">
        <Stack spacing={2} sx={{ p: 2 }}>
          {!action.spec.admission?.length && <Typography>No admission restrictions.</Typography>}
          {action.spec.admission?.map(rule => (
            <Box key={rule.name}>
              <Typography fontWeight="bold">{rule.name}</Typography>
              <Typography>
                Deny {rule.operations.join(', ')} when {rule.when || 'Active'} · targets{' '}
                {rule.targets.join(', ')} ·{' '}
                {(rule.subresources?.length ? rule.subresources : [''])
                  .map(value => value || 'parent')
                  .join(', ')}
              </Typography>
              <Typography variant="body2" color="text.secondary">
                Message template: {rule.message}
              </Typography>
            </Box>
          ))}
        </Stack>
      </SectionBox>
    </Box>
  );
}
