// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

// Package executor performs identity-constrained server-side apply as an explicit service account.
package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/go-openapi/jsonpointer"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	kjson "k8s.io/apimachinery/pkg/util/json"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/yaml"

	api "github.com/projectcapsule/schedule-action-controller/api/v1alpha1"
	"github.com/projectcapsule/schedule-action-controller/internal/metrics"
	"github.com/projectcapsule/schedule-action-controller/internal/policy"
	"github.com/projectcapsule/schedule-action-controller/internal/schedule"
)

const MaxParticipants = 256

type Participant struct {
	Resource   schema.GroupVersionResource `json:"resource"`
	APIVersion string                      `json:"apiVersion"`
	Kind       string                      `json:"kind"`
	Namespace  string                      `json:"namespace"`
	Name       string                      `json:"name"`
	UID        types.UID                   `json:"uid"`
	Action     string                      `json:"action"`
	Captured   map[string]json.RawMessage  `json:"captured,omitempty"`
	Start      *Prepared                   `json:"start,omitempty"`
	End        *Prepared                   `json:"end,omitempty"`
	// Attempted is persisted before applying; a lost response still requires restore.
	Attempted bool `json:"attempted,omitempty"`
	Started   bool `json:"started,omitempty"`
	Finished  bool `json:"finished,omitempty"`
}

type Prepared struct {
	Subresource string         `json:"subresource,omitempty"`
	Force       bool           `json:"force,omitempty"`
	Object      map[string]any `json:"object"`
}

type Interface interface {
	List(context.Context, policy.Target, string) ([]unstructured.Unstructured, error)
	Get(context.Context, Participant, string) (*unstructured.Unstructured, error)
	Prepare(context.Context, Participant, *api.ActionStep, map[string]any) (*Prepared, error)
	Apply(context.Context, Participant, *Prepared, string) (bool, error)
}
type Factory func(api.ServiceAccountReference, types.UID) (Interface, error)

type Client struct {
	Dynamic      dynamic.Interface
	Discovery    discovery.DiscoveryInterface
	FieldManager string
}

func NewFactory(config *rest.Config, disc discovery.DiscoveryInterface) Factory {
	return func(sa api.ServiceAccountReference, uid types.UID) (Interface, error) {
		cfg := rest.CopyConfig(config)
		cfg.Impersonate = rest.ImpersonationConfig{UserName: "system:serviceaccount:" + sa.Namespace + ":" + sa.Name, Extra: map[string][]string{policy.ExecutionExtra: {string(uid)}}}
		cfg.Timeout = 15 * time.Second
		d, err := dynamic.NewForConfig(cfg)
		if err != nil {
			return nil, err
		}
		return &Client{Dynamic: d, Discovery: disc, FieldManager: "change-window-" + string(uid)}, nil
	}
}

func (c *Client) List(ctx context.Context, t policy.Target, namespace string) ([]unstructured.Unstructured, error) {
	list, err := c.Dynamic.Resource(t.Resource).Namespace(namespace).List(ctx, metav1.ListOptions{LabelSelector: t.Selector.String(), Limit: MaxParticipants + 1})
	if err != nil {
		return nil, err
	}
	if list.GetContinue() != "" || len(list.Items) > MaxParticipants {
		return nil, fmt.Errorf("target selection exceeds %d participants", MaxParticipants)
	}
	return list.Items, nil
}

func (c *Client) Get(ctx context.Context, p Participant, sub string) (*unstructured.Unstructured, error) {
	subs := []string{}
	if sub != "" {
		subs = append(subs, sub)
	}
	obj, err := c.Dynamic.Resource(p.Resource).Namespace(p.Namespace).Get(ctx, p.Name, metav1.GetOptions{}, subs...)
	if err != nil {
		return nil, err
	}
	// ScaleSpec.Replicas is an int32 with omitempty: zero may be absent on the
	// wire. Restore its API-defined zero value before JSON-pointer captures.
	// Never default arbitrary missing fields or other subresource payloads.
	if sub == "scale" && obj.GetAPIVersion() == "autoscaling/v1" && obj.GetKind() == "Scale" {
		_, found, err := unstructured.NestedInt64(obj.Object, "spec", "replicas")
		if err != nil {
			return nil, fmt.Errorf("reading scale replicas: %w", err)
		}
		if !found {
			if err := unstructured.SetNestedField(obj.Object, int64(0), "spec", "replicas"); err != nil {
				return nil, err
			}
		}
	}
	return obj, nil
}

