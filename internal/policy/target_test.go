// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"reflect"
	"slices"
	"testing"

	capsuleruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	api "github.com/projectcapsule/schedule-action-controller/api/v1alpha1"
	"github.com/projectcapsule/schedule-action-controller/internal/testutil"
)

func TestTargetCapsulePatterns(t *testing.T) {
	for _, tc := range []struct{ apiVersion, kind string }{
		{"", "ConfigMap"},
		{"v1", "*"},
		{"apps", "Deployment"},
		{"apps/*", "Deployment"},
		{"apps/v*", "*Set"},
		{"*", "*"},
		{"*.example.com/v1*", "Wid*et"},
		{" apps/* ", "Deploy*"},
	} {
		t.Run(tc.apiVersion+"/"+tc.kind, func(t *testing.T) {
			target, err := compileTarget(api.Target{APIVersion: tc.apiVersion, Kind: tc.kind}, testutil.Mapper())
			if err != nil {
				t.Fatal(err)
			}
			pattern := capsuleruntime.VersionKind{APIVersion: tc.apiVersion, Kind: tc.kind}
			for _, gvk := range []schema.GroupVersionKind{
				{Version: "v1", Kind: "ConfigMap"},
				{Version: "v1", Kind: "Pod"},
				{Group: "apps", Version: "v1", Kind: "Deployment"},
				{Group: "apps", Version: "v1beta1", Kind: "Deployment"},
				{Group: "apps", Version: "v1", Kind: "StatefulSet"},
				{Group: "apps", Version: "v1", Kind: "deployment"},
				{Group: "test.example.com", Version: "v1alpha1", Kind: "Widget"},
				{Group: "other.dev", Version: "v1", Kind: "Widget"},
			} {
				gvr := gvk.GroupVersion().WithResource("unused")
				if target.Pattern == nil {
					if gvk == pattern.GroupVersionKind() {
						gvr = target.Resource
					}
				}
				if got, want := target.MatchesAPI(gvr) && target.MatchesKind(gvr, gvk.Kind), pattern.MatchesGroupVersionKind(gvk); got != want {
					t.Errorf("%v: got %t want %t", gvk, got, want)
				}
			}
		})
	}
}

func TestPatternValidation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(*api.ChangeWindow)
		invalid bool
	}{
		{"all", func(*api.ChangeWindow) {}, false},
		{"missing namespaces", func(a *api.ChangeWindow) { a.Spec.NamespaceSelectors = nil }, true},
		{"group only", func(a *api.ChangeWindow) {
			a.Spec.Targets[0].APIVersion = "apps"
			a.Spec.Targets[0].Kind = "Deployment"
		}, false},
		{"unknown group kind", func(a *api.ChangeWindow) {
			a.Spec.Targets[0].APIVersion = "unknown"
			a.Spec.Targets[0].Kind = "Missing"
		}, true},
		{"empty kind", func(a *api.ChangeWindow) { a.Spec.Targets[0].Kind = "" }, true},
		{"pattern action", func(a *api.ChangeWindow) {
			a.Spec.ServiceAccount = &api.ServiceAccountReference{Namespace: "dev", Name: "executor"}
			a.Spec.Actions = []api.ResourceAction{{Name: "invalid", Targets: []string{"deployments"}, OnStart: &api.ActionStep{Apply: api.Apply{Template: "spec: {}"}}}}
		}, true},
		{"subresource glob unsupported", func(a *api.ChangeWindow) { a.Spec.Admission[0].Subresources = []string{"sta*"} }, true},
		{"concrete action subresource wildcard", func(a *api.ChangeWindow) {
			a.Spec.Targets[0].APIVersion, a.Spec.Targets[0].Kind = "apps/v1", "Deployment"
			a.Spec.ServiceAccount = &api.ServiceAccountReference{Namespace: "dev", Name: "executor"}
			a.Spec.Actions = []api.ResourceAction{{Name: "invalid", Targets: []string{"deployments"}, OnStart: &api.ActionStep{Apply: api.Apply{Subresource: "*", Template: "spec: {}"}}}}
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := testutil.Action()
			a.Spec.Targets[0].APIVersion, a.Spec.Targets[0].Kind = "*", "*"
			a.Spec.Admission[0].Subresources = []string{"", "*"}
			tc.mutate(a)
			_, err := Compile(a, testutil.Mapper())
			if (err != nil) != tc.invalid {
				t.Fatalf("invalid=%t error=%v", tc.invalid, err)
			}
		})
	}
}

