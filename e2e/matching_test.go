//go:build e2e

// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	api "github.com/projectcapsule/schedule-action-controller/api/v1alpha1"
)

func matchingClients(t *testing.T) (client.Client, func(string, ...string) (client.Client, *warningRecorder)) {
	t.Helper()
	cfg := ctrl.GetConfigOrDie()
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{api.AddToScheme, corev1.AddToScheme, rbacv1.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	makeClient := func(config *rest.Config) client.Client {
		c, err := client.New(config, client.Options{Scheme: scheme})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	return makeClient(cfg), func(name string, groups ...string) (client.Client, *warningRecorder) {
		copy := rest.CopyConfig(cfg)
		copy.Impersonate = rest.ImpersonationConfig{UserName: name, Groups: groups}
		warnings := &warningRecorder{}
		copy.WarningHandler = warnings
		return makeClient(copy), warnings
	}
}

func createMatching(t *testing.T, c client.Client, obj client.Object) {
	t.Helper()
	if err := c.Create(t.Context(), obj); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := c.Delete(ctx, obj); client.IgnoreNotFound(err) != nil {
			t.Error(err)
		}
	})
}

func awaitWindow(t *testing.T, c client.Client, window *api.ChangeWindow) {
	t.Helper()
	poll(t, func(ctx context.Context) (bool, error) {
		fresh := &api.ChangeWindow{}
		if err := c.Get(ctx, client.ObjectKeyFromObject(window), fresh); err != nil {
			return false, err
		}
		return fresh.Status.ObservedGeneration == fresh.Generation && meta.IsStatusConditionTrue(fresh.Status.Conditions, api.ConditionReady), nil
	})
}

func requireMatchingDenial(t *testing.T, err error, reason string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), reason) {
		t.Fatalf("expected %q denial, got %v", reason, err)
	}
}

