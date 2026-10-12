//go:build tooling

// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tooling_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/yaml"
)

func playgroundFixture(t *testing.T) (string, []string) {
	t.Helper()
	dir, env := deploymentFixture(t)
	for _, file := range []string{"operator-values.yaml", "headlamp-values.yaml", "resources.yaml", "window.yaml"} {
		copyFile(t, repository(t), dir, "hack/playground/"+file)
	}
	copyFile(t, repository(t), dir, "examples/executor-rbac.yaml")
	write(t, filepath.Join(dir, "headlamp/dist/main.js"), "playground plugin\n")
	write(t, filepath.Join(dir, "headlamp/dist/package.json"), `{ "name": "capsule-change-windows" }`)
	for name, script := range map[string]string{
		"kind": `
if [[ "$1 $2" == 'get clusters' ]]; then
 if [[ "${EXISTING_CLUSTER:-}" == true ]]; then echo change-windows-playground; fi
elif [[ "$1 $2" == 'create cluster' ]]; then
 [[ "$KUBECONFIG" != "$ORIGINAL_CONFIG" ]]
 printf '%s' "$KUBECONFIG" > "$PRIVATE_CONFIG_RECORD"
 [[ "${FAIL_STAGE:-}" != cluster ]]
elif [[ "$1 $2" == 'get kubeconfig' ]]; then
 echo 'private fixture kubeconfig'
fi
`,
		"curl": `
if [[ "${FAIL_STAGE:-}" == forward ]]; then sleep 0.2; exit 37; fi
while (( $# )); do
 if [[ "$1" == -o ]]; then cp headlamp/dist/main.js "$2"; break; fi
 shift
done
`,
		"date": `
if [[ "$*" == '+%s' && "${WAIT_PLAYGROUND:-}" != true ]]; then
 if [[ -f "$CLOCK_RECORD" ]]; then echo 9999; else touch "$CLOCK_RECORD"; echo 1; fi
else
 /bin/date "$@"
fi
`,
	} {
		executable := filepath.Join(dir, "tools", name)
		write(t, executable, "#!/usr/bin/env bash\nset -euo pipefail\nprintf '%s' '${NAME}' >> \"$TOOL_LOG\"\nprintf '\\t%s' \"$@\" >> \"$TOOL_LOG\"\nprintf '\\n' >> \"$TOOL_LOG\"\n"+script)
		data, err := os.ReadFile(executable)
		if err != nil {
			t.Fatal(err)
		}
		write(t, executable, strings.ReplaceAll(string(data), "${NAME}", name))
		if err := os.Chmod(executable, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Keep existing dev:deploy responses, adding the long-lived port-forward.
	kubectl := filepath.Join(dir, "tools/kubectl")
	data, err := os.ReadFile(kubectl)
	if err != nil {
		t.Fatal(err)
	}
	write(t, kubectl, strings.Replace(string(data), "set -euo pipefail", `set -euo pipefail
if [[ "${FAIL_STAGE:-}" == certificates && "$*" == *'get crd'* ]]; then exit 37; fi`, 1)+`
if [[ "$*" == *port-forward* ]]; then
 [[ "${FAIL_STAGE:-}" != forward ]] || exit 37
 trap 'exit 0' TERM INT
 while true; do sleep 0.1; done
fi
`)
	original := filepath.Join(dir, "developer kubeconfig")
	write(t, original, "untouched developer context\n")
	return dir, append(env, "KUBECONFIG="+original, "ORIGINAL_CONFIG="+original,
		"PRIVATE_CONFIG_RECORD="+filepath.Join(dir, "private-config"), "CLOCK_RECORD="+filepath.Join(dir, "clock"))
}

func assertPlaygroundCleaned(t *testing.T, dir string) {
	t.Helper()
	log := logContents(t, dir)
	if !strings.Contains(log, "kind\tdelete\tcluster\t--name\tchange-windows-playground") {
		t.Fatalf("missing cluster cleanup:\n%s", log)
	}
	config, err := os.ReadFile(filepath.Join(dir, "private-config"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(string(config))); !os.IsNotExist(err) {
		t.Fatalf("temporary credentials or plugin assets remain: %v", err)
	}
	original, err := os.ReadFile(filepath.Join(dir, "developer kubeconfig"))
	if err != nil || string(original) != "untouched developer context\n" {
		t.Fatalf("developer context changed: %v %s", err, original)
	}
}

func TestPlaygroundLifecycle(t *testing.T) {
	for _, stage := range []string{"expiry", "cluster", "certificates", "forward"} {
		t.Run(stage, func(t *testing.T) {
			dir, env := playgroundFixture(t)
			out, err := command(t, dir, append(env, "FAIL_STAGE="+stage), "task", "playground", "PLAYGROUND_MINUTES=1")
			if stage == "expiry" {
				if err != nil {
					t.Fatalf("playground failed: %v\n%s", err, out)
				}
				if !strings.Contains(out, "Playground ready:") {
					t.Fatal("readiness missing")
				}
				log := logContents(t, dir)
				for _, want := range []string{"npm\tci", "npm\trun\tbuild", "--push=false", "--values\thack/playground/headlamp-values.yaml", "--version\t0.45.0", "--address=127.0.0.1", "changewindow/playground\t--for=condition=Ready"} {
					if !strings.Contains(log, want) {
						t.Errorf("missing %q in %s", want, log)
					}
				}
				if strings.Contains(log, "--push=true") || strings.Contains(log, "use-context") {
					t.Fatal("playground published or changed context")
				}
			} else if err == nil {
				t.Fatalf("%s failure accepted: %s", stage, out)
			}
			assertPlaygroundCleaned(t, dir)
		})
	}
}

func TestPlaygroundRejectsUnsafeSetup(t *testing.T) {
	for _, tc := range []struct{ name, arg, env string }{
		{"existing cluster", "", "EXISTING_CLUSTER=true"},
		{"zero lifetime", "PLAYGROUND_MINUTES=0", ""},
		{"unbounded lifetime", "PLAYGROUND_MINUTES=31", ""},
		{"bad lifetime", "PLAYGROUND_MINUTES=;echo bad", ""},
		{"bad port", "PLAYGROUND_PORT=70000", ""},
		{"bad name", "PLAYGROUND_CLUSTER=../../other", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, env := playgroundFixture(t)
			args := []string{"playground"}
			if tc.arg != "" {
				args = append(args, tc.arg)
			}
			if tc.env != "" {
				env = append(env, tc.env)
			}
			out, err := command(t, dir, env, "task", args...)
			if err == nil {
				t.Fatalf("unsafe setup accepted: %s", out)
			}
			log := logContents(t, dir)
			if strings.Contains(log, "kind\tcreate") || strings.Contains(log, "kind\tdelete") || strings.Contains(log, "npm\t") {
				t.Fatalf("unsafe setup modified resources: %s", log)
			}
		})
	}
}

func TestPlaygroundSignalCleanup(t *testing.T) {
	dir, env := playgroundFixture(t)
	output, err := os.Create(filepath.Join(dir, "output.log"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := output.Close(); err != nil {
			t.Error(err)
		}
	})
	cmd := exec.Command("task", "playground")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), append(env, "WAIT_PLAYGROUND=true")...)
	cmd.Stdout = output
	cmd.Stderr = output
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) })
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	deadline := time.Now().Add(15 * time.Second)
	for {
		data, err := os.ReadFile(output.Name())
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "Playground ready:") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("not ready: %s", data)
		}
		select {
		case err := <-done:
			t.Fatalf("exited early: %v\n%s", err, data)
		default:
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("cancellation did not exit")
	}
	assertPlaygroundCleaned(t, dir)
}

