// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant_test

import (
	"sync"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	tenant "github.com/projectcapsule/capsule/pkg/tenant"
)

// Helpers

func ns(name string, uid types.UID) *corev1.Namespace {
	n := &corev1.Namespace{}
	n.SetName(name)
	n.SetUID(uid)
	return n
}

func tenantWithName(name string) *capsulev1beta2.Tenant {
	t := &capsulev1beta2.Tenant{}
	t.SetName(name)
	if t.Annotations == nil {
		t.Annotations = map[string]string{}
	}
	return t
}

func mustInstance(t *capsulev1beta2.Tenant, name string, uid types.UID, labels, ann map[string]string) {
	// Ensure Status + instance storage exists in your impl;
	// if TenantStatus needs initialization in your project, do it here.

	item := &capsulev1beta2.TenantStatusNamespaceItem{
		Name: name,
		UID:  uid,
		Metadata: &capsulev1beta2.TenantStatusNamespaceMetadata{
			Labels:      labels,
			Annotations: ann,
		},
	}

	t.Status.UpdateInstance(item)
}

// --- Tests

func TestAddNamespaceNameLabels(t *testing.T) {
	t.Parallel()

	labels := map[string]string{"keep": "me"}
	n := ns("myns", "u1")

	tenant.AddNamespaceNameLabels(labels, n)

	if got := labels["kubernetes.io/metadata.name"]; got != "myns" {
		t.Fatalf("expected kubernetes.io/metadata.name to be %q, got %q", "myns", got)
	}
	if got := labels["keep"]; got != "me" {
		t.Fatalf("expected existing key to remain, got %q", got)
	}
}

func TestAddTenantNameLabel(t *testing.T) {
	t.Parallel()

	labels := map[string]string{}
	n := ns("myns", "u1")
	tt := tenantWithName("mytenant")

	tenant.AddTenantNameLabel(labels, n, tt)

	if got := labels[meta.TenantLabel]; got != "mytenant" {
		t.Fatalf("expected %s to be %q, got %q", meta.TenantLabel, "mytenant", got)
	}
}

func TestBuildInstanceMetadataForNamespace_NoInstance(t *testing.T) {
	t.Parallel()

	n := ns("myns", "u1")
	tt := tenantWithName("t1")

	labels, annotations := tenant.BuildInstanceMetadataForNamespace(n, tt)

	if labels == nil || annotations == nil {
		t.Fatalf("expected non-nil maps")
	}
	if len(labels) != 0 || len(annotations) != 0 {
		t.Fatalf("expected empty maps, got labels=%v annotations=%v", labels, annotations)
	}
}

func TestBuildInstanceMetadataForNamespace_WithInstance(t *testing.T) {
	t.Parallel()

	n := ns("myns", "u1")
	tt := tenantWithName("t1")

	origLabels := map[string]string{"a": "1"}
	origAnn := map[string]string{"x": "y"}
	mustInstance(tt, n.GetName(), n.GetUID(), origLabels, origAnn)

	labels, annotations := tenant.BuildInstanceMetadataForNamespace(n, tt)

	// Implementation returns instance.Metadata maps directly (not cloned)
	if labels["a"] != "1" || annotations["x"] != "y" {
		t.Fatalf("unexpected returned metadata: labels=%v annotations=%v", labels, annotations)
	}
}

func TestBuildNamespaceLabelsForTenant(t *testing.T) {
	t.Parallel()

	t.Run("additional labels copied + cordoned", func(t *testing.T) {
		t.Parallel()

		tt := tenantWithName("t1")
		tt.Spec.Cordoned = true
		tt.Spec.NamespaceOptions = &capsulev1beta2.NamespaceOptions{
			//nolint:staticcheck
			AdditionalMetadata: &api.AdditionalMetadataSpec{
				Labels: map[string]string{
					"base": "label",
				},
			},
		}

		labels := tenant.BuildNamespaceLabelsForTenant(tt)

		if labels["base"] != "label" {
			t.Fatalf("expected base label copied, got %v", labels)
		}
		if labels[meta.CordonedLabel] != "true" {
			t.Fatalf("expected cordoned label true, got %v", labels[meta.CordonedLabel])
		}
	})
}

