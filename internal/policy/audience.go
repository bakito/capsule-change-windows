// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"fmt"
	"strings"

	authenticationv1 "k8s.io/api/authentication/v1"
	"k8s.io/apimachinery/pkg/util/validation"

	api "github.com/projectcapsule/schedule-action-controller/api/v1alpha1"
)

type subjectSet struct {
	users, groups map[string]struct{}
}

type compiledAudience struct {
	include, exclude subjectSet
}

func compileAudience(a *api.Audience) (*compiledAudience, error) {
	if a == nil {
		return nil, nil
	}
	include, err := compileSubjects(a.Include)
	if err != nil {
		return nil, fmt.Errorf("audience.include: %w", err)
	}
	exclude, err := compileSubjects(a.Exclude)
	if err != nil {
		return nil, fmt.Errorf("audience.exclude: %w", err)
	}
	return &compiledAudience{include: include, exclude: exclude}, nil
}

func compileSubjects(subjects []api.Subject) (subjectSet, error) {
	s := subjectSet{}
	if len(subjects) > 64 {
		return s, fmt.Errorf("at most 64 subjects are allowed")
	}
	seen := make(map[api.Subject]bool, len(subjects))
	for _, subject := range subjects {
		if strings.TrimSpace(subject.Name) == "" || len(subject.Name) > 256 {
			return s, fmt.Errorf("subject name must contain 1 to 256 bytes")
		}
		if seen[subject] {
			return s, fmt.Errorf("duplicate subject %s %q", subject.Kind, subject.Name)
		}
		seen[subject] = true
		name := subject.Name
		switch subject.Kind {
		case "ServiceAccount":
			if len(validation.IsDNS1123Label(subject.Namespace)) != 0 || len(validation.IsDNS1123Subdomain(name)) != 0 {
				return s, fmt.Errorf("ServiceAccount requires a valid namespace and name")
			}
			name = "system:serviceaccount:" + subject.Namespace + ":" + name
		case "User", "Group":
			if subject.Namespace != "" {
				return s, fmt.Errorf("namespace is only valid for ServiceAccount subjects")
			}
		default:
			return s, fmt.Errorf("unsupported subject kind %q", subject.Kind)
		}
		if subject.Kind == "Group" {
			if s.groups == nil {
				s.groups = map[string]struct{}{}
			}
			s.groups[name] = struct{}{}
		} else {
			if s.users == nil {
				s.users = map[string]struct{}{}
			}
			s.users[name] = struct{}{}
		}
	}
	return s, nil
}

func (s subjectSet) matches(user authenticationv1.UserInfo) bool {
	if _, ok := s.users[user.Username]; ok {
		return true
	}
	if len(s.groups) == 0 {
		return false
	}
	for _, group := range user.Groups {
		if _, ok := s.groups[group]; ok {
			return true
		}
	}
	return false
}

func (m Message) MatchesAudience(user authenticationv1.UserInfo) bool {
	a := m.audience
	return a == nil || (!a.exclude.matches(user) && (len(a.include.users)+len(a.include.groups) == 0 || a.include.matches(user)))
}
