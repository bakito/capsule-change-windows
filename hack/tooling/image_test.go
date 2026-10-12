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

func TestKoImageTasks(t *testing.T) {
	for _, mode := range []string{"build", "push"} {
		t.Run(mode, func(t *testing.T) {
			dir, env := fixture(t)
			archive := filepath.Join(dir, "output with spaces", "controller.tar")
			successful(t, dir, env, "task", "image:"+mode, "IMG=localhost:5000/team/controller:dev", "IMAGE_ARCHIVE="+archive, "IMAGE_PLATFORM=linux/amd64")
			log := logContents(t, dir)
			for _, want := range []string{"ko\tbuild\t./cmd\t--bare\t--tags=dev", "repository=localhost:5000/team/controller", "--image-refs=dist/image-refs.txt"} {
				if !strings.Contains(log, want) {
					t.Fatalf("missing %q: %s", want, log)
				}
			}
			if mode == "build" {
				for _, want := range []string{"--push=false", "--platform=linux/amd64", "--tarball=" + archive, "--tag-only"} {
					if !strings.Contains(log, want) {
						t.Fatalf("local build lost %q: %s", want, log)
					}
				}
				if _, err := os.Stat(archive); err != nil {
					t.Fatal(err)
				}
			} else if !strings.Contains(log, "--push=true\t--platform=linux/amd64,linux/arm64") || strings.Contains(log, "--tarball") {
				t.Fatalf("publish did not use both platforms: %s", log)
			}
			if strings.Contains(log, "docker") || strings.Contains(log, "--local") {
				t.Fatalf("build unexpectedly uses a container daemon: %s", log)
			}
		})
	}
}

func TestDefaultImageReference(t *testing.T) {
	dir, env := fixture(t)
	successful(t, dir, env, "task", "image:build", "VERSION=1.2.3")
	log := logContents(t, dir)
	if !strings.Contains(log, "repository=ghcr.io/projectcapsule/change-windows") || !strings.Contains(log, "--tags=1.2.3") {
		t.Fatalf("wrong default image reference: %s", log)
	}
}

func TestImageRejectsInvalidInputs(t *testing.T) {
	for _, image := range []string{"repo/image", "repo/image@sha256:abc", "repo/image:dev; touch unexpected", "repo/$(touch unexpected):dev", "repo/image:dev,latest"} {
		t.Run(image, func(t *testing.T) {
			dir, env := fixture(t)
			if out, err := command(t, dir, env, "task", "image:build", "IMG="+image); err == nil || !strings.Contains(out, "IMG") {
				t.Fatalf("invalid image accepted: %v %s", err, out)
			}
			if strings.Contains(logContents(t, dir), "ko\t") {
				t.Fatal("ko ran for invalid input")
			}
			if _, err := os.Stat(filepath.Join(dir, "unexpected")); !os.IsNotExist(err) {
				t.Fatal("input executed as shell code")
			}
		})
	}
}

func TestImageFailureCannotReuseOldArtifact(t *testing.T) {
	dir, env := fixture(t)
	write(t, filepath.Join(dir, "dist/controller.tar"), "old archive")
	write(t, filepath.Join(dir, "dist/image-refs.txt"), "old reference")
	if _, err := command(t, dir, append(env, "FAIL_TOOL=ko"), "task", "image:build", "IMG=example.com/controller:dev"); err == nil {
		t.Fatal("ko failure was ignored")
	}
	for _, name := range []string{"dist/controller.tar", "dist/image-refs.txt"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("stale output %s remains: %v", name, err)
		}
	}
}

func TestLocalImagePlatform(t *testing.T) {
	dir, env := fixture(t)
	successful(t, dir, env, "task", "image:build", "IMG=example.com/controller:dev")
	if !strings.Contains(logContents(t, dir), "--platform=linux/arm64") {
		t.Fatal("local image did not default to the host's architecture on Linux")
	}
	for _, platform := range []string{"darwin/arm64", "linux/amd64,linux/arm64"} {
		if _, err := command(t, dir, env, "task", "image:build", "IMG=example.com/controller:dev", "IMAGE_PLATFORM="+platform); err == nil {
			t.Fatalf("invalid local image platform accepted: %s", platform)
		}
	}
}

