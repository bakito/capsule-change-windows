// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	api "github.com/projectcapsule/schedule-action-controller/api/v1alpha1"
	"github.com/projectcapsule/schedule-action-controller/internal/executor"
	"github.com/projectcapsule/schedule-action-controller/internal/policy"
	"github.com/projectcapsule/schedule-action-controller/internal/testutil"
)

type fakeExecutor struct {
	client              client.Client
	lists, starts, ends int
	failEnd             bool
	captureBeforeApply  bool
}

func (f *fakeExecutor) List(_ context.Context, _ policy.Target, ns string) ([]unstructured.Unstructured, error) {
	f.lists++
	return []unstructured.Unstructured{{Object: map[string]any{"apiVersion": "apps/v1", "kind": "Deployment", "metadata": map[string]any{"name": "app", "namespace": ns, "uid": "app-uid"}, "spec": map[string]any{"replicas": int64(3)}}}}, nil
}

func (f *fakeExecutor) Get(context.Context, executor.Participant, string) (*unstructured.Unstructured, error) {
	return nil, errors.New("unexpected read")
}

func (f *fakeExecutor) Prepare(ctx context.Context, p executor.Participant, s *api.ActionStep, data map[string]any) (*executor.Prepared, error) {
	return (&executor.Client{}).Prepare(ctx, p, s, data)
}

func (f *fakeExecutor) Apply(ctx context.Context, _ executor.Participant, p *executor.Prepared, phase string) (bool, error) {
	if p == nil {
		return false, nil
	}
	secret := &corev1.Secret{}
	if err := f.client.Get(ctx, types.NamespacedName{Namespace: "controller", Name: "change-window-test-uid"}, secret); err != nil {
		return false, err
	}
	var journal Journal
	if err := json.Unmarshal(secret.Data["journal.json"], &journal); err != nil {
		return false, err
	}
	f.captureBeforeApply = string(journal.Entries[0].Captured["replicas"]) == "3" && journal.Entries[0].Attempted
	if phase == "start" {
		f.starts++
	} else {
		f.ends++
		if f.failEnd {
			return false, errors.New("restore conflict")
		}
		v, _, _ := unstructured.NestedFieldNoCopy(p.Object, "spec", "replicas")
		if v != int64(3) {
			return false, errors.New("original capture lost")
		}
	}
	return false, nil
}

