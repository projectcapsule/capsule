// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

// cacheFirstReads counts the Gets of one reader by object type.
type cacheFirstReads struct {
	namespaces, tenants, ruleStatuses atomic.Int32
}

func (r *cacheFirstReads) total() int32 {
	return r.namespaces.Load() + r.tenants.Load() + r.ruleStatuses.Load()
}

func (r *cacheFirstReads) counts() [3]int32 {
	return [3]int32{r.namespaces.Load(), r.tenants.Load(), r.ruleStatuses.Load()}
}

func (r *cacheFirstReads) count(obj client.Object) {
	switch obj.(type) {
	case *corev1.Namespace:
		r.namespaces.Add(1)
	case *capsulev1beta2.Tenant:
		r.tenants.Add(1)
	case *capsulev1beta2.RuleStatus:
		r.ruleStatuses.Add(1)
	}
}

// cacheFirstReader wraps the objects in a counting reader. A non-nil err is
// returned by every Get, after it is counted.
func cacheFirstReader(t *testing.T, err error, objects ...client.Object) (client.Reader, *cacheFirstReads) {
	t.Helper()

	var fail func(client.Object) error
	if err != nil {
		fail = func(client.Object) error { return err }
	}

	return cacheFirstFailingReader(t, fail, objects...)
}

// cacheFirstFailingReader wraps the objects in a counting reader. A Get
// returns the non-nil error of fail for its object, after it is counted.
func cacheFirstFailingReader(t *testing.T, fail func(client.Object) error, objects ...client.Object) (client.Reader, *cacheFirstReads) {
	t.Helper()

	reads := &cacheFirstReads{}
	base := tenantFakeClient(t, objects...).(client.WithWatch)

	return interceptor.NewClient(base, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			reads.count(obj)

			if fail != nil {
				if err := fail(obj); err != nil {
					return err
				}
			}

			return c.Get(ctx, key, obj, opts...)
		},
	}), reads
}

func cacheFirstNamespace(name string, refs ...metav1.OwnerReference) *corev1.Namespace {
	return &corev1.Namespace{Name: name, UID: types.UID(name + "-uid"), Labels: map[string]string{"profile": "guarded"}, OwnerReferences: refs}
}

func terminatingNamespace(ns *corev1.Namespace) *corev1.Namespace {
	stamp := metav1.Now()
	ns.DeletionTimestamp = &stamp
	ns.Finalizers = []string{"kubernetes"}

	return ns
}

func ownedNamespace(tnt *capsulev1beta2.Tenant) *corev1.Namespace {
	return cacheFirstNamespace("team", tenantOwnerReference(tnt.Name, tnt.UID))
}

// managedRuleStatus returns the managed RuleStatus of the Namespace "team"
// with tnt as its controller, as the tenant controller writes it. The marker
// is the action of its only effective rule and names its origin.
func managedRuleStatus(tnt *capsulev1beta2.Tenant, generation, observed int64, marker string) *capsulev1beta2.RuleStatus {
	rs := &capsulev1beta2.RuleStatus{Name: meta.NameForManagedRuleStatus(), Namespace: "team", Generation: generation}
	rs.OwnerReferences = []metav1.OwnerReference{*metav1.NewControllerRef(tnt, capsulev1beta2.GroupVersion.WithKind("Tenant"))}
	rs.Status.ObservedGeneration = observed
	rs.Status.Rules = []*rules.NamespaceRuleBodyNamespace{{Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionType(marker)}}}

	return rs
}

func ruleStatusMarker(rs *capsulev1beta2.RuleStatus) string {
	if rs == nil {
		return ""
	}

	return string(rs.Status.Rules[0].Enforce.Action)
}

