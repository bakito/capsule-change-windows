// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package admission

import (
	"context"
	"fmt"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/dynamic"
	toolscache "k8s.io/client-go/tools/cache"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	api "github.com/projectcapsule/schedule-action-controller/api/v1alpha1"
	"github.com/projectcapsule/schedule-action-controller/internal/policy"
)

func Setup(ctx context.Context, mgr ctrl.Manager, store *policy.Store, namespace string) error {
	informer, err := mgr.GetCache().GetInformer(ctx, &api.ChangeWindow{})
	if err != nil {
		return err
	}
	registration, err := informer.AddEventHandler(toolscache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			if err := store.Upsert(obj.(*api.ChangeWindow)); err != nil {
				ctrl.Log.Error(err, "compiling ChangeWindow")
			}
		},
		UpdateFunc: func(_, obj any) {
			if err := store.Upsert(obj.(*api.ChangeWindow)); err != nil {
				ctrl.Log.Error(err, "compiling ChangeWindow")
			}
		},
		DeleteFunc: func(obj any) {
			if tombstone, ok := obj.(toolscache.DeletedFinalStateUnknown); ok {
				obj = tombstone.Obj
			}
			if action, ok := obj.(*api.ChangeWindow); ok {
				store.Delete(action.Name)
			}
		},
	})
	if err != nil {
		return err
	}
	namespaces, err := mgr.GetCache().GetInformer(ctx, &corev1.Namespace{})
	if err != nil {
		return err
	}
	ready := func() bool { return registration.HasSynced() && namespaces.HasSynced() }
	d, err := dynamic.NewForConfig(mgr.GetConfig())
	if err != nil {
		return err
	}
	mgr.GetWebhookServer().Register("/validate-resources", &admission.Webhook{Handler: &Handler{Store: store, Reader: mgr.GetClient(), Dynamic: d, SystemNamespace: namespace, Ready: ready}})
	mgr.GetWebhookServer().Register("/validate-changewindow", &admission.Webhook{Handler: &Validator{Mapper: mgr.GetRESTMapper()}})
	if err := mgr.AddReadyzCheck("policy-cache", func(_ *http.Request) error {
		if !ready() {
			return fmt.Errorf("policy caches are not synchronized")
		}
		return nil
	}); err != nil {
		return err
	}
	return mgr.AddReadyzCheck("webhook", mgr.GetWebhookServer().StartedChecker())
}
