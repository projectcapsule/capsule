// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package handlers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	webhookutils "github.com/projectcapsule/capsule/pkg/runtime/webhook/utils"
	"github.com/projectcapsule/capsule/pkg/users"
)

// The manager cache and the API reader are separate stores in these tests, so
// every case can tell which one answered.

type wrapperReads struct {
	namespaces, tenants, ruleStatuses atomic.Int32
}

func (r *wrapperReads) counts() [3]int32 {
	return [3]int32{r.namespaces.Load(), r.tenants.Load(), r.ruleStatuses.Load()}
}

func wrapperClient(tb testing.TB, objects ...client.Object) (client.Client, *wrapperReads) {
	tb.Helper()

	scheme := runtime.NewScheme()
	require.NoError(tb, corev1.AddToScheme(scheme))
	require.NoError(tb, capsulev1beta2.AddToScheme(scheme))

	reads := &wrapperReads{}
	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()

	return interceptor.NewClient(base, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			switch obj.(type) {
			case *corev1.Namespace:
				reads.namespaces.Add(1)
			case *capsulev1beta2.Tenant:
				reads.tenants.Add(1)
			case *capsulev1beta2.RuleStatus:
				reads.ruleStatuses.Add(1)
			}

			return c.Get(ctx, key, obj, opts...)
		},
	}), reads
}

// resolution records what a wrapper handed to its inner handler.
type resolution struct {
	calls  int
	tenant types.UID
	// source is the label key of the first metadata rule, which names the
	// store the rules came from.
	source string
}

func (r *resolution) record(tnt *capsulev1beta2.Tenant, bodies []*rules.NamespaceRuleBodyNamespace) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		r.calls++
		r.tenant = tnt.GetUID()

		r.source = ""
		for _, body := range bodies {
			if body.Enforce != nil && len(body.Enforce.Metadata) > 0 {
				for key := range body.Enforce.Metadata[0].Labels {
					r.source = key
				}

				break
			}
		}

		return nil
	}
}

type rulesetRecorder struct{ *resolution }

func (h rulesetRecorder) OnCreate(_ client.Client, _ client.Reader, _ *corev1.ConfigMap, _ admission.Decoder, _ events.EventRecorder, tnt *capsulev1beta2.Tenant, bodies []*rules.NamespaceRuleBodyNamespace) handlers.Func {
	return h.record(tnt, bodies)
}

func (h rulesetRecorder) OnUpdate(_ client.Client, _ client.Reader, _, _ *corev1.ConfigMap, _ admission.Decoder, _ events.EventRecorder, tnt *capsulev1beta2.Tenant, bodies []*rules.NamespaceRuleBodyNamespace) handlers.Func {
	return h.record(tnt, bodies)
}

func (h rulesetRecorder) OnDelete(_ client.Client, _ client.Reader, _ *corev1.ConfigMap, _ admission.Decoder, _ events.EventRecorder, tnt *capsulev1beta2.Tenant, bodies []*rules.NamespaceRuleBodyNamespace) handlers.Func {
	return h.record(tnt, bodies)
}

type tenantRecorder struct{ *resolution }

func (h tenantRecorder) OnCreate(_ client.Client, _ client.Reader, _ *corev1.ConfigMap, _ admission.Decoder, _ events.EventRecorder, tnt *capsulev1beta2.Tenant) handlers.Func {
	return h.record(tnt, nil)
}

func (h tenantRecorder) OnUpdate(_ client.Client, _ client.Reader, _, _ *corev1.ConfigMap, _ admission.Decoder, _ events.EventRecorder, tnt *capsulev1beta2.Tenant) handlers.Func {
	return h.record(tnt, nil)
}

func (h tenantRecorder) OnDelete(_ client.Client, _ client.Reader, _ *corev1.ConfigMap, _ admission.Decoder, _ events.EventRecorder, tnt *capsulev1beta2.Tenant) handlers.Func {
	return h.record(tnt, nil)
}

type userRecorder struct{ *resolution }

func (h userRecorder) OnCreate(_ client.Client, _ client.Reader, _ users.AdmissionUser, _ *corev1.ConfigMap, _ admission.Decoder, _ events.EventRecorder, tnt *capsulev1beta2.Tenant) handlers.Func {
	return h.record(tnt, nil)
}

