// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"fmt"
	"strings"

	capsuleruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	api "github.com/projectcapsule/schedule-action-controller/api/v1alpha1"
)

func compileTarget(spec api.Target, mapper meta.RESTMapper) (Target, error) {
	if len(spec.APIVersion) > 256 || len(spec.Kind) > 256 {
		return Target{}, fmt.Errorf("apiVersion and kind must not exceed 256 bytes")
	}
	spec.APIVersion = strings.TrimSpace(spec.APIVersion)
	if spec.APIVersion == "" {
		spec.APIVersion = "v1"
	}
	if spec.Kind == "" {
		return Target{}, fmt.Errorf("kind must not be empty")
	}
	selector, err := metav1.LabelSelectorAsSelector(&spec.Selector)
	if err != nil {
		return Target{}, err
	}
	target := Target{Spec: spec, Selector: selector, Namespaced: true}
	pattern := capsuleruntime.VersionKind{APIVersion: spec.APIVersion, Kind: spec.Kind}
	gvk := pattern.GroupVersionKind()
	if pattern.HasWildcard() || gvk.Version == "" {
		// Reuse Capsule's validation and matching. Patterns are evaluated against
		// incoming requests, so newly installed CRDs need no policy recompilation.
		kinds := capsuleruntime.VersionKinds{APIGroups: []string{spec.APIVersion}, Kinds: []string{spec.Kind}}
		if err := kinds.ValidateKnownKindsWithScope(mapper, "target", func(_ schema.GroupVersionKind, scope meta.RESTScope) bool {
			return scope.Name() == meta.RESTScopeNameNamespace
		}); err != nil {
			return Target{}, err
		}
		target.Pattern = &pattern
		return target, nil
	}
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return Target{}, err
	}
	target.Resource = mapping.Resource
	target.Namespaced = mapping.Scope.Name() == meta.RESTScopeNameNamespace
	if !target.Namespaced && gvk != (schema.GroupVersionKind{Version: "v1", Kind: "Node"}) {
		return Target{}, fmt.Errorf("must be namespaced or a v1 Node")
	}
	return target, nil
}

// MatchesAPI filters before resolving a subresource's parent kind or namespace.
func (t Target) MatchesAPI(gvr schema.GroupVersionResource) bool {
	if t.Pattern == nil {
		return t.Resource == gvr
	}
	pattern := *t.Pattern
	pattern.Kind = "*"
	return pattern.MatchesGroupVersionKind(gvr.GroupVersion().WithKind(""))
}

func (t Target) MatchesKind(gvr schema.GroupVersionResource, kind string) bool {
	return t.Pattern == nil || t.Pattern.MatchesGroupVersionKind(gvr.GroupVersion().WithKind(kind))
}
