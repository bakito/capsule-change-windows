// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	capmeta "github.com/projectcapsule/capsule/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ConditionReady    = capmeta.ReadyCondition
	ConditionActive   = "Active"
	ReasonPlanned     = "Planned"
	ReasonActive      = capmeta.ActiveReason
	ReasonFinished    = "Finished"
	ReasonFinishing   = "Finishing"
	ReasonUnscheduled = "Unscheduled"
	ReasonUnknown     = "Unknown"
)

// ChangeWindowSpec selects namespaces and resources for scheduled behavior.
type ChangeWindowSpec struct {
	// NamespaceSelectors are ORed for namespaced targets. Required when any target is namespaced.
	// Omit for Node-only windows; Node targets use their own label selector.
	// An explicit empty selector matches all non-protected namespaces.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=16
	// +optional
	NamespaceSelectors []metav1.LabelSelector `json:"namespaceSelectors,omitempty"`
	// Schedules form a union of overlapping and adjacent windows.
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=16
	Schedules []Schedule `json:"schedules"`
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=32
	Targets []Target `json:"targets"`
	// ServiceAccount is required for every window, including admission-only windows.
	// Resource actions use this identity for workload reads and SSA writes.
	// +kubebuilder:validation:Required
	ServiceAccount *ServiceAccountReference `json:"serviceAccount"`
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=16
	// +optional
	Actions []ResourceAction `json:"actions,omitempty"`
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=32
	// +optional
	Admission []AdmissionRule `json:"admission,omitempty"`
	// +optional
	Config Config `json:"config,omitempty"`
}

type Schedule struct {
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	Name string `json:"name"`
	// Cron is a standard five-field cron expression; seconds and TZ prefixes are not accepted.
	// +kubebuilder:validation:MaxLength=256
	Cron string `json:"cron"`
	// +kubebuilder:default=UTC
	TimeZone string `json:"timeZone,omitempty"`
	// Duration is elapsed time, greater than zero and at most 168h.
	Duration metav1.Duration `json:"duration"`
}

// Target selects a resource kind and labels. Admission targets support Capsule GVK
// patterns and match namespaced resources only, except concrete v1 Node targets.
// Scheduled actions require a concrete namespaced apiVersion and kind.
type Target struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// APIVersion uses Capsule GVK matching: omitted/empty means core v1;
	// apps matches any apps version, apps/v1 is exact, and * matches all groups
	// and versions. Partial patterns such as apps/* and *.example.com/v1 work too.
	// +kubebuilder:validation:MaxLength=256
	// +optional
	APIVersion string `json:"apiVersion,omitempty"`
	// Kind is case-sensitive and supports * patterns, for example *Set or Deploy*.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	Kind string `json:"kind"`
	// Selector selects resource labels. For Nodes it selects nodes directly;
	// for namespaced targets it applies within the selected namespaces.
	// +optional
	Selector metav1.LabelSelector `json:"selector,omitempty"`
}

type ServiceAccountReference struct {
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Namespace string `json:"namespace"`
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	Name string `json:"name"`
}

type ResourceAction struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// +kubebuilder:validation:MinItems=1
	Targets []string `json:"targets"`
	// Capture stores only explicitly selected JSON pointer values before any mutation.
	// +kubebuilder:validation:MaxProperties=32
	// +optional
	Capture map[string]Capture `json:"capture,omitempty"`
	// +optional
	OnStart *ActionStep `json:"onStart,omitempty"`
	// +optional
	OnEnd *ActionStep `json:"onEnd,omitempty"`
}

type Capture struct {
	// +optional
	Subresource string `json:"subresource,omitempty"`
	// Path is an RFC 6901 JSON pointer.
	// +kubebuilder:validation:Pattern="^/"
	Path string `json:"path"`
}

type ActionStep struct {
	Apply Apply `json:"apply"`
}

type Apply struct {
	// Subresource is an actual Kubernetes subresource, e.g. scale (not rollout).
	// +optional
	Subresource string `json:"subresource,omitempty"`
	// Force explicitly transfers ownership of conflicting fields.
	// +optional
	Force bool `json:"force,omitempty"`
	// Template renders a partial YAML/JSON object. Identity is supplied by the controller.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=65536
	Template string `json:"template"`
}

// RequestMatch restricts every admission rule and warning by request, identity and CEL conditions.
type RequestMatch struct {
	// +kubebuilder:validation:MinItems=1
	Targets []string `json:"targets"`
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:items:Enum=CREATE;UPDATE;DELETE
	Operations []string `json:"operations"`
	// Subresources defaults to the parent resource only. Use "scale" for /scale,
	// "*" for all subresources, and ["", "*"] for parent and subresource writes.
	// +optional
	Subresources []string `json:"subresources,omitempty"`
	// Audience selects authenticated request identities. Omitted or empty means everyone.
	// +optional
	Audience *Audience `json:"audience,omitempty"`
	// MatchConditions are ANDed CEL expressions. An empty list matches all requests.
	// A false result skips this entry. Evaluation errors fail closed for denial rules;
	// warning errors return a diagnostic warning without denying the request.
	// Variables: object, oldObject, request, namespaceObject. Objects are the actual
	// admission payloads (e.g. Scale for /scale); absent objects are null.
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=64
	// +optional
	MatchConditions []MatchCondition `json:"matchConditions,omitempty"`
}