func (h userRecorder) OnUpdate(_ client.Client, _ client.Reader, _ users.AdmissionUser, _, _ *corev1.ConfigMap, _ admission.Decoder, _ events.EventRecorder, tnt *capsulev1beta2.Tenant) handlers.Func {
	return h.record(tnt, nil)
}

func (h userRecorder) OnDelete(_ client.Client, _ client.Reader, _ users.AdmissionUser, _ *corev1.ConfigMap, _ admission.Decoder, _ events.EventRecorder, tnt *capsulev1beta2.Tenant) handlers.Func {
	return h.record(tnt, nil)
}

type cacheFirstWrapper struct {
	name    string
	ruleset bool
	// apiOnly wrappers feed authorization decisions (ServiceAccount
	// promotion, PersistentVolume tenant checks): they read only the API.
	apiOnly bool
	build   func(*resolution) handlers.Handler
}

func cacheFirstWrappers() []cacheFirstWrapper {
	factory := func() *corev1.ConfigMap { return &corev1.ConfigMap{} }

	return []cacheFirstWrapper{
		{name: "ruleset", ruleset: true, build: func(r *resolution) handlers.Handler {
			return &handlers.TypedTenantWithRulesetHandler[*corev1.ConfigMap]{
				Factory: factory, Handlers: []handlers.TypedHandlerWithTenantWithRuleset[*corev1.ConfigMap]{rulesetRecorder{r}},
			}
		}},
		{name: "tenant", apiOnly: true, build: func(r *resolution) handlers.Handler {
			return &handlers.TypedTenantHandler[*corev1.ConfigMap]{
				Factory: factory, Handlers: []handlers.TypedHandlerWithTenant[*corev1.ConfigMap]{tenantRecorder{r}},
			}
		}},
		{name: "user", apiOnly: true, build: func(r *resolution) handlers.Handler {
			return &handlers.TypedTenantWithUserHandler[*corev1.ConfigMap]{
				Factory: factory, Handlers: []handlers.TypedHandlerWithTenantUser[*corev1.ConfigMap]{userRecorder{r}},
				UserResolver: func(_ context.Context, _ client.Client, req admission.Request, _ configuration.Configuration) users.AdmissionUser {
					return users.NewAdmissionUser(users.AdmissionUserUnknown, req.UserInfo)
				},
			}
		}},
	}
}

func cacheFirstTenant(name string, uid types.UID) *capsulev1beta2.Tenant {
	return renderingTenant(name, uid, "rendered-"+name)
}

// renderingTenant returns a Tenant whose rule renders the given source.
func renderingTenant(name string, uid types.UID, source string) *capsulev1beta2.Tenant {
	tnt := &capsulev1beta2.Tenant{Name: name, UID: uid}
	tnt.Spec.Rules = []*rules.NamespaceRuleBodyTenant{{
		NamespaceSelector:          &metav1.LabelSelector{MatchLabels: map[string]string{"profile": "guarded"}},
		NamespaceRuleBodyNamespace: sourceRule(source),
	}}

	return tnt
}

func cacheFirstNS(refs ...metav1.OwnerReference) *corev1.Namespace {
	return &corev1.Namespace{Name: "team", UID: "team-uid", Labels: map[string]string{"profile": "guarded"}, OwnerReferences: refs}
}

func ownerRef(tnt *capsulev1beta2.Tenant) metav1.OwnerReference {
	return metav1.OwnerReference{APIVersion: capsulev1beta2.GroupVersion.String(), Kind: "Tenant", Name: tnt.Name, UID: tnt.UID}
}

func terminating(ns *corev1.Namespace) *corev1.Namespace {
	stamp := metav1.Now()
	ns.DeletionTimestamp, ns.Finalizers = &stamp, []string{"kubernetes"}

	return ns
}

func sourceRule(source string) *rules.NamespaceRuleBodyNamespace {
	return &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{
		Action:   rules.ActionTypeAllow,
		Metadata: []rules.MetadataRule{{Kinds: []string{"ConfigMap"}, Labels: map[string]rules.MetadataValueRule{source: {}}}},
	}}
}

