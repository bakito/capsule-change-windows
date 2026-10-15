// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

// Package policy compiles immutable selectors and templates shared by reconciliation and admission.
package policy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"text/template"
	"text/template/parse"
	"time"

	capsuleruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"

	api "github.com/projectcapsule/schedule-action-controller/api/v1alpha1"
	"github.com/projectcapsule/schedule-action-controller/internal/schedule"
)

const ExecutionExtra = "addons.projectcapsule.dev/execution"

type (
	Target struct {
		Namespaced bool
		Spec       api.Target
		Selector   labels.Selector
		Resource   schema.GroupVersionResource
		Pattern    *capsuleruntime.VersionKind
	}
	Message struct {
		Name        string
		audience    *compiledAudience
		conditions  []condition
		Match       api.RequestMatch
		When        string
		Within      time.Duration
		Template    *template.Template
		NeedsTarget bool
	}
	Compiled struct {
		Source          *api.ChangeWindow
		Engine          *schedule.Engine
		Namespaces      []labels.Selector
		Targets         map[string]Target
		Rules, Warnings []Message
	}
)

func Compile(obj *api.ChangeWindow, mapper meta.RESTMapper) (*Compiled, error) {
	e, err := schedule.Compile(obj.Spec.Schedules)
	if err != nil {
		return nil, err
	}
	c := &Compiled{Source: obj.DeepCopy(), Engine: e, Targets: map[string]Target{}}
	c.Source.Status = api.ChangeWindowStatus{}
	obj = c.Source // Compile only the private copy; published match slices must not alias informer objects.
	compiler := &conditionCompiler{}
	if len(obj.Spec.NamespaceSelectors) > 16 {
		return nil, fmt.Errorf("at most 16 namespaceSelectors are allowed")
	}
	for _, s := range obj.Spec.NamespaceSelectors {
		sel, err := metav1.LabelSelectorAsSelector(&s)
		if err != nil {
			return nil, err
		}
		c.Namespaces = append(c.Namespaces, sel)
	}
	if len(obj.Spec.Targets) == 0 || len(obj.Spec.Targets) > 32 {
		return nil, fmt.Errorf("require 1 to 32 targets")
	}
	for _, t := range obj.Spec.Targets {
		if _, exists := c.Targets[t.Name]; exists || t.Name == "" {
			return nil, fmt.Errorf("target names must be unique and nonempty")
		}
		target, err := compileTarget(t, mapper)
		if err != nil {
			return nil, fmt.Errorf("target %s: %w", t.Name, err)
		}
		if target.Namespaced && len(obj.Spec.NamespaceSelectors) == 0 {
			return nil, fmt.Errorf("namespaced target %s requires namespaceSelectors", t.Name)
		}
		c.Targets[t.Name] = target
	}
	hasNamespaced := false
	for _, target := range c.Targets {
		hasNamespaced = hasNamespaced || target.Namespaced
	}
	if !hasNamespaced && len(obj.Spec.NamespaceSelectors) > 0 {
		return nil, fmt.Errorf("omit namespaceSelectors for Node-only windows")
	}
	if obj.Spec.ServiceAccount == nil || len(validation.IsDNS1123Label(obj.Spec.ServiceAccount.Namespace)) != 0 || len(validation.IsDNS1123Subdomain(obj.Spec.ServiceAccount.Name)) != 0 {
		return nil, fmt.Errorf("spec.serviceAccount requires a valid namespace and name")
	}
	names := map[string]bool{}
	for _, a := range obj.Spec.Actions {
		if a.Name == "" || names[a.Name] {
			return nil, fmt.Errorf("action names must be unique and nonempty")
		}
		names[a.Name] = true
		if err := c.references(a.Targets); err != nil {
			return nil, err
		}
		for _, name := range a.Targets {
			if c.Targets[name].Pattern != nil {
				return nil, fmt.Errorf("action %s requires concrete apiVersion and kind; GVK patterns are admission-only", a.Name)
			}
			if !c.Targets[name].Namespaced {
				return nil, fmt.Errorf("action %s cannot target Nodes; Node targets are admission-only", a.Name)
			}
		}
		if a.OnStart == nil && a.OnEnd == nil {
			return nil, fmt.Errorf("action %s needs onStart or onEnd", a.Name)
		}
		for _, capture := range a.Capture {
			if !strings.HasPrefix(capture.Path, "/") {
				return nil, fmt.Errorf("capture path must be a JSON pointer")
			}
			if err := subresource(capture.Subresource); err != nil {
				return nil, err
			}
		}
		for _, step := range []*api.ActionStep{a.OnStart, a.OnEnd} {
			if step != nil {
				if err := subresource(step.Apply.Subresource); err != nil {
					return nil, err
				}
				if _, err := Parse(step.Apply.Template); err != nil {
					return nil, err
				}
			}
		}
	}
	for _, r := range obj.Spec.Admission {
		if r.When != "" && r.When != "Active" && r.When != "Inactive" {
			return nil, fmt.Errorf("invalid admission when %q", r.When)
		}
		if r.Action != "" && r.Action != "Deny" {
			return nil, fmt.Errorf("only Deny admission actions are supported")
		}
		m, err := c.message(r.Name, r.RequestMatch, r.When, r.Message, compiler)
		if err != nil {
			return nil, err
		}
		c.Rules = append(c.Rules, m)
	}
	for _, w := range obj.Spec.Config.Admission.Warnings {
		if w.When != "Active" && w.When != "BeforeStart" && w.When != "BeforeEnd" {
			return nil, fmt.Errorf("invalid warning when %q", w.When)
		}
		if w.When == "Active" {
			if w.Within != nil {
				return nil, fmt.Errorf("active warning cannot have within")
			}
		} else if w.Within == nil || w.Within.Duration <= 0 || w.Within.Duration > schedule.MaxDuration {
			return nil, fmt.Errorf("warning within must be positive and <=168h")
		}
		m, err := c.message(w.Name, w.RequestMatch, w.When, w.Message, compiler)
		if err != nil {
			return nil, err
		}
		if w.Within != nil {
			m.Within = w.Within.Duration
		}
		c.Warnings = append(c.Warnings, m)
	}
	return c, nil
}

