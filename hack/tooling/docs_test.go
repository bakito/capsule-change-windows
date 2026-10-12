//go:build tooling

// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tooling_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func hugoFixture(t *testing.T) (string, []string) {
	t.Helper()
	dir, env := fixture(t)
	copyFile(t, repository(t), dir, "docs/package.json")
	copyFile(t, repository(t), dir, "hack/check-hugo.sh")
	write(t, filepath.Join(dir, "tools.log"), "")
	write(t, filepath.Join(dir, "hack/docs-api.sh"), `#!/usr/bin/env bash
printf 'generate\t%s\n' "$@" >> "$TOOL_LOG"
`)
	hugo := filepath.Join(dir, "tools/hugo")
	write(t, hugo, `#!/usr/bin/env bash
if [[ "$1" == version ]]; then
  printf '%s\n' "$TEST_HUGO_VERSION"
  exit "${TEST_HUGO_VERSION_EXIT:-0}"
fi
printf 'hugo\t%s\n' "$@" >> "$TOOL_LOG"
`)
	if err := os.Chmod(hugo, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir, env
}

func TestHugoVersionCheck(t *testing.T) {
	for _, tc := range []struct {
		name, version, exit string
		accepted            bool
	}{
		{name: "old-homebrew", version: "hugo v0.134.0+extended darwin/arm64 VendorInfo=brew"},
		{name: "older-patch", version: "hugo v0.165.9+extended linux/amd64"},
		{name: "standard-edition", version: "hugo v0.166.0 linux/amd64"},
		{name: "missing", exit: "127"},
		{name: "malformed", version: "unexpected output"},
		{name: "version-failed", version: "hugo v0.166.0+extended linux/amd64", exit: "1"},
		{name: "pinned", version: "hugo v0.166.0-78400b4+extended darwin/arm64", accepted: true},
		{name: "newer-patch", version: "hugo v0.166.1+extended linux/amd64", accepted: true},
		{name: "newer-minor", version: "hugo v0.167.0+extended+withdeploy linux/amd64", accepted: true},
		{name: "newer-major", version: "hugo v1.0.0+extended linux/amd64", accepted: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, env := hugoFixture(t)
			env = append(env, "TEST_HUGO_VERSION="+tc.version, "TEST_HUGO_VERSION_EXIT="+tc.exit)
			out, err := command(t, dir, env, "bash", "hack/check-hugo.sh")
			if (err == nil) != tc.accepted {
				t.Fatalf("accepted=%v, error=%v, output=%s", tc.accepted, err, out)
			}
			if !tc.accepted && (!strings.Contains(out, "requires Hugo Extended >= 0.166.0") ||
				!strings.Contains(out, "devbox run -- task docs:serve")) {
				t.Fatalf("missing actionable version error: %s", out)
			}
		})
	}
}

func TestDocumentationTasksCheckHugoBeforeWork(t *testing.T) {
	for _, task := range []string{"docs:serve", "docs:build", "docs:check", "docs:test:browser"} {
		t.Run(task, func(t *testing.T) {
			dir, env := hugoFixture(t)
			out, err := command(t, dir, append(env, "TEST_HUGO_VERSION=hugo v0.134.0+extended darwin/arm64"), "task", task)
			if err == nil || !strings.Contains(out, "requires Hugo Extended") {
				t.Fatalf("old Hugo did not stop %s: %v\n%s", task, err, out)
			}
			if log := logContents(t, dir); log != "" {
				t.Fatalf("unsupported Hugo still generated/built files: %s", log)
			}
			successful(t, dir, append(env, "TEST_HUGO_VERSION=hugo v0.166.0+extended linux/amd64"), "task", task)
			log := logContents(t, dir)
			if !strings.Contains(log, "npm\tci\t--ignore-scripts") {
				t.Fatalf("Doks dependencies were not installed: %s", log)
			}
			if task != "docs:test:browser" && !strings.Contains(log, "generate\t") {
				t.Fatalf("compatible Hugo did not continue %s: %s", task, log)
			}
			if task == "docs:check" && !strings.Contains(log, "go\ttest") ||
				task == "docs:test:browser" && !strings.Contains(log, "npm\t--prefix\tdocs\trun\ttest:browser") ||
				task == "docs:build" && !strings.Contains(log, "npm\t--prefix\tdocs\trun\tbuild") ||
				task == "docs:serve" && !strings.Contains(log, "npm\t--prefix\tdocs\trun\tdev") {
				t.Fatalf("compatible Hugo did not run %s: %s", task, log)
			}
		})
	}
}

func TestHugoVersionRequirementsAgree(t *testing.T) {
	root := repository(t)
	var config struct {
		Packages map[string]string `json:"packages"`
	}
	data, err := os.ReadFile(filepath.Join(root, "devbox.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	version := config.Packages["hugo"]
	for file, expected := range map[string]string{
		"docs/hugo.toml":     "min = '" + version + "'",
		"hack/check-hugo.sh": "minimum=" + version + "\n",
	} {
		data, err := os.ReadFile(filepath.Join(root, file))
		if err != nil || !strings.Contains(string(data), expected) {
			t.Fatalf("%s must match the Devbox Hugo pin %s: %v", file, version, err)
		}
	}
}

func TestDocumentationTasksStopOnInstallFailure(t *testing.T) {
	for _, task := range []string{"docs:serve", "docs:build", "docs:check", "docs:test:browser"} {
		t.Run(task, func(t *testing.T) {
			dir, env := hugoFixture(t)
			env = append(env, "TEST_HUGO_VERSION=hugo v0.166.0+extended linux/amd64", "FAIL_TOOL=npm")
			if out, err := command(t, dir, env, "task", task); err == nil {
				t.Fatalf("failed dependency installation did not stop %s: %s", task, out)
			}
			log := logContents(t, dir)
			if !strings.Contains(log, "npm\tci") || strings.Contains(log, "generate\t") || strings.Contains(log, "hugo\t") || strings.Contains(log, "go\ttest") {
				t.Fatalf("unexpected work after installation failure: %s", log)
			}
		})
	}
}
