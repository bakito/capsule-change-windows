// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"encoding/json"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakediscovery "k8s.io/client-go/discovery/fake"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"

	api "github.com/projectcapsule/schedule-action-controller/api/v1alpha1"
	"github.com/projectcapsule/schedule-action-controller/internal/policy"
	"github.com/projectcapsule/schedule-action-controller/internal/schedule"
)

func BenchmarkPrepareScaleCapture(b *testing.B) {
	for _, zero := range []bool{false, true} {
		name := "nonzero"
		want := "3"
		spec := map[string]any{"replicas": int64(3)}
		if zero {
			name = "omitted-zero"
			want = "0"
			spec = map[string]any{}
		}
		b.Run(name, func(b *testing.B) {
			scale := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "autoscaling/v1", "kind": "Scale", "metadata": map[string]any{"uid": "uid"}, "spec": spec}}
			d := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
			d.PrependReactor("get", "deployments", func(clienttesting.Action) (bool, runtime.Object, error) { return true, scale.DeepCopy(), nil })
			c := &Client{Dynamic: d}
			target := policy.Target{Spec: api.Target{APIVersion: "apps/v1", Kind: "Deployment"}, Resource: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}}
			obj := unstructured.Unstructured{Object: map[string]any{"metadata": map[string]any{"uid": "uid", "name": "app", "namespace": "dev"}}}
			a := api.ResourceAction{Name: "capture", Capture: map[string]api.Capture{"replicas": {Subresource: "scale", Path: "/spec/replicas"}}}
			now := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
			b.ReportAllocs()
			for b.Loop() {
				p, err := PrepareParticipant(b.Context(), c, target, obj, a, schedule.Window{}, now)
				if err != nil || string(p.Captured["replicas"]) != want {
					b.Fatalf("capture=%s error=%v", p.Captured["replicas"], err)
				}
				d.ClearActions()
			}
		})
	}
}

func TestGetScaleZeroReplicas(t *testing.T) {
	for _, tc := range []struct {
		name, sub, kind, version, body string
		want                           any
		invalid                        bool
	}{
		{"omitted zero", "scale", "Scale", "autoscaling/v1", `{"spec":{}}`, int64(0), false},
		{"omitted spec", "scale", "Scale", "autoscaling/v1", `{}`, int64(0), false},
		{"explicit zero", "scale", "Scale", "autoscaling/v1", `{"spec":{"replicas":0}}`, int64(0), false},
		{"positive", "scale", "Scale", "autoscaling/v1", `{"spec":{"replicas":3}}`, int64(3), false},
		{"malformed", "scale", "Scale", "autoscaling/v1", `{"spec":{"replicas":"unknown"}}`, nil, true},
		{"parent", "", "Deployment", "apps/v1", `{"spec":{}}`, nil, false},
		{"other endpoint", "status", "Scale", "autoscaling/v1", `{"spec":{}}`, nil, false},
		{"other kind", "scale", "CustomScale", "example.dev/v1", `{"spec":{}}`, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var body map[string]any
			if err := json.Unmarshal([]byte(tc.body), &body); err != nil {
				t.Fatal(err)
			}
			// Convert JSON numbers to the representation returned by a dynamic client.
			if spec, ok := body["spec"].(map[string]any); ok {
				if v, ok := spec["replicas"].(float64); ok {
					spec["replicas"] = int64(v)
				}
			}
			obj := &unstructured.Unstructured{Object: body}
			obj.SetAPIVersion(tc.version)
			obj.SetKind(tc.kind)
			d := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
			d.PrependReactor("get", "deployments", func(action clienttesting.Action) (bool, runtime.Object, error) {
				if action.GetSubresource() != tc.sub {
					t.Error("wrong subresource")
				}
				return true, obj.DeepCopy(), nil
			})
			c := &Client{Dynamic: d}
			p := Participant{Resource: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, Namespace: "dev", Name: "app"}
			got, err := c.Get(t.Context(), p, tc.sub)
			if (err != nil) != tc.invalid {
				t.Fatalf("invalid=%t error=%v", tc.invalid, err)
			}
			if err != nil {
				return
			}
			value, _, err := unstructured.NestedFieldNoCopy(got.Object, "spec", "replicas")
			if err != nil || value != tc.want {
				t.Fatalf("got %v want %v error %v", value, tc.want, err)
			}
			if len(d.Actions()) != 1 {
				t.Fatal("redundant API reads")
			}
		})
	}
}

func TestPrepare(t *testing.T) {
	discovery := &fakediscovery.FakeDiscovery{Fake: &clienttesting.Fake{Resources: []*metav1.APIResourceList{{GroupVersion: "apps/v1", APIResources: []metav1.APIResource{{Name: "deployments/scale", Group: "autoscaling", Version: "v1", Kind: "Scale", Verbs: []string{"get", "patch"}}}}}}}
	c := &Client{Discovery: discovery}
	p := Participant{Resource: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, APIVersion: "apps/v1", Kind: "Deployment", Name: "app", Namespace: "dev", UID: "uid"}
	for _, tc := range []struct {
		name, sub, body string
		valid           bool
	}{
		{"parent", "", "spec:\n  replicas: 0", true},
		{"scale", "scale", "spec:\n  replicas: 0", true},
		{"rollout", "rollout", "spec: {}", false},
		{"redirect name", "", "metadata:\n  name: another", false},
		{"redirect namespace", "", "metadata:\n  namespace: another", false},
		{"redirect kind", "scale", "kind: Deployment", false},
		{"resource version", "", "metadata:\n  resourceVersion: '100'", false},
		{"invalid yaml", "", "spec: [", false},
		{"missing capture", "", "spec:\n  replicas: {{ .captured.missing }}", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prepared, err := c.Prepare(t.Context(), p, &api.ActionStep{Apply: api.Apply{Subresource: tc.sub, Template: tc.body}}, map[string]any{})
			if (err == nil) != tc.valid {
				t.Fatalf("prepared=%+v err=%v", prepared, err)
			}
			if tc.valid && tc.sub == "scale" && (prepared.Object["kind"] != "Scale" || prepared.Object["apiVersion"] != "autoscaling/v1") {
				t.Fatalf("wrong scale GVK %+v", prepared)
			}
		})
	}
}

func TestPreparePreservesIntegers(t *testing.T) {
	p := Participant{APIVersion: "example.dev/v1", Kind: "Counter", Name: "counter", Namespace: "dev"}
	prepared, err := (&Client{}).Prepare(t.Context(), p, &api.ActionStep{Apply: api.Apply{Template: "spec:\n  value: 9007199254740993"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	value := prepared.Object["spec"].(map[string]any)["value"]
	if value != int64(9007199254740993) {
		t.Fatalf("integer lost precision: %#v", value)
	}
}
