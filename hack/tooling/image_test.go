//go:build tooling

// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tooling_test

import (
	"bytes"
	"os"
	"os/exec"
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
			for _, want := range []string{"ko\tbuild\t./cmd\t--bare\t--tags=dev", "repository=localhost:5000/team/controller"} {
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
	if _, err := command(t, dir, append(env, "FAIL_TOOL=ko"), "task", "image:build", "IMG=example.com/controller:dev"); err == nil {
		t.Fatal("ko failure was ignored")
	}
	for _, name := range []string{"dist/controller.tar"} {
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
	env = append(env, "HELM_DIGEST="+digest, "CHART_REGISTRY=oci://localhost:5000/charts", "GENERATOR_STDOUT=Generated manifests")
	out, logs, err := stdoutCommand(t, dir, env, "task", "helm:push", "VERSION=v1.2.3-rc.1")
	if err != nil || out != "localhost:5000/charts/change-windows@"+digest+"\n" {
		t.Fatalf("incorrect published chart reference: %q %v\n%s", out, err, logs)
	}
	log := logContents(t, dir)
	for _, want := range []string{
		"helm\tpackage\tcharts/change-windows\t--version\t1.2.3-rc.1\t--app-version\t1.2.3-rc.1",
		"helm\tpush\tdist/charts/change-windows-1.2.3-rc.1.tgz\toci://localhost:5000/charts",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("release lost %q: %s", want, log)
		}
	}
	for _, file := range []string{"dist/chart-refs.txt", "dist/charts/push.log"} {
		if _, err := os.Stat(filepath.Join(dir, file)); !os.IsNotExist(err) {
			t.Fatalf("unexpected reference/log artifact %s: %v", file, err)
		}
	}
	for _, failure := range []string{"FAIL_TOOL=helm", "HELM_DIGEST=invalid", "HELM_DIGEST=" + digest + "\nDigest: " + digest} {
		out, logs, err := stdoutCommand(t, dir, append(env, failure), "task", "helm:push", "VERSION=1.2.3")
		if err == nil || out != "" {
			t.Fatalf("failed publishing returned a reference for %s: %q %v\n%s", failure, out, err, logs)
		}
	}
}

func TestReleaseDigestOutputsAndSigning(t *testing.T) {
	for _, tc := range []struct{ workflow, job, step, image string }{
		{"docker-publish.yml", "publish-images", "image", "localhost:5000/team/controller"},
		{"helm-publish.yml", "publish-helm-oci", "chart", "localhost:5000/charts/change-windows"},
	} {
		t.Run(tc.step, func(t *testing.T) {
			dir, env := fixture(t)
			digest := "sha256:" + strings.Repeat("0", 64)
			output := filepath.Join(dir, "github-output")
			env = append(env, "GITHUB_OUTPUT="+output, "VERSION=1.2.3", "IMG=localhost:5000/team/controller:1.2.3",
				"CHART_REGISTRY=oci://localhost:5000/charts", "HELM_DIGEST="+digest, "GENERATOR_STDOUT=Generated manifests")
			successful(t, dir, env, "bash", "-euo", "pipefail", "-c", workflowStep(t, tc.workflow, tc.job, tc.step))
			data, err := os.ReadFile(output)
			if err != nil || string(data) != "image="+tc.image+"\ndigest="+digest+"\n" {
				t.Fatalf("wrong provenance output: %s %v", data, err)
			}
			ref := tc.image + "@" + digest
			successful(t, dir, append(env, "IMAGE_REF="+ref), "task", "image:sign")
			if !strings.Contains(logContents(t, dir), "cosign\tsign\t--yes\t"+ref) {
				t.Fatal("signing did not use the published index/chart digest")
			}
		})
	}
}

func TestKoPublishedIndexReference(t *testing.T) {
	dir, env := fixture(t)
	env = append(env, "GENERATOR_STDOUT=Generated objects")
	out, logs, err := stdoutCommand(t, dir, env, "task", "image:push", "IMG=localhost:5000/team/controller:dev")
	want := "localhost:5000/team/controller@sha256:" + strings.Repeat("0", 64) + "\n"
	if err != nil || out != want {
		t.Fatalf("expected one tag-free index reference, got %q: %v\n%s", out, err, logs)
	}
	if !strings.Contains(logs, "ko build progress") || !strings.Contains(logs, "Generated objects") {
		t.Fatalf("build diagnostics were lost: %s", logs)
	}
	if strings.Contains(logContents(t, dir), "--image-refs") {
		t.Fatal("publishing still uses the multi-reference recorder")
	}
	if _, err := os.Stat(filepath.Join(dir, "dist/image-refs.txt")); !os.IsNotExist(err) {
		t.Fatalf("unexpected reference artifact: %v", err)
	}
}

func TestInvalidPublishedReferences(t *testing.T) {
	valid := "example.com/controller@sha256:" + strings.Repeat("a", 64)
	for _, invalid := range []string{"", "image:latest", valid + "\nmalicious=value", valid + "\n" + valid, "repo@sha256:short"} {
		t.Run(invalid, func(t *testing.T) {
			dir, env := fixture(t)
			env = append(env, "KO_TEST_REFERENCE="+invalid, "IMAGE_REF="+invalid)
			if _, err := command(t, dir, env, "task", "image:sign"); err == nil {
				t.Fatal("signer accepted invalid reference")
			}
			out, _, err := stdoutCommand(t, dir, env, "task", "image:push", "IMG=example.com/controller:dev")
			if err == nil || out != "" {
				t.Fatalf("publisher returned invalid reference: %q %v", out, err)
			}
			if strings.Contains(logContents(t, dir), "cosign\t") {
				t.Fatal("signer ran for invalid input")
			}
		})
	}
	for _, failure := range []string{"FAIL_TOOL=ko", "KO_TEST_REFERENCE=wrong/repository@sha256:" + strings.Repeat("a", 64)} {
		dir, env := fixture(t)
		out, _, err := stdoutCommand(t, dir, append(env, failure), "task", "image:push", "IMG=example.com/controller:dev")
		if err == nil || out != "" {
			t.Fatalf("publisher returned reference after %s: %q %v", failure, out, err)
		}
	}
}

func stdoutCommand(t *testing.T, dir string, environment []string, name string, args ...string) (string, string, error) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), environment...)
	var diagnostics bytes.Buffer
	cmd.Stderr = &diagnostics
	out, err := cmd.Output()
	return string(out), diagnostics.String(), err
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

