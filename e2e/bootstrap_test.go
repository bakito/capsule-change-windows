//go:build e2e

// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	api "github.com/projectcapsule/schedule-action-controller/api/v1alpha1"
)

const (
	systemNamespace          = "schedule-action-controller-system"
	controllerServiceAccount = "change-windows"
)

type warningRecorder struct {
	mu       sync.Mutex
	messages []string
}

func (w *warningRecorder) HandleWarningHeader(_ int, _ string, text string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.messages = append(w.messages, text)
}

func (w *warningRecorder) seen(fragment string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, message := range w.messages {
		if strings.Contains(message, fragment) {
			return true
		}
	}
	return false
}

func TestChangeWindow(t *testing.T) {
	cfg := ctrl.GetConfigOrDie()
	t.Run("serves the renamed cluster API", func(t *testing.T) {
		disc, err := discovery.NewDiscoveryClientForConfig(cfg)
		if err != nil {
			t.Fatal(err)
		}
		resources, err := disc.ServerResourcesForGroupVersion(api.GroupVersion.String())
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, resource := range resources.APIResources {
			if resource.Name == "scheduledactions" {
				t.Fatal("legacy ScheduledAction resource is still served")
			}
			if resource.Name == "changewindows" {
				found = true
				if resource.Kind != "ChangeWindow" || resource.SingularName != "changewindow" || resource.Namespaced || len(resource.ShortNames) != 1 || resource.ShortNames[0] != "cw" {
					t.Fatalf("wrong ChangeWindow discovery contract: %+v", resource)
				}
			}
		}
		if !found {
			t.Fatal("ChangeWindow resource is missing from discovery")
		}
	})
	warnings := &warningRecorder{}
	cfg.WarningHandler = warnings
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{api.AddToScheme, corev1.AddToScheme, appsv1.AddToScheme, rbacv1.AddToScheme, autoscalingv1.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	admin, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	create := func(obj client.Object) {
		t.Helper()
		if err := admin.Create(ctx, obj); err != nil {
			t.Fatal(err)
		}
	}
	for _, ns := range []string{"scheduled-dev", "scheduled-other"} {
		create(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns, Labels: map[string]string{"change-window-test": ns}}})
	}
	create(&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "executor", Namespace: "scheduled-dev"}})
	create(&rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "executor", Namespace: "scheduled-dev"}, Rules: []rbacv1.PolicyRule{{APIGroups: []string{"apps"}, Resources: []string{"deployments", "deployments/scale"}, Verbs: []string{"get", "list", "create", "update", "patch", "delete"}}}})
	create(&rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "executor", Namespace: "scheduled-dev"}, RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: "executor"}, Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Name: "executor", Namespace: "scheduled-dev"}}})
	// Explicit permission to impersonate this single executor, never arbitrary service accounts.
	create(&rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "impersonate-executor", Namespace: "scheduled-dev"}, Rules: []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"serviceaccounts"}, ResourceNames: []string{"executor"}, Verbs: []string{"impersonate"}}}})
	create(&rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "impersonate-executor", Namespace: "scheduled-dev"}, RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: "impersonate-executor"}, Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Name: controllerServiceAccount, Namespace: systemNamespace}}})
	selected := deployment("selected", "scheduled-dev", 3, true)
	other := deployment("other", "scheduled-other", 2, true)
	unmatched := deployment("unmatched", "scheduled-dev", 2, false)
	create(selected)
	create(other)
	create(unmatched)
	userCfg := rest.CopyConfig(cfg)
	userCfg.Impersonate = rest.ImpersonationConfig{UserName: "system:serviceaccount:scheduled-dev:executor"}
	user, err := client.New(userCfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}
	action := &api.ChangeWindow{ObjectMeta: metav1.ObjectMeta{Name: "maintenance"}, Spec: api.ChangeWindowSpec{
		NamespaceSelectors: []metav1.LabelSelector{{MatchLabels: map[string]string{"change-window-test": "scheduled-dev"}}, {MatchLabels: map[string]string{"duplicate-selector": "yes"}}},
		Schedules:          []api.Schedule{{Name: "continuous", Cron: "* * * * *", TimeZone: "UTC", Duration: metav1.Duration{Duration: 2 * time.Minute}}},
		Targets:            []api.Target{{Name: "deployments", APIVersion: "apps/v1", Kind: "Deployment", Selector: metav1.LabelSelector{MatchLabels: map[string]string{"maintenance": "enabled"}}}},
		ServiceAccount:     &api.ServiceAccountReference{Namespace: "scheduled-dev", Name: "executor"},
		Actions:            []api.ResourceAction{{Name: "downscale", Targets: []string{"deployments"}, Capture: map[string]api.Capture{"replicas": {Subresource: "scale", Path: "/spec/replicas"}}, OnStart: &api.ActionStep{Apply: api.Apply{Subresource: "scale", Force: true, Template: "spec:\n  replicas: 0"}}, OnEnd: &api.ActionStep{Apply: api.Apply{Subresource: "scale", Template: "spec:\n  replicas: {{ .captured.replicas }}"}}}},
		Admission:          []api.AdmissionRule{{Name: "freeze", When: "Active", Action: "Deny", Message: "Scheduled maintenance blocks this change", RequestMatch: api.RequestMatch{Targets: []string{"deployments"}, Operations: []string{"CREATE", "UPDATE", "DELETE"}, Subresources: []string{"", "scale"}}}},
	}}
	action.Spec.Admission[0].MatchConditions = []api.MatchCondition{{Name: "scale-payload", Expression: "request.subResource != 'scale' || (object.kind == 'Scale' && oldObject.kind == 'Scale')"}}
	action.Spec.Config.Admission.Warnings = []api.AdmissionWarning{{Name: "active", When: "Active", Message: "Maintenance is active", RequestMatch: action.Spec.Admission[0].RequestMatch}}
	t.Run("validate global API", func(t *testing.T) {
		invalid := action.DeepCopy()
		invalid.Name = "invalid"
		invalid.Spec.Schedules[0].Cron = "invalid"
		err := admin.Create(ctx, invalid)
		if err == nil || !strings.Contains(err.Error(), "five cron fields") {
			t.Fatalf("expected cron rejection, got %v", err)
		}
		if err := admin.Get(ctx, client.ObjectKey{Name: invalid.Name}, invalid); !apierrors.IsNotFound(err) {
			t.Fatalf("invalid object persisted: %v", err)
		}
		unauthorized := action.DeepCopy()
		unauthorized.Name = "unauthorized"
		if err := user.Create(ctx, unauthorized); !apierrors.IsForbidden(err) {
			t.Fatalf("expected global RBAC rejection, got %v", err)
		}
	})
	create(action)
	t.Run("capture and SSA scale with own-policy bypass", func(t *testing.T) {
		poll(t, func(ctx context.Context) (bool, error) {
			if err := admin.Get(ctx, client.ObjectKeyFromObject(action), action); err != nil {
				return false, err
			}
			if err := admin.Get(ctx, client.ObjectKeyFromObject(selected), selected); err != nil {
				return false, err
			}
			return *selected.Spec.Replicas == 0 && meta.IsStatusConditionTrue(action.Status.Conditions, api.ConditionReady) && action.Status.ObservedGeneration == action.Generation, nil
		})
		assertReplicas(t, admin, other, 2)
		assertReplicas(t, admin, unmatched, 2)
	})
	t.Run("deny create update delete scale and dry-run", func(t *testing.T) {
		deny := func(err error) {
			t.Helper()
			if err == nil || !strings.Contains(err.Error(), "Scheduled maintenance blocks this change") {
				t.Fatalf("expected maintenance denial, got %v", err)
			}
		}
		createAttempt := deployment("denied", "scheduled-dev", 1, true)
		deny(user.Create(ctx, createAttempt))
		if err := admin.Get(ctx, client.ObjectKeyFromObject(createAttempt), createAttempt); !apierrors.IsNotFound(err) {
			t.Fatalf("denied create persisted: %v", err)
		}
		if err := user.Get(ctx, client.ObjectKeyFromObject(selected), selected); err != nil {
			t.Fatal(err)
		}
		selected.Labels = nil
		deny(user.Update(ctx, selected, client.DryRunAll))
		deny(user.Delete(ctx, selected))
		scale := &autoscalingv1.Scale{}
		if err := user.SubResource("scale").Get(ctx, selected, scale); err != nil {
			t.Fatal(err)
		}
		scale.Spec.Replicas = 5
		deny(user.SubResource("scale").Update(ctx, selected, client.WithSubResourceBody(scale)))
		assertReplicas(t, admin, selected, 0)
		if !warnings.seen("Maintenance is active") {
			t.Fatal("admission warning not returned on denied request")
		}
	})
	t.Run("unselected namespaces and resources remain writable", func(t *testing.T) {
		updateDeployment(t, admin, other, 4)
		updateDeployment(t, user, unmatched, 4)
		assertReplicas(t, admin, other, 4)
		assertReplicas(t, admin, unmatched, 4)
	})
	t.Run("namespace label changes and deletion restore recorded participants", func(t *testing.T) {
		poll(t, func(ctx context.Context) (bool, error) {
			ns := &corev1.Namespace{}
			if err := admin.Get(ctx, client.ObjectKey{Name: "scheduled-dev"}, ns); err != nil {
				return false, err
			}
			ns.Labels["change-window-test"] = "no-longer-selected"
			err := admin.Update(ctx, ns)
			if apierrors.IsConflict(err) {
				return false, nil
			}
			return err == nil, err
		})
		poll(t, func(ctx context.Context) (bool, error) {
			fresh := &appsv1.Deployment{}
			if err := user.Get(ctx, client.ObjectKeyFromObject(selected), fresh); err != nil {
				return false, err
			}
			if fresh.Annotations == nil {
				fresh.Annotations = map[string]string{}
			}
			fresh.Annotations["test"] = "allowed"
			err := user.Update(ctx, fresh, client.DryRunAll)
			if apierrors.IsConflict(err) || (err != nil && strings.Contains(err.Error(), "Scheduled maintenance")) {
				return false, nil
			}
			return err == nil, err
		})
		if err := admin.Delete(ctx, action); err != nil {
			t.Fatal(err)
		}
		poll(t, func(ctx context.Context) (bool, error) {
			err := admin.Get(ctx, client.ObjectKeyFromObject(action), &api.ChangeWindow{})
			if apierrors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		})
		assertReplicas(t, admin, selected, 3)
	})
	t.Run("finite window start end and upcoming warning", func(t *testing.T) {
		finite := action.DeepCopy()
		finite.ObjectMeta = metav1.ObjectMeta{Name: "finite"}
		finite.Status = api.ChangeWindowStatus{}
		finite.Spec.NamespaceSelectors = []metav1.LabelSelector{{MatchLabels: map[string]string{"change-window-test": "no-longer-selected"}}}
		finite.Spec.Admission = nil
		start := time.Now().UTC().Truncate(time.Minute).Add(time.Minute)
		if time.Until(start) < 15*time.Second {
			start = start.Add(time.Minute)
		}
		finite.Spec.Schedules = []api.Schedule{{Name: "once-today", Cron: fmt.Sprintf("%d %d * * *", start.Minute(), start.Hour()), TimeZone: "UTC", Duration: metav1.Duration{Duration: 20 * time.Second}}}
		finite.Spec.Config.Admission.Warnings = []api.AdmissionWarning{{Name: "soon", When: "BeforeStart", Within: &metav1.Duration{Duration: 2 * time.Minute}, Message: "Upcoming maintenance", RequestMatch: api.RequestMatch{Targets: []string{"deployments"}, Operations: []string{"UPDATE"}}}}
		create(finite)
		poll(t, func(ctx context.Context) (bool, error) {
			fresh := &appsv1.Deployment{}
			if err := user.Get(ctx, client.ObjectKeyFromObject(selected), fresh); err != nil {
				return false, err
			}
			err := user.Update(ctx, fresh, client.DryRunAll)
			if apierrors.IsConflict(err) {
				return false, nil
			}
			return warnings.seen("Upcoming maintenance"), err
		})
		poll(t, func(ctx context.Context) (bool, error) {
			if err := admin.Get(ctx, client.ObjectKeyFromObject(selected), selected); err != nil {
				return false, err
			}
			return *selected.Spec.Replicas == 0, nil
		})
		poll(t, func(ctx context.Context) (bool, error) {
			if err := admin.Get(ctx, client.ObjectKeyFromObject(selected), selected); err != nil {
				return false, err
			}
			return *selected.Spec.Replicas == 3, nil
		})
		if err := admin.Delete(ctx, finite); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("protected infrastructure stays writable", func(t *testing.T) {
		guard := &api.ChangeWindow{ObjectMeta: metav1.ObjectMeta{Name: "infrastructure-guard"}, Spec: api.ChangeWindowSpec{
			ServiceAccount:     &api.ServiceAccountReference{Namespace: "scheduled-dev", Name: "executor"},
			NamespaceSelectors: []metav1.LabelSelector{{}},
			Schedules:          []api.Schedule{{Name: "active", Cron: "* * * * *", Duration: metav1.Duration{Duration: 2 * time.Minute}}},
			Targets:            []api.Target{{Name: "configs", APIVersion: "v1", Kind: "ConfigMap"}},
			Admission:          []api.AdmissionRule{{Name: "deny", Action: "Deny", When: "Active", Message: "ConfigMap maintenance", RequestMatch: api.RequestMatch{Targets: []string{"configs"}, Operations: []string{"CREATE"}}}},
		}}
		create(guard)
		poll(t, func(ctx context.Context) (bool, error) {
			err := admin.Create(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "denied-config", Namespace: "scheduled-dev"}}, client.DryRunAll)
			if err == nil {
				return false, nil
			}
			if strings.Contains(err.Error(), "ConfigMap maintenance") {
				return true, nil
			}
			return false, err
		})
		for _, namespace := range []string{systemNamespace, "kube-system", "kube-public", "kube-node-lease"} {
			create(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "protected-config", Namespace: namespace}})
		}
		if err := admin.Delete(ctx, guard); err != nil {
			t.Fatal(err)
		}
	})
}

