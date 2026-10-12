//go:build e2e

// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	api "github.com/projectcapsule/schedule-action-controller/api/v1alpha1"
)

func TestFreezeExceptPodsExample(t *testing.T) {
	admin, actor := matchingClients(t)
	for _, add := range []func() error{func() error { return appsv1.AddToScheme(admin.Scheme()) }, func() error { return autoscalingv1.AddToScheme(admin.Scheme()) }, func() error { return apiextensionsv1.AddToScheme(admin.Scheme()) }} {
		if err := add(); err != nil {
			t.Fatal(err)
		}
	}
	ctx := t.Context()
	for _, ns := range []string{"freeze-all", "freeze-other"} {
		labels := map[string]string{}
		if ns == "freeze-all" {
			labels["maintenance"] = "enabled"
		}
		createMatching(t, admin, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns, Labels: labels}})
		createMatching(t, admin, &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "tenant-writer", Namespace: ns}, Rules: []rbacv1.PolicyRule{{APIGroups: []string{"*"}, Resources: []string{"*"}, Verbs: []string{"get", "list", "create", "update", "patch", "delete"}}}})
		createMatching(t, admin, &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "tenant-writer", Namespace: ns}, RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: "tenant-writer"}, Subjects: []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: "Group", Name: "tenant-developers"}}})
	}
	dep := deployment("recover", "freeze-all", 1, false)
	createMatching(t, admin, dep)
	config := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "existing", Namespace: "freeze-all"}, Data: map[string]string{"value": "original"}}
	createMatching(t, admin, config)
	data, err := os.ReadFile("../examples/freeze-except-pods.yaml")
	if err != nil {
		t.Fatal(err)
	}
	window := &api.ChangeWindow{}
	if err := yaml.UnmarshalStrict(data, window); err != nil {
		t.Fatal(err)
	}
	window.Spec.Schedules = []api.Schedule{{Name: "active", Cron: "* * * * *", Duration: metav1.Duration{Duration: 2 * time.Minute}}}
	createMatching(t, admin, window)
	awaitWindow(t, admin, window)
	user, _ := actor("tenant-user", "tenant-developers")
	probe := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "probe", Namespace: "freeze-all"}}
	poll(t, func(ctx context.Context) (bool, error) {
		err := user.Create(ctx, probe.DeepCopy(), client.DryRunAll)
		if err == nil {
			return false, nil
		}
		if strings.Contains(err.Error(), "Tenant maintenance") {
			return true, nil
		}
		return false, err
	})
	t.Run("all non-Pod writes and scale are denied", func(t *testing.T) {
		requireMatchingDenial(t, user.Create(ctx, probe.DeepCopy()), "Tenant maintenance")
		if err := admin.Get(ctx, client.ObjectKeyFromObject(probe), &corev1.ConfigMap{}); !apierrors.IsNotFound(err) {
			t.Fatalf("denied create persisted: %v", err)
		}
		poll(t, func(ctx context.Context) (bool, error) {
			fresh := &corev1.ConfigMap{}
			if err := user.Get(ctx, client.ObjectKeyFromObject(config), fresh); err != nil {
				return false, err
			}
			fresh.Data["value"] = "changed"
			err := user.Update(ctx, fresh)
			if apierrors.IsConflict(err) {
				return false, nil
			}
			requireMatchingDenial(t, err, "Tenant maintenance")
			return true, nil
		})
		requireMatchingDenial(t, user.Delete(ctx, config), "Tenant maintenance")
		if err := admin.Get(ctx, client.ObjectKeyFromObject(config), config); err != nil || config.Data["value"] != "original" {
			t.Fatalf("denied mutation persisted: %v", err)
		}
		poll(t, func(ctx context.Context) (bool, error) {
			scale := &autoscalingv1.Scale{}
			if err := user.SubResource("scale").Get(ctx, dep, scale); err != nil {
				return false, err
			}
			scale.Spec.Replicas = 2
			err := user.SubResource("scale").Update(ctx, dep, client.WithSubResourceBody(scale))
			if apierrors.IsConflict(err) {
				return false, nil
			}
			requireMatchingDenial(t, err, "Tenant maintenance")
			return true, nil
		})
		assertReplicas(t, admin, dep, 1)
	})
	t.Run("new custom resources are covered", func(t *testing.T) {
		crd := &apiextensionsv1.CustomResourceDefinition{ObjectMeta: metav1.ObjectMeta{Name: "widgets.freeze.example.com"}, Spec: apiextensionsv1.CustomResourceDefinitionSpec{Group: "freeze.example.com", Scope: apiextensionsv1.NamespaceScoped, Names: apiextensionsv1.CustomResourceDefinitionNames{Plural: "widgets", Singular: "widget", Kind: "Widget", ListKind: "WidgetList"}, Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{Name: "v1alpha1", Served: true, Storage: true, Schema: &apiextensionsv1.CustomResourceValidation{OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{Type: "object", XPreserveUnknownFields: ptr.To(true)}}}}}}
		createMatching(t, admin, crd)
		poll(t, func(ctx context.Context) (bool, error) {
			fresh := &apiextensionsv1.CustomResourceDefinition{}
			if err := admin.Get(ctx, client.ObjectKeyFromObject(crd), fresh); err != nil {
				return false, err
			}
			for _, c := range fresh.Status.Conditions {
				if c.Type == apiextensionsv1.Established && c.Status == apiextensionsv1.ConditionTrue {
					return true, nil
				}
			}
			return false, nil
		})
		obj := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "freeze.example.com/v1alpha1", "kind": "Widget", "metadata": map[string]any{"name": "new", "namespace": "freeze-all"}}}
		requireMatchingDenial(t, user.Create(ctx, obj), "Tenant maintenance")
		if err := admin.Get(ctx, client.ObjectKeyFromObject(obj), obj); !apierrors.IsNotFound(err) {
			t.Fatalf("denied CR persisted: %v", err)
		}
		obj.SetNamespace("freeze-other")
		if err := user.Create(ctx, obj); err != nil {
			t.Fatal(err)
		}
		if err := admin.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("Pod writes and actual replacement scheduling continue", func(t *testing.T) {
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "manual", Namespace: "freeze-all"}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "pause", Image: "registry.k8s.io/pause:3.10"}}}}
		if err := user.Create(ctx, pod); err != nil {
			t.Fatal(err)
		}
		poll(t, func(ctx context.Context) (bool, error) {
			fresh := &corev1.Pod{}
			if err := user.Get(ctx, client.ObjectKeyFromObject(pod), fresh); err != nil {
				return false, err
			}
			fresh.Labels = map[string]string{"updated": "true"}
			err := user.Update(ctx, fresh)
			if apierrors.IsConflict(err) {
				return false, nil
			}
			return err == nil, err
		})
		if err := admin.Get(ctx, client.ObjectKeyFromObject(pod), pod); err != nil || pod.Labels["updated"] != "true" {
			t.Fatalf("Pod update: %v", err)
		}
		if err := user.Delete(ctx, pod, client.GracePeriodSeconds(0)); err != nil {
			t.Fatal(err)
		}
		var original corev1.Pod
		poll(t, func(ctx context.Context) (bool, error) {
			pods := &corev1.PodList{}
			if err := admin.List(ctx, pods, client.InNamespace(dep.Namespace), client.MatchingLabels{"app": dep.Name}); err != nil {
				return false, err
			}
			for _, p := range pods.Items {
				if p.Spec.NodeName != "" && p.DeletionTimestamp == nil {
					original = p
					return true, nil
				}
			}
			return false, nil
		})
		if err := user.Delete(ctx, &original, client.GracePeriodSeconds(0)); err != nil {
			t.Fatal(err)
		}
		poll(t, func(ctx context.Context) (bool, error) {
			pods := &corev1.PodList{}
			if err := admin.List(ctx, pods, client.InNamespace(dep.Namespace), client.MatchingLabels{"app": dep.Name}); err != nil {
				return false, err
			}
			for _, p := range pods.Items {
				if p.UID != original.UID && p.Spec.NodeName != "" && p.DeletionTimestamp == nil {
					return true, nil
				}
			}
			return false, nil
		})
	})
	t.Run("deletion removes the freeze", func(t *testing.T) {
		if err := admin.Delete(ctx, window); err != nil {
			t.Fatal(err)
		}
		poll(t, func(ctx context.Context) (bool, error) {
			err := user.Create(ctx, probe.DeepCopy(), client.DryRunAll)
			if err != nil && strings.Contains(err.Error(), "Tenant maintenance") {
				return false, nil
			}
			return err == nil, err
		})
		if err := user.Create(ctx, probe); err != nil {
			t.Fatal(err)
		}
		if err := admin.Get(ctx, client.ObjectKeyFromObject(probe), probe); err != nil {
			t.Fatal(err)
		}
	})
}