func TestReconcile(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := api.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	a := testutil.Action()
	a.Spec.ServiceAccount = &api.ServiceAccountReference{Namespace: "dev", Name: "executor"}
	a.Spec.Actions = []api.ResourceAction{{Name: "scale", Targets: []string{"deployments"}, Capture: map[string]api.Capture{"replicas": {Path: "/spec/replicas"}}, OnStart: &api.ActionStep{Apply: api.Apply{Template: "spec:\n  replicas: 0"}}, OnEnd: &api.ActionStep{Apply: api.Apply{Template: "spec:\n  replicas: {{ .captured.replicas }}"}}}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(a).WithObjects(a, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "dev", Labels: map[string]string{"environment": "development"}}}, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "other"}}, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "kube-system", Labels: map[string]string{"environment": "development"}}}).Build()
	f := &fakeExecutor{client: c}
	now, _ := time.Parse(time.RFC3339, "2026-10-01T10:30:00Z")
	r := &ChangeWindowReconciler{Client: c, Reader: c, Scheme: scheme, Mapper: testutil.Mapper(), SystemNamespace: "controller", Factory: func(api.ServiceAccountReference, types.UID) (executor.Interface, error) { return f, nil }, Now: func() time.Time { return now }}
	req := ctrl.Request{NamespacedName: client.ObjectKey{Name: a.Name}}
	if _, err := r.Reconcile(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	if f.starts != 1 || f.lists != 1 || !f.captureBeforeApply {
		t.Fatalf("not captured before mutation: %+v", f)
	}
	if _, err := r.Reconcile(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	if f.starts != 1 || f.lists != 1 {
		t.Fatal("start repeated or selection recaptured")
	}
	if err := c.Get(t.Context(), req.NamespacedName, a); err != nil {
		t.Fatal(err)
	}
	if !meta.IsStatusConditionTrue(a.Status.Conditions, api.ConditionReady) || a.Status.PendingResources != 1 {
		t.Fatalf("status %+v", a.Status)
	}
	// A fresh reconciler after restart must restore from the Secret even when labels change.
	ns := &corev1.Namespace{}
	if err := c.Get(t.Context(), client.ObjectKey{Name: "dev"}, ns); err != nil {
		t.Fatal(err)
	}
	ns.Labels = nil
	if err := c.Update(t.Context(), ns); err != nil {
		t.Fatal(err)
	}
	r = &ChangeWindowReconciler{Client: c, Reader: c, Scheme: scheme, Mapper: testutil.Mapper(), SystemNamespace: "controller", Factory: r.Factory, Now: func() time.Time { return now }}
	now = now.Add(time.Hour)
	f.failEnd = true
	if _, err := r.Reconcile(t.Context(), req); err == nil {
		t.Fatal("restore failure swallowed")
	}
	journal := &corev1.Secret{}
	if err := c.Get(t.Context(), client.ObjectKey{Namespace: "controller", Name: "change-window-test-uid"}, journal); err != nil {
		t.Fatal("capture not retained", err)
	}
	if err := c.Get(t.Context(), req.NamespacedName, a); err != nil {
		t.Fatal(err)
	}
	assertConditions(t, a, metav1.ConditionFalse, metav1.ConditionFalse, api.ReasonFinishing)
	f.failEnd = false
	if _, err := r.Reconcile(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(t.Context(), req.NamespacedName, a); err != nil {
		t.Fatal(err)
	}
	assertConditions(t, a, metav1.ConditionTrue, metav1.ConditionFalse, api.ReasonFinished)
	if f.ends != 2 || f.lists != 1 || f.starts != 1 {
		t.Fatalf("unexpected calls %+v", f)
	}
	// Missing resources must be a successful no-op.
	req.Name = "missing"
	if _, err := r.Reconcile(t.Context(), req); err != nil {
		t.Fatal(err)
	}
}

// A conflict on one participant must not starve unrelated workloads in the window.
func TestParticipantFailureDoesNotBlockOthers(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := api.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	a := testutil.Action()
	a.Spec.ServiceAccount = &api.ServiceAccountReference{Namespace: "dev", Name: "executor"}
	a.Finalizers = []string{Finalizer}
	now, _ := time.Parse(time.RFC3339, "2026-10-01T10:30:00Z")
	compiled, err := policy.Compile(a, testutil.Mapper())
	if err != nil {
		t.Fatal(err)
	}
	window := compiled.Engine.At(now)
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "change-window-test-uid", Namespace: "controller", OwnerReferences: []metav1.OwnerReference{{APIVersion: api.GroupVersion.String(), Kind: "ChangeWindow", Name: a.Name, UID: a.UID, Controller: ptr.To(true)}}}}
	j := Journal{Generation: a.Generation, Spec: a.Spec, Start: window.Start, End: window.End, Entries: []executor.Participant{{Name: "conflict", Attempted: true, Start: &executor.Prepared{}}, {Name: "healthy", Attempted: true, Start: &executor.Prepared{}}}}
	if err := encode(secret, &j); err != nil {
		t.Fatal(err)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(a, secret).Build()
	f := &isolatedExecutor{}
	r := &ChangeWindowReconciler{Client: c, Reader: c, Scheme: scheme, Mapper: testutil.Mapper(), SystemNamespace: "controller", Factory: func(api.ServiceAccountReference, types.UID) (executor.Interface, error) { return f, nil }}
	if _, err := r.reconcileActions(t.Context(), a, compiled, window, now); err == nil {
		t.Fatal("participant failure not reported")
	}
	if !f.healthy {
		t.Fatal("healthy participant was starved")
	}
	if err := c.Get(t.Context(), client.ObjectKeyFromObject(secret), secret); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(secret.Data["journal.json"], &j); err != nil {
		t.Fatal(err)
	}
	if j.Entries[0].Started || !j.Entries[1].Started {
		t.Fatalf("incorrect persisted progress %+v", j.Entries)
	}
}

type isolatedExecutor struct {
	fakeExecutor
	healthy bool
}

func (f *isolatedExecutor) Apply(_ context.Context, p executor.Participant, _ *executor.Prepared, _ string) (bool, error) {
	if p.Name == "conflict" {
		return false, errors.New("conflict")
	}
	f.healthy = true
	return false, nil
}