// TestGetTenantRuleStatusByNamespaceCacheFirstOwnership covers each case where
// the cached ownership cannot be trusted. When the ownership falls back, the
// RuleStatus comes from the API reader too ("api"), never from the cache.
func TestGetTenantRuleStatusByNamespaceCacheFirstOwnership(t *testing.T) {
	tenantA := func() *capsulev1beta2.Tenant { return tenantObject("tenant-a", withUID("tenant-a-uid")) }
	tenantB := func() *capsulev1beta2.Tenant { return tenantObject("tenant-b", withUID("tenant-b-uid")) }
	recreatedA := func() *capsulev1beta2.Tenant { return tenantObject("tenant-a", withUID("tenant-a-new-uid")) }
	// profile is the state of one store: a Tenant, the Namespace it owns and
	// its RuleStatus, marked with the name of the store.
	profile := func(store string, tnt *capsulev1beta2.Tenant) []client.Object {
		return []client.Object{tnt, ownedNamespace(tnt), managedRuleStatus(tnt, 1, 1, store)}
	}
	apiDown := errors.New("api unavailable")

	for _, tc := range []struct {
		name     string
		cache    []client.Object
		cacheErr error
		nilCache bool
		api      []client.Object
		apiErr   error
		// Expected result: the Tenant UID ("" for none) and the RuleStatus
		// marker, or an error.
		wantUID   types.UID
		wantRules string
		wantErr   string
		wantNS    bool
		// Gets per store: Namespace, Tenant, RuleStatus.
		cacheGet [3]int32
		apiGet   [3]int32
	}{
		{
			name:  "hit reads only the cache",
			cache: profile("cache", tenantA()), api: profile("api", tenantA()),
			wantUID: "tenant-a-uid", wantRules: "cache", wantNS: true, cacheGet: [3]int32{1, 1, 1},
		},
		{
			name: "cold cache falls back",
			api:  profile("api", tenantA()),
			// The cached Namespace Get misses; the API reads the Namespace, its Tenant and the RuleStatus.
			wantUID: "tenant-a-uid", wantRules: "api", wantNS: true, cacheGet: [3]int32{1, 0, 0}, apiGet: [3]int32{1, 1, 1},
		},
		{
			name:  "cache error falls back",
			cache: profile("cache", tenantA()), cacheErr: errors.New("informer not synced"),
			api:     profile("api", tenantA()),
			wantUID: "tenant-a-uid", wantRules: "api", wantNS: true, cacheGet: [3]int32{1, 0, 0}, apiGet: [3]int32{1, 1, 1},
		},
		{
			name:     "nil cache reads only the API",
			nilCache: true, api: profile("api", tenantA()),
			wantUID: "tenant-a-uid", wantRules: "api", wantNS: true, apiGet: [3]int32{1, 1, 1},
		},
		{
			name:  "cached Tenant missing falls back",
			cache: profile("cache", tenantA())[1:], api: profile("api", tenantA()),
			wantUID: "tenant-a-uid", wantRules: "api", wantNS: true, cacheGet: [3]int32{1, 1, 0}, apiGet: [3]int32{1, 1, 1},
		},
		{
			name:  "terminating cached Namespace falls back to the recreated Namespace",
			cache: []client.Object{tenantA(), tenantB(), terminatingNamespace(ownedNamespace(tenantA())), managedRuleStatus(tenantA(), 1, 1, "cache")},
			api:   append(profile("api", tenantB()), tenantA()),
			// The cached Tenant is never read: the Namespace check comes first.
			wantUID: "tenant-b-uid", wantRules: "api", wantNS: true, cacheGet: [3]int32{1, 0, 0}, apiGet: [3]int32{1, 1, 1},
		},
		{
			name:  "cached Namespace without Tenant ownerReference falls back",
			cache: []client.Object{tenantA(), cacheFirstNamespace("team"), managedRuleStatus(tenantA(), 1, 1, "cache")}, api: profile("api", tenantA()),
			wantUID: "tenant-a-uid", wantRules: "api", wantNS: true, cacheGet: [3]int32{1, 0, 0}, apiGet: [3]int32{1, 1, 1},
		},
		{
			name:  "API unowned answer is kept after a fallback",
			cache: []client.Object{tenantA(), cacheFirstNamespace("team")}, api: []client.Object{tenantA(), cacheFirstNamespace("team")},
			wantUID: "", wantNS: true, cacheGet: [3]int32{1, 0, 0}, apiGet: [3]int32{1, 0, 1},
		},
		{
			name: "duplicate Tenant ownerReferences fall back",
			cache: []client.Object{tenantA(), tenantB(), cacheFirstNamespace("team",
				tenantOwnerReference("tenant-a", "tenant-a-uid"), tenantOwnerReference("tenant-b", "tenant-b-uid"))},
			api:     append(profile("api", tenantB()), tenantA()),
			wantUID: "tenant-b-uid", wantRules: "api", wantNS: true, cacheGet: [3]int32{1, 0, 0}, apiGet: [3]int32{1, 1, 1},
		},
		{
			name:  "Tenant ownerReference without UID falls back",
			cache: []client.Object{tenantA(), cacheFirstNamespace("team", tenantOwnerReference("tenant-a", ""))},
			api:   profile("api", tenantA()),
			// The cached Tenant cannot be verified without a UID and is not read.
			wantUID: "tenant-a-uid", wantRules: "api", wantNS: true, cacheGet: [3]int32{1, 0, 0}, apiGet: [3]int32{1, 1, 1},
		},
		{
			name:  "UID mismatch falls back and the API mismatch denies",
			cache: []client.Object{recreatedA(), ownedNamespace(tenantA())},
			api:   []client.Object{recreatedA(), ownedNamespace(tenantA()), managedRuleStatus(recreatedA(), 1, 1, "api")},
			// The API path reports the mismatch, which the handlers turn into an errored response.
			wantErr: "tenant ownerReference UID mismatch", cacheGet: [3]int32{1, 1, 0}, apiGet: [3]int32{1, 1, 1},
		},
		{
			name:  "Tenant recreated with the same name is resolved by the API",
			cache: []client.Object{tenantA(), ownedNamespace(recreatedA())},
			api:   profile("api", recreatedA()),
			// The cached Tenant still has the old UID.
			wantUID: "tenant-a-new-uid", wantRules: "api", wantNS: true, cacheGet: [3]int32{1, 1, 0}, apiGet: [3]int32{1, 1, 1},
		},
		{
			name:  "API error after a fallback is returned",
			cache: []client.Object{ownedNamespace(tenantA())}, api: profile("api", tenantA()), apiErr: apiDown,
			wantErr: apiDown.Error(), cacheGet: [3]int32{1, 1, 0}, apiGet: [3]int32{1, 0, 1},
		},
		{
			name:    "missing Namespace in cache and API",
			wantUID: "", wantNS: false, cacheGet: [3]int32{1, 0, 0}, apiGet: [3]int32{1, 0, 1},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cache client.Reader

			cached, cacheReads := cacheFirstReader(t, tc.cacheErr, tc.cache...)
			if !tc.nilCache {
				cache = cached
			}

			api, apiReads := cacheFirstReader(t, tc.apiErr, tc.api...)

			tnt, ns, rs, err := tenant.GetTenantRuleStatusByNamespaceCacheFirst(t.Context(), cache, api, "team")

			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want %q", err, tc.wantErr)
				}

				if tnt != nil || rs != nil {
					t.Fatalf("tenant = %+v, RuleStatus = %+v, want nil on error", tnt, rs)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				var uid types.UID
				if tnt != nil {
					uid = tnt.UID
				}

				if uid != tc.wantUID {
					t.Fatalf("tenant UID = %q, want %q", uid, tc.wantUID)
				}

				if got := ruleStatusMarker(rs); got != tc.wantRules {
					t.Fatalf("RuleStatus = %q, want %q", got, tc.wantRules)
				}

				if (ns != nil) != tc.wantNS {
					t.Fatalf("namespace snapshot = %+v, want present=%v", ns, tc.wantNS)
				}
			}

			if got := cacheReads.counts(); got != tc.cacheGet {
				t.Fatalf("cache Gets = %v, want %v", got, tc.cacheGet)
			}

			if got := apiReads.counts(); got != tc.apiGet {
				t.Fatalf("API Gets = %v, want %v", got, tc.apiGet)
			}
		})
	}
}

