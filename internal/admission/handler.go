// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package admission

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kjson "k8s.io/apimachinery/pkg/util/json"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"
	cradmission "sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	api "github.com/projectcapsule/schedule-action-controller/api/v1alpha1"
	"github.com/projectcapsule/schedule-action-controller/internal/metrics"
	"github.com/projectcapsule/schedule-action-controller/internal/policy"
	"github.com/projectcapsule/schedule-action-controller/internal/schedule"
)

type Handler struct {
	Store           *policy.Store
	Reader          client.Reader
	Dynamic         dynamic.Interface
	SystemNamespace string
	Ready           func() bool
	Now             func() time.Time
}

func (h *Handler) Handle(ctx context.Context, req cradmission.Request) (response cradmission.Response) {
	start := time.Now()
	defer func() {
		decision := "allow"
		if !response.Allowed {
			decision = "deny"
			if response.Result != nil && response.Result.Code >= 500 {
				decision = "error"
			}
		}
		metrics.Admission.WithLabelValues(decision).Inc()
		metrics.AdmissionDuration.Observe(time.Since(start).Seconds())
		metrics.Warnings.Add(float64(len(response.Warnings)))
	}()
	if (req.Namespace == "" && (req.Resource.Group != "" || req.Resource.Version != "v1" || req.Resource.Resource != "nodes" || req.SubResource != "")) || (req.Namespace != "" && policy.ProtectedNamespace(req.Namespace, h.SystemNamespace)) || !slices.Contains([]admissionv1.Operation{admissionv1.Create, admissionv1.Update, admissionv1.Delete}, req.Operation) {
		return cradmission.Allowed("")
	}
	if h.Ready != nil && !h.Ready() {
		return cradmission.Errored(http.StatusServiceUnavailable, fmt.Errorf("policy cache is not synchronized"))
	}
	gvr := schema.GroupVersionResource{Group: req.Resource.Group, Version: req.Resource.Version, Resource: req.Resource.Resource}
	candidates, err := h.Store.Candidates(gvr)
	if err != nil {
		return cradmission.Errored(http.StatusServiceUnavailable, err)
	}
	if candidates.Empty() {
		return cradmission.Allowed("")
	}

	var namespace *corev1.Namespace
	var current, old, parent *unstructured.Unstructured
	var parentKind *schema.GroupVersionKind
	decoded := false
	decode := func(raw []byte) (*unstructured.Unstructured, error) {
		if len(raw) == 0 || string(raw) == "null" {
			return nil, nil
		}
		obj := &unstructured.Unstructured{}
		err := kjson.Unmarshal(raw, &obj.Object)
		return obj, err
	}
	// Decode actual admission objects once. Subresource selection separately uses
	// parent labels; CEL must still see Scale/status payloads and their old versions.
	decodeObjects := func() error {
		if decoded {
			return nil
		}
		var err error
		current, err = decode(req.Object.Raw)
		if err != nil {
			return err
		}
		old, err = decode(req.OldObject.Raw)
		if err != nil {
			return err
		}
		decoded = true
		return nil
	}
	var variables map[string]any
	budgets := [2]int64{policy.ConditionBudget, policy.ConditionBudget}
	now := time.Now()
	if h.Now != nil {
		now = h.Now()
	}
	deny := ""
	warnings := []string{}
	addWarning := func(message string) {
		message = truncate(message, 256)
		if len(warnings) < 8 && !slices.Contains(warnings, message) {
			warnings = append(warnings, message)
		}
	}
policyLoop:
	for c := range candidates.All() {
		if executorRequest(c, req) {
			continue
		}
		w := c.Engine.At(now)
		for groupIndex, group := range []struct {
			messages []policy.Message
			warning  bool
		}{{c.Rules, false}, {c.Warnings, true}} {
			for _, m := range group.messages {
				if !matchRequest(m.Match, req) || !m.MatchesAudience(req.UserInfo) || !inWindow(m, w, now) {
					continue
				}
				for _, name := range m.Match.Targets {
					target := c.Targets[name]
					if target.Namespaced != (req.Namespace != "") || !target.MatchesAPI(gvr) {
						continue
					}
					kind := req.Kind.Kind
					if target.Pattern != nil && target.Pattern.Kind != "*" && req.SubResource != "" {
						if parentKind == nil {
							resolved, resolveErr := h.Store.Mapper.KindFor(gvr)
							if resolveErr != nil {
								return cradmission.Errored(http.StatusServiceUnavailable, fmt.Errorf("resolving parent kind: %w", resolveErr))
							}
							parentKind = &resolved
						}
						kind = parentKind.Kind
					}
					if !target.MatchesKind(gvr, kind) {
						continue
					}
					if target.Namespaced {
						if namespace == nil {
							namespace = &corev1.Namespace{}
							if err := h.Reader.Get(ctx, client.ObjectKey{Name: req.Namespace}, namespace); err != nil {
								return cradmission.Errored(http.StatusServiceUnavailable, fmt.Errorf("resolving namespace: %w", err))
							}
						}
						if !c.SelectNamespace(labels.Set(namespace.Labels)) {
							continue policyLoop
						}
					}
					var selected, previous *unstructured.Unstructured
					if req.SubResource != "" && (!target.Selector.Empty() || m.NeedsTarget) {
						if parent == nil {
							parent, err = h.Dynamic.Resource(gvr).Namespace(req.Namespace).Get(ctx, req.Name, metav1.GetOptions{})
							if err != nil {
								return cradmission.Errored(http.StatusServiceUnavailable, fmt.Errorf("resolving parent for subresource: %w", err))
							}
						}
						selected = parent
					} else {
						if err := decodeObjects(); err != nil {
							return cradmission.Errored(http.StatusBadRequest, err)
						}
						selected, previous = current, old
					}
					matches := func(obj *unstructured.Unstructured) bool {
						return obj != nil && target.Selector.Matches(labels.Set(obj.GetLabels()))
					}
					// Match either old or new labels to prevent removing a label to evade a freeze.
					if !target.Selector.Empty() && !matches(selected) && !matches(previous) {
						continue
					}
					if m.HasConditions() {
						if variables == nil {
							if err := decodeObjects(); err != nil {
								return cradmission.Errored(http.StatusBadRequest, err)
							}
							variables, err = conditionInput(req, current, old, namespace)
							if err != nil {
								return cradmission.Errored(http.StatusBadRequest, err)
							}
						}
						matched, matchErr := m.MatchConditions(ctx, variables, &budgets[groupIndex])
						if matchErr != nil {
							// Do not expose evaluated object values in API responses or logs.
							message := fmt.Sprintf("ChangeWindow %s entry %s: matchConditions could not be evaluated", c.Source.Name, m.Name)
							if group.warning {
								addWarning(message)
								break
							}
							return cradmission.Errored(http.StatusInternalServerError, fmt.Errorf("%s", message))
						}
						if !matched {
							break
						} // Conditions are independent of the matching target alias.
					}
					obj := selected
					if obj == nil {
						obj = previous
					}
					var object map[string]any
					if obj != nil {
						object = obj.Object
					}
					message, renderErr := policy.Render(m.Template, policy.Context(object, nil, w, now), 4096)
					if renderErr != nil || strings.TrimSpace(message) == "" {
						message = "ChangeWindow " + c.Source.Name + " restricts this request; message could not be rendered."
					}
					if group.warning {
						addWarning(message)
					} else if deny == "" {
						deny = message
					}
					break
				}
			}
		}
	}
	response = cradmission.Allowed("")
	if deny != "" {
		response = cradmission.Denied(deny)
	}
	response.Warnings = warnings
	return response
}

