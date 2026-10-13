// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"time"

	capmeta "github.com/projectcapsule/capsule/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	api "github.com/projectcapsule/schedule-action-controller/api/v1alpha1"
	"github.com/projectcapsule/schedule-action-controller/internal/schedule"
)

// updateStatus keeps reconciliation health separate from wall-clock occupancy.
// A completed observed cycle stays Finished until the next cycle starts; future
// boundaries still come from the existing schedule engine, without extra lookups.
func updateStatus(action *api.ChangeWindow, w schedule.Window, now time.Time, pending int32, compileErr, reconcileErr error) {
	previous := meta.FindStatusCondition(action.Status.Conditions, api.ConditionActive)
	observedWindow := action.Status.Active || previous != nil && (previous.Reason == api.ReasonActive || previous.Reason == api.ReasonFinishing || previous.Reason == api.ReasonFinished)
	deleting := !action.DeletionTimestamp.IsZero()
	action.Status.ObservedGeneration = action.Generation
	action.Status.Active = w.Active && !deleting
	action.Status.WindowStart = asTime(w.Start)
	action.Status.WindowEnd = asTime(w.End)
	boundary := w.Start
	if w.Active {
		boundary = w.End
	}
	if deleting {
		boundary = time.Time{}
	}
	action.Status.NextTransition = asTime(boundary)
	action.Status.PendingResources = pending

	ready := metav1.Condition(capmeta.NewReadyCondition(action))
	ready.ObservedGeneration = action.Generation
	ready.LastTransitionTime = metav1.NewTime(now)
	ready.Message = "Schedules and action state are reconciled."
	if reconcileErr != nil {
		ready.Status = metav1.ConditionFalse
		ready.Reason = "ReconcileError"
		ready.Message = "Reconciliation failed; inspect controller logs and executor permissions."
	} else if deleting {
		ready.Status = metav1.ConditionFalse
		ready.Reason = capmeta.TerminatingReason
		ready.Message = "The ChangeWindow is being deleted."
	}
	meta.SetStatusCondition(&action.Status.Conditions, ready)

	active := metav1.Condition{
		Type: api.ConditionActive, Status: metav1.ConditionFalse,
		Reason: api.ReasonPlanned, Message: "Waiting for the first observed window to start.",
		ObservedGeneration: action.Generation, LastTransitionTime: metav1.NewTime(now),
	}
	switch {
	case deleting:
		active.Reason, active.Message = api.ReasonFinished, "Window actions have finished during deletion."
		if reconcileErr != nil || pending > 0 {
			active.Reason, active.Message = api.ReasonFinishing, "Restoring resources before deletion can complete."
		}
	case compileErr != nil:
		active.Status = metav1.ConditionUnknown
		active.Reason, active.Message = api.ReasonUnknown, "Window state is unavailable because the policy could not be compiled."
	case w.Active:
		active.Status = metav1.ConditionTrue
		active.Reason, active.Message = api.ReasonActive, "A scheduled window is active."
	case pending > 0 || observedWindow && reconcileErr != nil:
		active.Reason, active.Message = api.ReasonFinishing, "The window has ended; resource restoration is not complete."
	case observedWindow:
		active.Reason, active.Message = api.ReasonFinished, "The last observed window has finished; the next start is reported separately."
	case w.Start.IsZero():
		active.Reason, active.Message = api.ReasonUnscheduled, "No upcoming schedule occurrence is available."
	}
	meta.SetStatusCondition(&action.Status.Conditions, active)
}
