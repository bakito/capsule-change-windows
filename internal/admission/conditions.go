// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package admission

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	kjson "k8s.io/apimachinery/pkg/util/json"
	cradmission "sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// conditionInput creates one request-local activation shared by all conditions.
// It never reads the API or enters the compiled policy cache.
func conditionInput(req cradmission.Request, current, old *unstructured.Unstructured, namespace *corev1.Namespace) (map[string]any, error) {
	object := func(obj *unstructured.Unstructured) any {
		if obj == nil {
			return nil
		}
		return obj.Object
	}
	// Exclude raw objects from request: they already have dedicated variables.
	request := req.AdmissionRequest
	request.Object, request.OldObject = runtime.RawExtension{}, runtime.RawExtension{}
	// Decode options only once, using integer-preserving JSON conversion.
	if len(request.Options.Raw) > 0 {
		var options map[string]any
		if err := kjson.Unmarshal(request.Options.Raw, &options); err != nil {
			return nil, err
		}
		request.Options = runtime.RawExtension{Object: &unstructured.Unstructured{Object: options}}
	}
	requestValue, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&request)
	if err != nil {
		return nil, err
	}
	delete(requestValue, "object")
	delete(requestValue, "oldObject")
	// Stable optional request fields, including requests decoded from older clients.
	requestValue["subResource"] = req.SubResource
	requestValue["requestSubResource"] = req.RequestSubResource
	requestValue["namespace"] = req.Namespace
	requestValue["dryRun"] = req.DryRun != nil && *req.DryRun
	requestValue["userInfo"] = map[string]any{"username": req.UserInfo.Username, "uid": req.UserInfo.UID, "groups": req.UserInfo.Groups, "extra": req.UserInfo.Extra}
	var namespaceValue any
	if namespace != nil {
		namespaceValue, err = runtime.DefaultUnstructuredConverter.ToUnstructured(namespace)
		if err != nil {
			return nil, err
		}
	}
	return map[string]any{"request": requestValue, "object": object(current), "oldObject": object(old), "namespaceObject": namespaceValue}, nil
}
