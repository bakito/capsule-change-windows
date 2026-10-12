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

	"sigs.k8s.io/yaml"
)

func TestChartPublicationAuthenticatesHelmAndCosign(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repository(t), ".github/workflows/helm-publish.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]struct{ Steps []struct{ Name, Run string } }
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatal(err)
	}
	var login string
	for _, step := range workflow.Jobs["publish-helm-oci"].Steps {
		if step.Name == "Authenticate registry" {
			login = step.Run
		}
		if step.Name == "Package and publish chart" && login == "" {
			t.Fatal("chart publication precedes registry authentication")
		}
	}
	if login == "" {
		t.Fatal("missing registry authentication")
	}
	for _, failure := range []string{"", "helm", "ko"} {
		t.Run("failure="+failure, func(t *testing.T) {
			dir, env := fixture(t)
			env = append(env, "REGISTRY_USERNAME=release-bot", "REGISTRY_PASSWORD=fixture-password", "FAIL_TOOL="+failure)
			out, err := command(t, dir, env, "bash", "-euo", "pipefail", "-c", login)
			if (err != nil) != (failure != "") {
				t.Fatalf("wrong authentication result: %v\n%s", err, out)
			}
			log := logContents(t, dir)
			if failure == "" {
				for _, want := range []string{
					"helm\tregistry\tlogin\tghcr.io\t--username\trelease-bot\t--password-stdin",
					"ko\tlogin\tghcr.io\t--username\trelease-bot\t--password-stdin",
				} {
					if !strings.Contains(log, want) {
						t.Errorf("missing authentication %q: %s", want, log)
					}
				}
			}
			if strings.Contains(log+out, "fixture-password") {
				t.Fatal("registry password leaked into arguments or output")
			}
		})
	}
}

func TestGoReleaserRegistersSigningBundles(t *testing.T) {
	goreleaser, err := exec.LookPath("goreleaser")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(repository(t), ".goreleaser.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var release struct{ Signs []map[string]any }
	if err := yaml.Unmarshal(data, &release); err != nil {
		t.Fatal(err)
	}
	dir, env := fixture(t)
	// Exercise the real GoReleaser with a tiny binary and the production signing
	// configuration. Only the remote signer is replaced; no token or registry is used.
	if err := os.Remove(filepath.Join(dir, "tools/go")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "go.mod"), "module example.invalid/signing-test\n\ngo 1.25.0\n")
	write(t, filepath.Join(dir, "main.go"), "package main\nfunc main() {}\n")
	config := map[string]any{
		"version":      2,
		"project_name": "signing-test",
		"builds":       []map[string]any{{"main": ".", "goos": []string{"linux"}, "goarch": []string{"amd64"}, "env": []string{"CGO_ENABLED=0"}}},
		"changelog":    map[string]any{"disable": true},
		"signs":        release.Signs,
	}
	data, err = yaml.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, ".goreleaser.yml"), string(data))
	write(t, filepath.Join(dir, "tools/cosign"), `#!/usr/bin/env bash
set -euo pipefail
[[ "$1" == sign-blob ]]
shift
bundle=''
artifact=''
for arg in "$@"; do
  case "$arg" in
    --bundle=*) bundle="${arg#*=}" ;;
    --yes) ;;
    --output-signature=*) ;; # Cosign 3 ignores this for bundle signing.
    *) artifact="$arg" ;;
  esac
done
[[ -n "$bundle" && -f "$artifact" ]] || { echo 'missing bundle output or artifact' >&2; exit 1; }
printf '{"testBundle":true}\n' > "$bundle"
`)
	successful(t, dir, nil, "git", "remote", "add", "origin", "https://github.com/example/signing-test.git")
	successful(t, dir, nil, "git", "add", "go.mod", "main.go", ".goreleaser.yml")
	successful(t, dir, nil, "git", "-c", "commit.gpgsign=false", "commit", "-qm", "test: signing fixture")
	successful(t, dir, nil, "git", "-c", "tag.gpgsign=false", "tag", "v1.0.0")
	env = append(env, "GITHUB_TOKEN=", "GITLAB_TOKEN=", "GITEA_TOKEN=", "GOTOOLCHAIN=local")
	successful(t, dir, env, goreleaser, "release", "--skip=publish,validate", "--clean", "--parallelism=1")
	data, err = os.ReadFile(filepath.Join(dir, "dist/artifacts.json"))
	if err != nil {
		t.Fatal(err)
	}
	var artifacts []struct{ Name, Path, Type string }
	if err := json.Unmarshal(data, &artifacts); err != nil {
		t.Fatal(err)
	}
	bundles := 0
	for _, artifact := range artifacts {
		if artifact.Type != "Signature" {
			continue
		}
		bundles++
		if !strings.HasSuffix(artifact.Name, ".sigstore.json") {
			t.Errorf("release would upload a legacy signature: %s", artifact.Name)
		}
		if data, err := os.ReadFile(filepath.Join(dir, artifact.Path)); err != nil || !json.Valid(data) {
			t.Errorf("registered signature has no bundle file: %s: %v", artifact.Path, err)
		}
	}
	if bundles != 2 {
		t.Fatalf("expected archive and checksum signing bundles, got %d: %s", bundles, data)
	}
}
