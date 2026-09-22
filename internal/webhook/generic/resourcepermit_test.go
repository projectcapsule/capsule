// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package generic

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	authenticationv1 "k8s.io/api/authentication/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	webhooktest "github.com/projectcapsule/capsule/internal/webhook/test"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/users"
)

func TestResourcePermitResourceHandler(t *testing.T) {
	t.Setenv(configuration.EnvironmentServiceaccountName, "capsule-controller")
	t.Setenv(configuration.EnvironmentControllerNamespace, "capsule-system")

	handler := ResourcePermitResourceHandler()

	t.Run("controller may update", func(t *testing.T) {
		resp := handler.OnUpdate(nil, nil, nil, nil)(context.Background(), admission.Request{
			UserInfo: users.ServiceAccountUserInfo("capsule-system", "capsule-controller"),
		})
		if resp != nil {
			t.Fatalf("expected controller request to be allowed, got %#v", resp)
		}
	})

	t.Run("user may not update", func(t *testing.T) {
		resp := handler.OnUpdate(nil, nil, nil, nil)(context.Background(), admission.Request{
			UserInfo: authenticationv1.UserInfo{Username: "alice"},
		})
		webhooktest.VerifyResponse(t, resp, 403, "can only be changed by the Capsule controller")
	})

	t.Run("user may not delete", func(t *testing.T) {
		resp := handler.OnDelete(nil, nil, nil, nil)(context.Background(), admission.Request{
			UserInfo: authenticationv1.UserInfo{Username: "alice"},
		})
		webhooktest.VerifyResponse(t, resp, 403, "can only be changed by the Capsule controller")
	})

	t.Run("template ServiceAccount may update its protected resource", func(t *testing.T) {
		oldObj := protectedResourcePermitResource("system:serviceaccount:operations:runner")
		newObj := oldObj.DeepCopy()
		newObj.SetAnnotations(map[string]string{
			meta.ResourcePermitServiceAccountAnnotation: "system:serviceaccount:attacker:runner",
		})
		decoder := &webhooktest.Decoder[*unstructured.Unstructured]{Object: newObj, OldObject: oldObj}

		resp := handler.OnUpdate(nil, nil, decoder, nil)(context.Background(), admission.Request{
			UserInfo: users.ServiceAccountUserInfo("operations", "runner"),
		})
		if resp != nil {
			t.Fatalf("expected configured ServiceAccount request to be allowed, got %#v", resp)
		}
	})

	t.Run("caller cannot authorize itself on an existing protected resource", func(t *testing.T) {
		oldObj := protectedResourcePermitResource("system:serviceaccount:operations:runner")
		newObj := oldObj.DeepCopy()
		newObj.SetAnnotations(map[string]string{
			meta.ResourcePermitServiceAccountAnnotation: "system:serviceaccount:attacker:runner",
		})
		decoder := &webhooktest.Decoder[*unstructured.Unstructured]{Object: newObj, OldObject: oldObj}

		resp := handler.OnUpdate(nil, nil, decoder, nil)(context.Background(), admission.Request{
			UserInfo: users.ServiceAccountUserInfo("attacker", "runner"),
		})
		webhooktest.VerifyResponse(t, resp, 403, "can only be changed by the Capsule controller")
	})

	t.Run("template ServiceAccount may adopt and protect a resource", func(t *testing.T) {
		oldObj := &unstructured.Unstructured{}
		newObj := protectedResourcePermitResource("system:serviceaccount:operations:runner")
		decoder := &webhooktest.Decoder[*unstructured.Unstructured]{Object: newObj, OldObject: oldObj}

		resp := handler.OnUpdate(nil, nil, decoder, nil)(context.Background(), admission.Request{
			UserInfo: users.ServiceAccountUserInfo("operations", "runner"),
		})
		if resp != nil {
			t.Fatalf("expected configured ServiceAccount adoption to be allowed, got %#v", resp)
		}
	})

	t.Run("template ServiceAccount may delete its protected resource", func(t *testing.T) {
		oldObj := protectedResourcePermitResource("system:serviceaccount:operations:runner")
		decoder := &webhooktest.Decoder[*unstructured.Unstructured]{OldObject: oldObj}

		resp := handler.OnDelete(nil, nil, decoder, nil)(context.Background(), admission.Request{
			UserInfo: users.ServiceAccountUserInfo("operations", "runner"),
		})
		if resp != nil {
			t.Fatalf("expected configured ServiceAccount deletion to be allowed, got %#v", resp)
		}
	})

	t.Run("independent marker retains the old authorization identity", func(t *testing.T) {
		oldObj := protectedResourcePermitResource("system:serviceaccount:operations:runner")
		oldObj.SetLabels(map[string]string{
			meta.ProtectedByCapsuleLabel:       meta.ValueControllerReplications,
			meta.ResourcePermitProtectionLabel: meta.ValueTrue,
		})
		newObj := oldObj.DeepCopy()
		newObj.SetLabels(nil)
		newObj.SetAnnotations(map[string]string{meta.ResourcePermitServiceAccountAnnotation: "system:serviceaccount:attacker:runner"})
		decoder := &webhooktest.Decoder[*unstructured.Unstructured]{Object: newObj, OldObject: oldObj}
		for _, username := range []string{"attacker", "operations"} {
			response := handler.OnUpdate(nil, nil, decoder, nil)(t.Context(), admission.Request{UserInfo: users.ServiceAccountUserInfo(username, "runner")})
			if username == "attacker" {
				webhooktest.VerifyResponse(t, response, 403, "can only be changed by the Capsule controller")
			} else if response != nil {
				t.Fatalf("expected original ServiceAccount to remain authorized, got %#v", response)
			}
		}
		response := handler.OnDelete(nil, nil, decoder, nil)(t.Context(), admission.Request{UserInfo: users.ServiceAccountUserInfo("attacker", "runner")})
		webhooktest.VerifyResponse(t, response, 403, "can only be changed by the Capsule controller")
	})
}

