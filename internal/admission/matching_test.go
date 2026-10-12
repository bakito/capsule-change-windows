// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package admission

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	api "github.com/projectcapsule/schedule-action-controller/api/v1alpha1"
	"github.com/projectcapsule/schedule-action-controller/internal/testutil"
)

func TestAudiencesApplyToEveryAdmissionBlock(t *testing.T) {
	for _, warning := range []bool{false, true} {
		for _, when := range []string{"Active", "Inactive", "BeforeStart", "BeforeEnd"} {
			if !warning && when != "Active" && when != "Inactive" || warning && when == "Inactive" {
				continue
			}
			t.Run(when+map[bool]string{true: "/warning", false: "/deny"}[warning], func(t *testing.T) {
				h, reads := handler(t)
				a := testutil.Action()
				a.Generation++
				match := a.Spec.Admission[0].RequestMatch
				match.Audience = &api.Audience{Include: []api.Subject{{Kind: "User", Name: "alice"}, {Kind: "Group", Name: "developers"}, {Kind: "ServiceAccount", Name: "deployer", Namespace: "dev"}}, Exclude: []api.Subject{{Kind: "Group", Name: "emergency"}}}
				match.MatchConditions = []api.MatchCondition{{Name: "replicas", Expression: "object.spec.replicas == 2"}}
				if when == "BeforeStart" || when == "Inactive" {
					h.Now = func() time.Time { return time.Date(2026, 10, 1, 9, 30, 0, 0, time.UTC) }
				}
				if warning {
					a.Spec.Admission = nil
					w := api.AdmissionWarning{Name: "notice", When: when, Message: "Audience notice", RequestMatch: match}
					if when != "Active" {
						w.Within = &metav1.Duration{Duration: time.Hour}
					}
					a.Spec.Config.Admission.Warnings = []api.AdmissionWarning{w}
				} else {
					a.Spec.Admission[0].RequestMatch = match
					a.Spec.Admission[0].When = when
				}
				if err := h.Store.Upsert(a); err != nil {
					t.Fatal(err)
				}
				for _, tc := range []struct {
					user  authenticationv1.UserInfo
					match bool
				}{
					{authenticationv1.UserInfo{Username: "alice"}, true},
					{authenticationv1.UserInfo{Username: "bob", Groups: []string{"developers"}}, true},
					{authenticationv1.UserInfo{Username: "system:serviceaccount:dev:deployer"}, true},
					{authenticationv1.UserInfo{Username: "system:serviceaccount:other:deployer"}, false},
					{authenticationv1.UserInfo{Username: "alice", Groups: []string{"emergency"}}, false},
					{authenticationv1.UserInfo{Username: "unrelated"}, false},
				} {
					*reads = 0
					req := request(admissionv1.Update)
					req.UserInfo = tc.user
					response := h.Handle(t.Context(), req)
					if response.Allowed != (warning || !tc.match) || (len(response.Warnings) != 0) != (warning && tc.match) {
						t.Fatalf("user=%+v response=%+v", tc.user, response)
					}
					if !tc.match && *reads != 0 {
						t.Fatal("excluded audience performed an API read")
					}
				}
			})
		}
	}
}

func TestRequestConditions(t *testing.T) {
	for _, tc := range []struct {
		name, expression string
		operation        admissionv1.Operation
		allowed          bool
		code             int32
	}{
		{"create", "oldObject == null && object.spec.replicas == 2 && request.operation == 'CREATE'", admissionv1.Create, false, 403},
		{"update", "object.spec.replicas == oldObject.spec.replicas && request.dryRun == true && namespaceObject.metadata.labels.environment == 'development'", admissionv1.Update, false, 403},
		{"delete", "object == null && oldObject.spec.replicas == 2", admissionv1.Delete, false, 403},
		{"large integer", "object.metadata.generation == 9007199254740993", admissionv1.Update, false, 403},
		{"identity metadata", "request.userInfo.uid == 'uid' && request.userInfo.groups.exists(g, g == 'developers') && request.userInfo.extra['example.com/team'][0] == 'platform' && request.options.fieldManager == 'test'", admissionv1.Update, false, 403},
		{"non match", "request.userInfo.username == 'another'", admissionv1.Update, true, 200},
		{"missing field", "object.spec.secretValue == 'sensitive'", admissionv1.Update, false, 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := handler(t)
			a := testutil.Action()
			a.Generation++
			a.Spec.Admission[0].MatchConditions = []api.MatchCondition{{Name: "request-match", Expression: tc.expression}}
			if err := h.Store.Upsert(a); err != nil {
				t.Fatal(err)
			}
			req := request(tc.operation)
			if tc.name == "large integer" {
				req.Object.Raw = []byte(strings.Replace(string(req.Object.Raw), `"name":"app"`, `"name":"app","generation":9007199254740993`, 1))
			}
			req.UserInfo = authenticationv1.UserInfo{UID: "uid", Groups: []string{"developers"}, Extra: map[string]authenticationv1.ExtraValue{"example.com/team": {"platform"}}}
			req.Options.Raw = []byte(`{"apiVersion":"meta.k8s.io/v1","kind":"UpdateOptions","fieldManager":"test"}`)
			if tc.operation == admissionv1.Create {
				req.OldObject.Raw = nil
			}
			dry := true
			req.DryRun = &dry
			response := h.Handle(t.Context(), req)
			if response.Allowed != tc.allowed || response.Result.Code != tc.code {
				t.Fatalf("response=%+v", response)
			}
			if strings.Contains(response.Result.Message, "sensitive") {
				t.Fatal("CEL evaluation exposed object data")
			}
		})
	}
}