func TestGetTenantNameByNamespaceCacheFirst(t *testing.T) {
	ref := tenantOwnerReference("tenant-a", "tenant-a-uid")
	owned := func() *corev1.Namespace { return cacheFirstNamespace("team", ref) }
	apiDown := errors.New("api unavailable")

	for _, tc := range []struct {
		name     string
		cache    []client.Object
		api      []client.Object
		apiErr   error
		want     string
		wantErr  bool
		cacheGet int32
		apiGet   int32
	}{
		// The name comes from the ownerReference; no Tenant is read on either path.
		{name: "hit reads only the cached Namespace", cache: []client.Object{owned()}, api: []client.Object{owned()}, want: "tenant-a", cacheGet: 1, apiGet: 0},
		{name: "cold cache falls back", api: []client.Object{owned()}, want: "tenant-a", cacheGet: 1, apiGet: 1},
		{
			name:  "terminating cached Namespace falls back",
			cache: []client.Object{terminatingNamespace(owned())}, api: []client.Object{cacheFirstNamespace("team", tenantOwnerReference("tenant-b", "tenant-b-uid"))},
			want: "tenant-b", cacheGet: 1, apiGet: 1,
		},
		{name: "missing ownerReference falls back", cache: []client.Object{cacheFirstNamespace("team")}, api: []client.Object{owned()}, want: "tenant-a", cacheGet: 1, apiGet: 1},
		{name: "API unowned answer is kept", cache: []client.Object{cacheFirstNamespace("team")}, api: []client.Object{cacheFirstNamespace("team")}, want: "", cacheGet: 1, apiGet: 1},
		{
			name:  "duplicate ownerReferences fall back",
			cache: []client.Object{cacheFirstNamespace("team", ref, tenantOwnerReference("tenant-b", "tenant-b-uid"))}, api: []client.Object{owned()},
			want: "tenant-a", cacheGet: 1, apiGet: 1,
		},
		{
			name:  "ownerReference without UID falls back",
			cache: []client.Object{cacheFirstNamespace("team", tenantOwnerReference("tenant-a", ""))}, api: []client.Object{owned()},
			want: "tenant-a", cacheGet: 1, apiGet: 1,
		},
		{name: "API error after a fallback is returned", api: []client.Object{owned()}, apiErr: apiDown, wantErr: true, cacheGet: 1, apiGet: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cache, cacheReads := cacheFirstReader(t, nil, tc.cache...)
			api, apiReads := cacheFirstReader(t, tc.apiErr, tc.api...)

			got, err := tenant.GetTenantNameByNamespaceCacheFirst(t.Context(), cache, api, "team")
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, want error=%v", err, tc.wantErr)
			}

			if got != tc.want {
				t.Fatalf("tenant name = %q, want %q", got, tc.want)
			}

			if cacheReads.tenants.Load() != 0 || apiReads.tenants.Load() != 0 {
				t.Fatal("the tenant name lookup must not read Tenants")
			}

			if got := cacheReads.total(); got != tc.cacheGet {
				t.Fatalf("cache Gets = %d, want %d", got, tc.cacheGet)
			}

			if got := apiReads.total(); got != tc.apiGet {
				t.Fatalf("API Gets = %d, want %d", got, tc.apiGet)
			}
		})
	}
}

