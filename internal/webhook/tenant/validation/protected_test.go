// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func tenantDeletionRequest(tb testing.TB, state string, ruleCount int) (admission.Request, admission.Decoder) {
	tb.Helper()
	scheme := runtime.NewScheme()
	require.NoError(tb, capsulev1beta2.AddToScheme(scheme))
	tnt := &capsulev1beta2.Tenant{Name: "tenant-a", UID: "tenant-a-uid", Finalizers: []string{meta.ControllerFinalizer}}
	for range ruleCount {
		tnt.Spec.Rules = append(tnt.Spec.Rules, &rules.NamespaceRuleBodyTenant{NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{}})
	}
	switch state {
	case "legacy":
		tnt.Finalizers = []string{"example.com/other"}
	case "protected":
		tnt.Spec.PreventDeletion = true
	case "deleting":
		now := metav1.Now()
		tnt.DeletionTimestamp = &now
		tnt.Finalizers = []string{"example.com/other"}
	}
	raw, err := json.Marshal(tnt)
	require.NoError(tb, err)
	return admission.Request{Operation: admissionv1.Delete, OldObject: runtime.RawExtension{Raw: raw}}, admission.NewDecoder(scheme)
}

func TestTenantDeletionRequiresLifecycleProtection(t *testing.T) {
	for _, state := range []string{"ready", "legacy", "protected", "deleting"} {
		for _, dryRun := range []*bool{nil, new(false), new(true)} {
			t.Run(fmt.Sprintf("%s/dryRun=%v", state, dryRun), func(t *testing.T) {
				req, decoder := tenantDeletionRequest(t, state, 1)
				req.DryRun = dryRun
				before := append([]byte(nil), req.OldObject.Raw...)
				// Nil clients make any API lookup fail, including on the allow path.
				response := Handler(nil, ProtectedHandler()).OnDelete(nil, nil, decoder, nil)(t.Context(), req)
				switch state {
				case "ready", "deleting":
					require.Nil(t, response, "continue subsequent admission checks")
				case "legacy":
					require.NotNil(t, response)
					require.False(t, response.Allowed)
					require.Contains(t, response.Result.Message, "tenant lifecycle protection is not ready")
				case "protected":
					require.NotNil(t, response)
					require.False(t, response.Allowed)
					require.Contains(t, response.Result.Message, "tenant is protected and cannot be deleted")
				}
				require.Equal(t, before, req.OldObject.Raw)
			})
		}
	}
	_, decoder := tenantDeletionRequest(t, "ready", 0)
	response := Handler(nil, ProtectedHandler()).OnDelete(nil, nil, decoder, nil)(t.Context(), admission.Request{})
	require.NotNil(t, response)
	require.False(t, response.Allowed)
}

func BenchmarkTenantDeletionAdmission(b *testing.B) {
	for _, state := range []string{"ready", "legacy", "protected", "deleting"} {
		for _, ruleCount := range []int{1, 32} {
			b.Run(fmt.Sprintf("%s/rules=%d", state, ruleCount), func(b *testing.B) {
				req, decoder := tenantDeletionRequest(b, state, ruleCount)
				handler := Handler(nil, ProtectedHandler()).OnDelete(nil, nil, decoder, nil)
				b.ReportAllocs()
				for b.Loop() {
					response := handler(b.Context(), req)
					if denied := response != nil && !response.Allowed; denied != (state == "legacy" || state == "protected") {
						b.Fatalf("unexpected deletion decision: %+v", response)
					}
				}
				b.ReportMetric(0, "GET/op")
				b.ReportMetric(0, "LIST/op")
			})
		}
	}
}