func TestPlaygroundAdmissionScope(t *testing.T) {
	root := repository(t)
	output := successful(t, root, nil, "helm", "template", "change-windows", "charts/change-windows", "--namespace", "schedule-action-controller-system", "--values", "hack/playground/operator-values.yaml")
	found := false
	for _, document := range strings.Split(output, "\n---\n") {
		var config admissionv1.ValidatingWebhookConfiguration
		if err := yaml.Unmarshal([]byte(document), &config); err != nil {
			t.Fatal(err)
		}
		if config.Kind != "ValidatingWebhookConfiguration" {
			continue
		}
		for _, hook := range config.Webhooks {
			if hook.ClientConfig.Service == nil || hook.ClientConfig.Service.Path == nil || *hook.ClientConfig.Service.Path != "/validate-resources" {
				continue
			}
			found = true
			selector, err := metav1.LabelSelectorAsSelector(hook.NamespaceSelector)
			if err != nil {
				t.Fatal(err)
			}
			for _, tc := range []struct {
				name   string
				tenant bool
				want   bool
			}{
				{"development", false, true}, {"development", true, true}, {"other", false, false}, {"other", true, false}, {"kube-system", false, false}, {"schedule-action-controller-system", false, false},
			} {
				namespaceLabels := labels.Set{"kubernetes.io/metadata.name": tc.name}
				if tc.tenant {
					namespaceLabels["capsule.clastix.io/tenant"] = "example"
				}
				if got := selector.Matches(namespaceLabels); got != tc.want {
					t.Errorf("namespace %s tenant=%t: matched %t, want %t", tc.name, tc.tenant, got, tc.want)
				}
			}
		}
	}
	if !found {
		t.Fatal("resource admission webhook not rendered")
	}
}