func TestBuildNamespaceAnnotationsForTenant(t *testing.T) {
	t.Parallel()

	t.Run("copies additional annotations and forwards forbidden annotations", func(t *testing.T) {
		t.Parallel()

		tt := tenantWithName("t1")
		tt.Spec.NamespaceOptions = &capsulev1beta2.NamespaceOptions{
			//nolint:staticcheck
			AdditionalMetadata: &api.AdditionalMetadataSpec{
				Annotations: map[string]string{
					"a": "b",
				},
			},
		}

		tt.Annotations[meta.ForbiddenNamespaceLabelsAnnotation] = "l1,l2"
		tt.Annotations[meta.ForbiddenNamespaceAnnotationsAnnotation] = "a1,a2"

		ann := tenant.BuildNamespaceAnnotationsForTenant(tt)

		if ann["a"] != "b" {
			t.Fatalf("expected additional annotation copied, got %v", ann)
		}
		if ann[meta.ForbiddenNamespaceLabelsAnnotation] != "l1,l2" {
			t.Fatalf("expected forbidden labels annotation forwarded, got %v", ann[meta.ForbiddenNamespaceLabelsAnnotation])
		}
		if ann[meta.ForbiddenNamespaceAnnotationsAnnotation] != "a1,a2" {
			t.Fatalf("expected forbidden annotations forwarded, got %v", ann[meta.ForbiddenNamespaceAnnotationsAnnotation])
		}
	})

	t.Run("ingress/storage/registry exact join", func(t *testing.T) {
		t.Parallel()

		tt := tenantWithName("t1")
		tt.Spec.IngressOptions.AllowedClasses = &api.DefaultAllowedListSpec{
			SelectorAllowedListSpec: api.SelectorAllowedListSpec{
				AllowedListSpec: api.AllowedListSpec{
					Exact: []string{"nginx", "traefik"},
				},
			},
		}
		tt.Spec.StorageClasses = &api.DefaultAllowedListSpec{
			SelectorAllowedListSpec: api.SelectorAllowedListSpec{
				AllowedListSpec: api.AllowedListSpec{
					Exact: []string{"fast", "slow"},
				},
			},
		}
		tt.Spec.ContainerRegistries = &api.AllowedListSpec{
			Exact: []string{"docker.io", "ghcr.io"},
		}

		ann := tenant.BuildNamespaceAnnotationsForTenant(tt)

		if ann[meta.AvailableIngressClassesAnnotation] != "nginx,traefik" {
			t.Fatalf("unexpected ingress exact annotation: %v", ann[meta.AvailableIngressClassesAnnotation])
		}
		if ann[meta.AvailableStorageClassesAnnotation] != "fast,slow" {
			t.Fatalf("unexpected storage exact annotation: %v", ann[meta.AvailableStorageClassesAnnotation])
		}
		if ann[meta.AllowedRegistriesAnnotation] != "docker.io,ghcr.io" {
			t.Fatalf("unexpected registries exact annotation: %v", ann[meta.AllowedRegistriesAnnotation])
		}
	})
}

func TestBuildNamespaceMetadataForTenant_AppliesAdditionalMetadataAndDoesNotOverwrite(t *testing.T) {
	t.Parallel()

	tt := tenantWithName("tenant-x")

	// Base AdditionalMetadata (staticcheck path)
	tt.Spec.NamespaceOptions = &capsulev1beta2.NamespaceOptions{
		//nolint:staticcheck
		AdditionalMetadata: &api.AdditionalMetadataSpec{
			Labels: map[string]string{
				"base": "keep",
				"dup":  "base-val",
			},
			Annotations: map[string]string{
				"baseAnn": "keep",
				"dupAnn":  "base-ann",
			},
		},
		AdditionalMetadataList: []api.AdditionalMetadataSelectorSpec{
			{
				// Use an “empty selector” that should match everything in your selector logic.
				// If your IsNamespaceSelectedBySelector behaves differently, set selector accordingly.
				NamespaceSelector: nil,
				Labels: map[string]string{
					"extra":        "{{ tenant.name }}",
					"dup":          "should-not-overwrite",
					"namespaceKey": "{{ namespace }}",
				},
				Annotations: map[string]string{
					"extraAnn": "{{ tenant.name }}",
					"dupAnn":   "should-not-overwrite",
				},
			},
		},
	}

	n := ns("ns-1", "u1")

	labels, ann, err := tenant.BuildNamespaceMetadataForTenant(n, tt)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	// templating must apply
	if labels["extra"] != "tenant-x" {
		t.Fatalf("expected templated label extra=tenant-x, got %q", labels["extra"])
	}
	if labels["namespaceKey"] != "ns-1" {
		t.Fatalf("expected templated namespaceKey=ns-1, got %q", labels["namespaceKey"])
	}
	if ann["extraAnn"] != "tenant-x" {
		t.Fatalf("expected templated annotation extraAnn=tenant-x, got %q", ann["extraAnn"])
	}

	// MapMergeNoOverrite means base wins on duplicates
	if labels["dup"] != "base-val" {
		t.Fatalf("expected duplicate label to remain base-val, got %q", labels["dup"])
	}
	if ann["dupAnn"] != "base-ann" {
		t.Fatalf("expected duplicate annotation to remain base-ann, got %q", ann["dupAnn"])
	}

	// base keys remain
	if labels["base"] != "keep" || ann["baseAnn"] != "keep" {
		t.Fatalf("expected base metadata to remain, labels=%v ann=%v", labels, ann)
	}
}

func TestBuildNamespaceMetadataForTenant_Concurrency_NoConcurrentMapWrites(t *testing.T) {
	// Don’t run this test in parallel with other tests if your package has global shared state.
	// Keep it isolated; this is meant to be run with: go test -race ./...
	tt := tenantWithName("tenant-race")
	n := ns("ns-race", "u-race")

	// Critical: reuse the SAME maps inside AdditionalMetadataList across goroutines.
	// If TemplateForTenantAndNamespaceMap still mutates in-place, this can panic.
	sharedLabels := map[string]string{
		"l1": "{{ tenant.name }}",
		"l2": "{{ namespace }}",
	}
	sharedAnn := map[string]string{
		"a1": "{{ tenant.name }}",
	}

	tt.Spec.NamespaceOptions = &capsulev1beta2.NamespaceOptions{
		AdditionalMetadataList: []api.AdditionalMetadataSelectorSpec{
			{
				NamespaceSelector: nil,
				Labels:            sharedLabels,
				Annotations:       sharedAnn,
			},
		},
	}

	const goroutines = 50
	const iterations = 200

	var wg sync.WaitGroup
	wg.Add(goroutines)

	errCh := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()

			for j := 0; j < iterations; j++ {
				_, _, err := tenant.BuildNamespaceMetadataForTenant(n, tt)
				if err != nil {
					errCh <- err
					return
				}
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("unexpected error under concurrency: %v", err)
	}
}
