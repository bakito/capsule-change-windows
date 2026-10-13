import { Link, SectionBox } from '@kinvolk/headlamp-plugin/lib/CommonComponents';
import { getCluster } from '@kinvolk/headlamp-plugin/lib/Utils';
import {
  Alert,
  Box,
  Chip,
  CircularProgress,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TablePagination,
  TableRow,
  TextField,
  Typography,
} from '@mui/material';
import { useMemo, useState } from 'react';
import {
  lifecycleStatus,
  MAX_PREVIEW_ACTIONS,
  readyStatus,
  useCurrentProjections,
  useNow,
} from './hooks';
import { ChangeWindow } from './resources';
import { currentWindow, HORIZON } from './schedule';
import type { ChangeWindowData } from './types';
import { WindowCalendar } from './WindowCalendar';

export const ROUTE = 'capsule-change-windows';
export const DETAIL_ROUTE = 'capsule-change-window';
export function formatTime(value: number): string {
  return new Date(value).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' });
}

export function ActionLink({ action, cluster }: { action: ChangeWindowData; cluster?: string }) {
  return (
    <Link routeName={DETAIL_ROUTE} params={{ name: action.metadata.name }} activeCluster={cluster}>
      {action.metadata.name}
    </Link>
  );
}

export function LoadError({ error }: { error: { message?: string; status?: number } }) {
  return (
    <Alert severity="warning">
      {error.status === 404
        ? 'ChangeWindow API is not installed in this cluster.'
        : error.status === 403
        ? 'You do not have permission to read ChangeWindows in this cluster.'
        : `Unable to load ChangeWindows: ${error.message || 'request failed'}`}
    </Alert>
  );
}

export function Overview() {
  const cluster = getCluster();
  if (!cluster)
    return <Alert severity="info">Select a single cluster to view Change Windows.</Alert>;
  return <ClusterOverview key={cluster} cluster={cluster} />;
}

function ClusterOverview({ cluster }: { cluster: string }) {
  const [items, error] = ChangeWindow.useList<ChangeWindow>({ cluster });
  const [query, setQuery] = useState('');
  const [page, setPage] = useState(0);
  const now = useNow();
  const actions = useMemo(
    () =>
      (items || [])
        .map(item => item.jsonData)
        .filter(action =>
          `${action.metadata.name} ${action.spec.targets.map(target => target.kind).join(' ')}`
            .toLowerCase()
            .includes(query.toLowerCase())
        )
        .sort((a, b) => a.metadata.name.localeCompare(b.metadata.name)),
    [items, query]
  );
  const current = useCurrentProjections(actions, now);
  const activeCount = current.filter(
    item => !item.error && currentWindow(item.windows, now)
  ).length;
  const visiblePage = Math.min(page, Math.max(0, Math.ceil(current.length / 50) - 1));

  return (
    <Box sx={{ p: { xs: 1, md: 2 } }}>
      <Stack direction="row" alignItems="center" justifyContent="space-between" sx={{ mb: 2 }}>
        <Box>
          <Typography variant="h4">Change Windows</Typography>
          <Typography color="text.secondary">Scheduled maintenance and resource changes</Typography>
        </Box>
        <Stack direction="row" spacing={1}>
          <Chip
            label={`${activeCount} active${
              actions.length > MAX_PREVIEW_ACTIONS ? ' in preview' : ''
            }`}
            color={activeCount ? 'warning' : 'default'}
          />
          <Chip label={`${actions.length} windows`} />
        </Stack>
      </Stack>
      {error ? (
        <LoadError error={error} />
      ) : !items ? (
        <CircularProgress aria-label="Loading windows" />
      ) : (
        <>
          <TextField
            label="Filter windows by name or resource kind"
            value={query}
            onChange={event => {
              setQuery(event.target.value);
              setPage(0);
            }}
            size="small"
            fullWidth
            sx={{ mb: 2 }}
          />
          <WindowCalendar
            actions={actions}
            now={now}
            renderAction={action => <ActionLink action={action} cluster={cluster} />}
          />
          <SectionBox title="All windows">
            {!actions.length ? (
              <Typography sx={{ p: 2 }}>
                {query
                  ? 'No actions match this filter.'
                  : 'No ChangeWindows found in this cluster.'}
              </Typography>
            ) : (
              <TableContainer>
                <Table aria-label="Change windows">
                  <TableHead>
                    <TableRow>
                      {[
                        'Name',
                        'Window',
                        'Next transition',
                        'Targets',
                        'Behavior',
                        'Controller',
                        'Lifecycle',
                        'Pending',
                      ].map(title => (
                        <TableCell key={title}>{title}</TableCell>
                      ))}
                    </TableRow>
                  </TableHead>
                  <TableBody>
                    {current
                      .slice(visiblePage * 50, (visiblePage + 1) * 50)
                      .map(({ action, windows, error: projectionError }) => {
                        const active = currentWindow(windows, now);
                        const next = active
                          ? active.end
                          : windows.find(window => window.start > now)?.start;
                        const ready = readyStatus(action);
                        return (
                          <TableRow key={action.metadata.uid || action.metadata.name}>
                            <TableCell>
                              <ActionLink action={action} cluster={cluster} />
                            </TableCell>
                            <TableCell>
                              <Chip
                                size="small"
                                label={projectionError ? 'Unknown' : active ? 'Active' : 'Inactive'}
                                color={projectionError ? 'error' : active ? 'warning' : 'default'}
                              />
                            </TableCell>
                            <TableCell>
                              {projectionError
                                ? 'Unavailable'
                                : next && next <= now + HORIZON
                                ? formatTime(next)
                                : 'Beyond preview'}
                            </TableCell>
                            <TableCell>
                              {action.spec.targets.map(target => target.kind).join(', ')}
                            </TableCell>
                            <TableCell>
                              {action.spec.actions?.length || 0} actions ·{' '}
                              {action.spec.admission?.length || 0} rules ·{' '}
                              {action.spec.config?.admission?.warnings?.length || 0} warnings
                            </TableCell>
                            <TableCell>
                              <Chip
                                size="small"
                                label={ready}
                                color={ready === 'Ready' ? 'success' : 'default'}
                              />
                            </TableCell>
                            <TableCell>{lifecycleStatus(action)}</TableCell>
                            <TableCell>{action.status?.pendingResources || 0}</TableCell>
                          </TableRow>
                        );
                      })}
                  </TableBody>
                </Table>
                <TablePagination
                  component="div"
                  count={current.length}
                  page={visiblePage}
                  onPageChange={(_, next) => setPage(next)}
                  rowsPerPage={50}
                  rowsPerPageOptions={[50]}
                />
              </TableContainer>
            )}
          </SectionBox>
        </>
      )}
    </Box>
  );
}