func subresource(s string) error {
	if strings.ContainsAny(s, "/.*") {
		return fmt.Errorf("invalid subresource %q", s)
	}
	return nil
}

func (c *Compiled) references(names []string) error {
	if len(names) == 0 {
		return fmt.Errorf("at least one target reference is required")
	}
	seen := map[string]bool{}
	for _, n := range names {
		if _, ok := c.Targets[n]; !ok {
			return fmt.Errorf("unknown target %q", n)
		}
		if seen[n] {
			return fmt.Errorf("duplicate target %q", n)
		}
		seen[n] = true
	}
	return nil
}

func (c *Compiled) message(name string, match api.RequestMatch, when, text string, compiler *conditionCompiler) (Message, error) {
	if err := c.references(match.Targets); err != nil {
		return Message{}, err
	}
	if len(match.Operations) == 0 {
		return Message{}, fmt.Errorf("operations must not be empty")
	}
	for _, op := range match.Operations {
		if !slices.Contains([]string{"CREATE", "UPDATE", "DELETE"}, op) {
			return Message{}, fmt.Errorf("unsupported operation %q", op)
		}
	}
	for _, s := range match.Subresources {
		if s == "*" { // Admission only; actions still require concrete endpoints.
			continue
		}
		if err := subresource(s); err != nil {
			return Message{}, err
		}
	}
	for _, target := range match.Targets {
		if !c.Targets[target].Namespaced {
			for _, sub := range match.Subresources {
				if sub != "" {
					return Message{}, fmt.Errorf("node admission only supports the parent resource")
				}
			}
		}
	}
	audience, err := compileAudience(match.Audience)
	if err != nil {
		return Message{}, fmt.Errorf("entry %s: %w", name, err)
	}
	conditions, err := compiler.compile(match.MatchConditions)
	if err != nil {
		return Message{}, fmt.Errorf("entry %s: %w", name, err)
	}
	t, err := Parse(text)
	if err != nil {
		return Message{}, err
	}
	needsTarget := false
	for _, definition := range t.Templates() {
		needsTarget = needsTarget || templateNeedsTarget(definition.Root)
	}
	return Message{Name: name, Match: match, When: when, Template: t, NeedsTarget: needsTarget, audience: audience, conditions: conditions}, nil
}