func executorRequest(c *policy.Compiled, req cradmission.Request) bool {
	sa := c.Source.Spec.ServiceAccount
	if sa == nil || req.Operation != admissionv1.Update || req.UserInfo.Username != "system:serviceaccount:"+sa.Namespace+":"+sa.Name {
		return false
	}
	extra := req.UserInfo.Extra[policy.ExecutionExtra]
	return len(extra) == 1 && extra[0] == string(c.Source.UID)
}

func matchRequest(m api.RequestMatch, req cradmission.Request) bool {
	subs := m.Subresources
	if len(subs) == 0 {
		subs = []string{""}
	}
	return slices.Contains(m.Operations, string(req.Operation)) && (slices.Contains(subs, req.SubResource) || (req.SubResource != "" && slices.Contains(subs, "*")))
}

func inWindow(m policy.Message, w schedule.Window, now time.Time) bool {
	switch m.When {
	case "", "Active":
		return w.Active
	case "Inactive":
		return !w.Active
	case "BeforeStart":
		return !w.Active && !w.Start.IsZero() && w.Start.After(now) && w.Start.Sub(now) <= m.Within
	case "BeforeEnd":
		return w.Active && !w.End.IsZero() && w.End.Sub(now) <= m.Within
	default:
		return false
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

type Validator struct{ Mapper meta.RESTMapper }

func (v *Validator) Handle(_ context.Context, req cradmission.Request) cradmission.Response {
	if req.Operation == admissionv1.Delete {
		return cradmission.Allowed("")
	}
	obj := &api.ChangeWindow{}
	if err := json.Unmarshal(req.Object.Raw, obj); err != nil {
		return cradmission.Errored(http.StatusBadRequest, err)
	}
	if req.Operation == admissionv1.Update {
		old := &api.ChangeWindow{}
		if err := json.Unmarshal(req.OldObject.Raw, old); err != nil {
			return cradmission.Errored(http.StatusBadRequest, err)
		}
		if slices.Contains(old.Finalizers, "addons.projectcapsule.dev/restore") && !reflect.DeepEqual(old.Spec, obj.Spec) {
			return cradmission.Denied("spec cannot change while resources await restoration; delete the ChangeWindow to restore them first")
		}
	}
	if _, err := policy.Compile(obj, v.Mapper); err != nil {
		return cradmission.Denied(err.Error())
	}
	return cradmission.Allowed("")
}
