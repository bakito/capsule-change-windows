import {
  registerDetailsViewSectionsProcessor,
  registerPluginSettings,
  registerRoute,
  registerSidebarEntry,
} from '@kinvolk/headlamp-plugin/lib';
import { Details } from './Details';
import { addMaintenanceSection } from './MaintenanceSection';
import { DETAIL_ROUTE, Overview, ROUTE } from './Overview';
import { PluginSettings } from './settings';

registerSidebarEntry({
  parent: '',
  name: ROUTE,
  label: 'Change Windows',
  icon: 'mdi:calendar-clock',
  url: '/change-windows',
});
registerRoute({
  path: '/change-windows',
  exact: true,
  name: ROUTE,
  sidebar: ROUTE,
  component: Overview,
});
registerRoute({
  path: '/change-windows/:name',
  exact: true,
  name: DETAIL_ROUTE,
  sidebar: ROUTE,
  component: Details,
});
registerDetailsViewSectionsProcessor(addMaintenanceSection);
registerPluginSettings('capsule-change-windows', PluginSettings, true);
