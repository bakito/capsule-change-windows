import { ConfigStore } from '@kinvolk/headlamp-plugin/lib';
import { SectionBox } from '@kinvolk/headlamp-plugin/lib/CommonComponents';
import { Button, Stack, TextField, Typography } from '@mui/material';
import { useState } from 'react';
import { DEFAULT_SYSTEM_NAMESPACE } from './matching';

interface Settings {
  systemNamespaces?: Record<string, string>;
}
export const settings = new ConfigStore<Settings>('capsule-change-windows');
export const useSettings = settings.useConfig();

export function PluginSettings() {
  const config = useSettings();
  const [cluster, setCluster] = useState('');
  const [namespace, setNamespace] = useState(DEFAULT_SYSTEM_NAMESPACE);
  const valid =
    cluster.trim() && /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/.test(namespace) && namespace.length <= 63;
  return (
    <SectionBox title="Change Windows settings">
      <Stack spacing={2} sx={{ p: 2, maxWidth: 640 }}>
        <Typography>
          Set the controller namespace for each cluster when using a custom Helm namespace. This
          namespace is excluded from maintenance selection.
        </Typography>
        <TextField
          label="Headlamp cluster name"
          value={cluster}
          onChange={event => setCluster(event.target.value)}
        />
        <TextField
          label="Controller namespace"
          value={namespace}
          onChange={event => setNamespace(event.target.value)}
        />
        <Button
          variant="contained"
          disabled={!valid}
          onClick={() =>
            settings.update({
              systemNamespaces: { ...config?.systemNamespaces, [cluster.trim()]: namespace },
            })
          }
        >
          Save namespace
        </Button>
        {Object.entries(config?.systemNamespaces || {}).map(([name, value]) => (
          <Typography key={name}>
            {name}: {value}
          </Typography>
        ))}
      </Stack>
    </SectionBox>
  );
}
