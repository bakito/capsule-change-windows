import type { ChangeWindowData, LabelSelector, Resource, Target } from './types';

export const DEFAULT_SYSTEM_NAMESPACE = 'schedule-action-controller-system';

export function matchesSelector(
  selector: LabelSelector = {},
  labels: Record<string, string> = {}
): boolean {
  if (!Object.entries(selector.matchLabels || {}).every(([key, value]) => labels[key] === value))
    return false;
  return (selector.matchExpressions || []).every(({ key, operator, values = [] }) => {
    const exists = Object.hasOwn(labels, key);
    switch (operator) {
      case 'In':
        return exists && values.includes(labels[key]);
      case 'NotIn':
        return !exists || !values.includes(labels[key]);
      case 'Exists':
        return exists;
      case 'DoesNotExist':
        return !exists;
      default:
        return false;
    }
  });
}

export function matchingTargets(
  action: ChangeWindowData,
  resource: Resource,
  namespace: Resource,
  systemNamespace = DEFAULT_SYSTEM_NAMESPACE
): Target[] {
  const name = resource.metadata.namespace;
  if (
    !name ||
    namespace.kind !== 'Namespace' ||
    namespace.metadata.name !== name ||
    [systemNamespace, 'kube-system', 'kube-public', 'kube-node-lease'].includes(name)
  )
    return [];
  if (
    !action.spec.namespaceSelectors.some(selector =>
      matchesSelector(selector, namespace.metadata.labels)
    )
  )
    return [];
  return action.spec.targets.filter(
    target =>
      target.apiVersion === resource.apiVersion &&
      target.kind === resource.kind &&
      matchesSelector(target.selector, resource.metadata.labels)
  );
}

const OWNER_RESOURCES: Record<string, string> = {
  'apps/v1/ReplicaSet': 'replicasets',
  'apps/v1/Deployment': 'deployments',
  'apps/v1/StatefulSet': 'statefulsets',
  'apps/v1/DaemonSet': 'daemonsets',
  'batch/v1/Job': 'jobs',
  'batch/v1/CronJob': 'cronjobs',
};

/** At most three keyed same-namespace GETs; never list workloads or follow labels. */
export async function controllingOwners(
  resource: Resource,
  read: (path: string) => Promise<Resource>
): Promise<Resource[]> {
  const owners: Resource[] = [];
  const visited = new Set([resource.metadata.uid]);
  let child = resource;
  for (let depth = 0; depth < 3; depth++) {
    const owner = child.metadata.ownerReferences?.find(reference => reference.controller);
    if (!owner || !resource.metadata.namespace || visited.has(owner.uid)) break;
    const plural = OWNER_RESOURCES[`${owner.apiVersion}/${owner.kind}`];
    if (!plural) break;
    const parent = await read(
      `/apis/${owner.apiVersion}/namespaces/${encodeURIComponent(
        resource.metadata.namespace
      )}/${plural}/${encodeURIComponent(owner.name)}`
    );
    if (
      parent.metadata.uid !== owner.uid ||
      parent.metadata.namespace !== resource.metadata.namespace ||
      parent.apiVersion !== owner.apiVersion ||
      parent.kind !== owner.kind
    )
      break;
    owners.push(parent);
    visited.add(owner.uid);
    child = parent;
  }
  return owners;
}
