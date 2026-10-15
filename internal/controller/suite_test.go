// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/restmapper"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	api "github.com/projectcapsule/schedule-action-controller/api/v1alpha1"
	"github.com/projectcapsule/schedule-action-controller/internal/executor"
	"github.com/projectcapsule/schedule-action-controller/internal/testutil"
)

// TestAPI exercises CRD validation, impersonated SSA, capture/restart/restore,
// subresources, and UID protection against a real API server.
func TestAPI(t *testing.T) {
	environment := &envtest.Environment{CRDDirectoryPaths: []string{filepath.Join("..", "..", "charts", "change-windows", "crds")}, ErrorIfCRDPathMissing: true}
	cfg, err := environment.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := environment.Stop(); err != nil {
			t.Error(err)
		}
	})
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{api.AddToScheme, corev1.AddToScheme, appsv1.AddToScheme, rbacv1.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}
	disc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	resources, err := restmapper.GetAPIGroupResources(disc)
	if err != nil {
		t.Fatal(err)
	}
	mapper := restmapper.NewDiscoveryRESTMapper(resources)
	t.Run("ChangeWindow API identity", func(t *testing.T) {
		mapping, err := mapper.RESTMapping(api.GroupVersion.WithKind("ChangeWindow").GroupKind(), api.GroupVersion.Version)
		if err != nil || mapping.Resource.Resource != "changewindows" {
			t.Fatalf("ChangeWindow REST mapping: %v, %v", mapping, err)
		}
		if _, err := mapper.RESTMapping(api.GroupVersion.WithKind("ScheduledAction").GroupKind(), api.GroupVersion.Version); err == nil {
			t.Fatal("legacy ScheduledAction API is still served")
		}
	})
	ctx := t.Context()
	t.Run("status conditions and kubectl table", func(t *testing.T) {
		window := testutil.Action()
		window.ObjectMeta = metav1.ObjectMeta{Name: "status-table"}
		if err := c.Create(ctx, window); err != nil {
			t.Fatal(err)
		}
		now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
		r := &ChangeWindowReconciler{Client: c, Reader: c, Scheme: scheme, Mapper: mapper, Now: func() time.Time { return now }}
		for _, state := range []string{api.ReasonPlanned, api.ReasonActive, api.ReasonFinished} {
			if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(window)}); err != nil {
				t.Fatal(err)
			}
			if err := c.Get(ctx, client.ObjectKeyFromObject(window), window); err != nil {
				t.Fatal(err)
			}
			active := metav1.ConditionFalse
			if state == api.ReasonActive {
				active = metav1.ConditionTrue
			}
			assertConditions(t, window, metav1.ConditionTrue, active, state)
			raw, err := disc.RESTClient().Get().AbsPath("/apis/"+api.GroupVersion.String()+"/changewindows/"+window.Name).
				SetHeader("Accept", "application/json;as=Table;g=meta.k8s.io;v=v1").DoRaw(ctx)
			if err != nil {
				t.Fatal(err)
			}
			table := &metav1.Table{}
			if err := json.Unmarshal(raw, table); err != nil {
				t.Fatal(err)
			}
			if len(table.Rows) != 1 {
				t.Fatalf("expected one row: %s", raw)
			}
			cells := map[string]any{}
			for i, column := range table.ColumnDefinitions {
				cells[column.Name] = table.Rows[0].Cells[i]
			}
			if cells["Ready"] != "True" || cells["State"] != state || cells["Active"] != string(active) || cells["Next Transition"] != window.Status.NextTransition.UTC().Format(time.RFC3339) {
				t.Fatalf("incorrect kubectl columns: %s", raw)
			}
			now = now.Add(time.Hour)
		}
	})
	t.Run("service account schema", func(t *testing.T) {
		for _, mode := range []string{"admission", "actions", "nodes"} {
			for _, tc := range []struct {
				name    string
				account any
			}{
				{"missing", nil},
				{"null", nil},
				{"empty", map[string]any{}},
				{"missing-namespace", map[string]any{"name": "executor"}},
				{"missing-name", map[string]any{"namespace": "dev"}},
				{"empty-namespace", map[string]any{"namespace": "", "name": "executor"}},
				{"empty-name", map[string]any{"namespace": "dev", "name": ""}},
				{"invalid-namespace", map[string]any{"namespace": "Not/valid", "name": "executor"}},
				{"invalid-name", map[string]any{"namespace": "dev", "name": "Not/valid"}},
				{"long-namespace", map[string]any{"namespace": strings.Repeat("a", 64), "name": "executor"}},
				{"long-name", map[string]any{"namespace": "dev", "name": strings.Repeat("a", 254)}},
				{"valid", map[string]any{"namespace": "dev", "name": "window.executor"}},
			} {
				t.Run(mode+"/"+tc.name, func(t *testing.T) {
					window := testutil.Action()
					window.ObjectMeta = metav1.ObjectMeta{GenerateName: "service-account-"}
					switch mode {
					case "actions":
						window.Spec.Actions = []api.ResourceAction{{Name: "apply", Targets: []string{"deployments"}, OnStart: &api.ActionStep{Apply: api.Apply{Template: "spec: {}"}}}}
					case "nodes":
						window.Spec.NamespaceSelectors = nil
						window.Spec.Targets[0].APIVersion, window.Spec.Targets[0].Kind = "v1", "Node"
						window.Spec.Admission[0].Subresources = nil
					}
					// Create a valid object first so the same invalid reference is tested on updates.
					if err := c.Create(ctx, window); err != nil {
						t.Fatal(err)
					}
					data, err := runtime.DefaultUnstructuredConverter.ToUnstructured(window)
					if err != nil {
						t.Fatal(err)
					}
					obj := &unstructured.Unstructured{Object: data}
					obj.SetGroupVersionKind(api.GroupVersion.WithKind("ChangeWindow"))
					if tc.name == "missing" {
						unstructured.RemoveNestedField(obj.Object, "spec", "serviceAccount")
					} else if err := unstructured.SetNestedField(obj.Object, tc.account, "spec", "serviceAccount"); err != nil {
						t.Fatal(err)
					}
					check := func(err error) {
						t.Helper()
						if tc.name == "valid" {
							if err != nil {
								t.Fatal(err)
							}
						} else if !apierrors.IsInvalid(err) || !strings.Contains(err.Error(), "serviceAccount") {
							t.Fatalf("expected service account schema rejection, got %v", err)
						}
					}
					check(c.Update(ctx, obj.DeepCopy()))
					obj.SetName("")
					obj.SetResourceVersion("")
					obj.SetUID("")
					check(c.Create(ctx, obj))
				})
			}
		}
	})
	t.Run("GVK pattern schema", func(t *testing.T) {
		for _, version := range []string{"", "*", "apps", "apps/*", "*.example.com/v1*"} {
			obj := testutil.Action()
			obj.ObjectMeta = metav1.ObjectMeta{GenerateName: "gvk-pattern-"}
			obj.Spec.Targets[0].APIVersion, obj.Spec.Targets[0].Kind = version, "*Set"
			obj.Spec.Admission[0].Subresources = []string{"", "*"}
			if err := c.Create(ctx, obj); err != nil {
				t.Fatal(err)
			}
			fresh := &api.ChangeWindow{}
			if err := c.Get(ctx, client.ObjectKeyFromObject(obj), fresh); err != nil {
				t.Fatal(err)
			}
			if fresh.Spec.Targets[0].APIVersion != version || fresh.Spec.Targets[0].Kind != "*Set" || !reflect.DeepEqual(fresh.Spec.Admission[0].Subresources, []string{"", "*"}) {
				t.Fatalf("pattern fields changed or pruned: %+v", fresh.Spec)
			}
		}
	})
	t.Run("matching schema", func(t *testing.T) {
		for _, warning := range []bool{false, true} {
			for _, tc := range []struct {
				name    string
				mutate  func(*api.RequestMatch)
				invalid bool
			}{
				{"valid", func(*api.RequestMatch) {}, false},
				{"unknown-kind", func(m *api.RequestMatch) { m.Audience.Include[0].Kind = "Robot" }, true},
				{"missing-sa-namespace", func(m *api.RequestMatch) { m.Audience.Include[0].Namespace = "" }, true},
				{"user-namespace", func(m *api.RequestMatch) { m.Audience.Include[0].Kind = "User" }, true},
				{"empty-expression", func(m *api.RequestMatch) { m.MatchConditions[0].Expression = "" }, true},
				{"duplicate-condition", func(m *api.RequestMatch) { m.MatchConditions = append(m.MatchConditions, m.MatchConditions[0]) }, true},
				{"invalid-condition-name", func(m *api.RequestMatch) { m.MatchConditions[0].Name = "Not valid" }, true},
			} {
				t.Run(fmt.Sprintf("warning=%t/%s", warning, tc.name), func(t *testing.T) {
					obj := testutil.Action()
					obj.ObjectMeta = metav1.ObjectMeta{GenerateName: "matching-"}
					obj.Spec.NamespaceSelectors = nil
					obj.Spec.Targets[0].APIVersion, obj.Spec.Targets[0].Kind = "v1", "Node"
					m := &obj.Spec.Admission[0].RequestMatch
					m.Subresources = nil
					m.Audience = &api.Audience{Include: []api.Subject{{Kind: "ServiceAccount", Name: "deployer", Namespace: "dev"}}, Exclude: []api.Subject{{Kind: "User", Name: "break-glass"}}}
					m.MatchConditions = []api.MatchCondition{{Name: "check", Expression: "request.operation == 'UPDATE'"}}
					tc.mutate(m)
					if warning {
						obj.Spec.Config.Admission.Warnings = []api.AdmissionWarning{{Name: "notice", When: "Active", Message: "notice", RequestMatch: *m}}
						obj.Spec.Admission = nil
					}
					err := c.Create(ctx, obj)
					if tc.invalid {
						if !apierrors.IsInvalid(err) {
							t.Fatalf("expected schema rejection, got %v", err)
						}
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					fresh := &api.ChangeWindow{}
					if err := c.Get(ctx, client.ObjectKeyFromObject(obj), fresh); err != nil {
						t.Fatal(err)
					}
					got := fresh.Spec.Admission
					var match api.RequestMatch
					if warning {
						match = fresh.Spec.Config.Admission.Warnings[0].RequestMatch
					} else {
						match = got[0].RequestMatch
					}
					if !reflect.DeepEqual(match.Audience, m.Audience) || !reflect.DeepEqual(match.MatchConditions, m.MatchConditions) || len(fresh.Spec.NamespaceSelectors) != 0 {
						t.Fatalf("matching fields pruned: %+v", match)
					}
				})
			}
		}
	})
	for _, ns := range []string{"dev", "controller", "unselected"} {
		if err := c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns, Labels: map[string]string{"environment": map[string]string{"dev": "development"}[ns]}}}); err != nil {
			t.Fatal(err)
		}
	}
	sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "executor", Namespace: "dev"}}
	if err := c.Create(ctx, sa); err != nil {
		t.Fatal(err)
	}
	role := &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "executor", Namespace: "dev"}, Rules: []rbacv1.PolicyRule{{APIGroups: []string{"apps"}, Resources: []string{"deployments", "deployments/scale"}, Verbs: []string{"get", "list", "patch"}}}}
	if err := c.Create(ctx, role); err != nil {
		t.Fatal(err)
	}
	if err := c.Create(ctx, &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "executor", Namespace: "dev"}, RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: "executor"}, Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Namespace: "dev", Name: "executor"}}}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		sub      string
		replicas int32
	}{{"", 3}, {"scale", 3}, {"scale", 0}} {
		sub := tc.sub
		t.Run(fmt.Sprintf("subresource=%s/replicas=%d", sub, tc.replicas), func(t *testing.T) {
			name := "parent"
			if sub != "" {
				name = sub
			}
			name = fmt.Sprintf("%s-%d", name, tc.replicas)
			dep := deployment(name, "dev", tc.replicas)
			if err := c.Create(ctx, dep); err != nil {
				t.Fatal(err)
			}
			excluded := deployment(name, "unselected", 4)
			if err := c.Create(ctx, excluded); err != nil {
				t.Fatal(err)
			}
			a := testutil.Action()
			a.Name = name
			a.UID = ""
			a.ResourceVersion = ""
			a.Spec.Targets[0].Selector.MatchLabels["test"] = name
			a.Spec.ServiceAccount = &api.ServiceAccountReference{Namespace: "dev", Name: "executor"}
			a.Spec.Actions = []api.ResourceAction{{Name: "scale", Targets: []string{"deployments"}, Capture: map[string]api.Capture{"replicas": {Subresource: sub, Path: "/spec/replicas"}}, OnStart: &api.ActionStep{Apply: api.Apply{Subresource: sub, Force: true, Template: "spec:\n  replicas: 0"}}, OnEnd: &api.ActionStep{Apply: api.Apply{Subresource: sub, Template: "spec:\n  replicas: {{ .captured.replicas }}"}}}}
			if err := c.Create(ctx, a); err != nil {
				t.Fatal(err)
			}
			if a.Namespace != "" {
				t.Fatal("ChangeWindow is not cluster-scoped")
			}
			now, _ := time.Parse(time.RFC3339, "2026-10-01T10:30:00Z")
			r := &ChangeWindowReconciler{Client: c, Reader: c, Scheme: scheme, Mapper: mapper, SystemNamespace: "controller", Factory: executor.NewFactory(cfg, disc), Now: func() time.Time { return now }}
			req := ctrl.Request{NamespacedName: client.ObjectKey{Name: a.Name}}
			reconcile := func() {
				t.Helper()
				deadline := time.Now().Add(10 * time.Second)
				for {
					_, err := r.Reconcile(ctx, req)
					if err == nil {
						return
					}
					if time.Now().After(deadline) {
						t.Fatal(err)
					}
					select {
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					case <-time.After(100 * time.Millisecond):
					}
				}
			}
			reconcile()
			checkReplicas(t, ctx, c, dep, 0)
			checkReplicas(t, ctx, c, excluded, 4)
			// Repeated reconciliation leaves the capture unchanged.
			reconcile()
			checkReplicas(t, ctx, c, dep, 0)
			// Restart with a new reconciler after the end boundary.
			r = &ChangeWindowReconciler{Client: c, Reader: c, Scheme: scheme, Mapper: mapper, SystemNamespace: "controller", Factory: executor.NewFactory(cfg, disc), Now: func() time.Time { return now }}
			now = now.Add(time.Hour)
			reconcile()
			checkReplicas(t, ctx, c, dep, tc.replicas)
			if err := c.Delete(ctx, a); err != nil {
				t.Fatal(err)
			}
			reconcile()
			if err := c.Get(ctx, req.NamespacedName, &api.ChangeWindow{}); !apierrors.IsNotFound(err) {
				t.Fatalf("finalizer not removed: %v", err)
			}
		})
	}
	t.Run("UID replacement is untouched", func(t *testing.T) {
		dep := deployment("replaced", "dev", 7)
		if err := c.Create(ctx, dep); err != nil {
			t.Fatal(err)
		}
		exec, err := executor.NewFactory(cfg, disc)(api.ServiceAccountReference{Namespace: "dev", Name: "executor"}, types.UID("test"))
		if err != nil {
			t.Fatal(err)
		}
		p := executor.Participant{Resource: appsv1.SchemeGroupVersion.WithResource("deployments"), APIVersion: "apps/v1", Kind: "Deployment", Name: dep.Name, Namespace: dep.Namespace, UID: "old-uid"}
		prepared, err := exec.Prepare(ctx, p, &api.ActionStep{Apply: api.Apply{Force: true, Template: "spec:\n  replicas: 0"}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		gone, err := exec.Apply(ctx, p, prepared, "end")
		if err != nil || !gone {
			t.Fatalf("replacement: gone=%v err=%v", gone, err)
		}
		checkReplicas(t, ctx, c, dep, 7)
	})
	invalid := &unstructured.Unstructured{Object: map[string]any{"apiVersion": api.GroupVersion.String(), "kind": "ChangeWindow", "metadata": map[string]any{"name": "invalid"}, "spec": map[string]any{}}}
	if err := c.Create(ctx, invalid); !apierrors.IsInvalid(err) {
		t.Fatalf("missing required config accepted: %v", err)
	}
}

func deployment(name, ns string, replicas int32) *appsv1.Deployment {
	return &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: map[string]string{"maintenance": "enabled", "test": name}}, Spec: appsv1.DeploymentSpec{Replicas: ptr.To(replicas), Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}}, Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": name}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "pause", Image: "registry.k8s.io/pause:3.10"}}}}}}
}

func checkReplicas(t *testing.T, ctx context.Context, c client.Client, dep *appsv1.Deployment, want int32) {
	t.Helper()
	if err := c.Get(ctx, client.ObjectKeyFromObject(dep), dep); err != nil {
		t.Fatal(err)
	}
	if *dep.Spec.Replicas != want {
		t.Fatalf("%s/%s replicas=%d want %d", dep.Namespace, dep.Name, *dep.Spec.Replicas, want)
	}
}
