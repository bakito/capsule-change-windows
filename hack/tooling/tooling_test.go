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

	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/yaml"
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
	for _, name := range []string{"Taskfile.yml", "prek.toml", "hack/enterprise.sh", "hack/dev-tool.sh", "hack/check-tools.sh", "hack/image.sh", "hack/dev-setup.sh", "hack/sign-image.sh", "hack/ci-output.sh", "hack/helm-release.sh", "hack/verify-commits.sh"} {
		copyFile(t, root, dir, name)
	}
	if err := os.Chmod(filepath.Join(dir, "hack/enterprise.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	successful(t, dir, nil, "git", "init", "-q")
	successful(t, dir, nil, "git", "config", "user.name", "Tooling Test")
	successful(t, dir, nil, "git", "config", "user.email", "tooling-test@example.invalid")
	write(t, filepath.Join(dir, "config/crd/bases/example.yaml"), "kind: CustomResourceDefinition\n")
	write(t, filepath.Join(dir, "config/rbac/role.yaml"), "kind: ClusterRole\n")
	for _, name := range []string{"charts/change-windows/crds", "charts/change-windows/files", "assets with spaces"} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	bin := filepath.Join(dir, "tools")
	for _, name := range []string{"npm", "go", "controller-gen", "setup-envtest", "ko", "helm", "kubectl", "cosign", "goreleaser"} {
		filename := filepath.Join(bin, name)
		write(t, filename, `#!/usr/bin/env bash
set -euo pipefail
name="${0##*/}"
printf '%s' "$name" >> "$TOOL_LOG"
printf '\t%s' "$@" >> "$TOOL_LOG"
printf '\tassets=%s\n' "${KUBEBUILDER_ASSETS:-}" >> "$TOOL_LOG"
if [[ "$name" == ko ]]; then printf 'repository=%s\n' "${KO_DOCKER_REPO:-}" >> "$TOOL_LOG"; fi
if [[ "${FAIL_TOOL:-}" == "$name" ]]; then exit 37; fi
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
if [[ "$name" == helm && "${1:-}" == push ]]; then printf 'Digest: %s\n' "${HELM_DIGEST:-invalid}"; fi
if [[ "$name" == ko && "${1:-}" == build ]]; then
  for arg in "$@"; do
    case "$arg" in
      --image-refs=*) printf '%s@sha256:%064d\n' "$KO_DOCKER_REPO" 0 > "${arg#*=}" ;;
      --tarball=*) printf 'test image archive\n' > "${arg#*=}" ;;
    esac
  done
fi
`)
		if err := os.Chmod(filename, 0o755); err != nil {
			t.Fatal(err)
		}
	}
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
			for _, value := range []string{"helm package charts/change-windows", "hack/image.sh push", "helm push", "change-windows-0.1.0-rc.1.tgz"} {
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
			if _, err := command(t, dir, env, "task", "enterprise:"+channel, "ENTERPRISE_VERSION=v0.1.0", "DRY_RUN=true"); err == nil {
				t.Fatal("invalid enterprise version accepted")
			}
		})
	}
	dir, env := fixture(t)
	out := successful(t, dir, env, "task", "enterprise:deploy", "ENTERPRISE_VERSION=0.1.0")
	if !strings.Contains(out, "helm upgrade --install") {
		t.Fatal("missing deployment instructions")
	}
	if _, err := os.Stat(filepath.Join(dir, "tools.log")); !os.IsNotExist(err) {
		t.Fatal("deployment instructions executed a tool")
	}
}

