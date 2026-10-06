// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/quota"
	"github.com/projectcapsule/capsule/pkg/runtime/selectors"
)

func newGlobalPodCPUQuota(name, namespace string) *capsulev1beta2.GlobalCustomQuota {
	return &capsulev1beta2.GlobalCustomQuota{
		Name: name,
		Labels: map[string]string{
			"e2e.capsule.dev/test-suite": "globalcustomquota-ledger",
		},
		Spec: capsulev1beta2.GlobalCustomQuotaSpec{
			// Other parallel tests also use track=yes, including Pods without
			// CPU requests. Limit this fixture's accounting to its namespace.
			NamespaceSelectors: []selectors.NamespaceSelector{{
				LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{corev1.LabelMetadataName: namespace}},
			}},
			CustomQuotaSpec: capsulev1beta2.CustomQuotaSpec{
				Limit: resource.MustParse("500m"),
				Sources: []capsulev1beta2.CustomQuotaSpecSource{{
					APIVersion: "v1", Kind: "Pod", Operation: quota.OpAdd,
					Path: ".spec.containers[*].resources.requests.cpu",
					Selectors: []selectors.SelectorWithFields{{
						LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"track": "yes"}},
					}},
				}},
			},
		},
	}
}

func TestGlobalPodCPUQuotaFixtureNamespaceIsolation(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	namespace := func(name, tenant string) *corev1.Namespace {
		return &corev1.Namespace{Name: name, Labels: map[string]string{
			corev1.LabelMetadataName: name, meta.TenantLabel: tenant, "env": "e2e",
		}}
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		namespace("tenant-a-selected", "tenant-a"),
		namespace("tenant-a-other", "tenant-a"),
		namespace("tenant-b-selected", "tenant-b"),
	).Build()
	for _, selected := range []string{"tenant-a-selected", "tenant-b-selected", "absent"} {
		t.Run(selected, func(t *testing.T) {
			fixture := newGlobalPodCPUQuota("cpu", selected)
			require.NotEmpty(t, fixture.Spec.NamespaceSelectors, "an omitted selector makes the quota cluster-wide")
			actual, err := selectors.GetNamespacesMatchingSelectorsStrings(t.Context(), c, fixture.Spec.NamespaceSelectors)
			require.NoError(t, err)
			if selected == "absent" {
				require.Empty(t, actual)
			} else {
				require.Equal(t, []string{selected}, actual)
			}
		})
	}
}
