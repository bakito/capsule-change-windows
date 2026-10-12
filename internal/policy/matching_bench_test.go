// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"fmt"
	"testing"

	api "github.com/projectcapsule/schedule-action-controller/api/v1alpha1"
	"github.com/projectcapsule/schedule-action-controller/internal/testutil"
)

func BenchmarkCompiledMatching(b *testing.B) {
	for _, size := range []int{1, 8, 64} {
		for _, warm := range []bool{false, true} {
			b.Run(fmt.Sprintf("conditions=%d/warm=%t", size, warm), func(b *testing.B) {
				obj := testutil.Action()
				obj.Spec.Admission[0].Audience = &api.Audience{Include: []api.Subject{{Kind: "Group", Name: "developers"}}}
				for i := range size {
					obj.Spec.Admission[0].MatchConditions = append(obj.Spec.Admission[0].MatchConditions, api.MatchCondition{Name: fmt.Sprintf("condition-%d", i), Expression: "object.spec.replicas != oldObject.spec.replicas && request.operation == 'UPDATE'"})
				}
				store := NewStore(testutil.Mapper())
				if err := store.Upsert(obj); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				for b.Loop() {
					if !warm {
						obj.Generation++
					}
					if err := store.Upsert(obj); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