func TestGetTenantRuleStatusByNamespaceCacheFirst(t *testing.T) {
	tnt := func() *capsulev1beta2.Tenant { return tenantObject("tenant-a", withUID("tenant-a-uid")) }
	owned := func() *corev1.Namespace { return ownedNamespace(tnt()) }
	ruleStatus := func(generation, observed int64, marker string) *capsulev1beta2.RuleStatus {
		return managedRuleStatus(tnt(), generation, observed, marker)
	}
	failRuleStatus := func(obj client.Object) error {
		if _, ok := obj.(*capsulev1beta2.RuleStatus); ok {
			return errors.New("rules unavailable")
		}

		return nil
	}
	failAll := func(client.Object) error { return errors.New("api unavailable") }

	for _, tc := range []struct {
		name      string
		cache     []client.Object
		cacheFail func(client.Object) error
		nilCache  bool
		api       []client.Object
		apiFail   func(client.Object) error
		// want is the marker of the returned RuleStatus, "" for none.
		want       string
		observed   bool
		wantTenant bool
		wantErr    string
		// Gets per store: Namespace, Tenant, RuleStatus.
		cacheGet [3]int32
		apiGet   [3]int32
	}{
		{
			name:  "hit reads only the cache",
			cache: []client.Object{tnt(), owned(), ruleStatus(3, 3, "cache")}, api: []client.Object{tnt(), owned(), ruleStatus(4, 4, "api")},
			want: "cache", observed: true, wantTenant: true, cacheGet: [3]int32{1, 1, 1},
		},
		{
			name:  "missing cached RuleStatus is read from the API alone",
			cache: []client.Object{tnt(), owned()}, api: []client.Object{tnt(), owned(), ruleStatus(4, 4, "api")},
			want: "api", observed: true, wantTenant: true, cacheGet: [3]int32{1, 1, 1}, apiGet: [3]int32{0, 0, 1},
		},
		{
			name:  "cached RuleStatus error is read from the API alone",
			cache: []client.Object{tnt(), owned(), ruleStatus(3, 3, "cache")}, cacheFail: failRuleStatus,
			api:  []client.Object{tnt(), owned(), ruleStatus(4, 4, "api")},
			want: "api", observed: true, wantTenant: true, cacheGet: [3]int32{1, 1, 1}, apiGet: [3]int32{0, 0, 1},
		},
		{
			name:  "pending cached generation is read from the API alone",
			cache: []client.Object{tnt(), owned(), ruleStatus(4, 3, "cache")}, api: []client.Object{tnt(), owned(), ruleStatus(4, 4, "api")},
			want: "api", observed: true, wantTenant: true, cacheGet: [3]int32{1, 1, 1}, apiGet: [3]int32{0, 0, 1},
		},
		{
			// The pending API answer is returned as is, with the ownership read
			// again from the API reader: the caller renders the rules.
			name:  "pending API generation is returned as pending",
			cache: []client.Object{tnt(), owned(), ruleStatus(4, 3, "cache")}, api: []client.Object{tnt(), owned(), ruleStatus(5, 4, "api")},
			want: "api", observed: false, wantTenant: true, cacheGet: [3]int32{1, 1, 1}, apiGet: [3]int32{1, 1, 1},
		},
		{
			name:  "RuleStatus missing in cache and API",
			cache: []client.Object{tnt(), owned()}, api: []client.Object{tnt(), owned()},
			want: "", wantTenant: true, cacheGet: [3]int32{1, 1, 1}, apiGet: [3]int32{1, 1, 1},
		},
		{
			name:  "API RuleStatus error after a cached ownership is returned",
			cache: []client.Object{tnt(), owned()}, api: []client.Object{tnt(), owned()}, apiFail: failRuleStatus,
			wantErr: "rules unavailable", cacheGet: [3]int32{1, 1, 1}, apiGet: [3]int32{0, 0, 1},
		},
		{
			name: "cold cache reads everything from the API",
			api:  []client.Object{tnt(), owned(), ruleStatus(4, 4, "api")},
			want: "api", observed: true, wantTenant: true, cacheGet: [3]int32{1, 0, 0}, apiGet: [3]int32{1, 1, 1},
		},
		{
			// A cached RuleStatus is not paired with an API ownership answer.
			name:  "ownership fallback ignores the cached RuleStatus",
			cache: []client.Object{tnt(), terminatingNamespace(owned()), ruleStatus(3, 3, "cache")},
			api:   []client.Object{tnt(), owned(), ruleStatus(4, 4, "api")},
			want:  "api", observed: true, wantTenant: true, cacheGet: [3]int32{1, 0, 0}, apiGet: [3]int32{1, 1, 1},
		},
		{
			name:     "nil cache reads everything from the API",
			nilCache: true, api: []client.Object{tnt(), owned(), ruleStatus(4, 4, "api")},
			want: "api", observed: true, wantTenant: true, apiGet: [3]int32{1, 1, 1},
		},
		{
			// The API path ignores a RuleStatus error when no Tenant owns the Namespace.
			name: "unowned API Namespace ignores the RuleStatus error",
			api:  []client.Object{cacheFirstNamespace("team")}, apiFail: failRuleStatus,
			want: "", cacheGet: [3]int32{1, 0, 0}, apiGet: [3]int32{1, 0, 1},
		},
		{
			name: "API ownership error is returned",
			api:  []client.Object{tnt(), owned()}, apiFail: failAll,
			wantErr: "api unavailable", cacheGet: [3]int32{1, 0, 0}, apiGet: [3]int32{1, 0, 1},
		},
		{
			name: "API RuleStatus error with an owner is returned",
			api:  []client.Object{tnt(), owned()}, apiFail: failRuleStatus,
			wantErr: "rules unavailable", cacheGet: [3]int32{1, 0, 0}, apiGet: [3]int32{1, 1, 1},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cache client.Reader

			cached, cacheReads := cacheFirstFailingReader(t, tc.cacheFail, tc.cache...)
			if !tc.nilCache {
				cache = cached
			}

			api, apiReads := cacheFirstFailingReader(t, tc.apiFail, tc.api...)

			gotTenant, ns, rs, err := tenant.GetTenantRuleStatusByNamespaceCacheFirst(t.Context(), cache, api, "team")
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want %q", err, tc.wantErr)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tc.wantErr == "" {
				if (gotTenant != nil) != tc.wantTenant {
					t.Fatalf("tenant = %+v, want present=%v", gotTenant, tc.wantTenant)
				}

				if ns == nil {
					t.Fatal("lost the Namespace snapshot")
				}

				if got := ruleStatusMarker(rs); got != tc.want {
					t.Fatalf("RuleStatus = %q, want %q", got, tc.want)
				}

				if rs != nil && tenant.IsRuleStatusObserved(rs) != tc.observed {
					t.Fatalf("observed = %v, want %v", tenant.IsRuleStatusObserved(rs), tc.observed)
				}
			}

			if got := cacheReads.counts(); got != tc.cacheGet {
				t.Fatalf("cache Gets = %v, want %v", got, tc.cacheGet)
			}

			if got := apiReads.counts(); got != tc.apiGet {
				t.Fatalf("API Gets = %v, want %v", got, tc.apiGet)
			}
		})
	}
}

