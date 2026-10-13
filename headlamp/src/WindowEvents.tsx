import { K8s } from '@kinvolk/headlamp-plugin/lib';
import { DateLabel, SectionBox, SimpleTable } from '@kinvolk/headlamp-plugin/lib/CommonComponents';
import type Event from '@kinvolk/headlamp-plugin/lib/k8s/event';
import { Alert, CircularProgress, Typography } from '@mui/material';
import { useMemo } from 'react';
import type { ChangeWindow } from './resources';

export function WindowEvents({ item, cluster }: { item: ChangeWindow; cluster: string }) {
  return (
    <SectionBox title="Events" role="region" aria-label="Events">
      {item.metadata.uid ? (
        <EventsForUID uid={item.metadata.uid} cluster={cluster} />
      ) : (
        <Typography>Events are unavailable until the resource has a UID.</Typography>
      )}
    </SectionBox>
  );
}

function EventsForUID({ uid, cluster }: { uid: string; cluster: string }) {
  // The SDK's ObjectEventList hides fetch errors and matches names without UIDs.
  // Keep its standard table presentation, with an identity-scoped shared query.
  const [events, error] = K8s.event.default.useList({
    cluster,
    namespace: '',
    fieldSelector: `involvedObject.uid=${uid}`,
    limit: 2000,
    refetchInterval: 30000,
  });
  const selected = useMemo(
    () =>
      events?.filter(event => event.cluster === cluster && event.involvedObject?.uid === uid) ??
      null,
    [events, cluster, uid]
  );
  if (error)
    return (
      <Alert severity="warning">
        {error.status === 403
          ? 'You do not have permission to list events for this ChangeWindow.'
          : `Unable to load events: ${error.message || 'request failed'}`}
      </Alert>
    );
  if (!selected) return <CircularProgress aria-label="Loading events" />;
  return (
    <>
      {events && events.length >= 2000 && (
        <Typography>Showing at most 2,000 events for this ChangeWindow.</Typography>
      )}
      <SimpleTable
        data={selected}
        columns={[
          { label: 'Type', getter: (event: Event) => event.type || '—' },
          { label: 'Reason', getter: (event: Event) => event.reason || '—' },
          {
            label: 'From',
            getter: (event: Event) =>
              event.source?.component || event.jsonData.reportingController || '—',
          },
          { label: 'Message', getter: (event: Event) => event.message || '—' },
          {
            label: 'Last occurrence',
            getter: (event: Event) => <DateLabel date={event.lastOccurrence} />,
            sort: (event: Event) => -Date.parse(event.lastOccurrence),
          },
        ]}
        defaultSortingColumn={5}
        emptyMessage="No events for this ChangeWindow."
        reflectInURL={false}
      />
    </>
  );
}
