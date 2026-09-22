// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"encoding/base64"
	"os"
	"testing"

	"filippo.io/age"
	corev1 "k8s.io/api/core/v1"
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/yaml"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/processor"
)

func TestAgeRotationExampleRetainsAllKeys(t *testing.T) {
	data, err := os.ReadFile("../../../playground/platform/globaltenantresources/gtr-age-keys.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var example capsulev1beta2.GlobalTenantResource
	if err := yaml.UnmarshalStrict(data, &example); err != nil {
		t.Fatal(err)
	}
	mapper := k8smeta.NewDefaultRESTMapper([]schema.GroupVersion{{Version: "v1"}})
	mapper.Add(corev1.SchemeGroupVersion.WithKind("Secret"), k8smeta.RESTScopeNamespace)
	c := fake.NewClientBuilder().Build()
	collector := NewCollector(c, mapper)
	spec := example.Spec.Resources[0]
	histories := map[string]map[string]string{}
	for rotation := range 3 {
		for _, tenantName := range []string{"solar", "wind"} {
			tnt := &capsulev1beta2.Tenant{Name: tenantName}
			ns := &corev1.Namespace{Name: tenantName + "-gitops"}
			acc := processor.Accumulator{}
			opts := CollectorOptions{Accumulator: acc, Iterator: NewCollectorIteratorOptions(tnt, ns, spec)}
			if err := collector.Collect(t.Context(), c, opts, tnt, "0", spec, ns); err != nil {
				t.Fatal(err)
			}
			if len(acc) != 1 {
				t.Fatalf("collected %d resources", len(acc))
			}
			for _, item := range acc {
				generated := (*item.Objects)[0]
				if generated.Object.GetName() != tenantName+"-age-keys" || generated.Object.GetNamespace() != ns.Name {
					t.Fatal("incorrect templated identity")
				}
				if generated.ExpectedResourceVersion == nil {
					t.Fatal("target snapshot was not recorded")
				}
				if rotation == 0 && *generated.ExpectedResourceVersion != "" {
					t.Fatal("initial missing target was not recorded")
				}
				entries, found, err := unstructured.NestedStringMap(generated.Object.Object, "data")
				if err != nil || !found {
					t.Fatal("missing Secret data")
				}
				decodedRecipient, err := base64.StdEncoding.DecodeString(entries["recipient"])
				if err != nil {
					t.Fatal(err)
				}
				delete(entries, "recipient")
				if len(entries) != rotation+1 {
					t.Fatalf("rotation %d tenant %s has %d identity entries", rotation, tenantName, len(entries))
				}
				if _, exists := entries[string(decodedRecipient)+".agekey"]; !exists {
					t.Fatal("current recipient has no dedicated private-key entry")
				}
				if _, exists := histories[tenantName][string(decodedRecipient)+".agekey"]; exists {
					t.Fatal("current recipient did not change on rotation")
				}
				for field, encoded := range entries {
					identityBytes, err := base64.StdEncoding.DecodeString(encoded)
					if err != nil {
						t.Fatal("invalid identity encoding")
					}
					identity, err := age.ParseX25519Identity(string(identityBytes))
					if err != nil || field != identity.Recipient().String()+".agekey" {
						t.Fatal("entry must contain exactly one identity named after its recipient")
					}
					for otherTenant, otherEntries := range histories {
						if otherTenant != tenantName {
							if _, exists := otherEntries[field]; exists {
								t.Fatal("key entry crossed tenant boundaries")
							}
						}
					}
				}
				for field, previous := range histories[tenantName] {
					if entries[field] != previous {
						t.Fatal("rotation removed, renamed, or changed an earlier key entry")
					}
				}
				stored := generated.Object.DeepCopy()
				if rotation == 0 {
					if err := c.Create(t.Context(), stored); err != nil {
						t.Fatal(err)
					}
				} else {
					actual := &unstructured.Unstructured{}
					actual.SetGroupVersionKind(stored.GroupVersionKind())
					if err := c.Get(t.Context(), client.ObjectKeyFromObject(stored), actual); err != nil {
						t.Fatal(err)
					}
					if *generated.ExpectedResourceVersion != actual.GetResourceVersion() {
						t.Fatal("wrong snapshot version")
					}
					stored.SetResourceVersion(actual.GetResourceVersion())
					if err := c.Update(t.Context(), stored); err != nil {
						t.Fatal(err)
					}
				}
				histories[tenantName] = entries
			}
		}
	}
}
