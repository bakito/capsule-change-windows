//go:build tooling

// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tooling_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func repository(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func write(t *testing.T, filename, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func copyFile(t *testing.T, root, destination, filename string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filename))
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(destination, filename), string(data))
}

func command(t *testing.T, dir string, environment []string, name string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), environment...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func successful(t *testing.T, dir string, environment []string, name string, args ...string) string {
	t.Helper()
	out, err := command(t, dir, environment, name, args...)
	if err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, out)
	}
	return out
}

func fixture(t *testing.T) (string, []string) {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := repository(t)
	for _, name := range []string{"Taskfile.yml", "prek.toml", "hack/prek-builtins.toml", "hack/dev/readiness.yaml"} {
		copyFile(t, root, dir, name)
	}
	successful(t, dir, nil, "git", "init", "-q")
	successful(t, dir, nil, "git", "config", "user.name", "Tooling Test")
	successful(t, dir, nil, "git", "config", "user.email", "tooling-test@example.invalid")
	for _, name := range []string{"charts/change-windows/crds", "charts/change-windows/files", "assets with spaces"} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	bin := filepath.Join(dir, "tools")
	for _, name := range []string{"devbox", "npm", "go", "controller-gen", "setup-envtest", "ko", "helm", "kubectl", "cosign", "goreleaser"} {
		filename := filepath.Join(bin, name)
		write(t, filename, `#!/usr/bin/env bash
set -euo pipefail
name="${0##*/}"
printf '%s' "$name" >> "$TOOL_LOG"
printf '\t%s' "$@" >> "$TOOL_LOG"
printf '\tassets=%s\n' "${KUBEBUILDER_ASSETS:-}" >> "$TOOL_LOG"
if [[ "$name" == ko ]]; then printf 'repository=%s\n' "${KO_DOCKER_REPO:-}" >> "$TOOL_LOG"; fi
if [[ "${FAIL_TOOL:-}" == "$name" ]]; then exit 37; fi
if [[ "$name" == devbox ]]; then
  [[ "$1" == run && "$2" == -- ]] || exit 1
  shift 2
  exec "$@"
fi
if [[ "$name" == go && "${4:-}" == crd-ref-docs ]]; then
  for arg in "$@"; do
    case "$arg" in
      --output-path=*) printf '# API Reference\n#### Example\nA generated type.\n' > "${arg#*=}" ;;
    esac
  done
fi
if [[ "$name" == go && "${1:-}" == build && "${2:-}" == -o ]]; then
  cat > "$3" <<'MANAGER'
#!/usr/bin/env bash
printf 'manager' >> "$TOOL_LOG"
printf '\t%s' "$@" >> "$TOOL_LOG"
printf '\n' >> "$TOOL_LOG"
if [[ "${WAIT_MANAGER:-}" == true ]]; then
  echo "$$" > "$MANAGER_PID_FILE"
  trap 'exit 0' TERM INT
  while true; do sleep 0.1; done
fi
MANAGER
  chmod +x "$3"
fi
if [[ "$name" == setup-envtest ]]; then printf '%s\n' "$ASSET_DIRECTORY"; fi
if [[ "$name" == go && "${1:-}" == env && "${2:-}" == GOHOSTARCH ]]; then printf 'arm64\n'; fi
if [[ "$name" == controller-gen && -n "${GENERATOR_STDOUT:-}" ]]; then printf '%s\n' "$GENERATOR_STDOUT"; fi
if [[ "$name" == helm && "${1:-}" == package ]]; then printf 'Packaged chart\n'; fi
if [[ "$name" == helm && "${1:-}" == push ]]; then printf 'Digest: %s\n' "${HELM_DIGEST:-invalid}"; fi
if [[ "$name" == ko && "${1:-}" == build ]]; then
  tag=latest
  for arg in "$@"; do
    case "$arg" in
      --tags=*) tag="${arg#*=}" ;;
      # ko records the index AND child manifests for multi-platform builds.
      --image-refs=*)
        printf '%s@sha256:%064d\n' "$KO_DOCKER_REPO" 0 > "${arg#*=}"
        if [[ "$*" == *linux/amd64,linux/arm64* ]]; then
          printf '%s@sha256:%064d\n' "$KO_DOCKER_REPO" 1 "$KO_DOCKER_REPO" 2 >> "${arg#*=}"
        fi ;;
      --tarball=*) printf 'test image archive\n' > "${arg#*=}" ;;
    esac
  done
  printf 'ko build progress\n' >&2
  if [[ "${KO_TEST_REFERENCE+x}" ]]; then
    printf '%s\n' "$KO_TEST_REFERENCE"
  else
    printf '%s:%s@sha256:%064d\n' "$KO_DOCKER_REPO" "$tag" 0
  fi
fi
`)
		if err := os.Chmod(filename, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write(t, filepath.Join(dir, "tools.log"), "")
	return dir, []string{
		"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"TOOL_LOG=" + filepath.Join(dir, "tools.log"),
		"ASSET_DIRECTORY=" + filepath.Join(dir, "assets with spaces"),
		"KUBEBUILDER_ASSETS=",
	}
}

func logContents(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "tools.log"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestTaskTestEnvironment(t *testing.T) {
	for _, tc := range []struct {
		name     string
		supplied bool
		relative bool
	}{
		{name: "download"},
		{name: "download-relative", relative: true},
		{name: "supplied", supplied: true},
		{name: "supplied-relative", supplied: true, relative: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, env := fixture(t)
			assets := filepath.Join(dir, "assets with spaces")
			argument := assets
			if tc.relative {
				argument = filepath.Base(assets)
			}
			env = append(env, "ASSET_DIRECTORY="+argument)
			if tc.supplied {
				env = append(env, "KUBEBUILDER_ASSETS="+argument)
			}
			successful(t, dir, env, "task", "test", "GO_TEST_FLAGS=-race -count=1", "ENVTEST_K8S_VERSION=1.36.0")
			log := logContents(t, dir)
			if !strings.Contains(log, "go\ttest\t-race\t-count=1\t./...\t-coverprofile=coverage.out\tassets="+assets) {
				t.Fatalf("test flags/assets were lost: %s", log)
			}
			if strings.Contains(log, "setup-envtest\t") == tc.supplied {
				t.Fatalf("wrong envtest downloader behavior: %s", log)
			}
			if strings.Count(log, "controller-gen\t") != 2 {
				t.Fatalf("generation did not precede tests: %s", log)
			}
		})
	}
}

func TestTaskFailurePropagation(t *testing.T) {
	for _, tool := range []string{"controller-gen", "setup-envtest", "go"} {
		t.Run(tool, func(t *testing.T) {
			dir, env := fixture(t)
			_, err := command(t, dir, append(env, "FAIL_TOOL="+tool), "task", "test")
			if err == nil {
				t.Fatal("failed tool was accepted")
			}
			if tool != "go" && strings.Contains(logContents(t, dir), "go\ttest") {
				t.Fatal("tests ran after a prerequisite failed")
			}
		})
	}
}

func TestTaskUnitAlias(t *testing.T) {
	dir, env := fixture(t)
	successful(t, dir, env, "task", "test-unit", "GO_TEST_FLAGS=-race -count=1")
	if !strings.Contains(logContents(t, dir), "go\ttest\t-race\t-count=1\t./...\t-skip\t^TestAPI$") {
		t.Fatal("unit-test alias lost its API-server exclusion")
	}
}

func TestEnterpriseTasks(t *testing.T) {
	for _, channel := range []string{"release", "prerelease"} {
		t.Run(channel, func(t *testing.T) {
			dir, env := fixture(t)
			out := successful(t, dir, env, "task", "enterprise:"+channel, "ENTERPRISE_VERSION=0.1.0-rc.1", "DRY_RUN=true")
			for _, value := range []string{"task image:push", "task helm:push", "VERSION=0.1.0-rc.1", "manager.image.reference="} {
				if !strings.Contains(out, value) {
					t.Fatalf("dry run missing %q: %s", value, out)
				}
			}
			if channel == "prerelease" && !strings.Contains(out, "charts/prereleases") {
				t.Fatal("wrong prerelease repository")
			}
			log := logContents(t, dir)
			if strings.Contains(log, "helm\t") || strings.Contains(log, "ko\t") {
				t.Fatal("dry run executed publishing tools")
			}
		})
	}
}

func TestNativeHooks(t *testing.T) {
	dir, env := fixture(t)
	write(t, filepath.Join(dir, "valid.yaml"), "name: test\n---\nname: second\n")
	write(t, filepath.Join(dir, "invalid.yaml"), "name: [broken\n")
	write(t, filepath.Join(dir, "duplicate.yaml"), "name: first\nname: second\n")
	write(t, filepath.Join(dir, "charts/example/templates/template.yaml"), "{{ if .Values.enabled }}\n")
	successful(t, dir, nil, "prek", "validate-config", "prek.toml")
	successful(t, dir, env, "prek", "run", "files", "--files", "valid.yaml", "charts/example/templates/template.yaml")
	for _, file := range []string{"invalid.yaml", "duplicate.yaml"} {
		if out, err := command(t, dir, env, "prek", "run", "files", "--files", file); err == nil || !strings.Contains(out, "Failed") {
			t.Fatalf("invalid YAML not rejected: %s, %v", out, err)
		}
	}
	// A hook filename must survive both Devbox and Task argument forwarding.
	messageFile := "COMMIT MSG $(touch unexpected)"
	for _, message := range []string{"feat: add a maintenance action", "fix(api)!: reject invalid rules\n\nBREAKING CHANGE: validation is stricter"} {
		write(t, filepath.Join(dir, messageFile), message+"\n")
		successful(t, dir, env, "prek", "run", "conventional-commit", "--hook-stage", "commit-msg", "--commit-msg-filename", messageFile)
	}
	write(t, filepath.Join(dir, "COMMIT_MSG"), "not a conventional commit\n")
	if out, err := command(t, dir, env, "prek", "run", "conventional-commit", "--hook-stage", "commit-msg", "--commit-msg-filename", "COMMIT_MSG"); err == nil || !strings.Contains(out, "Failed") {
		t.Fatalf("invalid commit accepted: %s, %v", out, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "unexpected")); !os.IsNotExist(err) {
		t.Fatal("hook filename was evaluated as shell code")
	}
}

func TestHookInstallationReplacesLegacyShims(t *testing.T) {
	dir, env := fixture(t)
	for _, stage := range []string{"pre-commit", "commit-msg"} {
		write(t, filepath.Join(dir, ".git/hooks", stage), "#!/bin/sh\n# OLD_PYTHON_HOOK\nexit 1\n")
	}
	successful(t, dir, env, "task", "hooks:install")
	for _, stage := range []string{"pre-commit", "commit-msg"} {
		data, err := os.ReadFile(filepath.Join(dir, ".git/hooks", stage))
		if err != nil || !strings.Contains(string(data), "prek") || strings.Contains(string(data), "OLD_PYTHON_HOOK") {
			t.Fatalf("legacy %s hook was not replaced: %v", stage, err)
		}
	}
}

func TestDevboxLocks(t *testing.T) {
	root := repository(t)
	var config struct {
		Packages map[string]string `json:"packages"`
	}
	var lock struct {
		Packages map[string]struct {
			Resolved string                     `json:"resolved"`
			Version  string                     `json:"version"`
			Systems  map[string]json.RawMessage `json:"systems"`
		} `json:"packages"`
	}
	for file, destination := range map[string]any{"devbox.json": &config, "devbox.lock": &lock} {
		data, err := os.ReadFile(filepath.Join(root, file))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, destination); err != nil {
			t.Fatal(err)
		}
	}
	for name, version := range config.Packages {
		pkg, found := lock.Packages[name+"@"+version]
		if !found || pkg.Resolved == "" || pkg.Version == "" {
			t.Fatalf("package %s@%s has no lock entry", name, version)
		}
		for _, platform := range []string{"aarch64-darwin", "x86_64-linux"} {
			if _, found := pkg.Systems[platform]; !found {
				t.Errorf("%s has no %s package", name, platform)
			}
		}
	}
	module, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil || !strings.Contains(string(module), "\ngo "+config.Packages["go"]+"\n") {
		t.Fatal("Devbox Go version differs from the module toolchain")
	}
}
