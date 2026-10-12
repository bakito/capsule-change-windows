import type { ChangeWindowData, Resource } from './types';

export function actionFixture(): ChangeWindowData {
  return {
    apiVersion: 'addons.projectcapsule.dev/v1alpha1',
    kind: 'ChangeWindow',
    metadata: { name: 'development-downtime', uid: 'action-a', generation: 1 },
    spec: {
      namespaceSelectors: [{ matchLabels: { environment: 'development' } }],
      schedules: [{ name: 'daily', cron: '0 10 * * *', timeZone: 'UTC', duration: '2h' }],
      targets: [
        {
          name: 'deployments',
          apiVersion: 'apps/v1',
          kind: 'Deployment',
          selector: { matchLabels: { downscaling: 'enabled' } },
        },
      ],
      actions: [{ name: 'downscale', targets: ['deployments'], onStart: {}, onEnd: {} }],
      admission: [
        {
          name: 'freeze',
          targets: ['deployments'],
          operations: ['UPDATE'],
          subresources: ['', 'scale'],
          when: 'Active',
          message: 'Maintenance is active.',
        },
      ],
    },
    status: { observedGeneration: 1, conditions: [{ type: 'Ready', status: 'True' }] },
  };
}

export const namespaceFixture: Resource = {
  apiVersion: 'v1',
  kind: 'Namespace',
  metadata: { name: 'development', labels: { environment: 'development' } },
};
export const deploymentFixture: Resource = {
  apiVersion: 'apps/v1',
  kind: 'Deployment',
  metadata: {
    name: 'web',
    namespace: 'development',
    uid: 'deployment-a',
    labels: { downscaling: 'enabled' },
  },
};