// managedRules returns the managed RuleStatus of the Namespace "team" with
// tnt as its controller, as the tenant controller writes it.
func managedRules(tnt *capsulev1beta2.Tenant, generation, observed int64, source string) *capsulev1beta2.RuleStatus {
	rs := &capsulev1beta2.RuleStatus{Name: meta.NameForManagedRuleStatus(), Namespace: "team", Generation: generation}
	rs.OwnerReferences = []metav1.OwnerReference{*metav1.NewControllerRef(tnt, capsulev1beta2.GroupVersion.WithKind("Tenant"))}
	rs.Spec = []*rules.NamespaceRuleBodyNamespace{sourceRule(source)}
	rs.Status.ObservedGeneration = observed
	rs.Status.Rules = []*rules.NamespaceRuleBodyNamespace{sourceRule(source)}

	return rs
}

func configMapRequest(tb testing.TB, operation admissionv1.Operation) admission.Request {
	tb.Helper()

	raw, err := json.Marshal(&corev1.ConfigMap{APIVersion: "v1", Kind: "ConfigMap", Name: "settings", Namespace: "team"})
	require.NoError(tb, err)

	return admission.Request{
		Operation: operation, Namespace: "team", Name: "settings",
		Kind:   metav1.GroupVersionKind{Version: "v1", Kind: "ConfigMap"},
		Object: runtime.RawExtension{Raw: raw}, OldObject: runtime.RawExtension{Raw: raw}}
}

func runWrapper(tb testing.TB, h handlers.Handler, cache client.Client, reader client.Reader, operation admissionv1.Operation) *admission.Response {
	tb.Helper()

	decoder, recorder := admission.NewDecoder(cache.Scheme()), events.NewDiscardRecorder()
	req := configMapRequest(tb, operation)

	switch operation {
	case admissionv1.Update:
		return h.OnUpdate(cache, reader, decoder, recorder)(tb.Context(), req)
	case admissionv1.Delete:
		return h.OnDelete(cache, reader, decoder, recorder)(tb.Context(), req)
	default:
		return h.OnCreate(cache, reader, decoder, recorder)(tb.Context(), req)
	}
}

// TestTypedTenantWrappersReadPaths covers the hit path of every wrapper. The
// ruleset wrapper resolves the Namespace, Tenant and RuleStatus of CREATE and
// UPDATE from the cache with no API reads. The tenant and user wrappers feed
// authorization decisions and read only the API reader, as DELETE does.
func TestTypedTenantWrappersReadPaths(t *testing.T) {
	tenantA := cacheFirstTenant("tenant-a", "tenant-a-uid")
	state := []client.Object{tenantA, cacheFirstNS(ownerRef(tenantA)), managedRules(tenantA, 2, 2, "managed")}

	for _, wrapper := range cacheFirstWrappers() {
		t.Run(wrapper.name, func(t *testing.T) {
			cache, cacheReads := wrapperClient(t, state...)
			api, apiReads := wrapperClient(t, state...)

			for _, operation := range []admissionv1.Operation{admissionv1.Create, admissionv1.Update} {
				got := &resolution{}
				// The router wraps the API reader in a per-request reader for chains.
				require.Nil(t, runWrapper(t, wrapper.build(got), cache, webhookutils.NewRequestCachingReader(api), operation))
				require.Equal(t, 1, got.calls)
				require.Equal(t, tenantA.UID, got.tenant)

				if wrapper.apiOnly {
					require.Equal(t, [3]int32{}, cacheReads.counts(), "%s: authorization paths never read the cache", operation)
					require.Equal(t, [3]int32{1, 1, 0}, apiReads.counts(), "%s: the Namespace and Tenant come from the API", operation)
				} else {
					require.Equal(t, "managed", got.source, "rules come from the cached RuleStatus")
					require.Equal(t, [3]int32{}, apiReads.counts(), "%s: the hit path makes no API reads", operation)
					require.Equal(t, [3]int32{1, 1, 1}, cacheReads.counts(), operation)
				}

				*cacheReads, *apiReads = wrapperReads{}, wrapperReads{}
			}

			got := &resolution{}
			require.Nil(t, runWrapper(t, wrapper.build(got), cache, api, admissionv1.Delete))
			require.Equal(t, [3]int32{}, cacheReads.counts(), "DELETE never reads the cache")

			if wrapper.ruleset {
				// The ruleset wrapper does not handle DELETE at all.
				require.Zero(t, got.calls)
				require.Equal(t, [3]int32{}, apiReads.counts())

				return
			}

			require.Equal(t, 1, got.calls)
			require.Equal(t, tenantA.UID, got.tenant)
			require.Equal(t, [3]int32{1, 1, 0}, apiReads.counts(), "DELETE reads the Namespace and Tenant from the API")
		})
	}
}