func poll(t *testing.T, check func(context.Context) (bool, error)) {
	t.Helper()
	if err := wait.PollUntilContextTimeout(t.Context(), 200*time.Millisecond, 100*time.Second, true, check); err != nil {
		t.Fatal(err)
	}
}

func deployment(name, ns string, replicas int32, selected bool) *appsv1.Deployment {
	labels := map[string]string{}
	if selected {
		labels["maintenance"] = "enabled"
	}
	return &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: labels}, Spec: appsv1.DeploymentSpec{Replicas: ptr.To(replicas), Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}}, Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": name}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "pause", Image: "registry.k8s.io/pause:3.10"}}}}}}
}

func assertReplicas(t *testing.T, c client.Client, dep *appsv1.Deployment, want int32) {
	t.Helper()
	if err := c.Get(t.Context(), client.ObjectKeyFromObject(dep), dep); err != nil {
		t.Fatal(err)
	}
	if *dep.Spec.Replicas != want {
		t.Fatalf("%s/%s replicas=%d want %d", dep.Namespace, dep.Name, *dep.Spec.Replicas, want)
	}
}

func updateDeployment(t *testing.T, c client.Client, dep *appsv1.Deployment, replicas int32) {
	t.Helper()
	poll(t, func(ctx context.Context) (bool, error) {
		if err := c.Get(ctx, client.ObjectKeyFromObject(dep), dep); err != nil {
			return false, err
		}
		dep.Spec.Replicas = ptr.To(replicas)
		err := c.Update(ctx, dep)
		if apierrors.IsConflict(err) {
			return false, nil
		}
		return err == nil, err
	})
}
