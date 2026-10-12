//go:build tooling

// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tooling_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMissingDevelopmentTools(t *testing.T) {
	for _, tc := range []struct {
		target, tool, mode string
	}{
		{target: "generate", tool: "controller-gen"},
		{target: "manifests", tool: "controller-gen"},
		{target: "dev:deploy", tool: "controller-gen", mode: "kind"},
		{target: "dev:deploy", tool: "go", mode: "kind"},
		{target: "dev:deploy", tool: "ko", mode: "kind"},
		{target: "dev:deploy", tool: "helm", mode: "kind"},
		{target: "dev:deploy", tool: "kubectl", mode: "kind"},
		{target: "dev:deploy", tool: "openssl", mode: "kind"},
		{target: "dev:deploy", tool: "kind", mode: "kind"},
		{target: "dev:deploy", tool: "controller-gen", mode: "registry"},
	} {
		t.Run(tc.target+"/"+tc.mode+"/"+tc.tool, func(t *testing.T) {
			dir, env := deploymentFixture(t)
			bin := filepath.Join(dir, "tools")
			// Exclude the developer's PATH so installed binaries cannot hide missing tools.
			for _, tool := range []string{"bash", "openssl"} {
				binary, err := exec.LookPath(tool)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(binary, filepath.Join(bin, tool)); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Remove(filepath.Join(bin, tc.tool)); err != nil {
				t.Fatal(err)
			}
			if tc.mode == "registry" {
				if err := os.Remove(filepath.Join(bin, "kind")); err != nil {
					t.Fatal(err)
				}
			}
			write(t, filepath.Join(dir, "tools.log"), "")
			out, err := command(t, dir, append(env, "PATH="+bin), "task", tc.target, "DEV_IMAGE_MODE="+tc.mode)
			if err == nil {
				t.Fatalf("missing %s was accepted: %s", tc.tool, out)
			}
			for _, want := range []string{"Missing", "devbox shell", "devbox run --", "DEVELOPMENT.md"} {
				if !strings.Contains(out, want) {
					t.Errorf("missing recovery instruction %q: %s", want, out)
				}
			}
			if tc.mode == "registry" && strings.Contains(out, "Missing development tool: kind") {
				t.Fatal("registry deployment requires Kind")
			}
			if log := logContents(t, dir); log != "" {
				t.Fatalf("tools ran before preflight failed: %s", log)
			}
		})
	}
}
