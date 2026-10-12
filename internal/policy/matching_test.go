// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"context"
	"strings"
	"testing"

	authenticationv1 "k8s.io/api/authentication/v1"

	api "github.com/projectcapsule/schedule-action-controller/api/v1alpha1"
	"github.com/projectcapsule/schedule-action-controller/internal/testutil"
)

func TestAudience(t *testing.T) {
	for _, tc := range []struct {
		name     string
		audience *api.Audience
		user     authenticationv1.UserInfo
		match    bool
	}{
		{"omitted", nil, authenticationv1.UserInfo{}, true},
		{"empty", &api.Audience{}, authenticationv1.UserInfo{}, true},
		{"user", &api.Audience{Include: []api.Subject{{Kind: "User", Name: "alice"}}}, authenticationv1.UserInfo{Username: "alice"}, true},
		{"exact user", &api.Audience{Include: []api.Subject{{Kind: "User", Name: "Alice"}}}, authenticationv1.UserInfo{Username: "alice"}, false},
		{"group", &api.Audience{Include: []api.Subject{{Kind: "Group", Name: "developers"}}}, authenticationv1.UserInfo{Username: "alice", Groups: []string{"authenticated", "developers"}}, true},
		{"username is not a group", &api.Audience{Include: []api.Subject{{Kind: "Group", Name: "developers"}}}, authenticationv1.UserInfo{Username: "developers"}, false},
		{"service account", &api.Audience{Include: []api.Subject{{Kind: "ServiceAccount", Name: "deployer", Namespace: "team-a"}}}, authenticationv1.UserInfo{Username: "system:serviceaccount:team-a:deployer"}, true},
		{"other namespace", &api.Audience{Include: []api.Subject{{Kind: "ServiceAccount", Name: "deployer", Namespace: "team-a"}}}, authenticationv1.UserInfo{Username: "system:serviceaccount:team-b:deployer"}, false},
		{"include OR", &api.Audience{Include: []api.Subject{{Kind: "User", Name: "bob"}, {Kind: "Group", Name: "developers"}}}, authenticationv1.UserInfo{Username: "alice", Groups: []string{"developers"}}, true},
		{"exclude wins", &api.Audience{Include: []api.Subject{{Kind: "User", Name: "alice"}}, Exclude: []api.Subject{{Kind: "Group", Name: "emergency"}}}, authenticationv1.UserInfo{Username: "alice", Groups: []string{"emergency"}}, false},
		{"exclude only", &api.Audience{Exclude: []api.Subject{{Kind: "User", Name: "bob"}}}, authenticationv1.UserInfo{Username: "alice"}, true},
		{"no wildcard", &api.Audience{Include: []api.Subject{{Kind: "User", Name: "*"}}}, authenticationv1.UserInfo{Username: "alice"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, err := compileAudience(tc.audience)
			if err != nil {
				t.Fatal(err)
			}
			if got := (Message{audience: a}).MatchesAudience(tc.user); got != tc.match {
				t.Fatalf("matches=%v want=%v", got, tc.match)
			}
		})
	}
}

