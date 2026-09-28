// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	discoveryfake "k8s.io/client-go/discovery/fake"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/metrics"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	namespaceindex "github.com/projectcapsule/capsule/pkg/runtime/indexers/namespace"
)

// Measure the Tenant's namespace phase while other namespaces are terminating.
// Resource API calls are reported separately from fake-client execution time.
func BenchmarkTenantNamespaceChurn(b *testing.B) {
	for _, tenants := range []int{1, 16} {
		for _, terminating := range []int{1, 4} {
			for _, resources := range []int{30, 300} {
				b.Run(fmt.Sprintf("tenants=%d/terminating=%d/resourceTypes=%d", tenants, terminating, resources), func(b *testing.B) {
					scheme := runtime.NewScheme()
					if err := corev1.AddToScheme(scheme); err != nil {
						b.Fatal(err)
					}
					if err := capsulev1beta2.AddToScheme(scheme); err != nil {
						b.Fatal(err)
					}
					objects := []client.Object{}
					var target *capsulev1beta2.Tenant
					for t := 0; t < tenants; t++ {
						tnt := &capsulev1beta2.Tenant{Name: fmt.Sprintf("tenant-%d", t), UID: types.UID(fmt.Sprintf("tenant-%d", t))}
						objects = append(objects, tnt)
						if t == 0 {
							target = tnt.DeepCopy()
						}
						for n := 0; n <= terminating; n++ {
							ns := &corev1.Namespace{Name: fmt.Sprintf("tenant-%d-ns-%d", t, n), UID: types.UID(fmt.Sprintf("ns-%d-%d", t, n)), OwnerReferences: []metav1.OwnerReference{{APIVersion: capsulev1beta2.GroupVersion.String(), Kind: "Tenant", Name: tnt.Name, UID: tnt.UID}}}
							if n > 0 {
								stamp := metav1.NewTime(time.Now().Add(-time.Hour))
								ns.DeletionTimestamp = &stamp
								ns.Finalizers = []string{"example.com/hold"}
							}
							objects = append(objects, ns)
						}
					}
					cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithIndex(&corev1.Namespace{}, namespaceindex.OwnerReferenceIndex, func(o client.Object) []string { return []string{o.GetOwnerReferences()[0].Name} }).Build()
					listKinds := map[schema.GroupVersionResource]string{}
					discovery := &benchmarkDiscovery{FakeDiscovery: &discoveryfake.FakeDiscovery{Fake: &ktesting.Fake{}}}
					resourceList := &metav1.APIResourceList{GroupVersion: "example.com/v1"}
					for i := 0; i < resources; i++ {
						name, kind := fmt.Sprintf("objects%d", i), fmt.Sprintf("Object%d", i)
						listKinds[schema.GroupVersionResource{Group: "example.com", Version: "v1", Resource: name}] = kind + "List"
						resourceList.APIResources = append(resourceList.APIResources, metav1.APIResource{Name: name, Kind: kind, Namespaced: true, Verbs: metav1.Verbs{"get", "list", "delete", "patch"}})
					}
					discovery.Resources = []*metav1.APIResourceList{resourceList}
					dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, listKinds)
					manager := &Manager{Client: cl, reader: cl, DynamicClient: dyn, DiscoveryClient: discovery, Metrics: metrics.NewTenantRecorder()}
					// Warm the RuleStatus and discovery paths before measuring steady-state work.
					if err := manager.reconcileActiveTenantNamespaces(context.Background(), logr.Discard(), target); err != nil {
						b.Fatal(err)
					}
					lists := 0
					b.ReportAllocs()
					b.ResetTimer()
					for b.Loop() {
						dyn.ClearActions()
						if err := manager.reconcileActiveTenantNamespaces(context.Background(), logr.Discard(), target); err != nil {
							b.Fatal(err)
						}
						for _, action := range dyn.Actions() {
							if action.GetVerb() == "list" {
								lists++
							}
						}
						if len(target.Status.Spaces) != terminating+1 {
							b.Fatal("missing namespace status")
						}
						if target.Status.Spaces[0].Conditions.GetConditionByType(meta.ReadyCondition) == nil {
							b.Fatal("missing readiness")
						}
					}
					b.ReportMetric(float64(lists)/float64(b.N), "dynamic-LIST/op")
				})
			}
		}
	}
}

type benchmarkDiscovery struct{ *discoveryfake.FakeDiscovery }

func (d *benchmarkDiscovery) ServerPreferredNamespacedResources() ([]*metav1.APIResourceList, error) {
	return d.Resources, nil
}
