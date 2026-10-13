// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	api "github.com/projectcapsule/schedule-action-controller/api/v1alpha1"
	"github.com/projectcapsule/schedule-action-controller/internal/policy"
	"github.com/projectcapsule/schedule-action-controller/internal/testutil"
)

func assertConditions(t *testing.T, action *api.ChangeWindow, ready, active metav1.ConditionStatus, reason string) {
	t.Helper()
	for kind, want := range map[string]metav1.ConditionStatus{api.ConditionReady: ready, api.ConditionActive: active} {
		condition := meta.FindStatusCondition(action.Status.Conditions, kind)
		if condition == nil || condition.Status != want || condition.ObservedGeneration != action.Generation || condition.LastTransitionTime.IsZero() || condition.Message == "" {
			t.Fatalf("%s condition: %+v; want status=%s generation=%d", kind, condition, want, action.Generation)
		}
	}
	if got := meta.FindStatusCondition(action.Status.Conditions, api.ConditionActive).Reason; got != reason {
		t.Fatalf("lifecycle=%s, want %s", got, reason)
	}
	if action.Status.ObservedGeneration != action.Generation || action.Status.Active != (active == metav1.ConditionTrue) {
		t.Fatalf("inconsistent status: %+v", action.Status)
	}
}

func TestReconcileLifecycle(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := api.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	a := testutil.Action()
	a.Status.Conditions = []metav1.Condition{{Type: "External", Status: metav1.ConditionTrue, Reason: "ManagedElsewhere", LastTransitionTime: metav1.Now()}}
	writes := 0
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(a).WithObjects(a).WithInterceptorFuncs(interceptor.Funcs{
		SubResourcePatch: func(ctx context.Context, c client.Client, sub string, obj client.Object, p client.Patch, opts ...client.SubResourcePatchOption) error {
			writes++
			return c.SubResource(sub).Patch(ctx, obj, p, opts...)
		},
	}).Build()
	now := time.Date(2026, 10, 1, 9, 59, 59, 0, time.UTC)
	mapper := testutil.Mapper()
	store := policy.NewStore(mapper)
	if err := store.Upsert(a); err != nil {
		t.Fatal(err)
	}
	r := &ChangeWindowReconciler{Client: c, Reader: c, Scheme: scheme, Mapper: mapper, Policies: store, Now: func() time.Time { return now }}
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(a)}
	for _, tc := range []struct {
		at       time.Time
		reason   string
		active   metav1.ConditionStatus
		boundary time.Time
	}{
		{now, api.ReasonPlanned, metav1.ConditionFalse, now.Add(time.Second)},
		{now.Add(time.Second), api.ReasonActive, metav1.ConditionTrue, now.Add(time.Hour + time.Second)},
		{now.Add(time.Hour + time.Second), api.ReasonFinished, metav1.ConditionFalse, now.Add(24*time.Hour + time.Second)},
		{now.Add(12 * time.Hour), api.ReasonFinished, metav1.ConditionFalse, now.Add(24*time.Hour + time.Second)},
		{now.Add(24*time.Hour + time.Second), api.ReasonActive, metav1.ConditionTrue, now.Add(25*time.Hour + time.Second)},
	} {
		now = tc.at
		if _, err := r.Reconcile(t.Context(), req); err != nil {
			t.Fatal(err)
		}
		if err := c.Get(t.Context(), req.NamespacedName, a); err != nil {
			t.Fatal(err)
		}
		assertConditions(t, a, metav1.ConditionTrue, tc.active, tc.reason)
		if a.Status.NextTransition == nil || !a.Status.NextTransition.Time.Equal(tc.boundary) {
			t.Fatalf("next transition=%v want %v", a.Status.NextTransition, tc.boundary)
		}
		if !meta.IsStatusConditionTrue(a.Status.Conditions, "External") {
			t.Fatal("unrelated condition lost")
		}
		before := a.Status.DeepCopy()
		count := writes
		// Restarting the reconciler preserves Finished and must not rewrite stable conditions.
		r = &ChangeWindowReconciler{Client: c, Reader: c, Scheme: scheme, Mapper: mapper, Policies: store, Now: func() time.Time { return now }}
		if _, err := r.Reconcile(t.Context(), req); err != nil {
			t.Fatal(err)
		}
		if err := c.Get(t.Context(), req.NamespacedName, a); err != nil {
			t.Fatal(err)
		}
		if writes != count || !reflect.DeepEqual(*before, a.Status) {
			t.Fatalf("unchanged reconcile wrote status: %d -> %d", count, writes)
		}
	}
}