// Audience matches any include subject unless any exclude subject matches.
// Empty include matches everyone. Subjects are exact, case-sensitive identities;
// matching a subject does not grant RBAC permissions.
type Audience struct {
	// +kubebuilder:validation:MaxItems=64
	// +listType=atomic
	// +optional
	Include []Subject `json:"include,omitempty"`
	// +kubebuilder:validation:MaxItems=64
	// +listType=atomic
	// +optional
	Exclude []Subject `json:"exclude,omitempty"`
}

// Subject identifies an authenticated user, group, or namespaced service account.
// +kubebuilder:validation:XValidation:rule="(self.kind == 'ServiceAccount') == has(self.namespace)",message="namespace is required only for ServiceAccount subjects"
type Subject struct {
	// +kubebuilder:validation:Enum=User;Group;ServiceAccount
	Kind string `json:"kind"`
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	Name string `json:"name"`
	// Namespace is mandatory for ServiceAccounts and forbidden for Users and Groups.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// MatchCondition is a named CEL boolean expression compiled with the policy.
type MatchCondition struct {
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern="^[a-z0-9]([-a-z0-9]*[a-z0-9])?$"
	Name string `json:"name"`
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=4096
	Expression string `json:"expression"`
}

type AdmissionRule struct {
	// +kubebuilder:validation:MinLength=1
	Name         string `json:"name"`
	RequestMatch `json:",inline"`
	// +kubebuilder:validation:Enum=Active;Inactive
	// +kubebuilder:default=Active
	When string `json:"when,omitempty"`
	// +kubebuilder:validation:Enum=Deny
	// +kubebuilder:default=Deny
	Action string `json:"action,omitempty"`
	// Message is a Go template with target and window context.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=4096
	Message string `json:"message"`
}

type Config struct {
	// +optional
	Admission AdmissionConfig `json:"admission,omitempty"`
}

type AdmissionConfig struct {
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=32
	// +optional
	Warnings []AdmissionWarning `json:"warnings,omitempty"`
}

type AdmissionWarning struct {
	// +kubebuilder:validation:MinLength=1
	Name         string `json:"name"`
	RequestMatch `json:",inline"`
	// +kubebuilder:validation:Enum=BeforeStart;Active;BeforeEnd
	When string `json:"when"`
	// Within is required for BeforeStart/BeforeEnd and must be positive, at most 168h.
	// +optional
	Within *metav1.Duration `json:"within,omitempty"`
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=4096
	Message string `json:"message"`
}

type ChangeWindowStatus struct {
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Active mirrors the Active condition for compatibility. Conditions distinguish unknown state.
	// +optional
	Active bool `json:"active,omitempty"`
	// +optional
	WindowStart *metav1.Time `json:"windowStart,omitempty"`
	// WindowEnd is absent when the effective end is beyond the bounded scheduling horizon.
	// +optional
	WindowEnd *metav1.Time `json:"windowEnd,omitempty"`
	// NextTransition is the active window's end or the next planned start, including after Finished.
	// +optional
	NextTransition *metav1.Time `json:"nextTransition,omitempty"`
	// PendingResources counts journal entries still requiring completion.
	// +optional
	PendingResources int32 `json:"pendingResources,omitempty"`
	// Conditions report reconciliation readiness (Ready) and schedule occupancy (Active).
	// Active reasons are Planned before the first observed window, Active during a window,
	// Finishing while restoration is outstanding, and Finished after an observed window completes.
	// Finished persists until another window starts. Unknown denotes invalid policy;
	// Unscheduled means no occurrence is available and none has been observed.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=cw
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="State",type=string,JSONPath=".status.conditions[?(@.type=='Active')].reason"
// +kubebuilder:printcolumn:name="Next Transition",type=string,JSONPath=".status.nextTransition"
// +kubebuilder:printcolumn:name="Active",type=string,JSONPath=".status.conditions[?(@.type=='Active')].status",priority=1
// +kubebuilder:printcolumn:name="Pending",type=integer,JSONPath=".status.pendingResources",priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// ChangeWindow describes scheduled resource changes and admission restrictions.
type ChangeWindow struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`
	Spec              ChangeWindowSpec `json:"spec"`
	// +optional
	Status ChangeWindowStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

type ChangeWindowList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ChangeWindow `json:"items"`
}
