// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package webhook_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-logr/logr"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	ctrlwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	triggerhandler "github.com/projectcapsule/capsule/internal/webhook/replication"
	"github.com/projectcapsule/capsule/internal/webhook/route"
	"github.com/projectcapsule/capsule/pkg/runtime/indexers/tenantresource"
	"github.com/projectcapsule/capsule/pkg/runtime/replication"
	runtimewebhook "github.com/projectcapsule/capsule/pkg/runtime/webhook"
)

type deliveryManager struct {
	manager.Manager
	client client.Client
	server ctrlwebhook.Server
}

func (m deliveryManager) GetWebhookServer() ctrlwebhook.Server { return m.server }
func (m deliveryManager) GetClient() client.Client             { return m.client }
func (m deliveryManager) GetAPIReader() client.Reader          { return m.client }
func (m deliveryManager) GetScheme() *runtime.Scheme           { return m.client.Scheme() }

type deliveryNotifications struct{ calls atomic.Int64 }

func (n *deliveryNotifications) Notify(replication.Target, time.Duration) bool {
	n.calls.Add(1)
	return true
}

func TestRegisteredTriggerRejectsForgedAdmissionReviews(t *testing.T) {
	for _, tracing := range []bool{false, true} {
		t.Run(map[bool]string{false: "plain", true: "traced"}[tracing], func(t *testing.T) {
			scheme := runtime.NewScheme()
			if err := capsulev1beta2.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			trigger := capsulev1beta2.TenantResourceTrigger{Name: "events"}
			parent := &capsulev1beta2.TenantResource{Name: "target", Namespace: "tenant-b", UID: "target-uid", Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{Triggers: []capsulev1beta2.TenantResourceTrigger{trigger}}}}
			var reads atomic.Int64
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(parent).WithIndex(&capsulev1beta2.TenantResource{}, replication.TriggerIndexerField, (tenantresource.Triggers{}).Func()).WithIndex(&capsulev1beta2.GlobalTenantResource{}, replication.TriggerIndexerField, (tenantresource.Triggers{}).Func()).WithInterceptorFuncs(interceptor.Funcs{
				List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					reads.Add(1)
					return c.List(ctx, list, opts...)
				},
			}).Build()
			server := ctrlwebhook.NewServer(ctrlwebhook.Options{})
			notifications := &deliveryNotifications{}
			key, err := replication.NewDeliveryKey(make([]byte, 32))
			if err != nil {
				t.Fatal(err)
			}
			if err := runtimewebhook.Register(deliveryManager{client: c, server: server}, nil, runtimewebhook.RegistrationOptions{EnableTracing: tracing}, route.ReplicationTriggers(triggerhandler.TriggerHandler(nil, notifications, logr.Discard()), key)); err != nil {
				t.Fatal(err)
			}
			review := admissionv1.AdmissionReview{APIVersion: "admission.k8s.io/v1", Kind: "AdmissionReview", Request: &admissionv1.AdmissionRequest{UID: "forged", Namespace: "tenant-b", Operation: admissionv1.Create, Resource: metav1.GroupVersionResource{Version: "v1", Resource: "secrets"}, Kind: metav1.GroupVersionKind{Version: "v1", Kind: "Secret"}, UserInfo: authenticationv1.UserInfo{Username: "tenant-b-owner"}, Object: runtime.RawExtension{Raw: []byte(`{"apiVersion":"v1","kind":"Secret","metadata":{"name":"source","namespace":"tenant-b"}}`)}}}
			body, err := json.Marshal(review)
			if err != nil {
				t.Fatal(err)
			}
			id := replication.MatchID(&trigger)
			for _, path := range []string{replication.TriggerPath + id, replication.TriggerPath + id + "/" + id} {
				request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
				request.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				server.WebhookMux().ServeHTTP(w, request)
				if w.Code != http.StatusUnauthorized || reads.Load() != 0 || notifications.calls.Load() != 0 {
					t.Fatalf("forged review: status=%d reads=%d notifications=%d", w.Code, reads.Load(), notifications.calls.Load())
				}
			}
			request := httptest.NewRequest(http.MethodPost, key.Path(id), bytes.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			server.WebhookMux().ServeHTTP(w, request)
			if w.Code != http.StatusOK || reads.Load() != 2 || notifications.calls.Load() != 1 {
				t.Fatalf("authenticated review: status=%d reads=%d notifications=%d body=%s", w.Code, reads.Load(), notifications.calls.Load(), w.Body.String())
			}
		})
	}
}
