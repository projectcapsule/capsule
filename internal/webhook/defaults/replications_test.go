// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package defaults

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	jsonpatch "github.com/evanphx/json-patch/v5"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
)

func TestReplicationPolicyConversion(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	decoder := admission.NewDecoder(scheme)
	explicit := &apiruntime.ResourceTemplatePolicy{Creation: apiruntime.ResourceCreationPolicyOwner, Protect: new(false), Deletion: apiruntime.ResourceDeletionPolicyRemove}
	for _, kind := range []string{"TenantResource", "GlobalTenantResource"} {
		for _, operation := range []admissionv1.Operation{admissionv1.Create, admissionv1.Update} {
			for _, legacy := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/legacy=%t", kind, operation, legacy), func(t *testing.T) {
					spec := capsulev1beta2.TenantResourceCommonSpec{
						Settings:        capsulev1beta2.TenantResourceCommonSpecSettings{Adopt: &legacy, Force: &legacy},
						PruningOnDelete: new(!legacy),
						Resources: []capsulev1beta2.ResourceSpec{
							{RawItems: []capsulev1beta2.RawExtension{{Raw: []byte(`{"apiVersion":"example.com/v1","kind":"Custom","metadata":{"name":"raw"},"unknown":{"retained":true}}`)}}},
							{Policy: explicit},
						},
					}
					req := replicationPolicyRequest(t, kind, spec)
					req.Operation = operation
					h := Handler(nil, nil, nil)
					run := h.OnCreate(nil, nil, decoder, nil)
					if operation == admissionv1.Update {
						run = h.OnUpdate(nil, nil, decoder, nil)
					}
					response := run(t.Context(), req)
					if response == nil || !response.Allowed || len(response.Patches) != 1 || response.Patches[0].Path != "/spec/resources/0/policy" {
						t.Fatalf("unexpected conversion response: %#v", response)
					}
					encoded, err := json.Marshal(response.Patches)
					if err != nil {
						t.Fatal(err)
					}
					patch, err := jsonpatch.DecodePatch(encoded)
					if err != nil {
						t.Fatal(err)
					}
					mutated, err := patch.Apply(req.Object.Raw)
					if err != nil {
						t.Fatal(err)
					}
					var actual struct {
						Spec capsulev1beta2.TenantResourceCommonSpec `json:"spec"`
					}
					if err := json.Unmarshal(mutated, &actual); err != nil {
						t.Fatal(err)
					}
					want := &apiruntime.ResourceTemplatePolicy{Creation: apiruntime.ResourceCreationPolicyOwner, Protect: new(true), Force: legacy, Deletion: apiruntime.ResourceDeletionPolicyRemove}
					if legacy {
						want.Creation = apiruntime.ResourceCreationPolicyMerge
						want.Deletion = apiruntime.ResourceDeletionPolicyOrphan
					}
					if !reflect.DeepEqual(actual.Spec.Resources[0].Policy, want) || !reflect.DeepEqual(actual.Spec.Resources[1].Policy, explicit) {
						t.Fatalf("incorrect converted or explicit policies: %#v", actual.Spec.Resources)
					}
					// Removing only our new field must reconstruct the exact original JSON.
					var original, result map[string]any
					if err := json.Unmarshal(req.Object.Raw, &original); err != nil {
						t.Fatal(err)
					}
					if err := json.Unmarshal(mutated, &result); err != nil {
						t.Fatal(err)
					}
					delete(result["spec"].(map[string]any)["resources"].([]any)[0].(map[string]any), "policy")
					if !reflect.DeepEqual(original, result) {
						t.Fatal("conversion modified existing fields")
					}
					req.Object.Raw = mutated
					if next := run(t.Context(), req); !next.Allowed || len(next.Patches) != 0 {
						t.Fatal("conversion is not idempotent")
					}
				})
			}
		}
	}
}

func TestReplicationPolicyConversionDefaultsAndSkips(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	decoder := admission.NewDecoder(scheme)
	h := Handler(nil, nil, nil)
	run := h.OnCreate(nil, nil, decoder, nil)
	req := replicationPolicyRequest(t, "TenantResource", capsulev1beta2.TenantResourceCommonSpec{Resources: []capsulev1beta2.ResourceSpec{{}}})
	response := run(t.Context(), req)
	if !response.Allowed || len(response.Patches) != 1 {
		t.Fatalf("missing defaults: %#v", response)
	}
	policy := response.Patches[0].Value.(apiruntime.ResourceTemplatePolicy)
	if policy.AllowsAdoption() || policy.Force || !policy.IsProtected() || policy.ShouldOrphan() {
		t.Fatalf("incorrect defaults: %#v", policy)
	}
	req.SubResource = "status"
	req.Object.Raw = []byte(`invalid`)
	if response := run(t.Context(), req); !response.Allowed || len(response.Patches) != 0 {
		t.Fatal("status should skip decoding")
	}
	req.SubResource = ""
	if response := run(t.Context(), req); response.Allowed {
		t.Fatal("malformed replication was accepted")
	}
	if response := h.OnDelete(nil, nil, decoder, nil)(t.Context(), req); response != nil {
		t.Fatal("delete should not convert")
	}
	req.Resource.Resource = "unrelated"
	if response := run(t.Context(), req); !response.Allowed || len(response.Patches) != 0 {
		t.Fatal("unrelated resource was decoded")
	}
}

func BenchmarkReplicationPolicyConversion(b *testing.B) {
	scheme := runtime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		b.Fatal(err)
	}
	run := Handler(nil, nil, nil).OnCreate(nil, nil, admission.NewDecoder(scheme), nil)
	for _, blocks := range []int{1, 100, 1000} {
		for _, explicit := range []bool{false, true} {
			b.Run(fmt.Sprintf("blocks=%d/explicit=%t", blocks, explicit), func(b *testing.B) {
				spec := capsulev1beta2.TenantResourceCommonSpec{Resources: make([]capsulev1beta2.ResourceSpec, blocks)}
				for i := range spec.Resources {
					spec.Resources[i].RawItems = []capsulev1beta2.RawExtension{{Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"example"},"data":{"key":"value"}}`)}}
					if explicit {
						spec.Resources[i].Policy = &apiruntime.ResourceTemplatePolicy{}
					}
				}
				req := replicationPolicyRequest(b, "GlobalTenantResource", spec)
				wantPatches := blocks
				if explicit {
					wantPatches = 0
				}
				b.ReportAllocs()
				for b.Loop() {
					response := run(b.Context(), req)
					if !response.Allowed || len(response.Patches) != wantPatches {
						b.Fatalf("unexpected response: %#v", response)
					}
				}
			})
		}
	}
}

func replicationPolicyRequest(t testing.TB, kind string, spec capsulev1beta2.TenantResourceCommonSpec) admission.Request {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"apiVersion": capsulev1beta2.GroupVersion.String(), "kind": kind, "metadata": map[string]any{"name": "example", "namespace": "tenant-a"}, "spec": spec})
	if err != nil {
		t.Fatal(err)
	}
	resource := "tenantresources"
	if kind == "GlobalTenantResource" {
		resource = "globaltenantresources"
	}
	return admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Operation: admissionv1.Create,
		Resource:  metav1.GroupVersionResource{Group: capsulev1beta2.GroupVersion.Group, Version: "v1beta2", Resource: resource},
		Object:    runtime.RawExtension{Raw: raw},
	}}
}
