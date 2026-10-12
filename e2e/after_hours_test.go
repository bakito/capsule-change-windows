//go:build e2e

// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	api "github.com/projectcapsule/schedule-action-controller/api/v1alpha1"
)

func TestAfterHoursExample(t *testing.T) {
	admin, _ := matchingClients(t)
	if err := appsv1.AddToScheme(admin.Scheme()); err != nil {
		t.Fatal(err)
	}
	for _, ns := range []string{"after-hours", "after-hours-other"} {
		labels := map[string]string{}
		if ns == "after-hours" {
			labels["environment"] = "development"
		}
		createMatching(t, admin, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns, Labels: labels}})
	}
	data, err := os.ReadFile("../examples/after-hours-executor-rbac.yaml")
	if err != nil {
		t.Fatal(err)
	}
	decoder := kyaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	for {
		obj := &unstructured.Unstructured{}
		err := decoder.Decode(obj)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if obj.GetNamespace() == "development" {
			obj.SetNamespace("after-hours")
		}
		// Adapt the documented controller subject to this Helm release.
		if obj.GetKind() == "RoleBinding" && obj.GetName() == "impersonate-after-hours-executor" {
			if err := unstructured.SetNestedSlice(obj.Object, []any{map[string]any{
				"kind": "ServiceAccount", "name": controllerServiceAccount, "namespace": systemNamespace,
			}}, "subjects"); err != nil {
				t.Fatal(err)
			}
		}
		createMatching(t, admin, obj)
	}
	dep := deployment("all-deployments", "after-hours", 2, false)
	createMatching(t, admin, dep)
	zero := deployment("already-zero", "after-hours", 0, false)
	createMatching(t, admin, zero)
	other := deployment("outside-selection", "after-hours-other", 1, false)
	createMatching(t, admin, other)
	stateful := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "all-statefulsets", Namespace: "after-hours"}, Spec: appsv1.StatefulSetSpec{ServiceName: "example", Replicas: ptr.To(int32(3)), Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "stateful"}}, Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "stateful"}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "pause", Image: "registry.k8s.io/pause:3.10"}}}}}}
	createMatching(t, admin, stateful)
	data, err = os.ReadFile("../examples/after-hours.yaml")
	if err != nil {
		t.Fatal(err)
	}
	window := &api.ChangeWindow{}
	if err := yaml.UnmarshalStrict(data, window); err != nil {
		t.Fatal(err)
	}
	zone, err := time.LoadLocation(window.Spec.Schedules[0].TimeZone)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().In(zone)
	window.Spec.Schedules[0].Cron = fmt.Sprintf("%d %d * * *", now.Minute(), now.Hour())
	window.Spec.Schedules[0].Duration = metav1.Duration{Duration: 90 * time.Second}
	createMatching(t, admin, window)
	poll(t, func(ctx context.Context) (bool, error) {
		fresh := &api.ChangeWindow{}
		if err := admin.Get(ctx, client.ObjectKeyFromObject(window), fresh); err != nil {
			return false, err
		}
		if err := admin.Get(ctx, client.ObjectKeyFromObject(dep), dep); err != nil {
			return false, err
		}
		if err := admin.Get(ctx, client.ObjectKeyFromObject(stateful), stateful); err != nil {
			return false, err
		}
		return fresh.Status.Active && fresh.Status.ObservedGeneration == fresh.Generation && meta.IsStatusConditionTrue(fresh.Status.Conditions, api.ConditionReady) && *dep.Spec.Replicas == 0 && *stateful.Spec.Replicas == 0, nil
	})
	assertReplicas(t, admin, zero, 0)
	assertReplicas(t, admin, other, 1)
	poll(t, func(ctx context.Context) (bool, error) {
		fresh := &api.ChangeWindow{}
		if err := admin.Get(ctx, client.ObjectKeyFromObject(window), fresh); err != nil {
			return false, err
		}
		if err := admin.Get(ctx, client.ObjectKeyFromObject(dep), dep); err != nil {
			return false, err
		}
		if err := admin.Get(ctx, client.ObjectKeyFromObject(stateful), stateful); err != nil {
			return false, err
		}
		return !fresh.Status.Active && fresh.Status.PendingResources == 0 && *dep.Spec.Replicas == 2 && *stateful.Spec.Replicas == 3, nil
	})
	assertReplicas(t, admin, zero, 0)
	assertReplicas(t, admin, other, 1)
}