// TestCachedRuleStatusBelongsToTheResolvedNamespace covers a RuleStatus that
// the cache still holds for an earlier Namespace with the same name: it must
// be controlled by the resolved Tenant and not be older than the Namespace.
func TestCachedRuleStatusBelongsToTheResolvedNamespace(t *testing.T) {
	earlier := metav1.NewTime(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	later := metav1.NewTime(earlier.Add(time.Minute))
	tenantA := func() *capsulev1beta2.Tenant { return tenantObject("tenant-a", withUID("tenant-a-uid")) }
	tenantB := func() *capsulev1beta2.Tenant { return tenantObject("tenant-b", withUID("tenant-b-uid")) }
	namespace := func(owner *capsulev1beta2.Tenant, created metav1.Time) *corev1.Namespace {
		ns := ownedNamespace(owner)
		ns.CreationTimestamp = created

		return ns
	}
	rulesOf := func(controller *capsulev1beta2.Tenant, created metav1.Time, marker string) *capsulev1beta2.RuleStatus {
		rs := managedRuleStatus(controller, 1, 1, marker)
		rs.CreationTimestamp = created

		return rs
	}
	notController := func(rs *capsulev1beta2.RuleStatus) *capsulev1beta2.RuleStatus {
		rs.OwnerReferences[0].Controller = new(false)

		return rs
	}

	for _, tc := range []struct {
		name       string
		cache, api []client.Object
		wantTenant types.UID
		wantRules  string
		apiGet     [3]int32
	}{
		{
			// The Namespace informer has seen the recreation, the RuleStatus
			// informer not yet the deletion of the RuleStatus of Tenant A.
			name:       "Namespace recreated under another Tenant",
			cache:      []client.Object{tenantA(), tenantB(), namespace(tenantB(), later), rulesOf(tenantA(), earlier, "tenant-a")},
			api:        []client.Object{tenantA(), tenantB(), namespace(tenantB(), later), rulesOf(tenantB(), later, "tenant-b")},
			wantTenant: "tenant-b-uid", wantRules: "tenant-b", apiGet: [3]int32{0, 0, 1},
		},
		{
			// The caller renders the rules of Tenant B from the API snapshot.
			name:       "Namespace recreated under another Tenant before its RuleStatus exists",
			cache:      []client.Object{tenantA(), tenantB(), namespace(tenantB(), later), rulesOf(tenantA(), earlier, "tenant-a")},
			api:        []client.Object{tenantA(), tenantB(), namespace(tenantB(), later)},
			wantTenant: "tenant-b-uid", wantRules: "", apiGet: [3]int32{1, 1, 1},
		},
		{
			name:       "RuleStatus controlled by another Tenant",
			cache:      []client.Object{tenantA(), namespace(tenantA(), earlier), rulesOf(tenantB(), later, "tenant-b")},
			api:        []client.Object{tenantA(), namespace(tenantA(), earlier), rulesOf(tenantA(), later, "tenant-a")},
			wantTenant: "tenant-a-uid", wantRules: "tenant-a", apiGet: [3]int32{0, 0, 1},
		},
		{
			name:       "RuleStatus without a controller",
			cache:      []client.Object{tenantA(), namespace(tenantA(), earlier), notController(rulesOf(tenantA(), later, "cache"))},
			api:        []client.Object{tenantA(), namespace(tenantA(), earlier), rulesOf(tenantA(), later, "api")},
			wantTenant: "tenant-a-uid", wantRules: "api", apiGet: [3]int32{0, 0, 1},
		},
		{
			name:       "RuleStatus of an earlier Namespace of the same Tenant",
			cache:      []client.Object{tenantA(), namespace(tenantA(), later), rulesOf(tenantA(), earlier, "earlier")},
			api:        []client.Object{tenantA(), namespace(tenantA(), later), rulesOf(tenantA(), later, "current")},
			wantTenant: "tenant-a-uid", wantRules: "current", apiGet: [3]int32{0, 0, 1},
		},
		{
			// Timestamps have a resolution of one second.
			name:       "RuleStatus created in the same second as its Namespace",
			cache:      []client.Object{tenantA(), namespace(tenantA(), later), rulesOf(tenantA(), later, "cache")},
			api:        []client.Object{tenantA(), namespace(tenantA(), later), rulesOf(tenantA(), later, "api")},
			wantTenant: "tenant-a-uid", wantRules: "cache",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cache, cacheReads := cacheFirstReader(t, nil, tc.cache...)
			api, apiReads := cacheFirstReader(t, nil, tc.api...)

			tnt, ns, rs, err := tenant.GetTenantRuleStatusByNamespaceCacheFirst(t.Context(), cache, api, "team")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tnt == nil || tnt.UID != tc.wantTenant {
				t.Fatalf("tenant = %+v, want UID %q", tnt, tc.wantTenant)
			}

			if ns == nil || !tenant.IsTenantOwnerReferenceForTenant(ns.OwnerReferences[0], tnt) {
				t.Fatalf("namespace snapshot = %+v, want one owned by %q", ns, tnt.Name)
			}

			if got := ruleStatusMarker(rs); got != tc.wantRules {
				t.Fatalf("RuleStatus = %q, want %q", got, tc.wantRules)
			}

			if got := cacheReads.counts(); got != [3]int32{1, 1, 1} {
				t.Fatalf("cache Gets = %v, want the Namespace, Tenant and RuleStatus once", got)
			}

			if got := apiReads.counts(); got != tc.apiGet {
				t.Fatalf("API Gets = %v, want %v", got, tc.apiGet)
			}
		})
	}
}