func TestPatternStoreSnapshots(t *testing.T) {
	s := NewStore(testutil.Mapper())
	for _, name := range []string{"c-exact", "b-both", "a-pattern"} {
		a := testutil.Action()
		a.Name = name
		if name != "c-exact" {
			a.Spec.Targets = append(a.Spec.Targets, api.Target{Name: "all", APIVersion: "*", Kind: "*"})
		}
		if name == "a-pattern" {
			a.Spec.Targets = a.Spec.Targets[1:]
			a.Spec.Admission[0].Targets = []string{"all"}
		}
		if err := s.Upsert(a); err != nil {
			t.Fatal(err)
		}
	}
	gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	set, err := s.Candidates(gvr)
	if err != nil {
		t.Fatal(err)
	}
	names := func(set CandidateSet) []string {
		var out []string
		for c := range set.All() {
			out = append(out, c.Source.Name)
		}
		return out
	}
	if got := names(set); !reflect.DeepEqual(got, []string{"a-pattern", "b-both", "c-exact"}) {
		t.Fatal(got)
	}
	// A newly discovered GVR is covered without rebuilding or caching request keys.
	unknown, err := s.Candidates(schema.GroupVersionResource{Group: "new.example.com", Version: "v1", Resource: "widgets"})
	if err != nil || !reflect.DeepEqual(names(unknown), []string{"a-pattern", "b-both"}) {
		t.Fatalf("new resource: %v %v", names(unknown), err)
	}
	a := testutil.Action()
	a.Name = "b-both"
	a.Generation++
	if err := s.Upsert(a); err != nil {
		t.Fatal(err)
	}
	s.Delete("a-pattern")
	next, err := s.Candidates(gvr)
	if err != nil || !reflect.DeepEqual(names(next), []string{"b-both", "c-exact"}) {
		t.Fatal(names(next), err)
	}
	if len(slices.Collect(set.All())) != 3 {
		t.Fatal("published snapshot changed")
	}
	unknown, err = s.Candidates(schema.GroupVersionResource{})
	if err != nil || !unknown.Empty() {
		t.Fatal("pattern entries not retired")
	}
	for range set.All() {
		break
	} // Iterator supports early termination.
}

func TestTemplateTargetReads(t *testing.T) {
	for _, tc := range []struct {
		text string
		want bool
	}{
		{"until {{ .window.end }}", false},
		{"{{ if .window.active }}active{{ else }}inactive{{ end }}", false},
		{"{{ .target.metadata.name }}", true},
		{"{{ index . \"target\" }}", true},
		{"{{ $x := . }}{{ $x.target }}", true},
		{"{{ with .target }}{{ .metadata.name }}{{ end }}", true},
		{"{{ template \"name\" . }}{{ define \"name\" }}{{ .target }}{{ end }}", true},
	} {
		a := testutil.Action()
		a.Spec.Admission[0].Message = tc.text
		a.Spec.Targets[0].Selector = metav1.LabelSelector{}
		c, err := Compile(a, testutil.Mapper())
		if err != nil {
			t.Fatal(err)
		}
		if c.Rules[0].NeedsTarget != tc.want {
			t.Errorf("%s: needs target = %t", tc.text, c.Rules[0].NeedsTarget)
		}
	}
}