func TestWarningConditionsAreIndependent(t *testing.T) {
	h, _ := handler(t)
	a := testutil.Action()
	a.Generation++
	match := a.Spec.Admission[0].RequestMatch
	a.Spec.Admission[0].Audience = &api.Audience{Include: []api.Subject{{Kind: "User", Name: "blocked"}}}
	match.MatchConditions = []api.MatchCondition{{Name: "invalid-at-runtime", Expression: "object.missing == true"}}
	a.Spec.Config.Admission.Warnings = []api.AdmissionWarning{{Name: "notice", When: "Active", Message: "warning", RequestMatch: match}}
	if err := h.Store.Upsert(a); err != nil {
		t.Fatal(err)
	}
	req := request(admissionv1.Update)
	response := h.Handle(t.Context(), req)
	if !response.Allowed || len(response.Warnings) != 1 || !strings.Contains(response.Warnings[0], "matchConditions could not be evaluated") {
		t.Fatalf("warning error response=%+v", response)
	}
	req.UserInfo.Username = "blocked"
	response = h.Handle(t.Context(), req)
	if response.Allowed || !strings.Contains(response.Result.Message, "Maintenance until") {
		t.Fatal("warning error bypassed a matching denial")
	}
	// A matching warning does not inherit another entry's subject filter.
	a.Generation++
	a.Spec.Config.Admission.Warnings[0].MatchConditions[0].Expression = "true"
	if err := h.Store.Upsert(a); err != nil {
		t.Fatal(err)
	}
	req.UserInfo.Username = "unrelated"
	response = h.Handle(t.Context(), req)
	if !response.Allowed || len(response.Warnings) != 1 || response.Warnings[0] != "warning" {
		t.Fatalf("entry scope leaked: %+v", response)
	}
}

func TestConditionsUseSubresourcePayload(t *testing.T) {
	h, reads := handler(t)
	a := testutil.Action()
	a.Generation++
	a.Spec.Admission[0].MatchConditions = []api.MatchCondition{{Name: "scale", Expression: "request.subResource == 'scale' && request.resource.resource == 'deployments' && object.kind == 'Scale' && object.spec.replicas > oldObject.spec.replicas"}}
	a.Spec.Admission[0].Audience = &api.Audience{Include: []api.Subject{{Kind: "User", Name: "alice"}}}
	a.Spec.Config.Admission.Warnings = []api.AdmissionWarning{{Name: "notice", When: "Active", Message: "scale warning", RequestMatch: a.Spec.Admission[0].RequestMatch}}
	if err := h.Store.Upsert(a); err != nil {
		t.Fatal(err)
	}
	parent := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "apps/v1", "kind": "Deployment", "metadata": map[string]any{"name": "app", "namespace": "dev", "labels": map[string]any{"maintenance": "enabled"}}}}
	dynamic := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), parent)
	h.Dynamic = dynamic
	req := request(admissionv1.Update)
	req.SubResource = "scale"
	req.UserInfo.Username = "alice"
	req.Kind = metav1.GroupVersionKind{Group: "autoscaling", Version: "v1", Kind: "Scale"}
	req.Object.Raw = []byte(`{"apiVersion":"autoscaling/v1","kind":"Scale","spec":{"replicas":4}}`)
	req.OldObject.Raw = []byte(`{"apiVersion":"autoscaling/v1","kind":"Scale","spec":{"replicas":2}}`)
	response := h.Handle(t.Context(), req)
	if response.Allowed || len(response.Warnings) != 1 || len(dynamic.Actions()) != 1 || *reads != 1 {
		t.Fatalf("scale response=%+v, namespace reads=%d, parent reads=%d", response, *reads, len(dynamic.Actions()))
	}
	req.Object.Raw, req.OldObject.Raw = req.OldObject.Raw, req.Object.Raw
	if !h.Handle(t.Context(), req).Allowed {
		t.Fatal("nonmatching scale condition denied")
	}
	*reads = 0
	dynamic.ClearActions()
	req.UserInfo.Username = "other"
	if !h.Handle(t.Context(), req).Allowed || *reads != 0 || len(dynamic.Actions()) != 0 {
		t.Fatal("unmatched audience read parent or namespace")
	}
}