func TestReconcileInvalidPolicyAndRecovery(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := api.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	a := testutil.Action()
	a.Spec.Schedules[0].Cron = "invalid"
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(a).WithStatusSubresource(a).Build()
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	r := &ChangeWindowReconciler{Client: c, Reader: c, Scheme: scheme, Mapper: testutil.Mapper(), Now: func() time.Time { return now }}
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(a)}
	if _, err := r.Reconcile(t.Context(), req); err == nil {
		t.Fatal("invalid policy accepted")
	}
	if err := c.Get(t.Context(), req.NamespacedName, a); err != nil {
		t.Fatal(err)
	}
	assertConditions(t, a, metav1.ConditionFalse, metav1.ConditionUnknown, api.ReasonUnknown)
	a.Spec.Schedules[0].Cron = "0 10 * * *"
	a.Generation++
	if err := c.Update(t.Context(), a); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(t.Context(), req.NamespacedName, a); err != nil {
		t.Fatal(err)
	}
	assertConditions(t, a, metav1.ConditionTrue, metav1.ConditionFalse, api.ReasonPlanned)
}

func TestReconcileStatusFailures(t *testing.T) {
	for _, stage := range []string{"read", "status"} {
		t.Run(stage, func(t *testing.T) {
			scheme := runtime.NewScheme()
			if err := api.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			a := testutil.Action()
			failure := errors.New("injected failure")
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(a).WithStatusSubresource(a).WithInterceptorFuncs(interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if stage == "read" {
						return failure
					}
					return c.Get(ctx, key, obj, opts...)
				},
				SubResourcePatch: func(context.Context, client.Client, string, client.Object, client.Patch, ...client.SubResourcePatchOption) error {
					return failure
				},
			}).Build()
			r := &ChangeWindowReconciler{Client: c, Reader: c, Scheme: scheme, Mapper: testutil.Mapper()}
			if _, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(a)}); !errors.Is(err, failure) {
				t.Fatalf("lost %s failure: %v", stage, err)
			}
		})
	}
}

func TestReconcileUnscheduledAndContinuous(t *testing.T) {
	for _, tc := range []struct {
		cron   string
		active metav1.ConditionStatus
		reason string
	}{
		{"0 0 30 2 *", metav1.ConditionFalse, api.ReasonUnscheduled},
		{"* * * * *", metav1.ConditionTrue, api.ReasonActive},
	} {
		t.Run(tc.reason, func(t *testing.T) {
			scheme := runtime.NewScheme()
			if err := api.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			a := testutil.Action()
			a.Spec.Schedules[0].Cron = tc.cron
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(a).WithStatusSubresource(a).Build()
			r := &ChangeWindowReconciler{Client: c, Reader: c, Scheme: scheme, Mapper: testutil.Mapper(), Now: func() time.Time { return time.Date(2026, 10, 1, 10, 30, 0, 0, time.UTC) }}
			if _, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(a)}); err != nil {
				t.Fatal(err)
			}
			if err := c.Get(t.Context(), client.ObjectKeyFromObject(a), a); err != nil {
				t.Fatal(err)
			}
			assertConditions(t, a, metav1.ConditionTrue, tc.active, tc.reason)
			if a.Status.NextTransition != nil {
				t.Fatalf("invented next transition: %v", a.Status.NextTransition)
			}
		})
	}
}

func TestDeletionReportsRestorationFailure(t *testing.T) {
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{api.AddToScheme, corev1.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	a := testutil.Action()
	a.Finalizers = []string{Finalizer}
	deleted := metav1.Now()
	a.DeletionTimestamp = &deleted
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(a).WithStatusSubresource(a).WithInterceptorFuncs(interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if _, ok := obj.(*corev1.Secret); ok {
				return errors.New("journal unavailable")
			}
			return c.Get(ctx, key, obj, opts...)
		},
	}).Build()
	r := &ChangeWindowReconciler{Client: c, Reader: c, Scheme: scheme, Mapper: testutil.Mapper(), SystemNamespace: "controller"}
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(a)}
	if _, err := r.Reconcile(t.Context(), req); err == nil {
		t.Fatal("restoration error hidden")
	}
	if err := c.Get(t.Context(), req.NamespacedName, a); err != nil {
		t.Fatal(err)
	}
	assertConditions(t, a, metav1.ConditionFalse, metav1.ConditionFalse, api.ReasonFinishing)
	if len(a.Finalizers) != 1 || a.Status.NextTransition != nil {
		t.Fatalf("incorrect deleting state: %+v", a)
	}
}
