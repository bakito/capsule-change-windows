//go:build tooling

// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tooling_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/yaml"
)

func webhookTemplateValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(value))
		for key, entry := range value {
			result[key] = webhookTemplateValue(entry)
		}
		return result
	case []any:
		result := make([]any, len(value))
		for i, entry := range value {
			result[i] = webhookTemplateValue(entry)
		}
		return result
	case nil:
		return nil
	default:
		return fmt.Sprintf("{{ print %q }}", fmt.Sprint(value))
	}
}

func renderWebhookValues(t *testing.T, values map[string]any) map[string]map[string]any {
	t.Helper()
	data, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "values.json")
	write(t, file, string(data))
	out := successful(t, repository(t), nil, "helm", "template", "test", "charts/change-windows",
		"--namespace", "platform-system", "--set", "crds.install=false", "--values", file)
	result := map[string]map[string]any{}
	for _, document := range strings.Split(out, "\n---\n") {
		var object map[string]any
		if err := yaml.Unmarshal([]byte(document), &object); err != nil {
			t.Fatal(err)
		}
		if object["kind"] != nil {
			metadata := object["metadata"].(map[string]any)
			result[fmt.Sprint(object["kind"], "/", metadata["name"])] = object
		}
	}
	return result
}

func TestWebhookTemplating(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repository(t), "charts/change-windows/values.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]any{}
	if err := yaml.Unmarshal(data, &values); err != nil {
		t.Fatal(err)
	}
	webhooks := values["webhooks"].(map[string]any)
	t.Run("default values", func(t *testing.T) {
		expected := renderWebhookValues(t, map[string]any{"webhooks": webhooks})
		actual := renderWebhookValues(t, map[string]any{"webhooks": webhookTemplateValue(webhooks)})
		if !reflect.DeepEqual(actual, expected) {
			t.Fatal("templating default scalars changed the rendered manifests")
		}
	})
	webhooks["labels"] = map[string]any{"numeric": "123", "boolean": "false"}
	webhooks["annotations"] = map[string]any{"quoted": "a \"quoted\" value", "multiline": "one: 'value'\ntwo: value"}
	webhooks["service"] = map[string]any{"port": 8444, "caBundle": "Y2E=", "name": "", "namespace": "", "url": ""}
	webhooks["certificate"] = map[string]any{
		"secret": map[string]any{"name": "test-serving"},
		"generate": map[string]any{
			"enabled": true, "dnsNames": []any{"extra.example"}, "ipAddresses": []any{"192.0.2.1"},
			"uris": []any{"spiffe://example/serving"}, "duration": "2160h", "renewBefore": "360h",
		},
	}
	for _, entry := range webhooks["hooks"].(map[string]any) {
		hook := entry.(map[string]any)
		hook["failurePolicy"] = "Ignore"
		hook["matchPolicy"] = "Equivalent"
		hook["timeoutSeconds"] = 11
		hook["objectSelector"] = map[string]any{"matchLabels": map[string]any{"selected": "true"}}
		hook["namespaceSelector"] = map[string]any{"matchExpressions": []any{
			map[string]any{"key": "kubernetes.io/metadata.name", "operator": "NotIn", "values": []any{"platform-system"}},
		}}
		hook["matchConditions"] = []any{map[string]any{"name": "namespace-filter", "expression": `request.namespace != "platform-system"`}}
	}
	t.Run("all properties", func(t *testing.T) {
		expected := renderWebhookValues(t, map[string]any{"webhooks": webhooks})
		actual := renderWebhookValues(t, map[string]any{"webhooks": webhookTemplateValue(webhooks)})
		if !reflect.DeepEqual(actual, expected) {
			t.Fatal("templating webhook properties changed their values or scalar types")
		}
		service := actual["Service/change-windows-webhook"]["spec"].(map[string]any)
		port := service["ports"].([]any)[0].(map[string]any)
		if port["port"] != float64(8444) || port["targetPort"] != float64(9443) {
			t.Fatalf("service did not use the configured client port and fixed controller port: %v", port)
		}
	})
}

