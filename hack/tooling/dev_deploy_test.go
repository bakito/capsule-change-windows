//go:build tooling

// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tooling_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/yaml"
)

func deploymentFixture(t *testing.T) (string, []string) {
	t.Helper()
	dir, env := fixture(t)
	copyFile(t, repository(t), dir, "hack/dev-deploy.sh")
	for _, name := range []string{"kubectl", "kind"} {
		filename := filepath.Join(dir, "tools", name)
		write(t, filename, `#!/usr/bin/env bash
set -euo pipefail
name="${0##*/}"
printf '%s' "$name" >> "$TOOL_LOG"
printf '\t%s' "$@" >> "$TOOL_LOG"
printf '\n' >> "$TOOL_LOG"
if [[ "${FAIL_TOOL:-}" == "$name" ]]; then exit 37; fi
if [[ "$name" == kind ]]; then
  if [[ "$1" == load && "${FAIL_STAGE:-}" == load ]]; then exit 37; fi
  if [[ "$1" == get ]]; then echo 'private fixture kubeconfig'; fi
fi
if [[ "$name" == kubectl ]]; then
  case "$*" in
    *'get crd'*) [[ "${FAIL_STAGE:-}" != certificates ]] ;;
    *'config view'*)
      if [[ "$1" == --context && "${FAIL_STAGE:-}" == context ]]; then echo wrong-cluster; else echo expected-cluster; fi ;;
    *'get nodes'*) printf '%s\n' "${NODE_ARCH:-arm64}" ;;
    *'rollout status'*) [[ "${FAIL_STAGE:-}" != rollout ]] ;;
    *'wait certificate'*) [[ "${FAIL_STAGE:-}" != certificate-ready ]] ;;
    *'wait validatingwebhookconfiguration'*) [[ "${FAIL_STAGE:-}" != injection ]] ;;
    *'create --dry-run=server'*) [[ "${FAIL_STAGE:-}" != probe ]] ;;
  esac
fi
`)
		if err := os.Chmod(filename, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir, env
}

func TestDevDeploy(t *testing.T) {
	for _, mode := range []string{"kind", "registry"} {
		t.Run(mode, func(t *testing.T) {
			dir, env := deploymentFixture(t)
			values := filepath.Join(dir, "values with spaces.yaml")
			write(t, values, "manager:\n  resources: {}\n")
			args := []string{"dev:deploy", "DEV_CONTEXT=development", "DEV_KIND_CLUSTER=isolated", "DEV_IMAGE_MODE=" + mode, "IMG=localhost:5000/team/controller:dev", "DEV_NAMESPACE=dev-system", "DEV_RELEASE=custom", "DEV_HELM_VALUES=" + values, "DEV_TIMEOUT=1"}
			successful(t, dir, env, "task", args...)
			log := logContents(t, dir)
			wants := []string{"helm\t--kube-context\tdevelopment\tupgrade\t--install\tcustom\tcharts/change-windows", "--namespace\tdev-system", "--reset-values", "--skip-crds", "-f\t" + values, "crds.inline=true", "crds.exclusive=false", "webhooks.service.url=", "webhooks.service.caBundle=", "webhooks.certificate.generate.enabled=true", "rollout\tstatus\tdeployment", "--for=condition=Ready", "--for=jsonpath={.webhooks[1].clientConfig.caBundle}", "create\t--dry-run=server"}
			if mode == "kind" {
				wants = append(wants, "--push=false", "--platform=linux/arm64", "kind\tload\timage-archive\tdist/controller.tar\t--name\tisolated", "manager.image.pullPolicy=Never", "manager.hostUsers=true", "manager.image.reference=localhost:5000/team/controller:dev-")
			} else {
				wants = append(wants, "--push=true", "manager.image.reference=localhost:5000/team/controller@sha256:"+strings.Repeat("0", 64))
				if strings.Contains(log, "kind\t") {
					t.Fatal("registry mode used Kind")
				}
			}
			for _, want := range wants {
				if !strings.Contains(log, want) {
					t.Errorf("missing %q in %s", want, log)
				}
			}
			if strings.Index(log, "ko\tbuild") > strings.Index(log, "helm\t") {
				t.Fatal("Helm ran before the image was built")
			}
			if strings.Contains(log, "use-context") || strings.Contains(log, "docker\t") {
				t.Fatal("deployment changed current context or built through Docker")
			}
			if mode == "kind" {
				write(t, filepath.Join(dir, "tools.log"), "")
				successful(t, dir, env, "task", args...)
				first := strings.Split(strings.Split(log, "manager.image.reference=")[1], "\t")[0]
				second := strings.Split(strings.Split(logContents(t, dir), "manager.image.reference=")[1], "\t")[0]
				if first == second {
					t.Fatal("repeat deployment reused a mutable local tag")
				}
			}
		})
	}
}

func TestDevDeployFailures(t *testing.T) {
	for _, tc := range []struct {
		name       string
		env, args  []string
		beforeHelm bool
	}{
		{name: "missing context", args: []string{"DEV_CONTEXT="}, beforeHelm: true},
		{name: "invalid mode", args: []string{"DEV_IMAGE_MODE=invalid"}, beforeHelm: true},
		{name: "missing Kind cluster", args: []string{"DEV_KIND_CLUSTER="}, beforeHelm: true},
		{name: "missing values", args: []string{"DEV_HELM_VALUES=missing.yaml"}, beforeHelm: true},
		{name: "invalid timeout", args: []string{"DEV_TIMEOUT=oops"}, beforeHelm: true},
		{name: "missing cert-manager", env: []string{"FAIL_STAGE=certificates"}, beforeHelm: true},
		{name: "wrong context", env: []string{"FAIL_STAGE=context"}, beforeHelm: true},
		{name: "mixed architectures", env: []string{"NODE_ARCH=arm64\namd64"}, beforeHelm: true},
		{name: "wrong platform", args: []string{"IMAGE_PLATFORM=linux/amd64"}, beforeHelm: true},
		{name: "failed build", env: []string{"FAIL_TOOL=ko"}, beforeHelm: true},
		{name: "failed load", env: []string{"FAIL_STAGE=load"}, beforeHelm: true},
		{name: "failed Helm", env: []string{"FAIL_TOOL=helm"}},
		{name: "failed rollout", env: []string{"FAIL_STAGE=rollout"}},
		{name: "failed certificate", env: []string{"FAIL_STAGE=certificate-ready"}},
		{name: "failed injection", env: []string{"FAIL_STAGE=injection"}},
		{name: "failed admission probe", env: []string{"FAIL_STAGE=probe"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, env := deploymentFixture(t)
			args := append([]string{"dev:deploy", "DEV_CONTEXT=development", "DEV_KIND_CLUSTER=isolated", "DEV_TIMEOUT=1"}, tc.args...)
			out, err := command(t, dir, append(env, tc.env...), "task", args...)
			if err == nil || strings.Contains(out, "controller and admission are ready") {
				t.Fatalf("failure accepted: %v\n%s", err, out)
			}
			if tc.beforeHelm && strings.Contains(logContents(t, dir), "helm\t") {
				t.Fatal("Helm ran after a failed prerequisite")
			}
		})
	}
}

func TestDevDeployRejectsWrongDigest(t *testing.T) {
	dir, env := deploymentFixture(t)
	// A successful publisher must still report the exact repository that was requested.
	ko := filepath.Join(dir, "tools/ko")
	write(t, ko, "#!/usr/bin/env bash\nfor arg in \"$@\"; do case \"$arg\" in --image-refs=*) printf 'other/repository@sha256:%064d\\n' 0 > \"${arg#*=}\" ;; esac; done\n")
	if err := os.Chmod(ko, 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := command(t, dir, env, "task", "dev:deploy", "DEV_CONTEXT=development", "DEV_IMAGE_MODE=registry", "IMG=localhost:5000/team/controller:dev")
	if err == nil || !strings.Contains(out, "expected immutable image reference") {
		t.Fatalf("wrong published reference accepted: %v %s", err, out)
	}
	if strings.Contains(logContents(t, dir), "helm\t") {
		t.Fatal("deployed an unverified image")
	}
}

func TestDevDeployChartContract(t *testing.T) {
	root := repository(t)
	image := "registry.example.com/team/controller@sha256:" + strings.Repeat("a", 64)
	out := successful(t, root, nil, "helm", "template", "custom", "charts/change-windows", "--namespace", "dev-system", "--set", "crds.inline=true", "--set", "manager.replicaCount=2", "--set", "manager.image.reference="+image, "--set", "serviceAccount.name=custom-controller")
	var deployment appsv1.Deployment
	var role rbacv1.ClusterRole
	var serviceAccount corev1.ServiceAccount
	bindings := 0
	for _, document := range strings.Split(out, "\n---\n") {
		var meta struct {
			Kind string `json:"kind"`
		}
		if err := yaml.Unmarshal([]byte(document), &meta); err != nil {
			t.Fatal(err)
		}
		switch meta.Kind {
		case "Deployment":
			if err := yaml.UnmarshalStrict([]byte(document), &deployment); err != nil {
				t.Fatal(err)
			}
		case "ClusterRole":
			if err := yaml.Unmarshal([]byte(document), &role); err != nil {
				t.Fatal(err)
			}
		case "ServiceAccount":
			if err := yaml.UnmarshalStrict([]byte(document), &serviceAccount); err != nil {
				t.Fatal(err)
			}
		case "RoleBinding", "ClusterRoleBinding":
			var binding rbacv1.ClusterRoleBinding
			if err := yaml.Unmarshal([]byte(document), &binding); err != nil {
				t.Fatal(err)
			}
			if len(binding.Subjects) != 1 || binding.Subjects[0].Name != "custom-controller" || binding.Subjects[0].Namespace != "dev-system" {
				t.Fatalf("incorrect controller binding: %+v", binding.Subjects)
			}
			bindings++
		}
	}
	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 2 || bindings != 2 || serviceAccount.Name != "custom-controller" || serviceAccount.Namespace != "dev-system" {
		t.Fatal("chart ignored replicas or service-account settings")
	}
	pod := deployment.Spec.Template.Spec
	container := pod.Containers[0]
	if pod.ServiceAccountName != "custom-controller" || container.Image != image || len(container.Command) != 0 {
		t.Fatal("deployment lost its account, immutable image, or ko entrypoint")
	}
	for _, probe := range []*corev1.Probe{container.ReadinessProbe, container.LivenessProbe} {
		if probe == nil || probe.HTTPGet.Port.IntVal != 8081 {
			t.Fatal("probe does not match the controller's listener")
		}
	}
	foundNamespace := false
	for _, env := range container.Env {
		if env.Name == "POD_NAMESPACE" && env.ValueFrom != nil && env.ValueFrom.FieldRef.FieldPath == "metadata.namespace" {
			foundNamespace = true
		}
	}
	if !foundNamespace {
		t.Fatal("controller namespace was not configured")
	}
	for _, arg := range container.Args {
		if !strings.HasPrefix(arg, "--") {
			t.Fatalf("malformed argument %q", arg)
		}
	}
	data, err := os.ReadFile(filepath.Join(root, "config/rbac/role.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var generated rbacv1.ClusterRole
	if err := yaml.Unmarshal(data, &generated); err != nil {
		t.Fatal(err)
	}
	for _, want := range generated.Rules {
		found := false
		for _, actual := range role.Rules {
			if reflect.DeepEqual(actual, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("controller RBAC missing generated rule %+v", want)
		}
	}
	for _, rule := range role.Rules {
		for _, resource := range rule.Resources {
			if resource == "secrets" || resource == "serviceaccounts" {
				t.Fatal("controller has unscoped Secret access or service-account impersonation")
			}
		}
	}
}
