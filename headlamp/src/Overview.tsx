import { Link, SectionBox } from '@kinvolk/headlamp-plugin/lib/CommonComponents';
import { getCluster } from '@kinvolk/headlamp-plugin/lib/Utils';
import {
  Alert,
  Box,
  Button,
  Chip,
  CircularProgress,
  Paper,
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
  MAX_PREVIEW_ACTIONS,
  projectActions,
  readyStatus,
  useCurrentProjections,
  useNow,
} from './hooks';
import { ChangeWindow } from './resources';
import { currentWindow, HORIZON, shiftDays, weekStart } from './schedule';
import type { ChangeWindowData } from './types';

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
  const [week, setWeek] = useState(() => weekStart(new Date()));
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
  const calendar = useMemo(
    () => projectActions(actions, week.getTime() - HORIZON, shiftDays(week, 7).getTime() + HORIZON),
    [actions, week]
  );
  const days = Array.from({ length: 7 }, (_, index) => shiftDays(week, index));
  const errors = [
    ...new Set(
      [...current, ...calendar]
        .filter(item => item.error)
        .map(item => `${item.action.metadata.name}: ${item.error}`)
    ),
  ];
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
          {errors.slice(0, 5).map(message => (
            <Alert severity="warning" key={message} sx={{ mb: 1 }}>
              {message}
            </Alert>
          ))}
          {errors.length > 5 && (
            <Alert severity="warning" sx={{ mb: 1 }}>
              {errors.length - 5} more windows have unavailable previews. Filter by name or open an
              action for details.
            </Alert>
          )}
          <SectionBox
            title="Calendar"
            subtitle={`Times shown in ${
              Intl.DateTimeFormat().resolvedOptions().timeZone
            }. Each schedule keeps its configured time zone.`}
          >
            <Stack direction="row" spacing={1} alignItems="center" flexWrap="wrap" sx={{ p: 2 }}>
              <Button onClick={() => setWeek(shiftDays(week, -7))} aria-label="Previous week">
                Previous
              </Button>
              <Button onClick={() => setWeek(weekStart(new Date()))}>Today</Button>
              <Button onClick={() => setWeek(shiftDays(week, 7))} aria-label="Next week">
                Next
              </Button>
              <Typography>
                {week.toLocaleDateString(undefined, { dateStyle: 'medium' })} –{' '}
                {shiftDays(week, 6).toLocaleDateString(undefined, { dateStyle: 'medium' })}
              </Typography>
            </Stack>
            <Box sx={{ overflowX: 'auto', px: 2, pb: 2 }}>
              <Box
                role="region"
                aria-label="Weekly action calendar"
                sx={{
                  display: 'grid',
                  gridTemplateColumns: 'repeat(7, minmax(145px, 1fr))',
                  minWidth: 1015,
                  gap: 1,
                }}
              >
                {days.map(day => {
                  const start = day.getTime();
                  const end = shiftDays(day, 1).getTime();
                  const events = calendar
                    .flatMap(item =>
                      item.windows
                        .filter(window => window.start < end && window.end > start)
                        .map(window => ({ ...item, window }))
                    )
                    .sort(
                      (a, b) =>
                        a.window.start - b.window.start ||
                        a.action.metadata.name.localeCompare(b.action.metadata.name)
                    );
                  const today = start <= now && now < end;
                  return (
                    <Paper
                      variant="outlined"
                      key={start}
                      sx={{
                        p: 1,
                        minHeight: 240,
                        borderColor: today ? 'primary.main' : 'divider',
                        bgcolor: today ? 'action.hover' : 'background.paper',
                      }}
                    >
                      <Typography fontWeight="bold" sx={{ mb: 1 }}>
                        {day.toLocaleDateString(undefined, {
                          weekday: 'short',
                          month: 'short',
                          day: 'numeric',
                        })}
                      </Typography>
                      {!events.length && (
                        <Typography variant="body2" color="text.secondary">
                          No windows
                        </Typography>
                      )}
                      {events.slice(0, 100).map(({ action, window }) => (
                        <Box
                          key={`${action.metadata.uid || action.metadata.name}-${window.start}`}
                          sx={{
                            p: 1,
                            mb: 1,
                            borderLeft: 3,
                            borderColor: currentWindow([window], now)
                              ? 'warning.main'
                              : 'primary.main',
                            bgcolor: 'action.hover',
                            overflowWrap: 'anywhere',
                          }}
                        >
                          <ActionLink action={action} cluster={cluster} />
                          <Typography variant="caption" display="block">
                            {window.start < start
                              ? 'Continues'
                              : new Date(window.start).toLocaleTimeString(undefined, {
                                  hour: '2-digit',
                                  minute: '2-digit',
                                })}{' '}
                            –{' '}
                            {window.end >= end
                              ? 'next day'
                              : new Date(window.end).toLocaleTimeString(undefined, {
                                  hour: '2-digit',
                                  minute: '2-digit',
                                })}
                          </Typography>
                        </Box>
                      ))}
                      {events.length > 100 && (
                        <Typography variant="caption">
                          {events.length - 100} more windows. Filter windows to see them.
                        </Typography>
                      )}
                    </Paper>
                  );
                })}
              </Box>
            </Box>
            <Typography variant="body2" color="text.secondary" sx={{ px: 2, pb: 2 }}>
              Calendar entries show scheduled windows. Resource actions depend on controller
              readiness, executor permissions, and the resources selected at window start.
            </Typography>
          </SectionBox>
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
