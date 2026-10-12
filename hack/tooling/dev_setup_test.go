//go:build tooling

// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tooling_test

import (
	"bytes"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	"sigs.k8s.io/yaml"
)

func TestDevSetup(t *testing.T) {
	for _, host := range []string{"192.0.2.10", "host.docker.internal", "2001:db8::10"} {
		t.Run(host, func(t *testing.T) {
			dir, env := fixture(t)
			certDir := filepath.Join(dir, "certs with spaces")
			args := []string{"dev:setup", "DEV_CONTEXT=development", "LAPTOP_HOST_IP=" + host, "DEV_NAMESPACE=dev-system", "DEV_CERT_DIR=" + certDir}
			successful(t, dir, env, "task", args...)
			certPEM, err := os.ReadFile(filepath.Join(certDir, "tls.crt"))
			if err != nil {
				t.Fatal(err)
			}
			block, _ := pem.Decode(certPEM)
			if block == nil {
				t.Fatal("missing PEM certificate")
			}
			cert, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				t.Fatal(err)
			}
			if err := cert.VerifyHostname(host); err != nil {
				t.Fatal(err)
			}
			key, err := os.Stat(filepath.Join(certDir, "tls.key"))
			if err != nil || key.Mode().Perm() != 0o600 {
				t.Fatalf("private key permissions: %v, %v", key, err)
			}
			urlHost := host
			if strings.Contains(host, ":") {
				urlHost = "[" + host + "]"
			}
			log := logContents(t, dir)
			for _, want := range []string{
				"helm\t--kube-context\tdevelopment\tupgrade\t--install\tchange-windows\tcharts/change-windows",
				"--namespace\tdev-system", "--skip-crds", "crds.inline=true", "crds.keep=true",
				"crds.exclusive=true", "webhooks.exclusive=true", "webhooks.certificate.generate.enabled=false",
				"webhooks.service.url=https://" + urlHost + ":9443",
				"webhooks.service.caBundle=" + base64.StdEncoding.EncodeToString(certPEM),
				"kubectl\t--context\tdevelopment\twait\t--for=condition=Established",
			} {
				if !strings.Contains(log, want) {
					t.Errorf("missing argument %q", want)
				}
			}
			if strings.Contains(log, "create\tsecret") || strings.Contains(log, "use-context") {
				t.Fatal("setup changed credentials or current context")
			}
			successful(t, dir, env, "task", args...)
			again, _ := os.ReadFile(filepath.Join(certDir, "tls.crt"))
			if !bytes.Equal(again, certPEM) {
				t.Fatal("repeat setup rotated a valid certificate")
			}
			successful(t, dir, env, "task", append(args, "DEV_HOST=192.0.2.11")...)
			changed, _ := os.ReadFile(filepath.Join(certDir, "tls.crt"))
			if bytes.Equal(changed, certPEM) {
				t.Fatal("changing the host retained an invalid certificate")
			}
			successful(t, dir, env, "task", "dev:run", "DEV_CONTEXT=development", "DEV_NAMESPACE=dev-system", "DEV_CERT_DIR="+certDir)
			if !strings.Contains(logContents(t, dir), "manager\t--system-namespace=dev-system\t--webhook-cert-path="+certDir) {
				t.Fatal("run did not use setup's namespace and certificates")
			}
			files, _ := filepath.Glob(filepath.Join(certDir, "run.*"))
			if len(files) != 0 {
				t.Fatal("temporary kubeconfig was retained")
			}
		})
	}
}

func TestDevRunTermination(t *testing.T) {
	dir, env := fixture(t)
	certDir := filepath.Join(dir, "certs")
	write(t, filepath.Join(certDir, "tls.crt"), "certificate fixture")
	write(t, filepath.Join(certDir, "tls.key"), "key fixture")
	pidFile := filepath.Join(dir, "manager.pid")
	cmd := exec.Command("task", "dev:run", "DEV_CONTEXT=development", "DEV_CERT_DIR="+certDir)
	// A terminal delivers Ctrl-C/termination to the foreground process group.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), append(env, "WAIT_MANAGER=true", "MANAGER_PID_FILE="+pidFile)...)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) })
	deadline := time.Now().Add(10 * time.Second)
	var pid int
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(pidFile)
		if err == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
			if pid > 0 {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("controller did not start")
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Task did not terminate the controller")
	}
	if err := syscall.Kill(pid, 0); err != syscall.ESRCH {
		t.Fatalf("controller still running after stopping Task: %v", err)
	}
	files, err := filepath.Glob(filepath.Join(certDir, "run.*"))
	if err != nil || len(files) != 0 {
		t.Fatalf("temporary binary or credentials retained: %v, %v", files, err)
	}
}

