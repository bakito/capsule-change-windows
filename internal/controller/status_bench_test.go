// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"fmt"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	api "github.com/projectcapsule/schedule-action-controller/api/v1alpha1"
	"github.com/projectcapsule/schedule-action-controller/internal/policy"
	"github.com/projectcapsule/schedule-action-controller/internal/testutil"
)

func BenchmarkReconcileStatus(b *testing.B) {
	for _, size := range []int{1, 100} {
		for _, hour := range []int{9, 10, 11} {
			b.Run(fmt.Sprintf("policies=%d/hour=%d", size, hour), func(b *testing.B) {
				scheme := runtime.NewScheme()
				if err := api.AddToScheme(scheme); err != nil {
					b.Fatal(err)
				}
				objects := make([]client.Object, size)
				for i := range objects {
					a := testutil.Action()
					a.Name = fmt.Sprintf("window-%d", i)
					objects[i] = a
				}
				c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&api.ChangeWindow{}).WithObjects(objects...).Build()
				mapper := testutil.Mapper()
				store := policy.NewStore(mapper)
				if err := store.Upsert(objects[0].(*api.ChangeWindow)); err != nil {
					b.Fatal(err)
				}
				now := time.Date(2026, 10, 1, 9, 30, 0, 0, time.UTC)
				if hour == 11 {
					// Seed an observed active cycle before benchmarking Finished.
					now = now.Add(time.Hour)
				}
				r := &ChangeWindowReconciler{Client: c, Reader: c, Scheme: scheme, Mapper: mapper, Policies: store, Now: func() time.Time { return now }}
				req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(objects[0])}
				if _, err := r.Reconcile(b.Context(), req); err != nil {
					b.Fatal(err)
				}
				now = time.Date(2026, 10, 1, hour, 30, 0, 0, time.UTC)
				if _, err := r.Reconcile(b.Context(), req); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				for b.Loop() {
					if _, err := r.Reconcile(b.Context(), req); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
