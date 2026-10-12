// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package admission

import (
	"fmt"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	api "github.com/projectcapsule/schedule-action-controller/api/v1alpha1"
	"github.com/projectcapsule/schedule-action-controller/internal/testutil"
)

func BenchmarkAdmissionMatching(b *testing.B) {
	for _, count := range []int{1, 1000} {
		for _, size := range []int{1, 8} {
			for _, mode := range []string{"deny", "condition-skip", "audience-skip", "concurrent-deny"} {
				b.Run(fmt.Sprintf("policies=%d/conditions=%d/%s", count, size, mode), func(b *testing.B) {
					h, _ := handler(b)
					// Avoid a shared counting-client mutation in concurrent benchmarks.
					scheme := runtime.NewScheme()
					if err := corev1.AddToScheme(scheme); err != nil {
						b.Fatal(err)
					}
					h.Reader = fake.NewClientBuilder().WithScheme(scheme).WithObjects(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "dev", Labels: map[string]string{"environment": "development"}}}).Build()
					a := testutil.Action()
					a.Generation++
					a.Spec.Admission[0].Audience = &api.Audience{}
					for i := range 64 {
						a.Spec.Admission[0].Audience.Include = append(a.Spec.Admission[0].Audience.Include, api.Subject{Kind: "User", Name: fmt.Sprintf("user-%d", i)})
					}
					for i := range size {
						a.Spec.Admission[0].MatchConditions = append(a.Spec.Admission[0].MatchConditions, api.MatchCondition{Name: fmt.Sprintf("condition-%d", i), Expression: "request.operation == 'UPDATE' && object.spec.replicas > 1 && oldObject.spec.replicas == 2"})
					}
					if mode == "condition-skip" {
						a.Spec.Admission[0].MatchConditions[size-1].Expression = "false"
					}
					if err := h.Store.Upsert(a); err != nil {
						b.Fatal(err)
					}
					for i := 1; i < count; i++ {
						other := testutil.Action()
						other.Name = fmt.Sprint(i)
						other.Spec.Targets[0].APIVersion, other.Spec.Targets[0].Kind = "v1", "ConfigMap"
						if err := h.Store.Upsert(other); err != nil {
							b.Fatal(err)
						}
					}
					req := request(admissionv1.Update)
					req.UserInfo.Username = "user-63"
					if mode == "audience-skip" {
						req.UserInfo.Username = "other"
					}
					wantAllow := mode == "condition-skip" || mode == "audience-skip"
					run := func() {
						if h.Handle(b.Context(), req).Allowed != wantAllow {
							b.Fatal("wrong decision")
						}
					}
					run()
					b.ReportAllocs()
					b.ResetTimer()
					if mode == "concurrent-deny" {
						b.RunParallel(func(pb *testing.PB) {
							for pb.Next() {
								run()
							}
						})
					} else {
						for b.Loop() {
							run()
						}
					}
				})
			}
		}
	}
}

func BenchmarkNodeAdmission(b *testing.B) {
	h, _ := handler(b)
	a := testutil.Action()
	a.Generation++
	a.Spec.NamespaceSelectors = nil
	a.Spec.Targets[0].APIVersion, a.Spec.Targets[0].Kind = "v1", "Node"
	a.Spec.Admission[0].Subresources = nil
	a.Spec.Admission[0].Audience = &api.Audience{Include: []api.Subject{{Kind: "User", Name: "node-operator"}}}
	if err := h.Store.Upsert(a); err != nil {
		b.Fatal(err)
	}
	req := request(admissionv1.Update)
	req.Namespace = ""
	req.UserInfo.Username = "node-operator"
	req.Resource = metav1.GroupVersionResource{Version: "v1", Resource: "nodes"}
	req.Kind = metav1.GroupVersionKind{Version: "v1", Kind: "Node"}
	req.Object.Raw = []byte(`{"apiVersion":"v1","kind":"Node","metadata":{"name":"worker","labels":{"maintenance":"enabled"}}}`)
	req.OldObject.Raw = req.Object.Raw
	b.ReportAllocs()
	for b.Loop() {
		if h.Handle(b.Context(), req).Allowed {
			b.Fatal("Node allowed")
		}
	}
}
