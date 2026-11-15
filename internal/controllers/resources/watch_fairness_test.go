// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"fmt"
	"testing"
	"time"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func fairWatchFixture(t testing.TB) *watchFixture {
	t.Helper()
	f := watchFixtureFor(t, 0)
	for _, object := range []client.Object{&capsulev1beta2.TenantResource{Name: "a", Namespace: "a"}, &capsulev1beta2.TenantResource{Name: "b", Namespace: "b"}, &capsulev1beta2.GlobalTenantResource{Name: "global"}} {
		if err := f.client.Delete(t.Context(), object); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"a", "b"} {
		tenant := &capsulev1beta2.Tenant{Name: name, UID: types.UID("tenant-" + name)}
		if err := f.client.Create(t.Context(), tenant); err != nil {
			t.Fatal(err)
		}
		ns := &corev1.Namespace{}
		if err := f.client.Get(t.Context(), client.ObjectKey{Name: name}, ns); err != nil {
			t.Fatal(err)
		}
		ns.OwnerReferences = []metav1.OwnerReference{{APIVersion: capsulev1beta2.GroupVersion.String(), Kind: "Tenant", Name: name, UID: tenant.UID}}
		if err := f.client.Update(t.Context(), ns); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func fairWatchSource(namespace, name, account string, large bool) *capsulev1beta2.TenantResource {
	trigger := capsulev1beta2.TenantResourceTrigger{Name: "events", Operations: []admissionv1.OperationType{admissionv1.Update}, APIGroups: []string{""}, APIVersions: []string{"v1"}, Resources: []string{"configmaps"}}
	source := &capsulev1beta2.TenantResource{Name: name, Namespace: namespace, UID: types.UID(namespace + "/" + name), Generation: 1, Spec: capsulev1beta2.TenantResourceSpec{ServiceAccount: &meta.LocalRFC1123ObjectReference{Name: meta.RFC1123Name(account)}, TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{}}}
	count := 1
	if large {
		count = 32
		for index := range 60 {
			trigger.MatchConditions = append(trigger.MatchConditions, admissionv1.MatchCondition{Name: fmt.Sprintf("condition-%d", index), Expression: "true"})
		}
	}
	for index := range count {
		copy := *trigger.DeepCopy()
		copy.Name = fmt.Sprintf("events-%d", index)
		source.Spec.Triggers = append(source.Spec.Triggers, copy)
	}
	return source
}

func TestOneTenantCannotReserveEveryWatchStream(t *testing.T) {
	f := fairWatchFixture(t)
	for index := range maxResourceWatches {
		name := fmt.Sprintf("source-%02d", index)
		if err := f.client.Create(t.Context(), fairWatchSource("a", name, name, false)); err != nil {
			t.Fatal(err)
		}
	}
	other := fairWatchSource("b", "other", "reader", false)
	if err := f.client.Create(t.Context(), other); err != nil {
		t.Fatal(err)
	}
	if _, err := f.watcher.Reconcile(t.Context(), reconcile.Request{}); err != nil {
		t.Fatal(err)
	}
	key := resourceWatchKey{resource: configMapsGVR, namespace: "b", identity: resourceWatchIdentity{account: cache.Key{Namespace: "b", Name: "reader"}}}
	if _, ok := f.watcher.desired[key]; !ok {
		t.Fatal("tenant A's identities permanently excluded tenant B's stream")
	}
	if len(f.watcher.desired) != maxResourceWatches {
		t.Fatal("fair sharing changed the global stream bound")
	}
}

func TestPreparationRebalancesAcrossTenants(t *testing.T) {
	f := fairWatchFixture(t)
	for index := range 4 {
		if err := f.client.Create(t.Context(), fairWatchSource("a", fmt.Sprintf("source-%d", index), "reader", true)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.watcher.Reconcile(t.Context(), reconcile.Request{}); err != nil {
		t.Fatal(err)
	}
	other := fairWatchSource("b", "other", "reader", true)
	if err := f.client.Create(t.Context(), other); err != nil {
		t.Fatal(err)
	}
	if _, err := f.watcher.Reconcile(t.Context(), reconcile.Request{}); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.watcher.prepared.Get(other.UID, other.Generation); !ok {
		t.Fatal("tenant A's retained programs excluded a newly active tenant")
	}
}

func TestWatchBudgetRotatesTenantsWithoutEventDrivenChurn(t *testing.T) {
	f := fairWatchFixture(t)
	now := time.Unix(1_000, 0)
	f.watcher.now = func() time.Time { return now }
	f.watcher.rotationDue = time.Time{}
	for index := range maxResourceWatches + 1 {
		name := fmt.Sprintf("tenant-%03d", index)
		tenant := &capsulev1beta2.Tenant{Name: name, UID: types.UID(name)}
		if err := f.client.Create(t.Context(), tenant); err != nil {
			t.Fatal(err)
		}
		ns := &corev1.Namespace{Name: name, UID: types.UID("ns-" + name), OwnerReferences: []metav1.OwnerReference{{APIVersion: capsulev1beta2.GroupVersion.String(), Kind: "Tenant", Name: name, UID: tenant.UID}}}
		if err := f.client.Create(t.Context(), ns); err != nil {
			t.Fatal(err)
		}
		if err := f.client.Create(t.Context(), fairWatchSource(name, "source", "reader", false)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.watcher.Reconcile(t.Context(), reconcile.Request{}); err != nil {
		t.Fatal(err)
	}
	first := make(map[resourceWatchKey]struct{})
	for key := range f.watcher.desired {
		first[key] = struct{}{}
	}
	if len(first) != maxResourceWatches {
		t.Fatal("initial watch budget was not full")
	}
	for range 3 {
		if _, err := f.watcher.Reconcile(t.Context(), reconcile.Request{}); err != nil {
			t.Fatal(err)
		}
		for key := range f.watcher.desired {
			if _, ok := first[key]; !ok {
				t.Fatal("ordinary events rotated healthy streams before the interval")
			}
		}
	}
	now = now.Add(30 * time.Second)
	if _, err := f.watcher.Reconcile(t.Context(), reconcile.Request{}); err != nil {
		t.Fatal(err)
	}
	for key := range f.watcher.desired {
		first[key] = struct{}{}
	}
	if len(first) != maxResourceWatches+1 {
		t.Fatal("a tenant remained permanently excluded after budget rotation")
	}
}

func TestPlanningRejectsStaleTenantOwnership(t *testing.T) {
	f := fairWatchFixture(t)
	source := fairWatchSource("a", "source", "reader", false)
	if err := f.client.Create(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	ns := &corev1.Namespace{}
	if err := f.client.Get(t.Context(), client.ObjectKey{Name: "a"}, ns); err != nil {
		t.Fatal(err)
	}
	ns.OwnerReferences[0].UID = "replaced-tenant"
	if err := f.client.Update(t.Context(), ns); err != nil {
		t.Fatal(err)
	}
	if _, err := f.watcher.Reconcile(t.Context(), reconcile.Request{}); err != nil {
		t.Fatal(err)
	}
	if len(f.watcher.desired) != 0 {
		t.Fatal("stale Tenant ownership received a fair-share identity")
	}
}

func TestWatchKeyPlanningBoundsDeduplicatesAndPreservesAuthorizationScopes(t *testing.T) {
	keys := &watchKeySet{}
	for index := 2 * maxResourceWatches; index >= 0; index-- {
		keys.add(resourceWatchKey{resource: configMapsGVR, namespace: fmt.Sprintf("ns-%03d", index)})
	}
	if len(keys.keys) != maxResourceWatches || keys.ordered()[0].namespace != "ns-000" || keys.ordered()[maxResourceWatches-1].namespace != "ns-063" {
		t.Fatal("temporary per-group key selection is unbounded or non-deterministic")
	}
	controller := resourceWatchKey{resource: configMapsGVR}
	local := controller
	local.namespace = "a"
	globalImpersonated := controller
	globalImpersonated.identity.account = cache.Key{Namespace: "a", Name: "reader"}
	localImpersonated := globalImpersonated
	localImpersonated.namespace = "a"
	globalKeys, localKeys := &watchKeySet{}, &watchKeySet{}
	globalKeys.add(controller)
	globalKeys.add(globalImpersonated)
	localKeys.add(local)
	localKeys.add(localImpersonated)
	opportunity := map[string]bool{}
	selected, omitted := planWatchKeys([]string{"tenant/a", "system"}, map[string]*watchKeySet{"tenant/a": localKeys, "system": globalKeys}, opportunity)
	if omitted != 0 {
		t.Fatal("shared streams were counted as omitted")
	}
	if len(selected) != 3 || !opportunity["tenant/a"] || !opportunity["system"] {
		t.Fatal("sharing lost a tenant opportunity or replaced an independently authorized local stream")
	}
	if _, duplicate := selected[local]; duplicate {
		t.Fatal("controller global stream did not coalesce an equivalent local stream")
	}
}

func BenchmarkTenantFairWatchPlanning(b *testing.B) {
	for _, unrelated := range []int{0, 1000} {
		b.Run(fmt.Sprint(unrelated), func(b *testing.B) {
			f := fairWatchFixture(b)
			for index := range unrelated {
				name := fmt.Sprintf("unrelated-%d", index)
				if err := f.client.Create(b.Context(), &capsulev1beta2.Tenant{Name: name, UID: types.UID(name)}); err != nil {
					b.Fatal(err)
				}
			}
			for index := range maxResourceWatches {
				name := fmt.Sprintf("source-%02d", index)
				if err := f.client.Create(b.Context(), fairWatchSource("a", name, name, false)); err != nil {
					b.Fatal(err)
				}
			}
			if err := f.client.Create(b.Context(), fairWatchSource("b", "other", "reader", false)); err != nil {
				b.Fatal(err)
			}
			if _, err := f.watcher.Reconcile(b.Context(), reconcile.Request{}); err != nil {
				b.Fatal(err)
			}
			f.gets, f.lists = 0, 0
			b.ReportAllocs()
			for b.Loop() {
				if _, err := f.watcher.Reconcile(b.Context(), reconcile.Request{}); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(f.gets)/float64(b.N), "cached-GET/op")
			b.ReportMetric(float64(f.lists)/float64(b.N), "indexed-LIST/op")
		})
	}
}

type countingWatchMapper struct {
	apimeta.RESTMapper
	calls int
}

func (m *countingWatchMapper) KindFor(resource schema.GroupVersionResource) (schema.GroupVersionKind, error) {
	m.calls++
	return m.RESTMapper.KindFor(resource)
}

func TestFailedDiscoveryCannotExpandTheSharedWatchBudget(t *testing.T) {
	f := fairWatchFixture(t)
	source := fairWatchSource("a", "unknown", "reader", false)
	base := source.Spec.Triggers[0]
	source.Spec.Triggers = nil
	for index := range 32 {
		trigger := *base.DeepCopy()
		trigger.Name = fmt.Sprintf("events-%d", index)
		trigger.Resources = nil
		for resource := range 8 {
			trigger.Resources = append(trigger.Resources, fmt.Sprintf("unknown-%02d-%02d", index, resource))
		}
		source.Spec.Triggers = append(source.Spec.Triggers, trigger)
	}
	for _, object := range []client.Object{source, fairWatchSource("b", "valid", "reader", false)} {
		if err := f.client.Create(t.Context(), object); err != nil {
			t.Fatal(err)
		}
	}
	mapper := &countingWatchMapper{RESTMapper: f.watcher.mapper}
	f.watcher.mapper = mapper
	if _, err := f.watcher.Reconcile(t.Context(), reconcile.Request{}); err != nil {
		t.Fatal(err)
	}
	if mapper.calls > maxResourceWatches || len(f.watcher.desired) != 1 {
		t.Fatalf("failed candidates exhausted another tenant or exceeded discovery budget: attempts=%d streams=%d", mapper.calls, len(f.watcher.desired))
	}
}
