// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kjson "k8s.io/apimachinery/pkg/util/json"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	api "github.com/projectcapsule/schedule-action-controller/api/v1alpha1"
	"github.com/projectcapsule/schedule-action-controller/internal/executor"
	"github.com/projectcapsule/schedule-action-controller/internal/metrics"
	"github.com/projectcapsule/schedule-action-controller/internal/policy"
	"github.com/projectcapsule/schedule-action-controller/internal/schedule"
)

const (
	Finalizer       = "addons.projectcapsule.dev/restore"
	maxJournalBytes = 512 * 1024
)

// Journal freezes the action plan before mutations. It lives in a controller-only
// Secret, not public status; optimistic locking prevents losing saved progress.
type Journal struct {
	Generation int64                  `json:"generation"`
	Spec       api.ChangeWindowSpec   `json:"spec"`
	Start      time.Time              `json:"start"`
	End        time.Time              `json:"end"`
	Entries    []executor.Participant `json:"entries"`
	Complete   bool                   `json:"complete,omitempty"`
}

type ChangeWindowReconciler struct {
	client.Client
	Reader          client.Reader
	Scheme          *runtime.Scheme
	Mapper          meta.RESTMapper
	Policies        *policy.Store
	Factory         executor.Factory
	SystemNamespace string
	Now             func() time.Time
}

// +kubebuilder:rbac:groups=addons.projectcapsule.dev,resources=changewindows,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=addons.projectcapsule.dev,resources=changewindows/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=addons.projectcapsule.dev,resources=changewindows/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments;statefulsets;replicasets,verbs=get
// +kubebuilder:rbac:groups=authentication.k8s.io,resources=userextras/addons.projectcapsule.dev/execution,verbs=impersonate

func (r *ChangeWindowReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	action := &api.ChangeWindow{}
	if err := r.Get(ctx, req.NamespacedName, action); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	now := time.Now()
	if r.Now != nil {
		now = r.Now()
	}
	before := action.DeepCopy()
	var c *policy.Compiled
	var compileErr error
	if r.Policies != nil {
		c, compileErr = r.Policies.For(action)
	} else {
		c, compileErr = policy.Compile(action, r.Mapper)
	}
	var w schedule.Window
	if compileErr == nil {
		w = c.Engine.At(now)
	}
	err := compileErr
	pending := int32(0)
	if err == nil || !action.DeletionTimestamp.IsZero() {
		pending, err = r.reconcileActions(ctx, action, c, w, now)
	}
	action.Status.ObservedGeneration = action.Generation
	action.Status.Active = w.Active && action.DeletionTimestamp.IsZero()
	action.Status.WindowStart = asTime(w.Start)
	action.Status.WindowEnd = asTime(w.End)
	boundary := w.Start
	if w.Active {
		boundary = w.End
	}
	action.Status.NextTransition = asTime(boundary)
	action.Status.PendingResources = pending
	condition := metav1.Condition{Type: api.ConditionReady, Status: metav1.ConditionTrue, Reason: "Ready", Message: "Schedules and action state are reconciled.", ObservedGeneration: action.Generation}
	if err != nil {
		condition.Status = metav1.ConditionFalse
		condition.Reason = "ReconcileError"
		condition.Message = "Reconciliation failed; inspect controller logs and executor permissions."
		ctrl.LoggerFrom(ctx).Error(err, "reconciling ChangeWindow", "name", action.Name)
	}
	meta.SetStatusCondition(&action.Status.Conditions, condition)
	if !reflect.DeepEqual(before.Status, action.Status) && action.DeletionTimestamp.IsZero() {
		before.ResourceVersion = action.ResourceVersion
		if patchErr := r.Status().Patch(ctx, action, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})); patchErr != nil {
			return ctrl.Result{}, patchErr
		}
		if before.Status.Active != action.Status.Active {
			phase := "end"
			if action.Status.Active {
				phase = "start"
			}
			metrics.Windows.WithLabelValues(phase).Inc()
		}
	}
	if err != nil {
		return ctrl.Result{}, err
	}
	delay := time.Minute
	if pending > 0 && !w.Active {
		delay = time.Second
	}
	if !boundary.IsZero() && boundary.After(now) && boundary.Sub(now) < delay {
		delay = boundary.Sub(now)
	}
	return ctrl.Result{RequeueAfter: delay}, nil
}