// TestTypedTenantWrappersRevokedOwnership covers a cache that still holds an
// ownership the API server no longer has. The tenant and user wrappers resolve
// the Tenant of the API reader, whatever the cache says.
func TestTypedTenantWrappersRevokedOwnership(t *testing.T) {
	tenantA := cacheFirstTenant("tenant-a", "tenant-a-uid")
	tenantB := cacheFirstTenant("tenant-b", "tenant-b-uid")

	for _, wrapper := range cacheFirstWrappers() {
		if !wrapper.apiOnly {
			continue
		}

		for _, operation := range []admissionv1.Operation{admissionv1.Create, admissionv1.Update, admissionv1.Delete} {
			t.Run(fmt.Sprintf("%s/%s", wrapper.name, operation), func(t *testing.T) {
				// The cache still sees the Namespace in Tenant A, the API in Tenant B.
				cache, cacheReads := wrapperClient(t, tenantA, tenantB, cacheFirstNS(ownerRef(tenantA)))
				api, _ := wrapperClient(t, tenantA, tenantB, cacheFirstNS(ownerRef(tenantB)))
				got := &resolution{}

				require.Nil(t, runWrapper(t, wrapper.build(got), cache, api, operation))
				require.Equal(t, 1, got.calls)
				require.Equal(t, tenantB.UID, got.tenant, "the API answer decides")
				require.Equal(t, [3]int32{}, cacheReads.counts())
			})
		}
	}
}