func BenchmarkResourcePermitProtectionAdmission(b *testing.B) {
	for _, independent := range []bool{false, true} {
		for _, authorized := range []bool{false, true} {
			b.Run(fmt.Sprintf("independent=%t/authorized=%t", independent, authorized), func(b *testing.B) {
				obj := protectedResourcePermitResource("system:serviceaccount:test:runner")
				obj.SetAPIVersion("v1")
				obj.SetKind("ConfigMap")
				obj.SetName("shared")
				if independent {
					obj.SetLabels(map[string]string{meta.ProtectedByCapsuleLabel: meta.ValueControllerReplications, meta.ResourcePermitProtectionLabel: meta.ValueTrue})
				}
				raw, err := json.Marshal(obj)
				if err != nil {
					b.Fatal(err)
				}
				request := admission.Request{Object: runtime.RawExtension{Raw: raw}, OldObject: runtime.RawExtension{Raw: raw}, UserInfo: authenticationv1.UserInfo{Username: "tenant-owner"}}
				if authorized {
					request.UserInfo = users.ServiceAccountUserInfo("test", "runner")
				}
				handler := ResourcePermitResourceHandler().OnUpdate(nil, nil, admission.NewDecoder(runtime.NewScheme()), nil)
				b.ReportAllocs()
				for b.Loop() {
					response := handler(b.Context(), request)
					if (response == nil) != authorized {
						b.Fatalf("unexpected admission decision: %#v", response)
					}
				}
			})
		}
	}
}

func protectedResourcePermitResource(serviceAccount string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetLabels(map[string]string{
		meta.ProtectedByCapsuleLabel: meta.ValueControllerResourcePermit,
	})
	obj.SetAnnotations(map[string]string{
		meta.ResourcePermitServiceAccountAnnotation: serviceAccount,
	})

	return obj
}