func TestMatchingValidation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		match api.RequestMatch
		want  string
	}{
		{"bad kind", api.RequestMatch{Audience: &api.Audience{Include: []api.Subject{{Kind: "Robot", Name: "x"}}}}, "unsupported subject kind"},
		{"missing SA namespace", api.RequestMatch{Audience: &api.Audience{Include: []api.Subject{{Kind: "ServiceAccount", Name: "x"}}}}, "valid namespace"},
		{"invalid SA namespace", api.RequestMatch{Audience: &api.Audience{Include: []api.Subject{{Kind: "ServiceAccount", Name: "x", Namespace: "A/B"}}}}, "valid namespace"},
		{"user namespace", api.RequestMatch{Audience: &api.Audience{Exclude: []api.Subject{{Kind: "User", Name: "x", Namespace: "ns"}}}}, "only valid"},
		{"empty subject", api.RequestMatch{Audience: &api.Audience{Include: []api.Subject{{Kind: "Group", Name: " "}}}}, "subject name"},
		{"duplicate subject", api.RequestMatch{Audience: &api.Audience{Include: []api.Subject{{Kind: "User", Name: "x"}, {Kind: "User", Name: "x"}}}}, "duplicate subject"},
		{"too many subjects", api.RequestMatch{Audience: &api.Audience{Include: make([]api.Subject, 65)}}, "64 subjects"},
		{"parse", api.RequestMatch{MatchConditions: []api.MatchCondition{{Name: "check", Expression: "object."}}}, "matchCondition check"},
		{"unknown variable", api.RequestMatch{MatchConditions: []api.MatchCondition{{Name: "check", Expression: "missing == true"}}}, "undeclared reference"},
		{"network authorizer unavailable", api.RequestMatch{MatchConditions: []api.MatchCondition{{Name: "check", Expression: "authorizer.allowed()"}}}, "undeclared reference"},
		{"wrong return type", api.RequestMatch{MatchConditions: []api.MatchCondition{{Name: "check", Expression: "42"}}}, "must return bool"},
		{"duplicate condition", api.RequestMatch{MatchConditions: []api.MatchCondition{{Name: "check", Expression: "true"}, {Name: "check", Expression: "false"}}}, "unique DNS labels"},
		{"bad condition name", api.RequestMatch{MatchConditions: []api.MatchCondition{{Name: "CHECK", Expression: "true"}}}, "unique DNS labels"},
		{"empty expression", api.RequestMatch{MatchConditions: []api.MatchCondition{{Name: "check", Expression: " "}}}, "expression bytes"},
		{"long expression", api.RequestMatch{MatchConditions: []api.MatchCondition{{Name: "check", Expression: strings.Repeat(" ", 4097)}}}, "expression bytes"},
		{"too many conditions", api.RequestMatch{MatchConditions: make([]api.MatchCondition, 65)}, "64 entries"},
	} {
		for _, warning := range []bool{false, true} {
			name := tc.name
			if warning {
				name += "/warning"
			}
			t.Run(name, func(t *testing.T) {
				a := testutil.Action()
				match := tc.match
				match.Targets, match.Operations = []string{"deployments"}, []string{"UPDATE"}
				if warning {
					a.Spec.Admission = nil
					a.Spec.Config.Admission.Warnings = []api.AdmissionWarning{{Name: "notice", When: "Active", Message: "notice", RequestMatch: match}}
				} else {
					a.Spec.Admission[0].RequestMatch = match
				}
				if _, err := Compile(a, testutil.Mapper()); err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("wanted %q, got %v", tc.want, err)
				}
			})
		}
	}
}

func TestConditions(t *testing.T) {
	for _, tc := range []struct {
		name        string
		expressions []string
		match, fail bool
	}{
		{"empty", nil, true, false},
		{"all true", []string{"true", "object.spec.replicas > 1"}, true, false},
		{"false", []string{"false", "true"}, false, false},
		{"error", []string{"object.missing == true"}, false, true},
		{"false before error", []string{"false", "object.missing == true"}, false, false},
		{"false after error", []string{"object.missing == true", "false"}, false, false},
		{"null guard", []string{"oldObject == null || oldObject.spec.replicas == 3"}, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entries := []api.MatchCondition{}
			for i, expression := range tc.expressions {
				entries = append(entries, api.MatchCondition{Name: string(rune('a' + i)), Expression: expression})
			}
			compiled, err := (&conditionCompiler{}).compile(entries)
			if err != nil {
				t.Fatal(err)
			}
			budget := ConditionBudget
			match, err := (Message{conditions: compiled}).MatchConditions(t.Context(), map[string]any{"object": map[string]any{"spec": map[string]any{"replicas": int64(3)}}, "oldObject": nil}, &budget)
			if match != tc.match || (err != nil) != tc.fail {
				t.Fatalf("match=%v error=%v", match, err)
			}
		})
	}
	compiled, err := (&conditionCompiler{}).compile([]api.MatchCondition{{Name: "cost", Expression: "object.items.all(x, x == 1)"}})
	if err != nil {
		t.Fatal(err)
	}
	items := make([]int64, 20000)
	for i := range items {
		items[i] = 1
	}
	budget := ConditionBudget
	m := Message{conditions: compiled}
	if ok, err := m.MatchConditions(t.Context(), map[string]any{"object": map[string]any{"items": items}}, &budget); ok || err == nil {
		t.Fatal("cost limit not enforced")
	}
	budget = 0
	if _, err := m.MatchConditions(t.Context(), nil, &budget); err == nil {
		t.Fatal("request budget not enforced")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	budget = ConditionBudget
	if _, err := m.MatchConditions(ctx, nil, &budget); err == nil {
		t.Fatal("cancellation ignored")
	}
}