// TestRuleStatusFallbackRendersFromTheAPISnapshot covers a cached ownership
// whose RuleStatus falls back: when the API RuleStatus is missing or pending
// too, the Namespace and the Tenant the caller renders the rules from come
// from the API reader, not from the cache.
func TestRuleStatusFallbackRendersFromTheAPISnapshot(t *testing.T) {
	// Each store marks its copies with its name.
	tnt := func(store string) *capsulev1beta2.Tenant {
		tnt := tenantObject("tenant-a", withUID("tenant-a-uid"))
		tnt.Labels = map[string]string{"copy": store}

		return tnt
	}
	namespace := func(store string, refs ...metav1.OwnerReference) *corev1.Namespace {
		ns := cacheFirstNamespace("team", refs...)
		ns.Labels["copy"] = store

		return ns
	}
	ref := tenantOwnerReference("tenant-a", "tenant-a-uid")
	ruleStatus := func(generation, observed int64, marker string) *capsulev1beta2.RuleStatus {
		return managedRuleStatus(tnt(""), generation, observed, marker)
	}
	failNamespace := func(obj client.Object) error {
		if _, ok := obj.(*corev1.Namespace); ok {
			return errors.New("namespace unavailable")
		}

		return nil
	}

	for _, tc := range []struct {
		name    string
		cache   []client.Object
		api     []client.Object
		apiFail func(client.Object) error
		// The store of the returned Tenant ("" for none) and Namespace, and the
		// marker of the returned RuleStatus.
		wantTenant, wantNamespace, wantRules string
		wantErr                              string
		apiGet                               [3]int32
	}{
		{
			name:  "observed API RuleStatus keeps the cached ownership",
			cache: []client.Object{tnt("cache"), namespace("cache", ref)},
			api:   []client.Object{tnt("api"), namespace("api", ref), ruleStatus(4, 4, "api")},
			// The rules come from the RuleStatus, not from the snapshot.
			wantTenant: "cache", wantNamespace: "cache", wantRules: "api", apiGet: [3]int32{0, 0, 1},
		},
		{
			name:       "RuleStatus missing in cache and API",
			cache:      []client.Object{tnt("cache"), namespace("cache", ref)},
			api:        []client.Object{tnt("api"), namespace("api", ref)},
			wantTenant: "api", wantNamespace: "api", wantRules: "", apiGet: [3]int32{1, 1, 1},
		},
		{
			name:       "RuleStatus pending in cache and API",
			cache:      []client.Object{tnt("cache"), namespace("cache", ref), ruleStatus(4, 3, "cache")},
			api:        []client.Object{tnt("api"), namespace("api", ref), ruleStatus(5, 4, "api")},
			wantTenant: "api", wantNamespace: "api", wantRules: "api", apiGet: [3]int32{1, 1, 1},
		},
		{
			name:       "API Namespace no longer owned",
			cache:      []client.Object{tnt("cache"), namespace("cache", ref)},
			api:        []client.Object{tnt("api"), namespace("api")},
			wantTenant: "", wantNamespace: "api", wantRules: "", apiGet: [3]int32{1, 0, 1},
		},
		{
			name:    "API ownership error",
			cache:   []client.Object{tnt("cache"), namespace("cache", ref)},
			api:     []client.Object{tnt("api"), namespace("api", ref)},
			apiFail: failNamespace,
			wantErr: "namespace unavailable", apiGet: [3]int32{1, 0, 1},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cache, _ := cacheFirstReader(t, nil, tc.cache...)
			api, apiReads := cacheFirstFailingReader(t, tc.apiFail, tc.api...)

			gotTenant, ns, rs, err := tenant.GetTenantRuleStatusByNamespaceCacheFirst(t.Context(), cache, api, "team")

			if got := apiReads.counts(); got != tc.apiGet {
				t.Fatalf("API Gets = %v, want %v", got, tc.apiGet)
			}

			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) || gotTenant != nil || rs != nil {
					t.Fatalf("tenant = %+v, RuleStatus = %+v, error = %v, want only the error %q", gotTenant, rs, err, tc.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			gotCopy := ""
			if gotTenant != nil {
				gotCopy = gotTenant.GetLabels()["copy"]
			}

			if gotCopy != tc.wantTenant {
				t.Fatalf("tenant copy = %q, want %q", gotCopy, tc.wantTenant)
			}

			if got := ns.GetLabels()["copy"]; got != tc.wantNamespace {
				t.Fatalf("namespace copy = %q, want %q", got, tc.wantNamespace)
			}

			if got := ruleStatusMarker(rs); got != tc.wantRules {
				t.Fatalf("RuleStatus = %q, want %q", got, tc.wantRules)
			}
		})
	}
}

