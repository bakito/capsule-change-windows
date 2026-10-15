// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package admission

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	cradmission "sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	api "github.com/projectcapsule/schedule-action-controller/api/v1alpha1"
	"github.com/projectcapsule/schedule-action-controller/internal/policy"
	"github.com/projectcapsule/schedule-action-controller/internal/testutil"
)

func request(op admissionv1.Operation) cradmission.Request {
	raw := []byte(`{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"app","namespace":"dev","labels":{"maintenance":"enabled"}},"spec":{"replicas":2}}`)
	req := cradmission.Request{AdmissionRequest: admissionv1.AdmissionRequest{UID: "request", Namespace: "dev", Name: "app", Operation: op, Resource: metav1.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, Kind: metav1.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}, Object: runtime.RawExtension{Raw: raw}, OldObject: runtime.RawExtension{Raw: raw}}}
	if op == admissionv1.Delete {
		req.Object.Raw = nil
	}
	return req
}

func handler(t testing.TB) (*Handler, *int) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	reads := new(int)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "dev", Labels: map[string]string{"environment": "development"}}}, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "other"}}).WithInterceptorFuncs(interceptor.Funcs{Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
		*reads++
		return c.Get(ctx, key, obj, opts...)
	}}).Build()
	store := policy.NewStore(testutil.Mapper())
	if err := store.Upsert(testutil.Action()); err != nil {
		t.Fatal(err)
	}
	now, _ := time.Parse(time.RFC3339, "2026-10-01T10:30:00Z")
	return &Handler{Store: store, Reader: c, SystemNamespace: "controller", Now: func() time.Time { return now }}, reads
}

func TestAdmission(t *testing.T) {
	for _, op := range []admissionv1.Operation{admissionv1.Create, admissionv1.Update, admissionv1.Delete} {
		t.Run(string(op), func(t *testing.T) {
			h, _ := handler(t)
			req := request(op)
			dry := true
			req.DryRun = &dry
			response := h.Handle(t.Context(), req)
			if response.Allowed || !strings.Contains(response.Result.Message, "Maintenance until 2026-10-01T11:00:00Z") {
				t.Fatalf("unexpected response %+v", response)
			}
		})
	}
	t.Run("unselected namespace", func(t *testing.T) {
		h, _ := handler(t)
		req := request(admissionv1.Update)
		req.Namespace = "other"
		if !h.Handle(t.Context(), req).Allowed {
			t.Fatal("unselected namespace denied")
		}
	})
	t.Run("remove label cannot evade", func(t *testing.T) {
		h, _ := handler(t)
		req := request(admissionv1.Update)
		req.Object.Raw = []byte(`{"metadata":{"labels":{}}}`)
		if h.Handle(t.Context(), req).Allowed {
			t.Fatal("old labels ignored")
		}
	})
	t.Run("skip reads", func(t *testing.T) {
		h, reads := handler(t)
		req := request(admissionv1.Update)
		req.Resource.Resource = "configmaps"
		if !h.Handle(t.Context(), req).Allowed || *reads != 0 {
			t.Fatal("irrelevant request read API or was denied")
		}
	})
	t.Run("cache unsynced", func(t *testing.T) {
		h, _ := handler(t)
		h.Ready = func() bool { return false }
		if h.Handle(t.Context(), request(admissionv1.Create)).Allowed {
			t.Fatal("unsynced cache allowed")
		}
	})
	t.Run("render errors deny", func(t *testing.T) {
		h, _ := handler(t)
		a := testutil.Action()
		a.Generation++
		a.Spec.Admission[0].Message = "{{ .missing }}"
		if err := h.Store.Upsert(a); err != nil {
			t.Fatal(err)
		}
		if h.Handle(t.Context(), request(admissionv1.Create)).Allowed {
			t.Fatal("render error allowed")
		}
	})
	t.Run("lookup errors deny", func(t *testing.T) {
		h, _ := handler(t)
		req := request(admissionv1.Update)
		req.Namespace = "missing"
		if h.Handle(t.Context(), req).Allowed {
			t.Fatal("missing namespace allowed")
		}
	})
}

func TestWarnings(t *testing.T) {
	for _, tc := range []struct {
		when, at string
		warning  bool
	}{
		{"BeforeStart", "2026-10-01T09:30:00Z", true},
		{"BeforeStart", "2026-10-01T08:59:00Z", false},
		{"BeforeStart", "2026-10-01T10:30:00Z", false},
		{"Active", "2026-10-01T10:30:00Z", true},
		{"BeforeEnd", "2026-10-01T10:30:00Z", true},
		{"BeforeEnd", "2026-10-01T11:00:00Z", false},
	} {
		t.Run(tc.when+tc.at, func(t *testing.T) {
			h, _ := handler(t)
			a := testutil.Action()
			a.Generation++
			w := api.AdmissionWarning{Name: "notice", When: tc.when, Message: "Notice {{ .window.start }}", RequestMatch: a.Spec.Admission[0].RequestMatch}
			if tc.when != "Active" {
				w.Within = &metav1.Duration{Duration: time.Hour}
			}
			a.Spec.Config.Admission.Warnings = []api.AdmissionWarning{w}
			if err := h.Store.Upsert(a); err != nil {
				t.Fatal(err)
			}
			now, _ := time.Parse(time.RFC3339, tc.at)
			h.Now = func() time.Time { return now }
			response := h.Handle(t.Context(), request(admissionv1.Update))
			if (len(response.Warnings) > 0) != tc.warning {
				t.Fatalf("warnings %+v", response)
			}
		})
	}
}