func TestWebhookTemplateContext(t *testing.T) {
	out := renderWebhookValues(t, map[string]any{
		"manager": map[string]any{"podAnnotations": map[string]any{"runtime": "{{ .Namespace.Name }}"}},
		"webhooks": map[string]any{
			"labels":      map[string]any{"{{ .Release.Name }}/namespace": "{{ .Release.Namespace }}"},
			"annotations": map[string]any{"literal": "{{ \"{{ .Runtime }}\" }}"},
			"certificate": map[string]any{"secret": map[string]any{"name": "{{ include \"change-windows.name\" . }}-serving"}},
			"hooks": map[string]any{"resources": map[string]any{
				"namespaceSelector": map[string]any{"matchExpressions": []any{
					map[string]any{"key": "kubernetes.io/metadata.name", "operator": "NotIn", "values": []any{"{{ .Release.Namespace }}"}},
				}},
				"timeoutSeconds": "{{ .Values.webhooks.hooks.changewindows.timeoutSeconds }}",
			}},
		},
	})
	webhook := out["ValidatingWebhookConfiguration/change-windows"]
	metadata := webhook["metadata"].(map[string]any)
	if metadata["labels"].(map[string]any)["test/namespace"] != "platform-system" ||
		metadata["annotations"].(map[string]any)["literal"] != "{{ .Runtime }}" {
		t.Fatal("root context, templated map keys, or single-pass escaping failed")
	}
	resource := webhook["webhooks"].([]any)[0].(map[string]any)
	selector := resource["namespaceSelector"].(map[string]any)["matchExpressions"].([]any)[0].(map[string]any)
	if !reflect.DeepEqual(selector["values"], []any{"platform-system"}) || resource["timeoutSeconds"] != float64(5) {
		t.Fatalf("root context did not resolve selectors or values: %v", resource)
	}
	template := out["Deployment/change-windows"]["spec"].(map[string]any)["template"].(map[string]any)
	if template["metadata"].(map[string]any)["annotations"].(map[string]any)["runtime"] != "{{ .Namespace.Name }}" {
		t.Fatal("an unrelated runtime template was evaluated")
	}
	secret := template["spec"].(map[string]any)["volumes"].([]any)[0].(map[string]any)["secret"].(map[string]any)["secretName"]
	certificateSecret := out["Certificate/change-windows"]["spec"].(map[string]any)["secretName"]
	if secret != "change-windows-serving" || secret != certificateSecret {
		t.Fatal("deployment and certificate do not share the rendered serving Secret")
	}
}

func TestWebhookTemplateSwitches(t *testing.T) {
	for _, tc := range []struct {
		name    string
		values  map[string]any
		webhook bool
		cert    bool
		service bool
	}{
		{"disabled", map[string]any{"webhooks": map[string]any{"enabled": "{{ false }}"}}, false, false, false},
		{"external", map[string]any{"webhooks": map[string]any{"service": map[string]any{"url": "{{ printf \"https://%s.example:9443/\" .Release.Name }}", "caBundle": "{{ \"ca\" | b64enc }}"}}}, true, false, false},
		{"existing certificate", map[string]any{"webhooks": map[string]any{"certificate": map[string]any{"generate": map[string]any{"enabled": "{{ false }}"}}}}, true, false, true},
		{"exclusive enabled", map[string]any{"crds": map[string]any{"exclusive": true}, "webhooks": map[string]any{"exclusive": "{{ true }}"}}, true, true, true},
		{"exclusive disabled", map[string]any{"crds": map[string]any{"exclusive": true}, "webhooks": map[string]any{"exclusive": "{{ false }}"}}, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := renderWebhookValues(t, tc.values)
			for kind, want := range map[string]bool{"ValidatingWebhookConfiguration": tc.webhook, "Certificate": tc.cert, "Service": tc.service} {
				name := "change-windows"
				if kind == "Service" {
					name += "-webhook"
				}
				_, found := out[kind+"/"+name]
				if found != want {
					t.Errorf("%s present = %v, want %v", kind, found, want)
				}
			}
			if tc.name == "external" {
				hooks := out["ValidatingWebhookConfiguration/change-windows"]["webhooks"].([]any)
				for i, suffix := range []string{"validate-resources", "validate-changewindow"} {
					client := hooks[i].(map[string]any)["clientConfig"].(map[string]any)
					if client["url"] != "https://test.example:9443/"+suffix || client["caBundle"] != "Y2E=" || client["service"] != nil {
						t.Fatalf("external client config = %v", client)
					}
				}
			}
		})
	}
}