// TestUnownedNamespaceDoesNotWaitForTheRuleStatus covers the API path when
// the RuleStatus is not needed: the lookup cancels the concurrent RuleStatus
// read and waits only for the cancelled read to return.
func TestUnownedNamespaceDoesNotWaitForTheRuleStatus(t *testing.T) {
	// The slow RuleStatus read ends only with its context: without the
	// cancellation it would end with this deadline.
	const watchdog = 5 * time.Second

	apiDown := errors.New("api unavailable")

	for _, tc := range []struct {
		name    string
		cache   bool
		nsErr   error
		wantErr error
	}{
		{name: "unowned Namespace with a cold cache", cache: true},
		{name: "unowned Namespace without cache"},
		{name: "ownership error", nsErr: apiDown, wantErr: apiDown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), watchdog)
			defer cancel()

			var ruleStatusErr error

			finished := make(chan struct{})
			base := tenantFakeClient(t, cacheFirstNamespace("team")).(client.WithWatch)
			api := interceptor.NewClient(base, interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					switch obj.(type) {
					case *capsulev1beta2.RuleStatus:
						<-ctx.Done()

						ruleStatusErr = ctx.Err()
						// Returning takes a moment: a lookup that does not wait for
						// the cancelled read returns before it.
						time.Sleep(20 * time.Millisecond)
						close(finished)

						return ctx.Err()
					case *corev1.Namespace:
						if tc.nsErr != nil {
							return tc.nsErr
						}
					}

					return c.Get(ctx, key, obj, opts...)
				},
			})

			var cache client.Reader
			if tc.cache {
				cache = tenantFakeClient(t)
			}

			start := time.Now()
			tnt, ns, rs, err := tenant.GetTenantRuleStatusByNamespaceCacheFirst(ctx, cache, api, "team")
			elapsed := time.Since(start)

			select {
			case <-finished:
			default:
				t.Fatal("the RuleStatus read outlived the lookup")
			}

			if !errors.Is(ruleStatusErr, context.Canceled) {
				t.Fatalf("RuleStatus read ended with %v, want it cancelled by the lookup", ruleStatusErr)
			}

			if elapsed >= watchdog {
				t.Fatalf("lookup took %v, want it not to wait for the RuleStatus read", elapsed)
			}

			if !errors.Is(err, tc.wantErr) || tnt != nil || rs != nil {
				t.Fatalf("tenant = %+v, RuleStatus = %+v, error = %v, want no Tenant and error %v", tnt, rs, err, tc.wantErr)
			}

			if tc.wantErr == nil && ns == nil {
				t.Fatal("lost the Namespace snapshot")
			}
		})
	}
}

