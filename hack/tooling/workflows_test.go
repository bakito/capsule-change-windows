//go:build tooling

// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tooling_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

func TestWorkflowsUseExistingTaskTargets(t *testing.T) {
	root := repository(t)
	data, err := os.ReadFile(filepath.Join(root, "Taskfile.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var taskfile struct {
		Tasks map[string]struct {
			Aliases []string `json:"aliases"`
		} `json:"tasks"`
	}
	if err := yaml.Unmarshal(data, &taskfile); err != nil {
		t.Fatal(err)
	}
	targets := map[string]bool{}
	for name, task := range taskfile.Tasks {
		targets[name] = true
		for _, alias := range task.Aliases {
			targets[alias] = true
		}
	}
	files, err := filepath.Glob(filepath.Join(root, ".github/workflows/*.yml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("missing workflows: %v", err)
	}
	calls := 0
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, obsolete := range []string{"make ", "pip install", "makefile-target:", "charts/capsule", "./internal/controllers/..."} {
			if strings.Contains(string(data), obsolete) {
				t.Errorf("obsolete command %q in %s", obsolete, file)
			}
		}
		var workflow struct {
			Jobs map[string]struct {
				Steps []struct {
					Run  string `json:"run"`
					Uses string `json:"uses"`
				} `json:"steps"`
			} `json:"jobs"`
		}
		if err := yaml.Unmarshal(data, &workflow); err != nil {
			t.Fatal(err)
		}
		for _, job := range workflow.Jobs {
			for _, step := range job.Steps {
				if strings.HasPrefix(step.Uses, "./") {
					if _, err := os.Stat(filepath.Join(root, step.Uses, "action.yml")); err != nil {
						t.Errorf("missing local action %s in %s: %v", step.Uses, file, err)
					}
				}
				for _, line := range strings.Split(step.Run, "\n") {
					fields := strings.Fields(line)
					for i, field := range fields {
						if field != "task" {
							continue
						}
						for _, target := range fields[i+1:] {
							if strings.Contains(target, "=") || strings.HasPrefix(target, "-") {
								break
							}
							calls++
							if !targets[target] {
								t.Errorf("workflow %s calls missing task %q", file, target)
							}
						}
					}
				}
			}
		}
	}
	if calls < 20 {
		t.Fatalf("expected workflow build/check commands to use Task, found %d calls", calls)
	}
}