// Conservative analysis keeps parent reads lazy without changing template context.
// Only direct window/captured references are known not to need a resource. Dot,
// aliases, and rebased scopes may expose target, so they require the parent.
func templateNeedsTarget(n parse.Node) bool {
	switch n := n.(type) {
	case *parse.ListNode:
		if n != nil {
			for _, child := range n.Nodes {
				if templateNeedsTarget(child) {
					return true
				}
			}
		}
	case *parse.ActionNode:
		return templateNeedsTarget(n.Pipe)
	case *parse.PipeNode:
		if n != nil {
			for _, cmd := range n.Cmds {
				if templateNeedsTarget(cmd) {
					return true
				}
			}
		}
	case *parse.CommandNode:
		for _, arg := range n.Args {
			if templateNeedsTarget(arg) {
				return true
			}
		}
	case *parse.FieldNode:
		return len(n.Ident) == 0 || (n.Ident[0] != "window" && n.Ident[0] != "captured")
	case *parse.DotNode, *parse.VariableNode, *parse.ChainNode:
		return true
	case *parse.IfNode:
		return templateNeedsTarget(n.Pipe) || templateNeedsTarget(n.List) || templateNeedsTarget(n.ElseList)
	case *parse.RangeNode:
		return templateNeedsTarget(n.Pipe) || templateNeedsTarget(n.List) || templateNeedsTarget(n.ElseList)
	case *parse.WithNode:
		return true
	case *parse.TemplateNode:
		return templateNeedsTarget(n.Pipe)
	}
	return false
}

func (c *Compiled) SelectNamespace(set labels.Set) bool {
	for _, s := range c.Namespaces {
		if s.Matches(set) {
			return true
		}
	}
	return false
}

func Parse(text string) (*template.Template, error) {
	if text == "" || len(text) > 65536 {
		return nil, fmt.Errorf("template must contain 1 to 65536 bytes")
	}
	return template.New("action").Option("missingkey=error").Funcs(template.FuncMap{
		"toJson": func(v any) (string, error) { b, err := json.Marshal(v); return string(b), err },
		"quote":  strconv.Quote,
	}).Parse(text)
}

type boundedWriter struct {
	bytes.Buffer
	limit int
}

func (w *boundedWriter) Write(b []byte) (int, error) {
	if len(b) > w.limit-w.Len() {
		return 0, fmt.Errorf("template output exceeds %d bytes", w.limit)
	}
	return w.Buffer.Write(b)
}

var _ io.Writer = (*boundedWriter)(nil)

func Render(t *template.Template, data map[string]any, limit int) (string, error) {
	w := &boundedWriter{limit: limit}
	err := t.Execute(w, data)
	return w.String(), err
}

func Context(target map[string]any, captured map[string]any, w schedule.Window, now time.Time) map[string]any {
	phase := "Inactive"
	if w.Active {
		phase = "Active"
	}
	start, end := "", ""
	if !w.Start.IsZero() {
		start = w.Start.UTC().Format(time.RFC3339)
	}
	if !w.End.IsZero() {
		end = w.End.UTC().Format(time.RFC3339)
	}
	return map[string]any{"target": target, "captured": captured, "window": map[string]any{"start": start, "end": end, "phase": phase, "startsIn": w.Start.Sub(now).Round(time.Second).String()}}
}