func TestSubresourceAndExecutorIsolation(t *testing.T) {
	h, _ := handler(t)
	a := testutil.Action()
	a.Generation++
	a.Spec.ServiceAccount = &api.ServiceAccountReference{Namespace: "dev", Name: "executor"}
	if err := h.Store.Upsert(a); err != nil {
		t.Fatal(err)
	}
	obj := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "apps/v1", "kind": "Deployment", "metadata": map[string]any{"name": "app", "namespace": "dev", "labels": map[string]any{"maintenance": "enabled"}}}}
	dynamic := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), obj)
	h.Dynamic = dynamic
	req := request(admissionv1.Update)
	req.SubResource = "scale"
	req.Kind = metav1.GroupVersionKind{Group: "autoscaling", Version: "v1", Kind: "Scale"}
	req.Object.Raw = []byte(`{"kind":"Scale","apiVersion":"autoscaling/v1","spec":{"replicas":3}}`)
	if h.Handle(t.Context(), req).Allowed {
		t.Fatal("scale not blocked")
	}
	if len(dynamic.Actions()) != 1 {
		t.Fatalf("expected one parent read: %v", dynamic.Actions())
	}
	req.UserInfo = authenticationv1.UserInfo{Username: "system:serviceaccount:dev:executor"}
	if h.Handle(t.Context(), req).Allowed {
		t.Fatal("bare executor service account bypassed policy")
	}
	req.UserInfo.Extra = map[string]authenticationv1.ExtraValue{policy.ExecutionExtra: {string(a.UID)}}
	if !h.Handle(t.Context(), req).Allowed {
		t.Fatal("authenticated executor could not bypass own policy")
	}
	second := a.DeepCopy()
	second.Name = "another"
	second.UID = "another-uid"
	if err := h.Store.Upsert(second); err != nil {
		t.Fatal(err)
	}
	if h.Handle(t.Context(), req).Allowed {
		t.Fatal("executor bypassed another policy")
	}
}

func TestValidator(t *testing.T) {
	v := &Validator{Mapper: testutil.Mapper()}
	a := testutil.Action()
	a.Spec.Schedules[0].Cron = "bad"
	raw, _ := json.Marshal(a)
	req := request(admissionv1.Create)
	req.Object.Raw = raw
	if v.Handle(t.Context(), req).Allowed {
		t.Fatal("invalid cron accepted")
	}
}

func TestValidatorServiceAccount(t *testing.T) {
	v := &Validator{Mapper: testutil.Mapper()}
	for _, operation := range []admissionv1.Operation{admissionv1.Create, admissionv1.Update} {
		for _, account := range []*api.ServiceAccountReference{nil, {}, {Namespace: "dev"}, {Name: "executor"}, {Namespace: "Bad/namespace", Name: "executor"}, {Namespace: "dev", Name: "Bad/name"}, {Namespace: "dev", Name: "executor"}} {
			window := testutil.Action()
			old, err := json.Marshal(window)
			if err != nil {
				t.Fatal(err)
			}
			window.Spec.ServiceAccount = account
			raw, err := json.Marshal(window)
			if err != nil {
				t.Fatal(err)
			}
			req := request(operation)
			req.Object.Raw, req.OldObject.Raw = raw, old
			response := v.Handle(t.Context(), req)
			valid := account != nil && account.Namespace == "dev" && account.Name == "executor"
			if response.Allowed != valid {
				t.Fatalf("%s account %+v: unexpected response %+v", operation, account, response)
			}
			req.Operation = admissionv1.Delete
			if !v.Handle(t.Context(), req).Allowed {
				t.Fatal("service account validation prevented deletion")
			}
		}
	}
}

func BenchmarkAdmission(b *testing.B) {
	for _, n := range []int{1, 100, 1000} {
		for _, path := range []string{"deny", "allow", "skip"} {
			b.Run(fmt.Sprintf("policies=%d/%s", n, path), func(b *testing.B) {
				h, _ := handler(b)
				for i := 1; i < n; i++ {
					a := testutil.Action()
					a.Name = fmt.Sprint(i)
					a.Spec.Targets[0].APIVersion = "v1"
					a.Spec.Targets[0].Kind = "ConfigMap"
					if err := h.Store.Upsert(a); err != nil {
						b.Fatal(err)
					}
				}
				req := request(admissionv1.Update)
				switch path {
				case "allow":
					req.Namespace = "other"
				case "skip":
					req.Resource.Resource = "pods"
				}
				h.Handle(context.Background(), req)
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					response := h.Handle(context.Background(), req)
					if response.Allowed == (path == "deny") {
						b.Fatal("wrong decision")
					}
				}
			})
		}
	}
}

func TestProtectedNamespacesSkipAdmission(t *testing.T) {
	for _, namespace := range []string{"controller", "kube-system", "kube-public", "kube-node-lease"} {
		t.Run(namespace, func(t *testing.T) {
			h, reads := handler(t)
			h.Ready = func() bool { return false }
			req := request(admissionv1.Update)
			req.Namespace = namespace
			if !h.Handle(t.Context(), req).Allowed || *reads != 0 {
				t.Fatal("protected namespace depended on policy cache or API reads")
			}
		})
	}
}
