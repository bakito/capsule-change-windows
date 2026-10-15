//go:build e2e

// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	api "github.com/projectcapsule/schedule-action-controller/api/v1alpha1"
)

func TestWindowStatus(t *testing.T) {
	admin, _ := matchingClients(t)
	for _, ns := range []string{"status-selected", "status-other"} {
		createMatching(t, admin, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}})
	}
	start := time.Now().UTC().Add(time.Minute).Truncate(time.Minute)
	window := &api.ChangeWindow{ObjectMeta: metav1.ObjectMeta{Name: "status-lifecycle"}, Spec: api.ChangeWindowSpec{
		ServiceAccount:     &api.ServiceAccountReference{Namespace: "status-selected", Name: "default"},
		NamespaceSelectors: []metav1.LabelSelector{{MatchLabels: map[string]string{"kubernetes.io/metadata.name": "status-selected"}}},
		Schedules:          []api.Schedule{{Name: "daily", Cron: fmt.Sprintf("%d %d * * *", start.Minute(), start.Hour()), TimeZone: "UTC", Duration: metav1.Duration{Duration: 10 * time.Second}}},
		Targets:            []api.Target{{Name: "configs", APIVersion: "v1", Kind: "ConfigMap"}},
		Admission:          []api.AdmissionRule{{Name: "freeze", When: "Active", Action: "Deny", Message: "Status lifecycle maintenance", RequestMatch: api.RequestMatch{Targets: []string{"configs"}, Operations: []string{"CREATE"}}}},
	}}
	createMatching(t, admin, window)
	awaitLifecycle(t, admin, window, metav1.ConditionTrue, metav1.ConditionFalse, api.ReasonPlanned)
	assertWindowTable(t, window.Name, "True", api.ReasonPlanned, start)
	allowed := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "before-window", Namespace: "status-selected"}}
	createMatching(t, admin, allowed)
	awaitLifecycle(t, admin, window, metav1.ConditionTrue, metav1.ConditionTrue, api.ReasonActive)
	assertWindowTable(t, window.Name, "True", api.ReasonActive, start.Add(10*time.Second))
	denied := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "during-window", Namespace: "status-selected"}}
	if err := admin.Create(t.Context(), denied); err == nil || !strings.Contains(err.Error(), "Status lifecycle maintenance") {
		t.Fatalf("expected maintenance rejection, got %v", err)
	}
	if err := admin.Get(t.Context(), client.ObjectKeyFromObject(denied), &corev1.ConfigMap{}); !apierrors.IsNotFound(err) {
		t.Fatalf("denied object persisted: %v", err)
	}
	createMatching(t, admin, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "outside-selection", Namespace: "status-other"}})
	awaitLifecycle(t, admin, window, metav1.ConditionTrue, metav1.ConditionFalse, api.ReasonFinished)
	assertWindowTable(t, window.Name, "True", api.ReasonFinished, start.Add(24*time.Hour))
	createMatching(t, admin, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "after-window", Namespace: "status-selected"}})

	// Executor authorization failure must not hide the wall-clock active window.
	failed := window.DeepCopy()
	failed.ObjectMeta = metav1.ObjectMeta{Name: "status-executor-error"}
	failed.Status = api.ChangeWindowStatus{}
	failed.Spec.Schedules[0].Cron = "* * * * *"
	failed.Spec.Schedules[0].Duration.Duration = 2 * time.Minute
	failed.Spec.Admission = nil
	failed.Spec.ServiceAccount = &api.ServiceAccountReference{Name: "status-no-access", Namespace: systemNamespace}
	failed.Spec.Actions = []api.ResourceAction{{Name: "patch", Targets: []string{"configs"}, OnStart: &api.ActionStep{Apply: api.Apply{Template: "data:\n  touched: forbidden"}}}}
	createMatching(t, admin, failed)
	awaitLifecycle(t, admin, failed, metav1.ConditionFalse, metav1.ConditionTrue, api.ReasonActive)
	if err := admin.Get(t.Context(), client.ObjectKeyFromObject(allowed), allowed); err != nil {
		t.Fatal(err)
	}
	if len(allowed.Data) != 0 {
		t.Fatalf("unauthorized executor changed resource: %+v", allowed.Data)
	}
}

func awaitLifecycle(t *testing.T, c client.Client, window *api.ChangeWindow, ready, active metav1.ConditionStatus, reason string) {
	t.Helper()
	poll(t, func(ctx context.Context) (bool, error) {
		fresh := &api.ChangeWindow{}
		if err := c.Get(ctx, client.ObjectKeyFromObject(window), fresh); err != nil {
			return false, err
		}
		r := meta.FindStatusCondition(fresh.Status.Conditions, api.ConditionReady)
		a := meta.FindStatusCondition(fresh.Status.Conditions, api.ConditionActive)
		return fresh.Status.ObservedGeneration == fresh.Generation && r != nil && r.ObservedGeneration == fresh.Generation && r.Status == ready && a != nil && a.ObservedGeneration == fresh.Generation && a.Status == active && a.Reason == reason, nil
	})
}

func assertWindowTable(t *testing.T, name, ready, state string, next time.Time) {
	t.Helper()
	disc, err := discovery.NewDiscoveryClientForConfig(ctrl.GetConfigOrDie())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := disc.RESTClient().Get().AbsPath("/apis/"+api.GroupVersion.String()+"/changewindows/"+name).
		SetHeader("Accept", "application/json;as=Table;g=meta.k8s.io;v=v1").DoRaw(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	table := &metav1.Table{}
	if err := json.Unmarshal(raw, table); err != nil {
		t.Fatal(err)
	}
	if len(table.Rows) != 1 {
		t.Fatalf("expected one table row: %s", raw)
	}
	cells := map[string]any{}
	for i, column := range table.ColumnDefinitions {
		cells[column.Name] = table.Rows[0].Cells[i]
	}
	for column, want := range map[string]string{"Name": name, "Ready": ready, "State": state, "Next Transition": next.Format(time.RFC3339)} {
		if cells[column] != want {
			t.Fatalf("kubectl get column %s=%v, want %s; table=%s", column, cells[column], want, raw)
		}
	}
}