func (c *Client) Prepare(_ context.Context, p Participant, step *api.ActionStep, data map[string]any) (*Prepared, error) {
	if step == nil {
		return nil, nil
	}
	t, err := policy.Parse(step.Apply.Template)
	if err != nil {
		return nil, err
	}
	rendered, err := policy.Render(t, data, 65536)
	if err != nil {
		return nil, fmt.Errorf("rendering action: %w", err)
	}
	obj := map[string]any{}
	raw, err := yaml.YAMLToJSONStrict([]byte(rendered))
	if err != nil {
		return nil, fmt.Errorf("decoding apply template: %w", err)
	}
	if err := kjson.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("decoding apply template: %w", err)
	}
	if len(obj) == 0 {
		return nil, fmt.Errorf("apply template must be an object")
	}
	apiVersion, kind := p.APIVersion, p.Kind
	if sub := step.Apply.Subresource; sub != "" {
		resources, err := c.Discovery.ServerResourcesForGroupVersion(p.Resource.GroupVersion().String())
		if err != nil {
			return nil, err
		}
		found := false
		for _, r := range resources.APIResources {
			if r.Name == p.Resource.Resource+"/"+sub {
				if !contains(r.Verbs, "patch") {
					return nil, fmt.Errorf("subresource %s does not support patch", sub)
				}
				kind = r.Kind
				if r.Group != "" || r.Version != "" {
					apiVersion = schema.GroupVersion{Group: r.Group, Version: r.Version}.String()
				}
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("unsupported subresource %q on %s", sub, p.Resource)
		}
	}
	for key, value := range map[string]string{"apiVersion": apiVersion, "kind": kind} {
		if v, ok := obj[key]; ok && v != value {
			return nil, fmt.Errorf("template cannot redirect %s", key)
		}
		obj[key] = value
	}
	metadata, ok := obj["metadata"].(map[string]any)
	if obj["metadata"] != nil && !ok {
		return nil, fmt.Errorf("metadata must be an object")
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	for key, value := range map[string]string{"name": p.Name, "namespace": p.Namespace} {
		if v, exists := metadata[key]; exists && v != value {
			return nil, fmt.Errorf("template cannot redirect metadata.%s", key)
		}
		metadata[key] = value
	}
	for _, key := range []string{"uid", "resourceVersion", "generateName", "managedFields", "deletionTimestamp"} {
		if _, exists := metadata[key]; exists {
			return nil, fmt.Errorf("template cannot set metadata.%s", key)
		}
	}
	obj["metadata"] = metadata
	return &Prepared{Subresource: step.Apply.Subresource, Force: step.Apply.Force, Object: obj}, nil
}

func contains(v []string, s string) bool {
	for _, x := range v {
		if x == s {
			return true
		}
	}
	return false
}

// Apply never creates a resource. UID and resourceVersion preconditions protect
// deletion/recreation and concurrent writes, including subresource writes.
func (c *Client) Apply(ctx context.Context, p Participant, prepared *Prepared, phase string) (gone bool, err error) {
	started := time.Now()
	defer func() {
		result := "success"
		if err != nil {
			result = "error"
		} else if gone {
			result = "gone"
		}
		metrics.Actions.WithLabelValues(phase, result).Inc()
		metrics.ActionDuration.WithLabelValues(phase).Observe(time.Since(started).Seconds())
	}()
	if prepared == nil {
		return false, nil
	}
	live, err := c.Get(ctx, p, "")
	if apierrors.IsNotFound(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if live.GetUID() != p.UID {
		return true, nil
	}
	obj := (&unstructured.Unstructured{Object: prepared.Object}).DeepCopy()
	obj.SetUID(p.UID)
	if prepared.Subresource != "" {
		sub, err := c.Get(ctx, p, prepared.Subresource)
		if err != nil {
			return false, err
		}
		if sub.GetUID() != p.UID {
			return true, nil
		}
		obj.SetResourceVersion(sub.GetResourceVersion())
	} else {
		obj.SetResourceVersion(live.GetResourceVersion())
	}
	body, err := json.Marshal(obj.Object)
	if err != nil {
		return false, err
	}
	subs := []string{}
	if prepared.Subresource != "" {
		subs = append(subs, prepared.Subresource)
	}
	_, err = c.Dynamic.Resource(p.Resource).Namespace(p.Namespace).Patch(ctx, p.Name, types.ApplyPatchType, body, metav1.PatchOptions{FieldManager: c.FieldManager, Force: &prepared.Force}, subs...)
	return false, err
}

func PrepareParticipant(ctx context.Context, c Interface, target policy.Target, obj unstructured.Unstructured, a api.ResourceAction, w schedule.Window, now time.Time) (Participant, error) {
	p := Participant{Resource: target.Resource, APIVersion: target.Spec.APIVersion, Kind: target.Spec.Kind, Namespace: obj.GetNamespace(), Name: obj.GetName(), UID: obj.GetUID(), Action: a.Name, Captured: map[string]json.RawMessage{}}
	captured := map[string]any{}
	// Read each subresource at most once, and require the parent's UID throughout capture.
	snapshots := map[string]*unstructured.Unstructured{"": &obj}
	for name, ref := range a.Capture {
		snapshot := snapshots[ref.Subresource]
		if snapshot == nil {
			var err error
			snapshot, err = c.Get(ctx, p, ref.Subresource)
			if err != nil {
				return p, err
			}
			if snapshot.GetUID() != p.UID {
				return p, fmt.Errorf("resource replaced during capture")
			}
			snapshots[ref.Subresource] = snapshot
		}
		pointer, err := jsonpointer.New(ref.Path)
		if err != nil {
			return p, err
		}
		value, _, err := pointer.Get(snapshot.Object)
		if err != nil {
			return p, fmt.Errorf("capture %s: %w", name, err)
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return p, err
		}
		p.Captured[name] = raw
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.UseNumber()
		if err := decoder.Decode(&value); err != nil {
			return p, err
		}
		captured[name] = value
	}
	data := policy.Context(obj.Object, captured, w, now)
	var err error
	p.Start, err = c.Prepare(ctx, p, a.OnStart, data)
	if err != nil {
		return p, err
	}
	p.End, err = c.Prepare(ctx, p, a.OnEnd, data)
	return p, err
}