func TestCompiledMatchingIsolation(t *testing.T) {
	a := testutil.Action()
	a.Spec.Admission[0].Audience = &api.Audience{Include: []api.Subject{{Kind: "User", Name: "alice"}}}
	a.Spec.Admission[0].MatchConditions = []api.MatchCondition{{Name: "check", Expression: "true"}}
	store := NewStore(testutil.Mapper())
	if err := store.Upsert(a); err != nil {
		t.Fatal(err)
	}
	c, err := store.For(a)
	if err != nil {
		t.Fatal(err)
	}
	a.Spec.Admission[0].Targets[0] = "changed"
	a.Spec.Admission[0].Audience.Include[0].Name = "bob"
	a.Spec.Admission[0].MatchConditions[0].Expression = "false"
	if c.Rules[0].Match.Targets[0] != "deployments" || c.Rules[0].Match.Audience.Include[0].Name != "alice" || c.Rules[0].Match.MatchConditions[0].Expression != "true" {
		t.Fatal("compiled matching aliases caller memory")
	}
	a.Spec.Admission[0].Targets[0] = "deployments"
	a.Generation++
	if err := store.Upsert(a); err != nil {
		t.Fatal(err)
	}
	next, err := store.For(a)
	if err != nil {
		t.Fatal(err)
	}
	if next == c || !c.Rules[0].MatchesAudience(authenticationv1.UserInfo{Username: "alice"}) || next.Rules[0].MatchesAudience(authenticationv1.UserInfo{Username: "alice"}) {
		t.Fatal("generation update did not replace audience snapshot")
	}
	budget := ConditionBudget
	if ok, err := next.Rules[0].MatchConditions(t.Context(), nil, &budget); ok || err != nil {
		t.Fatalf("condition update: %v %v", ok, err)
	}
	store.Delete(a.Name)
	if entries, err := store.Candidates(c.Targets["deployments"].Resource); !entries.Empty() || err != nil {
		t.Fatalf("delete: %v %v", entries, err)
	}
}

func TestNodeTargets(t *testing.T) {
	a := testutil.Action()
	a.Spec.Targets[0].APIVersion, a.Spec.Targets[0].Kind = "v1", "Node"
	a.Spec.Admission[0].Subresources = nil
	if _, err := Compile(a, testutil.Mapper()); err == nil {
		t.Fatal("namespaceSelectors silently ignored for Nodes")
	}
	a.Spec.NamespaceSelectors = nil
	if _, err := Compile(a, testutil.Mapper()); err != nil {
		t.Fatal(err)
	}
	a.Spec.Admission[0].Subresources = []string{"status"}
	if _, err := Compile(a, testutil.Mapper()); err == nil {
		t.Fatal("unregistered Node subresource accepted")
	}
	a.Spec.Admission[0].Subresources = nil
	a.Spec.ServiceAccount = &api.ServiceAccountReference{Name: "executor", Namespace: "dev"}
	a.Spec.Actions = []api.ResourceAction{{Name: "node-action", Targets: []string{"deployments"}, OnStart: &api.ActionStep{Apply: api.Apply{Template: "spec: {}"}}}}
	if _, err := Compile(a, testutil.Mapper()); err == nil || !strings.Contains(err.Error(), "admission-only") {
		t.Fatalf("Node action accepted: %v", err)
	}
}
