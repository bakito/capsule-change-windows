// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"fmt"
	"iter"
	"sort"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"

	api "github.com/projectcapsule/schedule-action-controller/api/v1alpha1"
)

// Store publishes immutable compiled policies indexed by the parent GVR. Status
// updates do not recompile. Deletion retires all references; no request snapshots
// or authorization decisions are cached here.
type Store struct {
	mu       sync.RWMutex
	policies map[string]*Compiled
	index    map[schema.GroupVersionResource][]*Compiled
	patterns []*Compiled
	errors   map[string]error
	Mapper   meta.RESTMapper
}

func NewStore(mapper meta.RESTMapper) *Store {
	return &Store{Mapper: mapper, policies: map[string]*Compiled{}, index: map[schema.GroupVersionResource][]*Compiled{}, errors: map[string]error{}}
}

func (s *Store) Upsert(obj *api.ChangeWindow) error {
	s.mu.RLock()
	prev := s.policies[obj.Name]
	s.mu.RUnlock()
	if prev != nil && prev.Source.UID == obj.UID && prev.Source.Generation == obj.Generation {
		return nil
	}
	c, err := Compile(obj, s.Mapper)
	if err == nil {
		c.Engine.At(time.Now())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.errors[obj.Name] = err
		return err
	}
	delete(s.errors, obj.Name)
	s.policies[obj.Name] = c
	s.rebuild()
	return nil
}

func (s *Store) Delete(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.policies, name)
	delete(s.errors, name)
	s.rebuild()
}

func (s *Store) rebuild() {
	idx := map[schema.GroupVersionResource][]*Compiled{}
	var patterns []*Compiled
	for _, c := range s.policies {
		seen := map[schema.GroupVersionResource]bool{}
		hasPattern := false
		for _, t := range c.Targets {
			if t.Pattern != nil {
				hasPattern = true
				continue
			}
			if !seen[t.Resource] {
				idx[t.Resource] = append(idx[t.Resource], c)
				seen[t.Resource] = true
			}
		}
		if hasPattern {
			patterns = append(patterns, c)
		}
	}
	for _, list := range idx {
		sort.Slice(list, func(i, j int) bool { return list[i].Source.Name < list[j].Source.Name })
	}
	sort.Slice(patterns, func(i, j int) bool { return patterns[i].Source.Name < patterns[j].Source.Name })
	s.index = idx
	s.patterns = patterns
}

// CandidateSet merges immutable exact and pattern indexes without allocating a
// result per request. Only pattern policies need checking beyond the exact GVR.
type CandidateSet struct{ exact, patterns []*Compiled }

func (c CandidateSet) Empty() bool { return len(c.exact) == 0 && len(c.patterns) == 0 }

func (c CandidateSet) All() iter.Seq[*Compiled] {
	return func(yield func(*Compiled) bool) {
		i, j := 0, 0
		for i < len(c.exact) || j < len(c.patterns) {
			var next *Compiled
			switch {
			case j == len(c.patterns) || (i < len(c.exact) && c.exact[i].Source.Name < c.patterns[j].Source.Name):
				next = c.exact[i]
				i++
			case i == len(c.exact) || c.patterns[j].Source.Name < c.exact[i].Source.Name:
				next = c.patterns[j]
				j++
			default:
				next = c.exact[i]
				i++
				j++
			}
			if !yield(next) {
				return
			}
		}
	}
}

// Candidates returns a read-only snapshot; published slices are never modified.
func (s *Store) Candidates(gvr schema.GroupVersionResource) (CandidateSet, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.errors) > 0 {
		return CandidateSet{}, fmt.Errorf("a ChangeWindow could not be compiled; inspect controller status")
	}
	return CandidateSet{exact: s.index[gvr], patterns: s.patterns}, nil
}

// For returns the compiled snapshot for this exact generation, or compiles a
// transient value when reconciliation runs ahead of the informer callback.
func (s *Store) For(obj *api.ChangeWindow) (*Compiled, error) {
	s.mu.RLock()
	c := s.policies[obj.Name]
	s.mu.RUnlock()
	if c != nil && c.Source.UID == obj.UID && c.Source.Generation == obj.Generation {
		return c, nil
	}
	return Compile(obj, s.Mapper)
}