func TestDevSetupFailures(t *testing.T) {
	for _, tc := range []struct{ name, context, host, fail string }{
		{name: "missing context", host: "192.0.2.1"},
		{name: "missing host", context: "development"},
		{name: "URL instead of host", context: "development", host: "https://example.com:9443"},
		{name: "loopback", context: "development", host: "127.0.0.1"},
		{name: "extension injection", context: "development", host: "example.com\nDNS:other"},
		{name: "invalid IPv4", context: "development", host: "999.999.999.999"},
		{name: "inaccessible cluster", context: "development", host: "192.0.2.1", fail: "kubectl"},
		{name: "failed Helm installation", context: "development", host: "192.0.2.1", fail: "helm"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, env := fixture(t)
			out, err := command(t, dir, append(env, "FAIL_TOOL="+tc.fail), "task", "dev:setup", "DEV_CONTEXT="+tc.context, "DEV_HOST="+tc.host)
			if err == nil || strings.Contains(out, "Admission points to") {
				t.Fatalf("setup accepted invalid input or a failed tool: %v\n%s", err, out)
			}
			if tc.fail != "helm" && strings.Contains(logContents(t, dir), "helm\t") {
				t.Fatal("Helm ran after failed preflight")
			}
		})
	}
	dir, env := fixture(t)
	if out, err := command(t, dir, env, "task", "dev:run", "DEV_CONTEXT=development"); err == nil || !strings.Contains(out, "Run task dev:setup first") {
		t.Fatalf("run accepted missing certificates: %v\n%s", err, out)
	}
}

func TestDevSetupRendering(t *testing.T) {
	root := repository(t)
	for _, enabled := range []bool{false, true} {
		args := []string{"template", "dev", "charts/change-windows", "--namespace", "dev-system", "--set", "crds.inline=true", "--set", "crds.exclusive=true", "--set", "webhooks.certificate.generate.enabled=false", "--set", "webhooks.service.url=https://192.0.2.10:9443", "--set", "webhooks.service.caBundle=dGVzdA=="}
		if enabled {
			args = append(args, "--set", "webhooks.exclusive=true")
		}
		out := successful(t, root, nil, "helm", args...)
		webhooks, crds := 0, 0
		for _, document := range strings.Split(out, "\n---\n") {
			var meta struct {
				Kind string `json:"kind"`
			}
			if err := yaml.Unmarshal([]byte(document), &meta); err != nil {
				t.Fatal(err)
			}
			switch meta.Kind {
			case "CustomResourceDefinition":
				crds++
			case "ValidatingWebhookConfiguration":
				webhooks++
				var config admissionv1.ValidatingWebhookConfiguration
				if err := yaml.UnmarshalStrict([]byte(document), &config); err != nil {
					t.Fatal(err)
				}
				if len(config.Webhooks) != 2 {
					t.Fatal("expected both admission endpoints")
				}
				for i, w := range config.Webhooks {
					endpoint := []string{"/validate-resources", "/validate-changewindow"}[i]
					if w.ClientConfig.URL == nil || *w.ClientConfig.URL != "https://192.0.2.10:9443"+endpoint || w.ClientConfig.Service != nil || string(w.ClientConfig.CABundle) != "test" {
						t.Fatalf("incorrect external webhook: %+v", w.ClientConfig)
					}
					if w.FailurePolicy == nil || *w.FailurePolicy != admissionv1.Fail {
						t.Fatal("development webhook must fail closed")
					}
					if i == 0 {
						if w.NamespaceSelector == nil || len(w.NamespaceSelector.MatchExpressions) != 1 {
							t.Fatal("resource admission must use the default tenant namespace selector")
						}
						selector := w.NamespaceSelector.MatchExpressions[0]
						if selector.Key != "capsule.clastix.io/tenant" || selector.Operator != "Exists" {
							t.Fatalf("unexpected tenant namespace selector: %+v", selector)
						}
					} else if w.NamespaceSelector != nil {
						t.Fatal("cluster-scoped ChangeWindow validation must not need namespace selection")
					}
				}
			default:
				t.Fatalf("unexpected in-cluster component %q", meta.Kind)
			}
		}
		if crds != 1 || (webhooks == 1) != enabled {
			t.Fatalf("unexpected CRDs/webhooks: %d/%d", crds, webhooks)
		}
	}
}