func (r *ChangeWindowReconciler) reconcileActions(ctx context.Context, action *api.ChangeWindow, c *policy.Compiled, w schedule.Window, now time.Time) (int32, error) {
	if len(action.Spec.Actions) == 0 && !controllerutil.ContainsFinalizer(action, Finalizer) {
		return 0, nil
	}
	secret := &corev1.Secret{}
	key := types.NamespacedName{Namespace: r.SystemNamespace, Name: "change-window-" + string(action.UID)}
	err := r.Reader.Get(ctx, key, secret)
	exists := err == nil
	if err != nil && !apierrors.IsNotFound(err) {
		return 0, err
	}
	var journal Journal
	if exists {
		owner := metav1.GetControllerOf(secret)
		if owner == nil || owner.UID != action.UID || owner.Kind != "ChangeWindow" {
			return 0, fmt.Errorf("journal ownership mismatch")
		}
		if err := kjson.Unmarshal(secret.Data["journal.json"], &journal); err != nil {
			return 0, fmt.Errorf("decoding journal: %w", err)
		}
	}
	deleting := !action.DeletionTimestamp.IsZero()
	if exists && !journal.Complete {
		var engine *schedule.Engine
		if c != nil && journal.Generation == action.Generation {
			engine = c.Engine
		} else {
			var err error
			engine, err = schedule.Compile(journal.Spec.Schedules)
			if err != nil {
				return 0, err
			}
		}
		oldWindow := engine.At(now)
		continuing := oldWindow.Active && (journal.End.IsZero() || !oldWindow.Start.After(journal.End))
		finish := deleting || journal.Generation != action.Generation || !continuing
		if !finish && !journal.End.Equal(oldWindow.End) {
			journal.End = oldWindow.End
			if err := r.save(ctx, secret, &journal); err != nil {
				return pending(journal), err
			}
		}
		exec, err := r.Factory(*journal.Spec.ServiceAccount, action.UID)
		if err != nil {
			return pending(journal), err
		}
		var failures []error
		for i := range journal.Entries {
			entry := &journal.Entries[i]
			if finish {
				if entry.Finished {
					continue
				}
				if entry.Attempted || entry.Start == nil {
					if _, err := exec.Apply(ctx, *entry, entry.End, "end"); err != nil {
						failures = append(failures, fmt.Errorf("end action %s on %s/%s: %w", entry.Action, entry.Namespace, entry.Name, err))
						continue
					}
				}
				entry.Finished = true
			} else {
				if entry.Started {
					continue
				}
				if !entry.Attempted {
					entry.Attempted = true
					if err := r.save(ctx, secret, &journal); err != nil {
						return pending(journal), err
					}
				}
				if _, err := exec.Apply(ctx, *entry, entry.Start, "start"); err != nil {
					failures = append(failures, fmt.Errorf("start action %s on %s/%s: %w", entry.Action, entry.Namespace, entry.Name, err))
					continue
				}
				entry.Started = true
			}
			if err := r.save(ctx, secret, &journal); err != nil {
				return pending(journal), err
			}
		}
		if len(failures) > 0 {
			return pending(journal), errors.Join(failures...)
		}
		if finish {
			journal.Complete = true
			if err := r.save(ctx, secret, &journal); err != nil {
				return pending(journal), err
			}
		}
		if !journal.Complete {
			return pending(journal), nil
		}
	}
	if deleting {
		if exists {
			if err := r.Delete(ctx, secret); err != nil && !apierrors.IsNotFound(err) {
				return 0, err
			}
		}
		if controllerutil.ContainsFinalizer(action, Finalizer) {
			base := action.DeepCopy()
			controllerutil.RemoveFinalizer(action, Finalizer)
			if err := r.Patch(ctx, action, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
				return 0, err
			}
		}
		return 0, nil
	}
	if c == nil {
		return 0, fmt.Errorf("invalid ChangeWindow")
	}
	// A completed journal is a tombstone preventing duplicate starts during the same window.
	if exists {
		if journal.Generation == action.Generation && w.Active && (journal.End.IsZero() || !w.Start.After(journal.End)) {
			return 0, nil
		}
		if err := r.Delete(ctx, secret); err != nil {
			return 0, err
		}
		return 0, nil
	}
	if !w.Active || len(action.Spec.Actions) == 0 {
		if controllerutil.ContainsFinalizer(action, Finalizer) {
			base := action.DeepCopy()
			controllerutil.RemoveFinalizer(action, Finalizer)
			if err := r.Patch(ctx, action, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
				return 0, err
			}
		}
		return 0, nil
	}
	if !controllerutil.ContainsFinalizer(action, Finalizer) {
		base := action.DeepCopy()
		controllerutil.AddFinalizer(action, Finalizer)
		if err := r.Patch(ctx, action, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
			return 0, err
		}
		// The persisted finalizer must exist before a journal or any mutation.
	}
	exec, err := r.Factory(*action.Spec.ServiceAccount, action.UID)
	if err != nil {
		return 0, err
	}
	journal = Journal{Generation: action.Generation, Spec: *action.Spec.DeepCopy(), Start: w.Start, End: w.End}
	namespaces := map[string]bool{}
	for _, selector := range c.Namespaces {
		list := &corev1.NamespaceList{}
		if err := r.List(ctx, list, client.MatchingLabelsSelector{Selector: selector}); err != nil {
			return 0, err
		}
		for _, namespace := range list.Items {
			if !policy.ProtectedNamespace(namespace.Name, r.SystemNamespace) && namespace.DeletionTimestamp.IsZero() {
				namespaces[namespace.Name] = true
			}
		}
	}
	for _, a := range action.Spec.Actions {
		seen := map[types.UID]bool{}
		for _, targetName := range a.Targets {
			target := c.Targets[targetName]
			for ns := range namespaces {
				objects, err := exec.List(ctx, target, ns)
				if err != nil {
					return 0, err
				}
				for _, obj := range objects {
					if seen[obj.GetUID()] || !obj.GetDeletionTimestamp().IsZero() {
						continue
					}
					seen[obj.GetUID()] = true
					if len(journal.Entries) >= executor.MaxParticipants {
						return 0, fmt.Errorf("window exceeds %d participants", executor.MaxParticipants)
					}
					entry, err := executor.PrepareParticipant(ctx, exec, target, obj, a, w, now)
					if err != nil {
						return 0, err
					}
					journal.Entries = append(journal.Entries, entry)
				}
			}
		}
	}
	secret = &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace, Labels: map[string]string{"addons.projectcapsule.dev/journal": "true"}}, Type: corev1.SecretTypeOpaque}
	if err := controllerutil.SetControllerReference(action, secret, r.Scheme); err != nil {
		return 0, err
	}
	if err := encode(secret, &journal); err != nil {
		return 0, err
	}
	if err := r.Create(ctx, secret); err != nil {
		return 0, err
	}
	// Mutation begins only after durable capture is acknowledged.
	return r.reconcileActions(ctx, action, c, w, now)
}

func pending(j Journal) int32 {
	var count int32
	for _, e := range j.Entries {
		if !e.Finished {
			count++
		}
	}
	return count
}

func encode(secret *corev1.Secret, j *Journal) error {
	raw, err := json.Marshal(j)
	if err != nil {
		return err
	}
	if len(raw) > maxJournalBytes {
		return fmt.Errorf("journal exceeds %d bytes", maxJournalBytes)
	}
	secret.Data = map[string][]byte{"journal.json": raw}
	return nil
}

func (r *ChangeWindowReconciler) save(ctx context.Context, secret *corev1.Secret, j *Journal) error {
	if err := encode(secret, j); err != nil {
		return err
	}
	return r.Update(ctx, secret)
}

func asTime(t time.Time) *metav1.Time {
	if t.IsZero() {
		return nil
	}
	m := metav1.NewTime(t)
	return &m
}

func (r *ChangeWindowReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).For(&api.ChangeWindow{}).Named("changewindow").Complete(r)
}
