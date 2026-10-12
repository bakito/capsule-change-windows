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

func BenchmarkWildcardAdmission(b *testing.B) {
	for _, count := range []int{1, 100} {
		for _, mode := range []string{"deny", "pod-skip", "audience-skip", "namespace-skip", "subresource", "concurrent"} {
			b.Run(fmt.Sprintf("patterns=%d/%s", count, mode), func(b *testing.B) {
				h, _ := handler(b)
				scheme := runtime.NewScheme()
				if err := corev1.AddToScheme(scheme); err != nil {
					b.Fatal(err)
				}
				h.Reader = fake.NewClientBuilder().WithScheme(scheme).WithObjects(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "dev", Labels: map[string]string{"environment": "development"}}}, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "other"}}).Build()
				for i := range count {
					a := testutil.Action()
					if i > 0 {
						a.Name = fmt.Sprintf("window-%03d", i)
					}
					a.Generation++
					a.Spec.Targets[0].APIVersion, a.Spec.Targets[0].Kind, a.Spec.Targets[0].Selector = "*", "*", metav1.LabelSelector{}
					a.Spec.Admission[0].Subresources = []string{"", "*"}
					a.Spec.Admission[0].Audience = &api.Audience{Include: []api.Subject{{Kind: "Group", Name: "developers"}}}
					a.Spec.Admission[0].MatchConditions = []api.MatchCondition{{Name: "except-pods", Expression: "!(request.resource.group == '' && request.resource.resource == 'pods')"}}
					if err := h.Store.Upsert(a); err != nil {
						b.Fatal(err)
					}
				}
				req := request(admissionv1.Update)
				req.UserInfo.Groups = []string{"developers"}
				switch mode {
				case "pod-skip":
					req.Resource = metav1.GroupVersionResource{Version: "v1", Resource: "pods"}
					req.Kind = metav1.GroupVersionKind{Version: "v1", Kind: "Pod"}
				case "audience-skip":
					req.UserInfo.Groups = nil
				case "namespace-skip":
					req.Namespace = "other"
				case "subresource":
					req.SubResource = "scale"
				}
				want := mode == "pod-skip" || mode == "audience-skip" || mode == "namespace-skip"
				run := func() {
					if h.Handle(b.Context(), req).Allowed != want {
						b.Error("wrong decision")
					}
				}
				run()
				b.ReportAllocs()
				b.ResetTimer()
				if mode == "concurrent" {
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
