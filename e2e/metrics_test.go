//go:build e2e

// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestMetrics(t *testing.T) {
	cfg := ctrl.GetConfigOrDie()
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{corev1.AddToScheme, rbacv1.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	admin, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "metrics-test", Namespace: systemNamespace}}
	if err := admin.Create(ctx, sa); err != nil {
		t.Fatal(err)
	}
	role := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "metrics-test"}, Rules: []rbacv1.PolicyRule{{NonResourceURLs: []string{"/metrics"}, Verbs: []string{"get"}}}}
	if err := admin.Create(ctx, role); err != nil {
		t.Fatal(err)
	}
	binding := &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "metrics-test"}, RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: role.Name}, Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Name: sa.Name, Namespace: sa.Namespace}}}
	if err := admin.Create(ctx, binding); err != nil {
		t.Fatal(err)
	}
	kube, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	token, err := kube.CoreV1().ServiceAccounts(sa.Namespace).CreateToken(ctx, sa.Name, &authenticationv1.TokenRequest{Spec: authenticationv1.TokenRequestSpec{ExpirationSeconds: ptr.To(int64(600))}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// Bind an OS-selected loopback port; no external connection or port collision.
	command := exec.CommandContext(ctx, "kubectl", "-n", systemNamespace, "port-forward", "--address=127.0.0.1", "service/change-windows-metrics", ":8443")
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = command.Process.Kill(); _ = command.Wait() }()
	endpoint := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		pattern := regexp.MustCompile(`Forwarding from 127\.0\.0\.1:([0-9]+)`)
		for scanner.Scan() {
			if matches := pattern.FindStringSubmatch(scanner.Text()); len(matches) > 1 {
				endpoint <- "https://127.0.0.1:" + matches[1] + "/metrics"
				return
			}
		}
		endpoint <- ""
	}()
	var url string
	select {
	case url = <-endpoint:
		if url == "" {
			t.Fatal("port-forward did not start")
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	// Helm uses a cert-manager certificate for the in-cluster Service name.
	// The endpoint is the authenticated Kubernetes port-forward to this exact service.
	transport := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} //nolint:gosec
	defer transport.CloseIdleConnections()
	httpClient := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	request := func(authorized bool) (int, string, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return 0, "", err
		}
		if authorized {
			req.Header.Set("Authorization", "Bearer "+token.Status.Token)
		}
		response, err := httpClient.Do(req)
		if err != nil {
			return 0, "", err
		}
		defer func() { _ = response.Body.Close() }()
		body, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
		return response.StatusCode, string(body), err
	}
	status, _, err := request(false)
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusUnauthorized && status != http.StatusForbidden {
		t.Fatalf("unauthenticated metrics returned %d", status)
	}
	poll(t, func(context.Context) (bool, error) {
		status, body, err := request(true)
		if err != nil {
			return false, err
		}
		if status == http.StatusForbidden {
			return false, nil
		}
		if status != http.StatusOK {
			return false, fmt.Errorf("authenticated metrics returned %d", status)
		}
		for _, name := range []string{"change_window_executions_total", "change_window_admission_requests_total", "change_window_admission_warnings_total"} {
			if !strings.Contains(body, name) {
				return false, fmt.Errorf("missing metric %s", name)
			}
		}
		return true, nil
	})
}
