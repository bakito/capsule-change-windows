// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"strings"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

func TestMetrics(t *testing.T) {
	Actions.WithLabelValues("start", "success").Inc()
	ActionDuration.WithLabelValues("start").Observe(.01)
	Admission.WithLabelValues("deny").Inc()
	Warnings.Inc()
	Windows.WithLabelValues("start").Inc()
	AdmissionDuration.Observe(.001)
	families, err := metrics.Registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, family := range families {
		if !strings.HasPrefix(family.GetName(), "change_window_") {
			continue
		}
		seen++
		for _, m := range family.Metric {
			for _, label := range m.Label {
				if label.GetName() != "phase" && label.GetName() != "result" && label.GetName() != "decision" {
					t.Fatalf("unbounded label %s", label.GetName())
				}
			}
		}
	}
	if seen != 6 {
		t.Fatalf("registered metric families %d want 6", seen)
	}
}
