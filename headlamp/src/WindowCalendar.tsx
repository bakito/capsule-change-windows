import { SectionBox } from '@kinvolk/headlamp-plugin/lib/CommonComponents';
import {
  Alert,
  Box,
  Button,
  CircularProgress,
  Paper,
  Stack,
  ToggleButton,
  ToggleButtonGroup,
  Typography,
} from '@mui/material';
import { type ReactNode, useMemo, useState } from 'react';
import {
  calendarDates,
  calendarDays,
  calendarLabels,
  calendarRange,
  type CalendarView,
  shiftPeriod,
} from './calendar';
import { useCalendarProjections } from './hooks';
import { currentWindow, HORIZON, shiftDays, weekStart } from './schedule';
import type { ChangeWindowData } from './types';

const dateLabel = (date: Date) => date.toLocaleDateString(undefined, { dateStyle: 'full' });

export function WindowCalendar({
  actions,
  now,
  renderAction,
}: {
  actions: ChangeWindowData[];
  now: number;
  renderAction: (action: ChangeWindowData) => ReactNode;
}) {
  const [view, setView] = useState<CalendarView>('week');
  const [focus, setFocus] = useState(() => new Date(now));
  const range = useMemo(() => calendarRange(focus, view), [focus, view]);
  const projections = useCalendarProjections(
    actions,
    range.start.getTime() - HORIZON,
    range.end.getTime() + HORIZON
  );
  const days = useMemo(
    () =>
      calendarDays(
        projections || [],
        calendarDates(range.start, range.end),
        view === 'week' ? 100 : 3
      ),
    [projections, range, view]
  );
  const byDate = useMemo(() => new Map(days.map(day => [day.date.getTime(), day])), [days]);
  const errors = (projections || []).filter(item => item.error);
  const openPeriod = (date: Date, nextView: CalendarView) => {
    setFocus(date);
    setView(nextView);
  };
  const title =
    view === 'year'
      ? String(focus.getFullYear())
      : view === 'month'
      ? focus.toLocaleDateString(undefined, { month: 'long', year: 'numeric' })
      : `${range.start.toLocaleDateString(undefined, { dateStyle: 'medium' })} – ${shiftDays(
          range.end,
          -1
        ).toLocaleDateString(undefined, { dateStyle: 'medium' })}`;
  const today = new Date(now).toDateString();
  return (
    <SectionBox
      title="Calendar"
      subtitle={`Times shown in ${
        Intl.DateTimeFormat().resolvedOptions().timeZone
      }. Each schedule keeps its configured time zone.`}
    >
      <Stack direction="row" alignItems="center" flexWrap="wrap" gap={1} sx={{ p: 2 }}>
        <ToggleButtonGroup
          value={view}
          exclusive
          size="small"
          aria-label="Calendar view"
          onChange={(_, next: CalendarView | null) => next && setView(next)}
        >
          {Object.entries(calendarLabels).map(([key, label]) => (
            <ToggleButton key={key} value={key}>
              {label}
            </ToggleButton>
          ))}
        </ToggleButtonGroup>
        <Button
          onClick={() => setFocus(shiftPeriod(focus, view, -1))}
          aria-label={`Previous ${view}`}
        >
          Previous
        </Button>
        <Button onClick={() => setFocus(new Date(now))}>Today</Button>
        <Button onClick={() => setFocus(shiftPeriod(focus, view, 1))} aria-label={`Next ${view}`}>
          Next
        </Button>
        <Typography aria-live="polite" data-testid="calendar-period">
          {title}
        </Typography>
      </Stack>
      {errors.slice(0, 5).map(item => (
        <Alert key={item.action.metadata.name} severity="warning" sx={{ mx: 2, mb: 1 }}>
          {item.action.metadata.name}: {item.error}
        </Alert>
      ))}
      {errors.length > 5 && (
        <Alert severity="warning" sx={{ mx: 2, mb: 1 }}>
          {errors.length - 5} more windows have unavailable calendar previews. Filter by name to
          narrow the view.
        </Alert>
      )}
      {!projections ? (
        <Stack direction="row" alignItems="center" spacing={1} sx={{ p: 2 }} role="status">
          <CircularProgress size={20} />
          <Typography>Calculating calendar…</Typography>
        </Stack>
      ) : view === 'year' ? (
        <Box
          role="region"
          aria-label="Yearly action calendar"
          sx={{
            display: 'grid',
            gridTemplateColumns: 'repeat(auto-fit, minmax(250px, 1fr))',
            gap: 2,
            px: 2,
            pb: 2,
          }}
        >
          {Array.from({ length: 12 }, (_, month) => {
            const first = new Date(focus.getFullYear(), month, 1);
            const label = first.toLocaleDateString(undefined, { month: 'long', year: 'numeric' });
            const gridStart = weekStart(first);
            return (
              <Paper variant="outlined" key={month} sx={{ p: 1 }}>
                <Button onClick={() => openPeriod(first, 'month')} aria-label={`Open ${label}`}>
                  {label}
                </Button>
                <Box
                  sx={{
                    display: 'grid',
                    gridTemplateColumns: 'repeat(7, minmax(0, 1fr))',
                    gap: 0.25,
                  }}
                >
                  {Array.from({ length: 7 }, (_, index) => (
                    <Typography key={index} variant="caption" align="center">
                      {shiftDays(gridStart, index).toLocaleDateString(undefined, {
                        weekday: 'short',
                      })}
                    </Typography>
                  ))}
                  {Array.from({ length: 42 }, (_, index) => {
                    const date = shiftDays(gridStart, index);
                    if (date.getMonth() !== month) return <span key={index} />;
                    const day = byDate.get(date.getTime())!;
                    return (
                      <Button
                        key={index}
                        size="small"
                        aria-label={`${dateLabel(date)}: ${day.count} window${
                          day.count === 1 ? '' : 's'
                        }. Open week`}
                        title={day.events.map(event => event.action.metadata.name).join(', ')}
                        onClick={() => openPeriod(date, 'week')}
                        sx={{
                          minWidth: 0,
                          px: 0,
                          border: 1,
                          borderColor:
                            date.toDateString() === today ? 'primary.main' : 'transparent',
                          bgcolor: day.count ? 'action.selected' : undefined,
                          fontWeight: day.count ? 'bold' : 'normal',
                        }}
                      >
                        {date.getDate()}
                        {day.count ? '•' : ''}
                      </Button>
                    );
                  })}
                </Box>
              </Paper>
            );
          })}
        </Box>
      ) : (
        <Box sx={{ overflowX: 'auto', px: { xs: 1, sm: 2 }, pb: 2 }}>
          <Box
            role="region"
            aria-label={calendarLabels[view] + ' action calendar'}
            sx={{
              display: 'grid',
              gridTemplateColumns:
                view === 'week'
                  ? { xs: '1fr', sm: 'repeat(7, minmax(0, 1fr))' }
                  : 'repeat(7, minmax(0, 1fr))',
              minWidth: view === 'month' ? 630 : 0,
              gap: '1px',
              border: 1,
              borderColor: 'divider',
              borderRadius: 1,
              overflow: 'hidden',
              bgcolor: 'divider',
            }}
          >
            {view === 'month' &&
              days.slice(0, 7).map(({ date }) => (
                <Typography
                  key={date.getTime()}
                  variant="caption"
                  align="center"
                  sx={{ py: 0.5, bgcolor: 'background.paper', fontWeight: 'bold' }}
                >
                  {date.toLocaleDateString(undefined, { weekday: 'short' })}
                </Typography>
              ))}
            {days.map(({ date, events, count }) => {
              const start = date.getTime();
              const end = shiftDays(date, 1).getTime();
              const isToday = date.toDateString() === today;
              return (
                <Box
                  role="group"
                  aria-label={dateLabel(date)}
                  key={start}
                  sx={{
                    p: 0.75,
                    minWidth: 0,
                    minHeight: view === 'week' ? { xs: 48, sm: 112 } : 96,
                    display: view === 'week' ? { xs: 'grid', sm: 'block' } : 'block',
                    gridTemplateColumns: '80px minmax(0, 1fr)',
                    gap: 1,
                    bgcolor: 'background.paper',
                  }}
                >
                  <Typography
                    component="div"
                    variant="caption"
                    fontWeight="bold"
                    color={
                      isToday
                        ? 'primary.main'
                        : view === 'month' && date.getMonth() !== focus.getMonth()
                        ? 'text.secondary'
                        : 'text.primary'
                    }
                    sx={{ mb: 0.5 }}
                  >
                    <Box
                      component="time"
                      dateTime={date.toLocaleDateString('en-CA')}
                      aria-current={isToday ? 'date' : undefined}
                      sx={{
                        display: 'inline-block',
                        px: 0.5,
                        borderRadius: 0.5,
                        bgcolor: isToday ? 'action.selected' : undefined,
                      }}
                    >
                      {view === 'week'
                        ? date.toLocaleDateString(undefined, {
                            weekday: 'short',
                            day: 'numeric',
                          })
                        : date.getDate()}
                    </Box>
                  </Typography>
                  <Box sx={{ minWidth: 0 }}>
                    {events.map(({ action, window }) => (
                      <Box
                        key={(action.metadata.uid || action.metadata.name) + '-' + window.start}
                        sx={{
                          px: 0.5,
                          py: 0.25,
                          mb: 0.5,
                          borderLeft: 2,
                          borderColor: currentWindow([window], now)
                            ? 'warning.main'
                            : 'primary.main',
                          bgcolor: 'action.hover',
                          fontSize: '0.75rem',
                          lineHeight: 1.5,
                        }}
                      >
                        <Box
                          title={action.metadata.name}
                          sx={{
                            overflow: 'hidden',
                            textOverflow: 'ellipsis',
                            whiteSpace: 'nowrap',
                            '& a': { fontSize: 'inherit' },
                          }}
                        >
                          {renderAction(action)}
                        </Box>
                        <Typography variant="caption" display="block" color="text.secondary">
                          {window.start < start
                            ? 'Continues'
                            : new Date(window.start).toLocaleTimeString(undefined, {
                                hour: '2-digit',
                                minute: '2-digit',
                              })}
                          {' – '}
                          {window.end >= end
                            ? 'next day'
                            : new Date(window.end).toLocaleTimeString(undefined, {
                                hour: '2-digit',
                                minute: '2-digit',
                              })}
                        </Typography>
                      </Box>
                    ))}
                    {count > events.length && (
                      <Button
                        size="small"
                        sx={{ p: 0.25, minWidth: 0, fontSize: '0.75rem' }}
                        onClick={() => (view === 'month' ? openPeriod(date, 'week') : undefined)}
                        disabled={view === 'week'}
                      >
                        {count - events.length} more windows
                        {view === 'month' ? ' · Open week' : ' · Filter to see more'}
                      </Button>
                    )}
                  </Box>
                </Box>
              );
            })}
          </Box>
          {!errors.length && !days.some(day => day.count) && (
            <Typography variant="body2" color="text.secondary" sx={{ pt: 1 }}>
              No scheduled windows in this period.
            </Typography>
          )}
        </Box>
      )}
      <Typography variant="body2" color="text.secondary" sx={{ px: 2, pb: 2 }}>
        Calendar entries show scheduled windows. Resource actions depend on controller readiness,
        executor permissions, and the resources selected at window start.
        {view === 'year' &&
          ' Highlighted days have scheduled windows. Select a month or day to explore them.'}
      </Typography>
    </SectionBox>
  );
}