func TestNativeHooks(t *testing.T) {
	dir, _ := fixture(t)
	write(t, filepath.Join(dir, "valid.yaml"), "name: test\n---\nname: second\n")
	write(t, filepath.Join(dir, "invalid.yaml"), "name: [broken\n")
	write(t, filepath.Join(dir, "duplicate.yaml"), "name: first\nname: second\n")
	write(t, filepath.Join(dir, "charts/example/templates/template.yaml"), "{{ if .Values.enabled }}\n")
	successful(t, dir, nil, "prek", "validate-config", "prek.toml")
	successful(t, dir, nil, "prek", "run", "check-yaml", "--files", "valid.yaml", "charts/example/templates/template.yaml")
	for _, file := range []string{"invalid.yaml", "duplicate.yaml"} {
		if out, err := command(t, dir, nil, "prek", "run", "check-yaml", "--files", file); err == nil || !strings.Contains(out, "Failed") {
			t.Fatalf("invalid YAML not rejected: %s, %v", out, err)
		}
	}
	env := []string{"DEVBOX_SHELL_ENABLED=1"}
	for _, message := range []string{"feat: add a maintenance action", "fix(api)!: reject invalid rules\n\nBREAKING CHANGE: validation is stricter"} {
		write(t, filepath.Join(dir, "COMMIT_MSG"), message+"\n")
		successful(t, dir, env, "prek", "run", "conventional-commit", "--hook-stage", "commit-msg", "--commit-msg-filename", "COMMIT_MSG")
	}
	write(t, filepath.Join(dir, "COMMIT_MSG"), "not a conventional commit\n")
	if out, err := command(t, dir, env, "prek", "run", "conventional-commit", "--hook-stage", "commit-msg", "--commit-msg-filename", "COMMIT_MSG"); err == nil || !strings.Contains(out, "Failed") {
		t.Fatalf("invalid commit accepted: %s, %v", out, err)
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

func TestHookEnvironmentLauncher(t *testing.T) {
	dir := t.TempDir()
	copyFile(t, repository(t), dir, "hack/dev-tool.sh")
	bin := filepath.Join(dir, "tools")
	for _, name := range []string{"devbox", "example-tool"} {
		filename := filepath.Join(bin, name)
		write(t, filename, "#!/usr/bin/env bash\nprintf '%s\\n' \"${0##*/}\" \"$@\"\n")
		if err := os.Chmod(filename, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, inside := range []string{"", "1"} {
		env := []string{"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"), "DEVBOX_SHELL_ENABLED=" + inside}
		out := successful(t, dir, env, "bash", "hack/dev-tool.sh", "example-tool", "path with spaces")
		want := "devbox\nrun\n--\nexample-tool\npath with spaces\n"
		if inside == "1" {
			want = "example-tool\npath with spaces\n"
		}
		if out != want {
			t.Fatalf("wrong launcher behavior: %q, want %q", out, want)
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

func TestInstallerRendering(t *testing.T) {
	root := repository(t)
	before, err := os.ReadFile(filepath.Join(root, "config/manager/kustomization.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	out := successful(t, root, []string{"IMG=example.com/controller:tooling-test", "KUSTOMIZE=kustomize"}, "bash", "hack/render-installer.sh")
	if !strings.Contains(out, "image: example.com/controller:tooling-test") || !strings.Contains(out, "kind: ValidatingWebhookConfiguration") {
		t.Fatal("installer lost its image override or webhook")
	}
	managers := 0
	for document := range strings.SplitSeq(out, "\n---\n") {
		var deployment appsv1.Deployment
		if err := yaml.Unmarshal([]byte(document), &deployment); err != nil {
			t.Fatal(err)
		}
		if deployment.Kind != "Deployment" {
			continue
		}
		for _, container := range deployment.Spec.Template.Spec.Containers {
			if container.Name == "manager" {
				managers++
				if len(container.Command) != 0 || len(container.Args) == 0 {
					t.Fatal("manager must preserve runtime arguments and use the ko image's entrypoint")
				}
			}
		}
	}
	if managers != 1 {
		t.Fatalf("expected one manager, found %d", managers)
	}
	after, err := os.ReadFile(filepath.Join(root, "config/manager/kustomization.yaml"))
	if err != nil || string(before) != string(after) {
		t.Fatal("installer rendering changed its source manifests")
	}
}