func TestWebhookTemplateErrors(t *testing.T) {
	for _, tc := range []struct{ name, values, want string }{
		{"syntax", `webhooks: {labels: {bad: "{{ broken"}}`, "function \"broken\" not defined"},
		{"boolean", `webhooks: {enabled: '{{ print "invalid" }}'}`, "webhooks.enabled must render to true or false"},
		{"port", `webhooks: {service: {port: '{{ print "invalid" }}'}}`, "webhooks.service.port must render to an integer"},
		{"timeout", `webhooks: {hooks: {resources: {timeoutSeconds: '{{ print "1.5" }}'}}}`, "webhooks.hooks.resources.timeoutSeconds must render to an integer"},
		{"overflow", `webhooks: {hooks: {resources: {timeoutSeconds: '{{ print "99999999999999999999" }}'}}}`, "within the signed 64-bit range"},
		{"duplicate keys", `webhooks: {labels: {test: one, '{{ .Release.Name }}': two}}`, "contains duplicate key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "values.yaml")
			write(t, file, tc.values)
			out, err := command(t, repository(t), nil, "helm", "template", "test", "charts/change-windows", "--values", file)
			if err == nil || !strings.Contains(out, tc.want) {
				t.Fatalf("expected %q, got %v: %s", tc.want, err, out)
			}
		})
	}
}

func TestWebhookDocumentationExamples(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repository(t), "charts/change-windows/README.md"))
	if err != nil {
		t.Fatal(err)
	}
	section := strings.Split(strings.Split(string(data), "## Admission configuration\n")[1], "\n## Values\n")[0]
	examples := regexp.MustCompile("(?s)```yaml\n(.*?)```").FindAllStringSubmatch(section, -1)
	if len(examples) != 4 {
		t.Fatalf("found %d admission examples, want 4", len(examples))
	}
	for i, example := range examples {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			values := map[string]any{}
			if err := yaml.Unmarshal([]byte(example[1]), &values); err != nil {
				t.Fatal(err)
			}
			out := renderWebhookValues(t, values)
			resource := out["ValidatingWebhookConfiguration/change-windows"]["webhooks"].([]any)[0].(map[string]any)
			if i == 1 {
				selector := resource["namespaceSelector"].(map[string]any)["matchExpressions"].([]any)[0].(map[string]any)
				if selector["key"] != "kubernetes.io/metadata.name" || selector["operator"] != "NotIn" ||
					!reflect.DeepEqual(selector["values"], []any{"platform-system", "kube-system", "kube-public", "kube-node-lease"}) {
					t.Fatalf("documented override does not replace tenant selection: %v", selector)
				}
			}
			if i == 2 {
				rules := resource["rules"].([]any)
				if len(rules) != 1 || rules[0].(map[string]any)["scope"] != "Namespaced" {
					t.Fatalf("documented rules still select cluster-scoped resources: %v", rules)
				}
			}
		})
	}
}

func TestWebhookStandaloneE2ESelection(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repository(t), "charts/change-windows/ci/admission-values.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]any{}
	if err := yaml.Unmarshal(data, &values); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct {
		name   string
		values map[string]any
	}{
		{"defaults", map[string]any{}},
		{"standalone", values},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			out := renderWebhookValues(t, fixture.values)
			resource := out["ValidatingWebhookConfiguration/change-windows"]["webhooks"].([]any)[0].(map[string]any)
			encoded, err := json.Marshal(resource["namespaceSelector"])
			if err != nil {
				t.Fatal(err)
			}
			var selector metav1.LabelSelector
			if err := json.Unmarshal(encoded, &selector); err != nil {
				t.Fatal(err)
			}
			compiled, err := metav1.LabelSelectorAsSelector(&selector)
			if err != nil {
				t.Fatal(err)
			}
			for _, tc := range []struct {
				name   string
				labels labels.Set
				want   bool
			}{
				{"tenant", labels.Set{"capsule.clastix.io/tenant": "tenant-a", "kubernetes.io/metadata.name": "application"}, true},
				{"standalone", labels.Set{"kubernetes.io/metadata.name": "application"}, fixture.name == "standalone"},
				{"system", labels.Set{"kubernetes.io/metadata.name": "kube-system"}, false},
				{"controller", labels.Set{"kubernetes.io/metadata.name": "platform-system"}, false},
			} {
				if got := compiled.Matches(tc.labels); got != tc.want {
					t.Errorf("%s namespace matched = %v, want %v", tc.name, got, tc.want)
				}
			}
		})
	}
	script, err := os.ReadFile(filepath.Join(repository(t), "hack/e2e.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(script), "export DEV_HELM_VALUES=charts/change-windows/ci/admission-values.yaml") {
		t.Fatal("e2e deployments must use the standalone namespace selector")
	}
}
