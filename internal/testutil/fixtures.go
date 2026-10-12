// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

// Package testutil provides shared API fixtures without a second test framework.
package testutil

import (
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	api "github.com/projectcapsule/schedule-action-controller/api/v1alpha1"
)

func Mapper() meta.RESTMapper {
	m := meta.NewDefaultRESTMapper([]schema.GroupVersion{{Group: "apps", Version: "v1"}, {Version: "v1"}})
	m.Add(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}, meta.RESTScopeNamespace)
	m.Add(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "StatefulSet"}, meta.RESTScopeNamespace)
	m.Add(schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}, meta.RESTScopeNamespace)
	m.Add(schema.GroupVersionKind{Version: "v1", Kind: "Node"}, meta.RESTScopeRoot)
	m.Add(schema.GroupVersionKind{Version: "v1", Kind: "Namespace"}, meta.RESTScopeRoot)
	return m
}

func Action() *api.ChangeWindow {
	return &api.ChangeWindow{ObjectMeta: metav1.ObjectMeta{Name: "maintenance", UID: "test-uid", Generation: 1}, Spec: api.ChangeWindowSpec{
		NamespaceSelectors: []metav1.LabelSelector{{MatchLabels: map[string]string{"environment": "development"}}},
		Schedules:          []api.Schedule{{Name: "daily", Cron: "0 10 * * *", TimeZone: "UTC", Duration: metav1.Duration{Duration: time.Hour}}},
		Targets:            []api.Target{{Name: "deployments", APIVersion: "apps/v1", Kind: "Deployment", Selector: metav1.LabelSelector{MatchLabels: map[string]string{"maintenance": "enabled"}}}},
		Admission:          []api.AdmissionRule{{Name: "freeze", When: "Active", Action: "Deny", Message: "Maintenance until {{ .window.end }}", RequestMatch: api.RequestMatch{Targets: []string{"deployments"}, Operations: []string{"CREATE", "UPDATE", "DELETE"}, Subresources: []string{"", "scale"}}}},
	}}
}