// TestBlockedCacheReadsFallBack covers an informer that is not synced, whose
// cached Get waits for its context: the lookup gives up on it after the cache
// read bound and asks the API reader with the request context.
func TestBlockedCacheReadsFallBack(t *testing.T) {
	const (
		watchdog = 5 * time.Second
		// cacheReadBound is tenant.cacheFirstReadTimeout.
		cacheReadBound = 25 * time.Millisecond
	)

	tnt := func() *capsulev1beta2.Tenant { return tenantObject("tenant-a", withUID("tenant-a-uid")) }
	state := func(marker string) []client.Object {
		return []client.Object{tnt(), ownedNamespace(tnt()), managedRuleStatus(tnt(), 1, 1, marker)}
	}

	for _, tc := range []struct {
		name    string
		blocked client.Object
		// names runs the Tenant name lookup instead of the profile lookup.
		names     bool
		wantRules string
		apiGet    [3]int32
	}{
		{name: "ownership with a blocked Namespace read", blocked: &corev1.Namespace{}, wantRules: "api", apiGet: [3]int32{1, 1, 1}},
		{name: "ownership with a blocked Tenant read", blocked: &capsulev1beta2.Tenant{}, wantRules: "api", apiGet: [3]int32{1, 1, 1}},
		{name: "RuleStatus only with a blocked RuleStatus read", blocked: &capsulev1beta2.RuleStatus{}, wantRules: "api", apiGet: [3]int32{0, 0, 1}},
		{name: "Tenant name with a blocked Namespace read", blocked: &corev1.Namespace{}, names: true, apiGet: [3]int32{1, 0, 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), watchdog)
			defer cancel()

			requestDeadline, _ := ctx.Deadline()

			// The cached reads run on the calling goroutine.
			var (
				blockedDeadline time.Time
				blockedBounded  bool
			)

			cache := interceptor.NewClient(tenantFakeClient(t, state("cache")...).(client.WithWatch), interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if reflect.TypeOf(obj) == reflect.TypeOf(tc.blocked) {
						blockedDeadline, blockedBounded = ctx.Deadline()
						<-ctx.Done()

						return ctx.Err()
					}

					return c.Get(ctx, key, obj, opts...)
				},
			})

			// The API reader honours the context of each read, as a REST
			// client does, and records reads without the request deadline.
			apiReads := &cacheFirstReads{}

			var foreignContexts atomic.Int32

			api := interceptor.NewClient(tenantFakeClient(t, state("api")...).(client.WithWatch), interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					apiReads.count(obj)

					if deadline, ok := ctx.Deadline(); !ok || !deadline.Equal(requestDeadline) {
						foreignContexts.Add(1)
					}

					if err := ctx.Err(); err != nil {
						return err
					}

					return c.Get(ctx, key, obj, opts...)
				},
			})

			before := time.Now()

			if tc.names {
				name, err := tenant.GetTenantNameByNamespaceCacheFirst(ctx, cache, api, "team")
				if err != nil || name != "tenant-a" {
					t.Fatalf("tenant name = %q, error = %v, want tenant-a from the API reader", name, err)
				}
			} else {
				gotTenant, ns, rs, err := tenant.GetTenantRuleStatusByNamespaceCacheFirst(ctx, cache, api, "team")
				if err != nil || gotTenant == nil || gotTenant.UID != "tenant-a-uid" || ns == nil {
					t.Fatalf("tenant = %+v, namespace = %+v, error = %v, want tenant-a from the API reader", gotTenant, ns, err)
				}

				if got := ruleStatusMarker(rs); got != tc.wantRules {
					t.Fatalf("RuleStatus = %q, want %q", got, tc.wantRules)
				}
			}

			after := time.Now()

			if !blockedBounded || !blockedDeadline.Before(requestDeadline) ||
				blockedDeadline.Before(before.Add(cacheReadBound)) || blockedDeadline.After(after.Add(cacheReadBound)) {
				t.Fatalf("blocked cache read deadline = %v (bounded %v), want %v after the call started", blockedDeadline, blockedBounded, cacheReadBound)
			}

			if after.Sub(before) >= watchdog {
				t.Fatalf("lookup took %v, want it to give up on the blocked cache read", after.Sub(before))
			}

			if got := foreignContexts.Load(); got != 0 {
				t.Fatalf("%d API reads did not use the request context", got)
			}

			if got := apiReads.counts(); got != tc.apiGet {
				t.Fatalf("API Gets = %v, want %v", got, tc.apiGet)
			}
		})
	}
}

func TestIsRuleStatusObserved(t *testing.T) {
	if tenant.IsRuleStatusObserved(nil) {
		t.Fatal("a missing RuleStatus is not observed")
	}

	rs := &capsulev1beta2.RuleStatus{Generation: 2}
	rs.Status.ObservedGeneration = 1

	if tenant.IsRuleStatusObserved(rs) {
		t.Fatal("a pending generation is not observed")
	}

	rs.Status.ObservedGeneration = 2
	if !tenant.IsRuleStatusObserved(rs) {
		t.Fatal("the current generation is observed")
	}
}
