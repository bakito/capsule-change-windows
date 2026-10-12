//go:build tooling

// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tooling_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

type headlampWorkflow struct {
	On map[string]struct {
		Tags []string `yaml:"tags"`
	} `yaml:"on"`
	Permissions map[string]string `yaml:"permissions"`
	Jobs        map[string]struct {
		Needs       []string          `yaml:"needs"`
		Permissions map[string]string `yaml:"permissions"`
		Concurrency struct {
			Group  string `yaml:"group"`
			Cancel bool   `yaml:"cancel-in-progress"`
		} `yaml:"concurrency"`
		Steps []struct {
			Uses string            `yaml:"uses"`
			With map[string]string `yaml:"with"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

func TestHeadlampWorkflowPublicationBoundary(t *testing.T) {
	root := repository(t)
	read := func(name string) headlampWorkflow {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(root, ".github/workflows", name))
		if err != nil {
			t.Fatal(err)
		}
		var workflow headlampWorkflow
		if err := yaml.Unmarshal(data, &workflow); err != nil {
			t.Fatal(err)
		}
		return workflow
	}
	pr, release := read("ci.yml"), read("releaser.yml")
	if _, ok := pr.On["pull_request"]; !ok {
		t.Fatal("Headlamp checks do not run on PRs")
	}
	if _, ok := pr.On["pull_request_target"]; ok {
		t.Fatal("untrusted plugin code must not run under pull_request_target")
	}
	for _, job := range pr.Jobs {
		for _, permission := range job.Permissions {
			if permission == "write" {
				t.Fatal("PR checks have write permissions")
			}
		}
		for _, step := range job.Steps {
			if strings.Contains(step.Uses, "headlamp-publish@") {
				t.Fatal("PR workflow includes publication")
			}
		}
	}
	if pr.Permissions["contents"] != "read" || release.Permissions["contents"] != "read" ||
		len(release.On) != 1 || !slices.Equal(release.On["push"].Tags, []string{"v*"}) {
		t.Fatal("release trigger or default token scope is incorrect")
	}
	publish := release.Jobs["headlamp-publish"]
	if !slices.Equal(publish.Needs, []string{"release"}) || publish.Permissions["contents"] != "write" {
		t.Fatal("plugin publication must wait for the controller release without rerunning PR checks")
	}
	if publish.Concurrency.Group != "headlamp-plugin-publication" || publish.Concurrency.Cancel {
		t.Fatal("metadata branch writers must serialize across tags without cancellation")
	}
	node, err := os.ReadFile(filepath.Join(root, "headlamp/.node-version"))
	if err != nil {
		t.Fatal(err)
	}
	shared := regexp.MustCompile(`^projectcapsule/github-actions/headlamp-(test|publish)@([a-f0-9]{40})$`)
	var revision string
	for name, workflow := range map[string]headlampWorkflow{"pr": pr, "release": release} {
		checks, publications := 0, 0
		for _, job := range workflow.Jobs {
			for _, step := range job.Steps {
				if !strings.Contains(step.Uses, "/headlamp-") {
					continue
				}
				match := shared.FindStringSubmatch(step.Uses)
				if match == nil {
					t.Fatalf("shared action must be SHA pinned: %s", step.Uses)
				}
				if revision != "" && revision != match[2] {
					t.Fatal("Headlamp actions use inconsistent shared revisions")
				}
				revision = match[2]
				if step.With["build-root"] != "headlamp/" || step.With["node-version"] != strings.TrimSpace(string(node)) {
					t.Fatal("shared action lost its plugin directory or pinned Node version")
				}
				if match[1] == "test" {
					checks++
				} else {
					publications++
					if step.With["target-branch"] != "headlamp-plugin" || step.With["metadata-template"] != "artifacthub-pkg.yml" {
						t.Fatal("publication branch or first-release metadata is incorrect")
					}
				}
			}
		}
		if (name == "pr" && (checks != 1 || publications != 0)) || (name == "release" && (checks != 0 || publications != 1)) {
			t.Fatalf("%s: checks=%d publications=%d", name, checks, publications)
		}
	}
}

func TestHeadlampMetadataMatchesPackage(t *testing.T) {
	root := filepath.Join(repository(t), "headlamp")
	data, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	var pkg struct{ Name string }
	if err := json.Unmarshal(data, &pkg); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(filepath.Join(root, "artifacthub-pkg.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var metadata struct {
		Name        string
		Annotations map[string]string
	}
	if err := yaml.Unmarshal(data, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.Name != pkg.Name || metadata.Annotations["headlamp/plugin/version-compat"] == "" || metadata.Annotations["headlamp/plugin/distro-compat"] == "" {
		t.Fatal("first release metadata does not describe the plugin")
	}
}
