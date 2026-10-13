import { describe, expect, it, vi } from 'vitest';
import { controllingOwners, matchesSelector, matchingTargets } from './matching';
import { actionFixture, deploymentFixture, namespaceFixture } from './testFixtures';
import type { Resource } from './types';

describe('namespace and resource selection', () => {
  it('ORs namespace selectors and ANDs requirements within each selector', () => {
    const action = actionFixture();
    action.spec.namespaceSelectors!.unshift({ matchLabels: { environment: 'staging' } });
    expect(matchingTargets(action, deploymentFixture, namespaceFixture)).toHaveLength(1);
    expect(
      matchingTargets(
        action,
        { ...deploymentFixture, metadata: { ...deploymentFixture.metadata, namespace: 'other' } },
        namespaceFixture
      )
    ).toEqual([]);
    action.spec.namespaceSelectors = [
      { matchLabels: { environment: 'development', owner: 'alice' } },
    ];
    expect(matchingTargets(action, deploymentFixture, namespaceFixture)).toEqual([]);
  });
  it('implements Kubernetes label expressions, including absent keys for NotIn', () => {
    expect(
      matchesSelector(
        { matchExpressions: [{ key: 'env', operator: 'NotIn', values: ['prod'] }] },
        {}
      )
    ).toBe(true);
    expect(
      matchesSelector({ matchExpressions: [{ key: 'env', operator: 'In', values: ['dev'] }] }, {})
    ).toBe(false);
    expect(
      matchesSelector({ matchExpressions: [{ key: 'env', operator: 'Exists' }] }, { env: '' })
    ).toBe(true);
    expect(
      matchesSelector({ matchExpressions: [{ key: 'env', operator: 'DoesNotExist' }] }, {})
    ).toBe(true);
    expect(matchesSelector({ matchLabels: { env: '' } }, {})).toBe(false);
  });
  it('excludes protected namespaces and never treats empty selector arrays as all', () => {
    const action = actionFixture();
    action.spec.namespaceSelectors = [{}];
    for (const name of ['kube-system', 'kube-public', 'kube-node-lease', 'custom-controller']) {
      expect(
        matchingTargets(
          action,
          { ...deploymentFixture, metadata: { ...deploymentFixture.metadata, namespace: name } },
          { ...namespaceFixture, metadata: { name } },
          'custom-controller'
        )
      ).toEqual([]);
    }
    action.spec.namespaceSelectors = [];
    expect(matchingTargets(action, deploymentFixture, namespaceFixture)).toEqual([]);
  });
  it('requires the exact GVK and current resource labels', () => {
    const action = actionFixture();
    expect(
      matchingTargets(action, { ...deploymentFixture, kind: 'Pod' }, namespaceFixture)
    ).toEqual([]);
    expect(
      matchingTargets(
        action,
        { ...deploymentFixture, apiVersion: 'apps/v1beta1' },
        namespaceFixture
      )
    ).toEqual([]);
    expect(
      matchingTargets(
        action,
        { ...deploymentFixture, metadata: { ...deploymentFixture.metadata, labels: {} } },
        namespaceFixture
      )
    ).toEqual([]);
  });
});

describe('workload owner traversal', () => {
  const pod: Resource = {
    apiVersion: 'v1',
    kind: 'Pod',
    metadata: {
      name: 'web-abc',
      namespace: 'development',
      uid: 'pod-a',
      ownerReferences: [
        {
          kind: 'ReplicaSet',
          apiVersion: 'apps/v1',
          name: 'web-rs',
          uid: 'rs-a',
          controller: true,
        },
      ],
    },
  };
  const rs: Resource = {
    apiVersion: 'apps/v1',
    kind: 'ReplicaSet',
    metadata: {
      name: 'web-rs',
      namespace: 'development',
      uid: 'rs-a',
      ownerReferences: [
        {
          kind: 'Deployment',
          apiVersion: 'apps/v1',
          name: 'web',
          uid: 'deployment-a',
          controller: true,
        },
      ],
    },
  };
  it('follows Pod → ReplicaSet → Deployment with keyed same-namespace reads', async () => {
    const read = vi.fn().mockResolvedValueOnce(rs).mockResolvedValueOnce(deploymentFixture);
    expect(await controllingOwners(pod, read)).toEqual([rs, deploymentFixture]);
    expect(read.mock.calls.map(call => call[0])).toEqual([
      '/apis/apps/v1/namespaces/development/replicasets/web-rs',
      '/apis/apps/v1/namespaces/development/deployments/web',
    ]);
  });
  it.each([
    { ...rs, metadata: { ...rs.metadata, uid: 'replacement' } },
    { ...rs, metadata: { ...rs.metadata, namespace: 'production' } },
    { ...rs, kind: 'Deployment' },
  ])('rejects replacement/cross-namespace/wrong-kind owners', async parent => {
    const read = vi.fn().mockResolvedValue(parent);
    expect(await controllingOwners(pod, read)).toEqual([]);
    expect(read).toHaveBeenCalledTimes(1);
  });
  it('propagates missing permissions instead of claiming no active windows', async () => {
    await expect(
      controllingOwners(pod, vi.fn().mockRejectedValue(new Error('Forbidden')))
    ).rejects.toThrow('Forbidden');
  });
  it('does no reads for unowned resources, unsupported owners, or cycles', async () => {
    const read = vi.fn();
    expect(await controllingOwners(deploymentFixture, read)).toEqual([]);
    expect(
      await controllingOwners(
        {
          ...pod,
          metadata: {
            ...pod.metadata,
            ownerReferences: [
              {
                kind: 'Unknown',
                apiVersion: 'example.com/v1',
                name: 'owner',
                uid: 'owner',
                controller: true,
              },
            ],
          },
        },
        read
      )
    ).toEqual([]);
    expect(
      await controllingOwners({ ...pod, metadata: { ...pod.metadata, uid: 'rs-a' } }, read)
    ).toEqual([]);
    expect(read).not.toHaveBeenCalled();
  });
});
