//go:build tooling

// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tooling_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

func TestTaskShell(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repository(t), "Taskfile.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var taskfile struct {
		Tasks map[string]struct {
			Cmds []any
		}
	}
	if err := yaml.Unmarshal(data, &taskfile); err != nil {
		t.Fatal(err)
	}
	template := regexp.MustCompile("{{.*?}}")
	for name, task := range taskfile.Tasks {
		for index, command := range task.Cmds {
			source, ok := command.(string)
			if !ok {
				mapping, ok := command.(map[string]any)
				if !ok {
					t.Fatalf("unsupported command in %s: %T", name, command)
				}
				if _, ok := mapping["task"]; ok {
					continue // Task calls have no shell source.
				}
				source, ok = mapping["cmd"].(string)
				if !ok {
					t.Fatalf("missing shell command in %s: %v", name, mapping)
				}
			}
			t.Run(fmt.Sprintf("%s/%d", name, index), func(t *testing.T) {
				// Lint the actual Bash body, not just its enclosing here-document.
				if strings.HasPrefix(source, "bash -euo pipefail <<'BASH'\n") {
					source = strings.TrimPrefix(source, "bash -euo pipefail <<'BASH'\n")
					source = strings.TrimSuffix(strings.TrimSpace(source), "\nBASH")
				}
				// Task substitutes these before execution; forwarding/quoting is
				// exercised by the hook and command-behavior tests.
				source = template.ReplaceAllString(source, "task_argument")
				cmd := exec.Command("shellcheck", "--shell=bash", "-")
				cmd.Stdin = strings.NewReader("set -euo pipefail\n" + source + "\n")
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("inline shell failed ShellCheck: %v\n%s", err, out)
				}
			})
		}
	}
}

func TestE2EClusterCleanup(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprintf("existing=%t", existing), func(t *testing.T) {
			dir, env := fixture(t)
			kind := filepath.Join(dir, "tools/kind")
			write(t, kind, `#!/usr/bin/env bash
set -euo pipefail
printf 'kind' >> "$TOOL_LOG"
printf '\t%s' "$@" >> "$TOOL_LOG"
printf '\tkubeconfig=%s\n' "$KUBECONFIG" >> "$TOOL_LOG"
if [[ "$1 $2" == "get clusters" ]]; then
  if [[ "$EXISTING_CLUSTER" == true ]]; then printf 'isolated\n'; fi
elif [[ "$1 $2" == "create cluster" ]]; then
  [[ "$KUBECONFIG" != "$ORIGINAL_CONFIG" && -f "$KUBECONFIG" ]]
  printf '%s' "$KUBECONFIG" > "$PRIVATE_CONFIG_RECORD"
  exit 37
fi
`)
			if err := os.Chmod(kind, 0o755); err != nil {
				t.Fatal(err)
			}
			original := filepath.Join(dir, "developer kubeconfig")
			write(t, original, "developer context\n")
			privateRecord := filepath.Join(dir, "private-config")
			env = append(env, "KUBECONFIG="+original, "ORIGINAL_CONFIG="+original,
				"PRIVATE_CONFIG_RECORD="+privateRecord, fmt.Sprintf("EXISTING_CLUSTER=%t", existing))
			out, err := command(t, dir, env, "task", "e2e", "KIND_CLUSTER=isolated")
			if err == nil {
				t.Fatalf("expected setup failure, got success: %s", out)
			}
			log := logContents(t, dir)
			if existing {
				if !strings.Contains(out, "Refusing to reuse or delete existing cluster isolated") ||
					strings.Contains(log, "kind\tcreate") || strings.Contains(log, "kind\tdelete") {
					t.Fatalf("existing cluster was not preserved: %s\n%s", out, log)
				}
			} else {
				for _, call := range []string{"kind\tcreate\tcluster", "kind\texport\tlogs\t--name\tisolated", "kind\tdelete\tcluster\t--name\tisolated"} {
					if !strings.Contains(log, call) {
						t.Errorf("cleanup missing %q: %s", call, log)
					}
				}
				config, err := os.ReadFile(privateRecord)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(string(config)); !os.IsNotExist(err) {
					t.Fatalf("temporary credentials remained after failure: %v", err)
				}
			}
			data, err := os.ReadFile(original)
			if err != nil || string(data) != "developer context\n" {
				t.Fatalf("developer kubeconfig changed: %s %v", data, err)
			}
		})
	}
}

func TestHooksUseTaskTargets(t *testing.T) {
	root := repository(t)
	data, err := os.ReadFile(filepath.Join(root, "prek.toml"))
	if err != nil {
		t.Fatal(err)
	}
	entries := regexp.MustCompile("(?m)^entry = \"([^\"]+)\"$").FindAllStringSubmatch(string(data), -1)
	if len(entries) == 0 {
		t.Fatal("no hook entries")
	}
	out := successful(t, root, nil, "task", "--list-all", "--json")
	var list struct {
		Tasks []struct{ Name string }
	}
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		t.Fatal(err)
	}
	targets := make(map[string]bool)
	for _, task := range list.Tasks {
		targets[task.Name] = true
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry[1], "devbox run -- task ") {
			t.Fatalf("hook bypasses Task or the locked environment: %s", entry[1])
		}
		for _, target := range strings.Fields(strings.TrimPrefix(entry[1], "devbox run -- task ")) {
			if target == "--" {
				break
			}
			if !targets[target] {
				t.Fatalf("hook calls an unknown task %q", target)
			}
		}
	}
	scripts, err := filepath.Glob(filepath.Join(root, "hack/*.sh"))
	if err != nil || len(scripts) != 0 {
		t.Fatalf("task implementations must live in Taskfile.yml: %v %v", scripts, err)
	}
}

func TestManifestsTargetHelm(t *testing.T) {
	dir, env := fixture(t)
	successful(t, dir, env, "task", "manifests")
	log := logContents(t, dir)
	for _, want := range []string{
		"rbac:roleName=manager-role,fileName=manager-role.yaml",
		"output:crd:dir=charts/change-windows/crds",
		"output:rbac:dir=charts/change-windows/files",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("missing Helm output %q: %s", want, log)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "config")); !os.IsNotExist(err) {
		t.Fatalf("manifest generation must not recreate config/: %v", err)
	}
}
