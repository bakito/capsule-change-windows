// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package admission

import (
	"fmt"
	"os"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"sigs.k8s.io/yaml"

	api "github.com/projectcapsule/schedule-action-controller/api/v1alpha1"
	"github.com/projectcapsule/schedule-action-controller/internal/testutil"
)

func TestFreezeExceptPodsExample(t *testing.T) {
	data, err := os.ReadFile("../../config/samples/freeze-except-pods.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range []admissionv1.Operation{admissionv1.Create, admissionv1.Update, admissionv1.Delete} {
		for _, resource := range []struct{ group, version, kind, name string }{
			{"", "v1", "Pod", "pods"}, {"", "v1", "ConfigMap", "configmaps"}, {"apps", "v1", "Deployment", "deployments"}, {"new.example.com", "v1alpha1", "Widget", "widgets"},
		} {
			for _, sub := range []string{"", "status", "scale", "binding", "eviction"} {
				t.Run(fmt.Sprintf("%s/%s/%s", op, resource.name, sub), func(t *testing.T) {
					h, reads := handler(t)
					var a api.ChangeWindow
					if err := yaml.UnmarshalStrict(data, &a); err != nil {
						t.Fatal(err)
					}
					a.Name = "maintenance"
					a.Generation = 2
					a.Spec.Schedules = testutil.Action().Spec.Schedules
					a.Spec.NamespaceSelectors = testutil.Action().Spec.NamespaceSelectors
					if err := h.Store.Upsert(&a); err != nil {
						t.Fatal(err)
					}
					req := request(op)
					req.UserInfo.Groups = []string{"tenant-developers"}
					req.SubResource = sub
					req.Resource = metav1.GroupVersionResource{Group: resource.group, Version: resource.version, Resource: resource.name}
					req.Kind = metav1.GroupVersionKind{Group: resource.group, Version: resource.version, Kind: resource.kind}
					if sub == "binding" {
						req.Kind.Kind = "Binding"
					}
					if sub == "eviction" {
						req.Kind = metav1.GroupVersionKind{Group: "policy", Version: "v1", Kind: "Eviction"}
					}
					// No dynamic client: selecting everything needs no parent-object GET,
					// including ephemeral subresource requests and as-yet unknown CRDs.
					response := h.Handle(t.Context(), req)
					if response.Allowed != (resource.name == "pods") || *reads != 1 {
						t.Fatalf("response=%+v namespace reads=%d", response, *reads)
					}
					if !response.Allowed && !strings.Contains(response.Result.Message, "Tenant maintenance") {
						t.Fatal(response.Result)
					}
					req.Namespace = "other"
					if !h.Handle(t.Context(), req).Allowed {
						t.Fatal("other namespace affected")
					}
					req.Namespace = "dev"
					req.UserInfo.Groups = nil
					*reads = 0
					if !h.Handle(t.Context(), req).Allowed || *reads != 0 {
						t.Fatal("controller/unselected audience read API or denied")
					}
				})
			}
		}
	}
}

func TestGVKPatternsAndSubresources(t *testing.T) {
	for _, tc := range []struct {
		apiVersion, kind, sub string
		allow                 bool
		parent                bool
	}{
		{"apps", "Deploy*", "scale", false, false},
		{"apps/*", "*Set", "scale", true, false},
		{"batch/*", "*", "scale", true, false},
		{"*", "*", "", false, false},
		{"*", "*", "status", false, false},
		{"*", "*", "scale", false, true},
	} {
		t.Run(fmt.Sprintf("%s/%s/%s/parent=%t", tc.apiVersion, tc.kind, tc.sub, tc.parent), func(t *testing.T) {
			h, _ := handler(t)
			a := testutil.Action()
			a.Generation++
			a.Spec.Targets[0].APIVersion, a.Spec.Targets[0].Kind, a.Spec.Targets[0].Selector = tc.apiVersion, tc.kind, metav1.LabelSelector{}
			a.Spec.Admission[0].Subresources = []string{"", "*"}
			if tc.parent {
				a.Spec.Admission[0].Message = "Frozen {{ .target.kind }} {{ .target.metadata.name }}"
			}
			if err := h.Store.Upsert(a); err != nil {
				t.Fatal(err)
			}
			req := request(admissionv1.Update)
			req.SubResource = tc.sub
			if tc.sub != "" {
				req.Kind = metav1.GroupVersionKind{Group: "autoscaling", Version: "v1", Kind: "Scale"}
				req.Object.Raw = []byte(`{"apiVersion":"autoscaling/v1","kind":"Scale","spec":{"replicas":1}}`)
			}
			parent := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "apps/v1", "kind": "Deployment", "metadata": map[string]any{"name": "app", "namespace": "dev"}}}
			d := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), parent)
			h.Dynamic = d
			response := h.Handle(t.Context(), req)
			if response.Allowed != tc.allow {
				t.Fatalf("%+v", response)
			}
			if tc.parent {
				if len(d.Actions()) != 1 || response.Result.Message != "Frozen Deployment app" {
					t.Fatalf("parent context: %+v, reads=%v", response, d.Actions())
				}
			} else if len(d.Actions()) != 0 {
				t.Fatal("unneeded parent read")
			}
		})
	}
}

func TestWildcardWarningAndScope(t *testing.T) {
	h, reads := handler(t)
	a := testutil.Action()
	a.Generation++
	a.Spec.Targets[0].APIVersion, a.Spec.Targets[0].Kind, a.Spec.Targets[0].Selector = "*", "*", metav1.LabelSelector{}
	a.Spec.Admission[0].Subresources = []string{"*"}
	a.Spec.Config.Admission.Warnings = []api.AdmissionWarning{{Name: "notice", When: "Active", Message: "Wildcard notice", RequestMatch: a.Spec.Admission[0].RequestMatch}}
	a.Spec.Admission = nil
	if err := h.Store.Upsert(a); err != nil {
		t.Fatal(err)
	}
	req := request(admissionv1.Update)
	if r := h.Handle(t.Context(), req); !r.Allowed || len(r.Warnings) != 0 {
		t.Fatal("subresource wildcard matched parent")
	}
	req.SubResource = "scale"
	if r := h.Handle(t.Context(), req); !r.Allowed || len(r.Warnings) != 1 {
		t.Fatal("wildcard warning missing")
	}
	for _, resource := range []string{"nodes", "namespaces", "changewindows"} {
		req.Namespace = ""
		req.SubResource = ""
		req.Resource = metav1.GroupVersionResource{Version: "v1", Resource: resource}
		*reads = 0
		if r := h.Handle(t.Context(), req); !r.Allowed || len(r.Warnings) != 0 || *reads != 0 {
			t.Fatal("wildcard selected cluster-scoped resource")
		}
	}
}