func TestHelmReleaseTasks(t *testing.T) {
	dir, env := fixture(t)
	digest := "sha256:" + strings.Repeat("b", 64)
	successful(t, dir, append(env, "HELM_DIGEST="+digest, "CHART_REGISTRY=oci://localhost:5000/charts"), "task", "helm:push", "VERSION=v1.2.3-rc.1")
	log := logContents(t, dir)
	for _, want := range []string{
		"helm\tpackage\tcharts/change-windows\t--version\t1.2.3-rc.1\t--app-version\t1.2.3-rc.1",
		"helm\tpush\tdist/charts/change-windows-1.2.3-rc.1.tgz\toci://localhost:5000/charts",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("release lost %q: %s", want, log)
		}
	}
	refs := filepath.Join(dir, "dist/chart-refs.txt")
	data, err := os.ReadFile(refs)
	if err != nil || string(data) != "localhost:5000/charts/change-windows@"+digest+"\n" {
		t.Fatalf("incorrect published chart reference: %s %v", data, err)
	}
	for _, failure := range []string{"FAIL_TOOL=helm", "HELM_DIGEST=invalid"} {
		write(t, refs, "old digest")
		if _, err := command(t, dir, append(env, failure), "task", "helm:push", "VERSION=1.2.3"); err == nil {
			t.Fatalf("publishing accepted %s", failure)
		}
		if _, err := os.Stat(refs); !os.IsNotExist(err) {
			t.Fatalf("failed publishing left a stale chart reference: %v", err)
		}
	}
}

func TestReleaseDigestOutputsAndSigning(t *testing.T) {
	dir, env := fixture(t)
	digest := "sha256:" + strings.Repeat("a", 64)
	ref := "ghcr.io/example/controller:1.2.3@" + digest
	write(t, filepath.Join(dir, "dist/image-refs.txt"), ref+"\n")
	output := filepath.Join(dir, "github-output")
	successful(t, dir, append(env, "GITHUB_OUTPUT="+output), "bash", "-euo", "pipefail", "-c", workflowStep(t, "docker-publish.yml", "publish-images", "image"))
	data, err := os.ReadFile(output)
	if err != nil || string(data) != "image=ghcr.io/example/controller\ndigest="+digest+"\n" {
		t.Fatalf("wrong provenance output: %s %v", data, err)
	}
	successful(t, dir, env, "task", "image:sign")
	if !strings.Contains(logContents(t, dir), "cosign\tsign\t--yes\t"+ref) {
		t.Fatal("signing did not use the immutable digest")
	}
	for _, invalid := range []string{"image:latest", ref + "\nmalicious=value", "repo@sha256:short"} {
		write(t, filepath.Join(dir, "dist/image-refs.txt"), invalid)
		for _, args := range [][]string{
			{"task", "image:sign"},
			{"bash", "-euo", "pipefail", "-c", workflowStep(t, "docker-publish.yml", "publish-images", "image")},
		} {
			if _, err := command(t, dir, append(env, "GITHUB_OUTPUT="+output), args[0], args[1:]...); err == nil {
				t.Fatalf("%s accepted invalid digest %q", args[0], invalid)
			}
		}
	}
}

func TestReleaseVersionAndSnapshot(t *testing.T) {
	dir, env := fixture(t)
	output := filepath.Join(dir, "github-output")
	successful(t, dir, append(env, "GITHUB_REF_NAME=v1.2.3-rc.1", "GITHUB_OUTPUT="+output), "bash", "-euo", "pipefail", "-c", workflowStep(t, "docker-publish.yml", "publish-images", "version"))
	data, err := os.ReadFile(output)
	if err != nil || string(data) != "version=1.2.3-rc.1\n" {
		t.Fatalf("wrong release version: %s %v", data, err)
	}
	if _, err := command(t, dir, append(env, "GITHUB_REF_NAME=v1.2.3+build", "GITHUB_OUTPUT="+output), "bash", "-euo", "pipefail", "-c", workflowStep(t, "docker-publish.yml", "publish-images", "version")); err == nil {
		t.Fatal("unsupported image tag accepted")
	}
	successful(t, dir, env, "task", "release:snapshot")
	if !strings.Contains(logContents(t, dir), "goreleaser\trelease\t--snapshot\t--clean\t--skip=publish,sign") {
		t.Fatal("snapshot could publish or sign artifacts")
	}
}

// Exercise GitHub metadata at its workflow consumer; build and publish remain Task targets.
func workflowStep(t *testing.T, file, job, id string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repository(t), ".github/workflows", file))
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]struct{ Steps []struct{ ID, Run string } }
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatal(err)
	}
	for _, step := range workflow.Jobs[job].Steps {
		if step.ID == id {
			return step.Run
		}
	}
	t.Fatalf("missing step %s/%s in %s", job, id, file)
	return ""
}
