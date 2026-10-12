// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

// Labels are finite enums, never policy/resource/namespace names or error text.
var (
	Actions           = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "change_window_executions_total", Help: "SSA attempts by phase (start/end) and result (success/error/gone)."}, []string{"phase", "result"})
	ActionDuration    = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "change_window_execution_duration_seconds", Help: "Duration of SSA attempts, including API latency.", Buckets: prometheus.DefBuckets}, []string{"phase"})
	Admission         = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "change_window_admission_requests_total", Help: "Admission responses by decision (allow/deny/error)."}, []string{"decision"})
	Warnings          = prometheus.NewCounter(prometheus.CounterOpts{Name: "change_window_admission_warnings_total", Help: "Deduplicated warning messages emitted."})
	AdmissionDuration = prometheus.NewHistogram(prometheus.HistogramOpts{Name: "change_window_admission_duration_seconds", Help: "Admission handler duration.", Buckets: []float64{.0001, .0005, .001, .005, .01, .05, .1, .5, 1}})
	Windows           = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "change_window_transitions_total", Help: "Durably recorded window transitions."}, []string{"phase"})
)

func init() {
	metrics.Registry.MustRegister(Actions, ActionDuration, Admission, Warnings, AdmissionDuration, Windows)
}