func TestEnterpriseRejectsInvalidControls(t *testing.T) {
	for _, argument := range []string{"DRY_RUN=tru", "CHANNEL=unknown", "ENTERPRISE_VERSION="} {
		t.Run(argument, func(t *testing.T) {
			dir, env := fixture(t)
			out, err := command(t, dir, env, "task", "enterprise:release", "ENTERPRISE_VERSION=1.2.3", argument)
			if err == nil {
				t.Fatalf("invalid control accepted: %s", out)
			}
			if log := logContents(t, dir); strings.Contains(log, "helm\t") || strings.Contains(log, "ko\t") {
				t.Fatalf("invalid control reached release tools: %s", log)
			}
		})
	}
}

func TestEnterpriseStopsBeforePublishingOnPackageFailure(t *testing.T) {
	dir, env := fixture(t)
	if out, err := command(t, dir, append(env, "FAIL_TOOL=helm"), "task", "enterprise:release", "ENTERPRISE_VERSION=1.2.3"); err == nil {
		t.Fatalf("failed package accepted: %s", out)
	}
	log := logContents(t, dir)
	if !strings.Contains(log, "helm\tpackage") || strings.Contains(log, "ko\t") || strings.Contains(log, "helm\tpush") {
		t.Fatalf("publishing ran after package failure: %s", log)
	}
}

func TestChartVersionValidationUsesHelm(t *testing.T) {
	helm, err := exec.LookPath("helm")
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"v1.2.3-rc.1", "invalid", "1.2.3; touch unexpected"} {
		t.Run(version, func(t *testing.T) {
			dir, env := fixture(t)
			copyFile(t, repository(t), dir, "charts/change-windows/Chart.yaml")
			write(t, filepath.Join(dir, "tools/helm"), "#!/usr/bin/env bash\nexec \"$REAL_HELM\" \"$@\"\n")
			out, err := command(t, dir, append(env, "REAL_HELM="+helm), "task", "helm:package", "VERSION="+version)
			if version == "v1.2.3-rc.1" {
				if err != nil {
					t.Fatalf("valid version rejected: %v %s", err, out)
				}
				if _, err := os.Stat(filepath.Join(dir, "dist/charts/change-windows-1.2.3-rc.1.tgz")); err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(strings.ToLower(out), "invalid semantic version") {
				t.Fatalf("expected Helm's version error: %v %s", err, out)
			}
			if _, err := os.Stat(filepath.Join(dir, "unexpected")); !os.IsNotExist(err) {
				t.Fatal("version executed as shell code")
			}
		})
	}
}
