// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"

	api "github.com/projectcapsule/schedule-action-controller/api/v1alpha1"
	"github.com/projectcapsule/schedule-action-controller/internal/testutil"
)

func TestValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*api.ChangeWindow)
	}{
		{"empty selectors", func(a *api.ChangeWindow) { a.Spec.NamespaceSelectors = nil }},
		{"bad selector", func(a *api.ChangeWindow) {
			a.Spec.NamespaceSelectors = []metav1.LabelSelector{{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "x", Operator: "invalid"}}}}
		}},
		{"cluster target", func(a *api.ChangeWindow) {
			a.Spec.Targets[0].APIVersion = "v1"
			a.Spec.Targets[0].Kind = "Namespace"
		}},
		{"missing target", func(a *api.ChangeWindow) { a.Spec.Admission[0].Targets = []string{"missing"} }},
		{"invalid template", func(a *api.ChangeWindow) { a.Spec.Admission[0].Message = "{{" }},
		{"invalid warning", func(a *api.ChangeWindow) {
			a.Spec.Config.Admission.Warnings = []api.AdmissionWarning{{Name: "soon", When: "BeforeStart", Message: "soon", RequestMatch: a.Spec.Admission[0].RequestMatch}}
		}},
		{"missing executor", func(a *api.ChangeWindow) {
			a.Spec.Actions = []api.ResourceAction{{Name: "a", Targets: []string{"deployments"}}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := testutil.Action()
			tc.mutate(a)
			if _, err := Compile(a, testutil.Mapper()); err == nil {
				t.Fatal("accepted invalid policy")
			}
		})
	}
}

func TestStoreLifecycle(t *testing.T) {
	s := NewStore(testutil.Mapper())
	a := testutil.Action()
	gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	if err := s.Upsert(a); err != nil {
		t.Fatal(err)
	}
	firstSet, _ := s.Candidates(gvr)
	first := slices.Collect(firstSet.All())
	a.Spec.Admission[0].Message = "mutated"
	if first[0].Source.Spec.Admission[0].Message == "mutated" {
		t.Fatal("published source was not copied")
	}
	if err := s.Upsert(a); err != nil {
		t.Fatal(err)
	}
	secondSet, _ := s.Candidates(gvr)
	second := slices.Collect(secondSet.All())
	if first[0] != second[0] {
		t.Fatal("status/same-generation update recompiled")
	}
	a.Generation++
	if err := s.Upsert(a); err != nil {
		t.Fatal(err)
	}
	secondSet, _ = s.Candidates(gvr)
	second = slices.Collect(secondSet.All())
	if first[0] == second[0] {
		t.Fatal("spec change not invalidated")
	}
	a.Spec.Schedules[0].Cron = "bad"
	a.Generation++
	if err := s.Upsert(a); err == nil {
		t.Fatal("invalid update accepted")
	}
	if _, err := s.Candidates(gvr); err == nil {
		t.Fatal("invalid policy failed open")
	}
	s.Delete(a.Name)
	list, err := s.Candidates(gvr)
	if err != nil || !list.Empty() {
		t.Fatalf("delete did not prune: %v %v", list, err)
	}
}

func TestStoreConcurrent(t *testing.T) {
	s := NewStore(testutil.Mapper())
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a := testutil.Action()
			a.Name = fmt.Sprint(i)
			if i%2 == 0 {
				a.Spec.Targets[0].APIVersion, a.Spec.Targets[0].Kind = "*", "*"
			}
			for range 20 {
				if err := s.Upsert(a); err != nil {
					t.Error(err)
				}
				if candidates, err := s.Candidates(schema.GroupVersionResource{}); err != nil {
					t.Error(err)
				} else {
					for c := range candidates.All() {
						if c.Source == nil {
							t.Error("incomplete snapshot")
						}
					}
				}
			}
			s.Delete(a.Name)
		}()
	}
	wg.Wait()
}

func TestRenderLimits(t *testing.T) {
	tmpl, _ := Parse("{{ .missing }}")
	if _, err := Render(tmpl, map[string]any{}, 100); err == nil {
		t.Fatal("missing key accepted")
	}
	tmpl, _ = Parse("0123456789")
	if _, err := Render(tmpl, nil, 5); err == nil {
		t.Fatal("output limit ignored")
	}
}

func TestChangeWindowExamples(t *testing.T) {
	files, err := filepath.Glob("../../playground/manifests/examples/*.yaml")
	if err != nil || len(files) == 0 {
		t.Fatalf("no examples found: %v", err)
	}
	count := 0
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var kind struct {
			Kind string `json:"kind"`
		}
		if err := yaml.Unmarshal(data, &kind); err != nil {
			t.Fatal(err)
		}
		if kind.Kind != "ChangeWindow" {
			continue
		}
		count++
		t.Run(filepath.Base(file), func(t *testing.T) {
			var action api.ChangeWindow
			if err := yaml.UnmarshalStrict(data, &action); err != nil {
				t.Fatal(err)
			}
			if _, err := Compile(&action, testutil.Mapper()); err != nil {
				t.Fatalf("documented example fails policy validation: %v", err)
			}
		})
	}
	if count == 0 {
		t.Fatal("no ChangeWindow examples found")
	}
}
