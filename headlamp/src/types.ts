export interface LabelSelector {
  matchLabels?: Record<string, string>;
  matchExpressions?: Array<{
    key: string;
    operator: 'In' | 'NotIn' | 'Exists' | 'DoesNotExist';
    values?: string[];
  }>;
}

export interface Resource {
  apiVersion?: string;
  kind?: string;
  metadata: {
    name: string;
    namespace?: string;
    uid?: string;
    generation?: number;
    labels?: Record<string, string>;
    ownerReferences?: Array<{
      apiVersion: string;
      kind: string;
      name: string;
      uid: string;
      controller?: boolean;
    }>;
  };
}

export interface Schedule {
  name: string;
  cron: string;
  timeZone?: string;
  duration: string;
}

export interface Target {
  name: string;
  apiVersion: string;
  kind: string;
  selector?: LabelSelector;
}

export interface ChangeWindowData extends Resource {
  spec: {
    namespaceSelectors: LabelSelector[];
    schedules: Schedule[];
    targets: Target[];
    serviceAccount?: { namespace: string; name: string };
    actions?: Array<{ name: string; targets: string[]; onStart?: unknown; onEnd?: unknown }>;
    admission?: Array<{
      name: string;
      targets: string[];
      operations: string[];
      subresources?: string[];
      when?: 'Active' | 'Inactive';
      action?: 'Deny';
      message: string;
    }>;
    config?: {
      admission?: { warnings?: Array<{ name: string; targets: string[]; when: string }> };
    };
  };
  status?: {
    active?: boolean;
    windowStart?: string;
    windowEnd?: string;
    nextTransition?: string;
    pendingResources?: number;
    observedGeneration?: number;
    conditions?: Array<{ type: string; status: string; reason?: string; message?: string }>;
  };
}

export interface WindowInterval {
  start: number;
  end: number;
}
