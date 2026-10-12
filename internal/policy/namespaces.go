// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package policy

// ProtectedNamespace keeps the controller and Kubernetes recovery paths outside
// maintenance selection. Match the exclusions in both installation manifests.
func ProtectedNamespace(namespace, controllerNamespace string) bool {
	return namespace == controllerNamespace || namespace == "kube-system" || namespace == "kube-public" || namespace == "kube-node-lease"
}
