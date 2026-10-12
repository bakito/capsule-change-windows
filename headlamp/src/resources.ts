import { KubeObject, type KubeObjectInterface } from '@kinvolk/headlamp-plugin/lib/k8s/cluster';
import type { ChangeWindowData } from './types';

interface ChangeWindowObject extends KubeObjectInterface {
  spec: ChangeWindowData['spec'];
  status?: ChangeWindowData['status'];
}

export class ChangeWindow extends KubeObject<ChangeWindowObject> {
  static kind = 'ChangeWindow';
  static apiVersion = 'addons.projectcapsule.dev/v1alpha1';
  static apiName = 'changewindows';
  static isNamespaced = false;
}