func TestAdmissionMatching(t *testing.T) {
	admin, actor := matchingClients(t)
	ctx := t.Context()
	for _, ns := range []string{"audience-a", "audience-b"} {
		createMatching(t, admin, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns, Labels: map[string]string{"audience-test": ns}}})
		createMatching(t, admin, &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "deployer", Namespace: ns}})
		createMatching(t, admin, &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "writer", Namespace: ns}, Rules: []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"configmaps"}, Verbs: []string{"get", "create", "update", "delete"}}}})
		createMatching(t, admin, &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "writer", Namespace: ns}, RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: "writer"}, Subjects: []rbacv1.Subject{{Kind: "User", Name: "alice", APIGroup: rbacv1.GroupName}, {Kind: "User", Name: "bob", APIGroup: rbacv1.GroupName}, {Kind: "User", Name: "carol", APIGroup: rbacv1.GroupName}, {Kind: "ServiceAccount", Namespace: "audience-a", Name: "deployer"}, {Kind: "ServiceAccount", Namespace: "audience-b", Name: "deployer"}}})
	}
	alice, aliceWarnings := actor("alice", "developers")
	bob, bobWarnings := actor("bob", "developers")
	carol, _ := actor("carol")
	saA, _ := actor("system:serviceaccount:audience-a:deployer")
	saB, _ := actor("system:serviceaccount:audience-b:deployer")
	match := api.RequestMatch{Targets: []string{"configs"}, Operations: []string{"CREATE", "UPDATE", "DELETE"}, Audience: &api.Audience{Include: []api.Subject{{Kind: "User", Name: "alice"}, {Kind: "Group", Name: "developers"}, {Kind: "ServiceAccount", Name: "deployer", Namespace: "audience-a"}}, Exclude: []api.Subject{{Kind: "User", Name: "bob"}}}, MatchConditions: []api.MatchCondition{{Name: "locked", Expression: "(object != null ? object : oldObject).data.mode == 'locked'"}}}
	window := &api.ChangeWindow{ObjectMeta: metav1.ObjectMeta{Name: "audience-test"}, Spec: api.ChangeWindowSpec{NamespaceSelectors: []metav1.LabelSelector{{MatchLabels: map[string]string{"audience-test": "audience-a"}}}, Schedules: []api.Schedule{{Name: "active", Cron: "* * * * *", Duration: metav1.Duration{Duration: 2 * time.Minute}}}, Targets: []api.Target{{Name: "configs", APIVersion: "v1", Kind: "ConfigMap"}}, Admission: []api.AdmissionRule{{Name: "freeze", When: "Active", Message: "Audience maintenance denial", RequestMatch: match}}}}
	warnMatch := *match.DeepCopy()
	warnMatch.MatchConditions[0].Expression = "(object != null ? object : oldObject).data.mode == 'warn'"
	window.Spec.Config.Admission.Warnings = []api.AdmissionWarning{{Name: "notice", When: "Active", Message: "Audience maintenance warning", RequestMatch: warnMatch}}
	createMatching(t, admin, window)
	awaitWindow(t, admin, window)
	config := func(name, ns, mode string) *corev1.ConfigMap {
		return &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}, Data: map[string]string{"mode": mode}}
	}
	// Wait for admission's generation to be visible using side-effect-free probes.
	poll(t, func(ctx context.Context) (bool, error) {
		err := alice.Create(ctx, config("probe", "audience-a", "locked"), client.DryRunAll)
		if err == nil {
			return false, nil
		}
		if strings.Contains(err.Error(), "Audience maintenance denial") {
			return true, nil
		}
		return false, err
	})
	t.Run("identities and namespace profiles", func(t *testing.T) {
		for i, tc := range []struct {
			c    client.Client
			ns   string
			deny bool
		}{{alice, "audience-a", true}, {bob, "audience-a", false}, {carol, "audience-a", false}, {saA, "audience-a", true}, {saB, "audience-a", false}, {alice, "audience-b", false}} {
			obj := config(fmt.Sprintf("identity-%d", i), tc.ns, "locked")
			err := tc.c.Create(ctx, obj)
			if tc.deny {
				requireMatchingDenial(t, err, "Audience maintenance denial")
				if err := admin.Get(ctx, client.ObjectKeyFromObject(obj), &corev1.ConfigMap{}); !apierrors.IsNotFound(err) {
					t.Fatalf("denied object persisted: %v", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if err := admin.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil || obj.Data["mode"] != "locked" {
					t.Fatalf("allowed state: %v", err)
				}
			}
		}
	})
	t.Run("CEL create update delete and warnings", func(t *testing.T) {
		open := config("open", "audience-a", "open")
		if err := alice.Create(ctx, open); err != nil {
			t.Fatal(err)
		}
		poll(t, func(ctx context.Context) (bool, error) {
			fresh := &corev1.ConfigMap{}
			if err := alice.Get(ctx, client.ObjectKeyFromObject(open), fresh); err != nil {
				return false, err
			}
			fresh.Data["mode"] = "locked"
			err := alice.Update(ctx, fresh)
			if apierrors.IsConflict(err) {
				return false, nil
			}
			requireMatchingDenial(t, err, "Audience maintenance denial")
			return true, nil
		})
		if err := admin.Get(ctx, client.ObjectKeyFromObject(open), open); err != nil || open.Data["mode"] != "open" {
			t.Fatalf("rejected update persisted: %v", err)
		}
		locked := config("delete-locked", "audience-a", "locked")
		if err := bob.Create(ctx, locked); err != nil {
			t.Fatal(err)
		}
		requireMatchingDenial(t, alice.Delete(ctx, locked), "Audience maintenance denial")
		if err := admin.Get(ctx, client.ObjectKeyFromObject(locked), locked); err != nil {
			t.Fatal(err)
		}
		if err := alice.Delete(ctx, open); err != nil {
			t.Fatal(err)
		}
		if err := alice.Create(ctx, config("notice-alice", "audience-a", "warn")); err != nil {
			t.Fatal(err)
		}
		if !aliceWarnings.seen("Audience maintenance warning") {
			t.Fatal("matching warning missing")
		}
		if err := bob.Create(ctx, config("notice-bob", "audience-a", "warn")); err != nil {
			t.Fatal(err)
		}
		if bobWarnings.seen("Audience maintenance warning") {
			t.Fatal("excluded identity received warning")
		}
	})
	t.Run("invalid expressions are rejected", func(t *testing.T) {
		bad := window.DeepCopy()
		bad.ObjectMeta = metav1.ObjectMeta{Name: "bad-condition"}
		bad.Status = api.ChangeWindowStatus{}
		bad.Spec.Config.Admission.Warnings[0].MatchConditions[0].Expression = "unknown == true"
		requireMatchingDenial(t, admin.Create(ctx, bad), "matchCondition")
		if err := admin.Get(ctx, client.ObjectKeyFromObject(bad), bad); !apierrors.IsNotFound(err) {
			t.Fatalf("invalid policy persisted: %v", err)
		}
	})
	t.Run("policy update invalidates audiences and conditions", func(t *testing.T) {
		poll(t, func(ctx context.Context) (bool, error) {
			fresh := &api.ChangeWindow{}
			if err := admin.Get(ctx, client.ObjectKeyFromObject(window), fresh); err != nil {
				return false, err
			}
			fresh.Spec.Admission[0].Audience = &api.Audience{Include: []api.Subject{{Kind: "User", Name: "carol"}}}
			fresh.Spec.Admission[0].MatchConditions[0].Expression = "true"
			err := admin.Update(ctx, fresh)
			if apierrors.IsConflict(err) {
				return false, nil
			}
			return err == nil, err
		})
		awaitWindow(t, admin, window)
		poll(t, func(ctx context.Context) (bool, error) {
			err := carol.Create(ctx, config("new-probe", "audience-a", "open"), client.DryRunAll)
			if err == nil {
				return false, nil
			}
			if strings.Contains(err.Error(), "Audience maintenance denial") {
				return true, nil
			}
			return false, err
		})
		if err := alice.Create(ctx, config("after-update", "audience-a", "locked")); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("namespace updates change applicability", func(t *testing.T) {
		poll(t, func(ctx context.Context) (bool, error) {
			ns := &corev1.Namespace{}
			if err := admin.Get(ctx, client.ObjectKey{Name: "audience-b"}, ns); err != nil {
				return false, err
			}
			ns.Labels["audience-test"] = "audience-a"
			err := admin.Update(ctx, ns)
			if apierrors.IsConflict(err) {
				return false, nil
			}
			return err == nil, err
		})
		poll(t, func(ctx context.Context) (bool, error) {
			err := carol.Create(ctx, config("label-probe", "audience-b", "open"), client.DryRunAll)
			if err == nil {
				return false, nil
			}
			if strings.Contains(err.Error(), "Audience maintenance denial") {
				return true, nil
			}
			return false, err
		})
		requireMatchingDenial(t, carol.Create(ctx, config("label-denied", "audience-b", "open")), "Audience maintenance denial")
		if err := admin.Get(ctx, client.ObjectKey{Namespace: "audience-b", Name: "label-denied"}, &corev1.ConfigMap{}); !apierrors.IsNotFound(err) {
			t.Fatalf("denied object persisted: %v", err)
		}
	})
	t.Run("deletion invalidates compiled policy", func(t *testing.T) {
		if err := admin.Delete(ctx, window); err != nil {
			t.Fatal(err)
		}
		poll(t, func(ctx context.Context) (bool, error) {
			err := carol.Create(ctx, config("deleted-probe", "audience-a", "locked"), client.DryRunAll)
			if err != nil && strings.Contains(err.Error(), "Audience maintenance denial") {
				return false, nil
			}
			return err == nil, err
		})
		obj := config("after-delete", "audience-a", "locked")
		if err := carol.Create(ctx, obj); err != nil {
			t.Fatal(err)
		}
		if err := admin.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil || obj.Data["mode"] != "locked" {
			t.Fatalf("allowed state after policy deletion: %v", err)
		}
	})
}

func TestNodePoolExample(t *testing.T) {
	admin, actor := matchingClients(t)
	ctx := t.Context()
	createMatching(t, admin, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "platform"}})
	createMatching(t, admin, &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "deployer", Namespace: "platform"}})
	for _, name := range []string{"window-workers", "window-other"} {
		pool := "workers"
		if name == "window-other" {
			pool = "other"
		}
		createMatching(t, admin, &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"nodepool.example.com/name": pool}}})
	}
	createMatching(t, admin, &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "node-window-writer"}, Rules: []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"nodes"}, ResourceNames: []string{"window-workers", "window-other"}, Verbs: []string{"get", "update", "patch", "delete"}}}})
	createMatching(t, admin, &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "node-window-writer"}, RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: "node-window-writer"}, Subjects: []rbacv1.Subject{{Kind: "User", Name: "node-operator", APIGroup: rbacv1.GroupName}, {Kind: "Group", Name: "platform-developers", APIGroup: rbacv1.GroupName}, {Kind: "User", Name: "unrelated-node-user", APIGroup: rbacv1.GroupName}, {Kind: "ServiceAccount", Namespace: "platform", Name: "deployer"}}})
	data, err := os.ReadFile("../config/samples/node-pool-maintenance.yaml")
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
	selectedUser, _ := actor("node-operator")
	poll(t, func(ctx context.Context) (bool, error) {
		node := &corev1.Node{}
		if err := selectedUser.Get(ctx, client.ObjectKey{Name: "window-workers"}, node); err != nil {
			return false, err
		}
		node.Spec.Unschedulable = true
		err := selectedUser.Update(ctx, node, client.DryRunAll)
		if err == nil || apierrors.IsConflict(err) {
			return false, nil
		}
		if strings.Contains(err.Error(), "Worker pool maintenance") {
			return true, nil
		}
		return false, err
	})
	for _, tc := range []struct {
		name   string
		groups []string
		denied bool
	}{{"node-operator", nil, true}, {"group-operator", []string{"platform-developers"}, true}, {"system:serviceaccount:platform:deployer", nil, true}, {"break-glass", []string{"platform-developers"}, false}, {"unrelated-node-user", nil, false}} {
		t.Run(tc.name, func(t *testing.T) {
			user, _ := actor(tc.name, tc.groups...)
			var want bool
			poll(t, func(ctx context.Context) (bool, error) {
				node := &corev1.Node{}
				if err := user.Get(ctx, client.ObjectKey{Name: "window-workers"}, node); err != nil {
					return false, err
				}
				want = node.Spec.Unschedulable
				node.Spec.Unschedulable = !node.Spec.Unschedulable
				if !tc.denied {
					want = node.Spec.Unschedulable
				}
				err := user.Update(ctx, node)
				if apierrors.IsConflict(err) {
					return false, nil
				}
				if tc.denied {
					requireMatchingDenial(t, err, "Worker pool maintenance")
					return true, nil
				}
				return err == nil, err
			})
			node := &corev1.Node{}
			if err := admin.Get(ctx, client.ObjectKey{Name: "window-workers"}, node); err != nil || node.Spec.Unschedulable != want {
				t.Fatalf("node state after admission: got=%t want=%t error=%v", node.Spec.Unschedulable, want, err)
			}
		})
	}
	t.Run("unselected pool", func(t *testing.T) {
		poll(t, func(ctx context.Context) (bool, error) {
			node := &corev1.Node{}
			if err := selectedUser.Get(ctx, client.ObjectKey{Name: "window-other"}, node); err != nil {
				return false, err
			}
			node.Spec.Unschedulable = true
			err := selectedUser.Update(ctx, node)
			if apierrors.IsConflict(err) {
				return false, nil
			}
			return err == nil, err
		})
		node := &corev1.Node{}
		if err := admin.Get(ctx, client.ObjectKey{Name: "window-other"}, node); err != nil || !node.Spec.Unschedulable {
			t.Fatalf("allowed node write not persisted: %v", err)
		}
	})
	t.Run("cannot evade by removing pool label", func(t *testing.T) {
		poll(t, func(ctx context.Context) (bool, error) {
			node := &corev1.Node{}
			if err := selectedUser.Get(ctx, client.ObjectKey{Name: "window-workers"}, node); err != nil {
				return false, err
			}
			delete(node.Labels, "nodepool.example.com/name")
			err := selectedUser.Update(ctx, node)
			if apierrors.IsConflict(err) {
				return false, nil
			}
			requireMatchingDenial(t, err, "Worker pool maintenance")
			return true, nil
		})
		node := &corev1.Node{}
		if err := admin.Get(ctx, client.ObjectKey{Name: "window-workers"}, node); err != nil || node.Labels["nodepool.example.com/name"] != "workers" {
			t.Fatalf("rejected label removal persisted: %v", err)
		}
	})
	t.Run("selected node deletion", func(t *testing.T) {
		node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "window-workers"}}
		requireMatchingDenial(t, selectedUser.Delete(ctx, node), "Worker pool maintenance")
		if err := admin.Get(ctx, client.ObjectKeyFromObject(node), node); err != nil {
			t.Fatal(err)
		}
	})
}