func TestNodeAdmission(t *testing.T) {
	h, reads := handler(t)
	a := testutil.Action()
	a.Generation++
	a.Spec.NamespaceSelectors = nil
	a.Spec.Targets[0].APIVersion, a.Spec.Targets[0].Kind = "v1", "Node"
	a.Spec.Admission[0].Subresources = nil
	a.Spec.Admission[0].Audience = &api.Audience{Include: []api.Subject{{Kind: "User", Name: "alice"}}}
	a.Spec.Admission[0].MatchConditions = []api.MatchCondition{{Name: "node", Expression: "namespaceObject == null && request.namespace == ''"}}
	if err := h.Store.Upsert(a); err != nil {
		t.Fatal(err)
	}
	for _, operation := range []admissionv1.Operation{admissionv1.Create, admissionv1.Update, admissionv1.Delete} {
		req := request(operation)
		req.Namespace = ""
		req.UserInfo.Username = "alice"
		req.Resource = metav1.GroupVersionResource{Version: "v1", Resource: "nodes"}
		req.Kind = metav1.GroupVersionKind{Version: "v1", Kind: "Node"}
		node := []byte(`{"apiVersion":"v1","kind":"Node","metadata":{"name":"worker","labels":{"maintenance":"enabled"}}}`)
		req.Object.Raw, req.OldObject.Raw = node, node
		if operation == admissionv1.Create {
			req.OldObject.Raw = nil
		}
		if operation == admissionv1.Delete {
			req.Object.Raw = nil
		}
		if operation == admissionv1.Update {
			req.Object.Raw = []byte(`{"metadata":{"labels":{}}}`)
		}
		if response := h.Handle(t.Context(), req); response.Allowed || response.Result.Code != 403 {
			t.Fatalf("node %s: %+v", operation, response)
		}
		req.UserInfo.Username = "bob"
		if !h.Handle(t.Context(), req).Allowed {
			t.Fatal("unselected user denied")
		}
		req.UserInfo.Username = "alice"
		req.Object.Raw = []byte(`{"metadata":{"labels":{}}}`)
		req.OldObject.Raw = nil
		if !h.Handle(t.Context(), req).Allowed {
			t.Fatal("unselected node denied")
		}
		req.SubResource = "status"
		h.Ready = func() bool { return false }
		if !h.Handle(t.Context(), req).Allowed {
			t.Fatal("node status depends on policy cache")
		}
		h.Ready = nil
	}
	if *reads != 0 {
		t.Fatal("Node admission read namespaces")
	}
}

func TestConcurrentMatching(t *testing.T) {
	h, _ := handler(t)
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	h.Reader = fake.NewClientBuilder().WithScheme(scheme).WithObjects(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "dev", Labels: map[string]string{"environment": "development"}}}).Build()
	a := testutil.Action()
	a.Generation++
	a.Spec.Admission[0].MatchConditions = []api.MatchCondition{{Name: "identity", Expression: "request.userInfo.username == 'blocked' && object.spec.replicas == 2"}}
	if err := h.Store.Upsert(a); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Go(func() {
			req := request(admissionv1.Update)
			blocked := i%2 == 0
			if blocked {
				req.UserInfo.Username = "blocked"
			}
			for range 50 {
				if response := h.Handle(t.Context(), req); response.Allowed == blocked {
					t.Errorf("request context leaked: %+v", response)
					return
				}
			}
		})
	}
	wg.Wait()
}

func TestMatchingValidator(t *testing.T) {
	v := &Validator{Mapper: testutil.Mapper()}
	a := testutil.Action()
	a.Spec.Config.Admission.Warnings = []api.AdmissionWarning{{Name: "notice", When: "Active", Message: "notice", RequestMatch: a.Spec.Admission[0].RequestMatch}}
	a.Spec.Config.Admission.Warnings[0].MatchConditions = []api.MatchCondition{{Name: "check", Expression: "invalid.variable"}}
	raw, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	req := request(admissionv1.Create)
	req.Object.Raw = raw
	if response := v.Handle(t.Context(), req); response.Allowed || !strings.Contains(response.Result.Message, "matchCondition") {
		t.Fatalf("invalid warning accepted: %+v", response)
	}
}
