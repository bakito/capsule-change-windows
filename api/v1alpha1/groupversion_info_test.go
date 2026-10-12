// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

func TestChangeWindowWireIdentity(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	decoder := serializer.NewCodecFactory(scheme).UniversalDeserializer()
	for _, kind := range []string{"ChangeWindow", "ChangeWindowList"} {
		data := []byte(`{"apiVersion":"addons.projectcapsule.dev/v1alpha1","kind":"` + kind + `","metadata":{"name":"maintenance"}}`)
		obj, gvk, err := decoder.Decode(data, nil, nil)
		if err != nil || gvk == nil || *gvk != GroupVersion.WithKind(kind) {
			t.Fatalf("decode %s: object=%T gvk=%v error=%v", kind, obj, gvk, err)
		}
	}
	for _, kind := range []string{"ScheduledAction", "ScheduledActionList"} {
		if _, err := scheme.New(GroupVersion.WithKind(kind)); !runtime.IsNotRegisteredError(err) {
			t.Fatalf("legacy kind %s is still registered: %v", kind, err)
		}
	}
}