// TestTypedTenantWithRulesetFallsBackToTheAPIReader covers each case where the
// cache cannot be trusted. Cache and API disagree, and the API answer must win.
func TestTypedTenantWithRulesetFallsBackToTheAPIReader(t *testing.T) {
	tenantA := cacheFirstTenant("tenant-a", "tenant-a-uid")
	tenantB := cacheFirstTenant("tenant-b", "tenant-b-uid")
	recreatedA := cacheFirstTenant("tenant-a", "tenant-a-new-uid")
	// The API copy of Tenant A after a rule change the cache has not seen.
	changedA := renderingTenant("tenant-a", "tenant-a-uid", "rendered-api")
	earlier := metav1.NewTime(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	later := metav1.NewTime(earlier.Add(time.Minute))
	current := func(tnt *capsulev1beta2.Tenant) *capsulev1beta2.RuleStatus { return managedRules(tnt, 2, 2, "managed") }
	stale := func() *capsulev1beta2.RuleStatus { return managedRules(tenantA, 2, 2, "stale") }
	createdAt := func(obj client.Object, created metav1.Time) client.Object {
		obj.SetCreationTimestamp(created)

		return obj
	}

	var ruleset cacheFirstWrapper

	for _, wrapper := range cacheFirstWrappers() {
		if wrapper.ruleset {
			ruleset = wrapper
		}
	}

	for _, tc := range []struct {
		name       string
		cache, api []client.Object
		wantTenant types.UID
		wantError  string
		wantCache  [3]int32
		wantAPI    [3]int32
		wantSource string
	}{
		// When the ownership falls back, the RuleStatus comes from the API
		// reader too: the cached one ("stale") is never used.
		{
			name: "cold cache", api: []client.Object{tenantA, cacheFirstNS(ownerRef(tenantA)), current(tenantA)},
			wantTenant: tenantA.UID, wantCache: [3]int32{1, 0, 0}, wantAPI: [3]int32{1, 1, 1}, wantSource: "managed",
		},
		{
			name:       "terminating cached Namespace",
			cache:      []client.Object{tenantA, tenantB, terminating(cacheFirstNS(ownerRef(tenantA))), stale()},
			api:        []client.Object{tenantA, tenantB, cacheFirstNS(ownerRef(tenantB)), current(tenantB)},
			wantTenant: tenantB.UID, wantCache: [3]int32{1, 0, 0}, wantAPI: [3]int32{1, 1, 1}, wantSource: "managed",
		},
		{
			name:       "cached Namespace without Tenant ownerReference",
			cache:      []client.Object{tenantA, cacheFirstNS(), stale()},
			api:        []client.Object{tenantA, cacheFirstNS(ownerRef(tenantA)), current(tenantA)},
			wantTenant: tenantA.UID, wantCache: [3]int32{1, 0, 0}, wantAPI: [3]int32{1, 1, 1}, wantSource: "managed",
		},
		{
			name:       "duplicate Tenant ownerReferences",
			cache:      []client.Object{tenantA, tenantB, cacheFirstNS(ownerRef(tenantA), ownerRef(tenantB)), stale()},
			api:        []client.Object{tenantA, tenantB, cacheFirstNS(ownerRef(tenantB)), current(tenantB)},
			wantTenant: tenantB.UID, wantCache: [3]int32{1, 0, 0}, wantAPI: [3]int32{1, 1, 1}, wantSource: "managed",
		},
		{
			name: "Tenant ownerReference without UID",
			cache: []client.Object{tenantA, cacheFirstNS(metav1.OwnerReference{
				APIVersion: capsulev1beta2.GroupVersion.String(), Kind: "Tenant", Name: tenantA.Name,
			}), stale()},
			api:        []client.Object{tenantA, cacheFirstNS(ownerRef(tenantA)), current(tenantA)},
			wantTenant: tenantA.UID, wantCache: [3]int32{1, 0, 0}, wantAPI: [3]int32{1, 1, 1}, wantSource: "managed",
		},
		{
			name:      "UID mismatch denied by the API",
			cache:     []client.Object{recreatedA, cacheFirstNS(ownerRef(tenantA)), stale()},
			api:       []client.Object{recreatedA, cacheFirstNS(ownerRef(tenantA)), current(recreatedA)},
			wantError: "tenant ownerReference UID mismatch", wantCache: [3]int32{1, 1, 0}, wantAPI: [3]int32{1, 1, 1},
		},
		{
			name:       "Tenant recreated with the same name",
			cache:      []client.Object{tenantA, cacheFirstNS(ownerRef(recreatedA)), stale()},
			api:        []client.Object{recreatedA, cacheFirstNS(ownerRef(recreatedA)), current(recreatedA)},
			wantTenant: recreatedA.UID, wantCache: [3]int32{1, 1, 0}, wantAPI: [3]int32{1, 1, 1}, wantSource: "managed",
		},
		{
			name:       "RuleStatus missing in the cache",
			cache:      []client.Object{tenantA, cacheFirstNS(ownerRef(tenantA))},
			api:        []client.Object{tenantA, cacheFirstNS(ownerRef(tenantA)), managedRules(tenantA, 3, 3, "api")},
			wantTenant: tenantA.UID, wantCache: [3]int32{1, 1, 1}, wantAPI: [3]int32{0, 0, 1}, wantSource: "api",
		},
		{
			name:       "RuleStatus generation pending in the cache",
			cache:      []client.Object{tenantA, cacheFirstNS(ownerRef(tenantA)), managedRules(tenantA, 3, 2, "cache")},
			api:        []client.Object{tenantA, cacheFirstNS(ownerRef(tenantA)), managedRules(tenantA, 3, 3, "api")},
			wantTenant: tenantA.UID, wantCache: [3]int32{1, 1, 1}, wantAPI: [3]int32{0, 0, 1}, wantSource: "api",
		},
		{
			// The API RuleStatus is pending too: the rules are rendered from the
			// Tenant and Namespace of the API reader, not from the cached ones.
			name:       "RuleStatus generation pending in the cache and the API",
			cache:      []client.Object{tenantA, cacheFirstNS(ownerRef(tenantA)), managedRules(tenantA, 3, 2, "cache")},
			api:        []client.Object{changedA, cacheFirstNS(ownerRef(tenantA)), managedRules(tenantA, 4, 3, "api")},
			wantTenant: tenantA.UID, wantCache: [3]int32{1, 1, 1}, wantAPI: [3]int32{1, 1, 1}, wantSource: "rendered-api",
		},
		{
			name:       "RuleStatus missing in the cache and the API",
			cache:      []client.Object{tenantA, cacheFirstNS(ownerRef(tenantA))},
			api:        []client.Object{changedA, cacheFirstNS(ownerRef(tenantA))},
			wantTenant: tenantA.UID, wantCache: [3]int32{1, 1, 1}, wantAPI: [3]int32{1, 1, 1}, wantSource: "rendered-api",
		},
		{
			// The RuleStatus informer has not seen the RuleStatus of the earlier
			// Namespace of Tenant A deleted yet.
			name: "Namespace recreated in Tenant B with the RuleStatus of Tenant A cached",
			cache: []client.Object{tenantA, tenantB, createdAt(cacheFirstNS(ownerRef(tenantB)), later),
				createdAt(managedRules(tenantA, 2, 2, "stale"), earlier)},
			api: []client.Object{tenantA, tenantB, createdAt(cacheFirstNS(ownerRef(tenantB)), later),
				createdAt(managedRules(tenantB, 1, 1, "managed"), later)},
			wantTenant: tenantB.UID, wantCache: [3]int32{1, 1, 1}, wantAPI: [3]int32{0, 0, 1}, wantSource: "managed",
		},
		{
			name: "Namespace recreated in Tenant B before its RuleStatus exists",
			cache: []client.Object{tenantA, tenantB, createdAt(cacheFirstNS(ownerRef(tenantB)), later),
				createdAt(managedRules(tenantA, 2, 2, "stale"), earlier)},
			api:        []client.Object{tenantA, tenantB, createdAt(cacheFirstNS(ownerRef(tenantB)), later)},
			wantTenant: tenantB.UID, wantCache: [3]int32{1, 1, 1}, wantAPI: [3]int32{1, 1, 1}, wantSource: "rendered-tenant-b",
		},
	} {
		for _, operation := range []admissionv1.Operation{admissionv1.Create, admissionv1.Update} {
			t.Run(fmt.Sprintf("%s/%s", tc.name, operation), func(t *testing.T) {
				cache, cacheReads := wrapperClient(t, tc.cache...)
				api, apiReads := wrapperClient(t, tc.api...)
				got := &resolution{}

				response := runWrapper(t, ruleset.build(got), cache, api, operation)

				require.Equal(t, tc.wantCache, cacheReads.counts(), "the cache is consulted first")
				require.Equal(t, tc.wantAPI, apiReads.counts(), "the API reader answers the fallback")

				if tc.wantError != "" {
					require.NotNil(t, response)
					require.False(t, response.Allowed)
					require.Contains(t, response.Result.Message, tc.wantError)
					require.Zero(t, got.calls, "a denied resolution must not reach the inner handlers")

					return
				}

				require.Nil(t, response)
				require.Equal(t, 1, got.calls)
				require.Equal(t, tc.wantTenant, got.tenant, "the API answer decides")
				require.Equal(t, tc.wantSource, got.source)
			})
		}
	}
}

func BenchmarkTypedTenantWrapperReads(b *testing.B) {
	for _, warm := range []bool{true, false} {
		for _, tenants := range []int{1, 16} {
			for _, wrapper := range cacheFirstWrappers() {
				b.Run(fmt.Sprintf("warm=%t/tenants=%d/%s", warm, tenants, wrapper.name), func(b *testing.B) {
					tenantA := cacheFirstTenant("tenant-a", "tenant-a-uid")
					state := []client.Object{tenantA, cacheFirstNS(ownerRef(tenantA)), managedRules(tenantA, 2, 2, "managed")}

					for i := 1; i < tenants; i++ {
						state = append(state, cacheFirstTenant(fmt.Sprintf("tenant-%d", i), types.UID(fmt.Sprintf("tenant-%d-uid", i))))
					}

					var cached []client.Object
					if warm {
						cached = state
					}

					cache, _ := wrapperClient(b, cached...)
					api, apiReads := wrapperClient(b, state...)
					got := &resolution{}
					h := wrapper.build(got)
					decoder, recorder := admission.NewDecoder(cache.Scheme()), events.NewDiscardRecorder()
					req := configMapRequest(b, admissionv1.Create)

					b.ReportAllocs()

					for b.Loop() {
						if response := h.OnCreate(cache, webhookutils.NewRequestCachingReader(api), decoder, recorder)(b.Context(), req); response != nil {
							b.Fatalf("unexpected response: %+v", response)
						}
					}

					if got.tenant != tenantA.UID {
						b.Fatalf("resolved tenant %q", got.tenant)
					}

					counts := apiReads.counts()
					b.ReportMetric(float64(counts[0]+counts[1]+counts[2])/float64(b.N), "API-GET/op")
				})
			}
		}
	}
}
