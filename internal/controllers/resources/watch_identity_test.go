// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/rest"
	clientcache "k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func TestWatchEventsNeverEvaluateAnotherExecutionIdentity(t *testing.T) {
	for _, expression := range []string{"object.data.value == 'private'", "object.data.unreadable.startsWith('x')"} {
		t.Run(expression, func(t *testing.T) {
			f := watchFixtureFor(t, 0)
			local := &capsulev1beta2.TenantResource{}
			if err := f.client.Get(t.Context(), client.ObjectKey{Name: "a", Namespace: "a"}, local); err != nil {
				t.Fatal(err)
			}
			local.Spec.ServiceAccount = &meta.LocalRFC1123ObjectReference{Name: "restricted"}
			local.Spec.Triggers[0].MatchConditions = []admissionv1.MatchCondition{{Name: "private-predicate", Expression: expression}}
			local.Generation++
			if err := f.client.Update(t.Context(), local); err != nil {
				t.Fatal(err)
			}
			reader := local.DeepCopy()
			reader.Name, reader.UID, reader.ResourceVersion = "reader", "reader", ""
			reader.Spec.ServiceAccount.Name = "reader"
			reader.Spec.Triggers[0].MatchConditions[0].Expression = "object.data.value == 'private'"
			if err := f.client.Create(t.Context(), reader); err != nil {
				t.Fatal(err)
			}
			if _, err := f.watcher.Reconcile(t.Context(), reconcile.Request{}); err != nil {
				t.Fatal(err)
			}
			source := sourceObject("a")
			source.Object["data"] = map[string]any{"value": "private"}
			// The controller stream is privileged, but may feed only its global
			// subscription. Neither local identity may inspect those contents.
			f.watcher.matchEvent(t.Context(), resourceWatchEvent{resource: configMapsGVR, operation: admissionv1.Create, object: source})
			if targets := f.notifications.take(); len(targets) != 1 || targets["global"] != 1 {
				t.Fatalf("controller data crossed identity boundary: %v", targets)
			}
			metric := &dto.Metric{}
			if err := f.watcher.metrics.Events.WithLabelValues("condition-error").Write(metric); err != nil {
				t.Fatal(err)
			}
			if metric.GetCounter().GetValue() != 0 {
				t.Fatal("unauthorized CEL was evaluated")
			}
			identity := watchIdentity(reader, capsulev1beta2.ServiceAccountClient{})
			f.watcher.matchEvent(t.Context(), resourceWatchEvent{resource: configMapsGVR, identity: identity, operation: admissionv1.Create, object: source})
			if targets := f.notifications.take(); len(targets) != 1 || targets["reader"] != 1 {
				t.Fatalf("authorized stream reached wrong targets: %v", targets)
			}
			reader.Spec.ServiceAccount.Name = "restricted"
			reader.Generation++
			if err := f.client.Update(t.Context(), reader); err != nil {
				t.Fatal(err)
			}
			if _, err := f.watcher.Reconcile(t.Context(), reconcile.Request{}); err != nil {
				t.Fatal(err)
			}
			f.watcher.matchEvent(t.Context(), resourceWatchEvent{resource: configMapsGVR, identity: identity, operation: admissionv1.Create, object: source})
			if targets := f.notifications.take(); len(targets) != 0 {
				t.Fatalf("stale execution identity notified edited source: %v", targets)
			}
			local.Spec.ServiceAccount = nil
			local.Generation++
			if err := f.client.Update(t.Context(), local); err != nil {
				t.Fatal(err)
			}
			f.watcher.configuration = watchConfiguration{properties: capsulev1beta2.ServiceAccountClient{TenantDefaultServiceAccount: "reader"}}
			if _, err := f.watcher.Reconcile(t.Context(), reconcile.Request{}); err != nil {
				t.Fatal(err)
			}
			// Fence old streams immediately against the current configuration,
			// before the asynchronous subscription rebuild catches up.
			f.watcher.configuration = watchConfiguration{properties: capsulev1beta2.ServiceAccountClient{TenantDefaultServiceAccount: "restricted"}}
			f.watcher.matchEvent(t.Context(), resourceWatchEvent{resource: configMapsGVR, identity: identity, operation: admissionv1.Create, object: source})
			if targets := f.notifications.take(); len(targets) != 0 {
				t.Fatalf("stale default identity notified source: %v", targets)
			}
		})
	}
}

func TestWatchConstructionFailureRetriesWithoutChangingSubscription(t *testing.T) {
	f := watchFixtureFor(t, 0)
	for len(f.watcher.changed) != 0 {
		<-f.watcher.changed
	}
	var attempts atomic.Int64
	f.watcher.newInformer = func(ctx context.Context, key resourceWatchKey, kind schema.GroupVersionKind) (clientcache.SharedIndexInformer, error) {
		if attempts.Add(1) == 1 {
			return nil, fmt.Errorf("execution configuration temporarily unavailable")
		}
		return f.watcher.resourceInformer(ctx, key, kind)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- f.watcher.Start(ctx) }()
	defer func() { cancel(); <-done }()
	poll := func(count int64) {
		t.Helper()
		if err := wait.PollUntilContextTimeout(t.Context(), time.Millisecond, 5*time.Second, true, func(context.Context) (bool, error) { return attempts.Load() >= count, nil }); err != nil {
			t.Fatal(err)
		}
	}
	poll(1)
	if _, err := f.watcher.Reconcile(t.Context(), reconcile.Request{}); err != nil {
		t.Fatal(err)
	}
	poll(2)
}

func TestWatchIdentityIncludesDefaultsNamespaceAndTransport(t *testing.T) {
	a := &capsulev1beta2.TenantResource{Namespace: "a"}
	b := &capsulev1beta2.TenantResource{Namespace: "b"}
	props := capsulev1beta2.ServiceAccountClient{TenantDefaultServiceAccount: "reader"}
	first := watchIdentity(a, props)
	if first.account != (cache.Key{Namespace: "a", Name: "reader"}) || first == watchIdentity(b, props) {
		t.Fatal("identity lost its namespace")
	}
	props.TenantDefaultServiceAccount = "restricted"
	if first == watchIdentity(a, props) {
		t.Fatal("default identity change reused old stream")
	}
	a.Spec.ServiceAccount = &meta.LocalRFC1123ObjectReference{Name: "reader"}
	if first != watchIdentity(a, props) {
		t.Fatal("unrelated default change replaced explicit identity")
	}
	props.Endpoint = "https://other.example"
	if first == watchIdentity(a, props) {
		t.Fatal("transport change reused old stream")
	}
	global := &capsulev1beta2.GlobalTenantResource{Spec: capsulev1beta2.GlobalTenantResourceSpec{ServiceAccount: &meta.NamespacedRFC1123ObjectReferenceWithNamespace{Name: "reader", Namespace: "a"}}}
	identity := watchIdentity(global, capsulev1beta2.ServiceAccountClient{})
	streams := resourceWatchKeys(configMapsGVR, map[resourceWatchIdentity]map[string]struct{}{identity: {"": {}, "a": {}}, {}: {"a": {}}})
	if len(streams) != 3 {
		t.Fatal("global stream retired a local stream with independent authorization")
	}
}

func TestWatchReadsConfigurationOnceForAllCandidates(t *testing.T) {
	f := watchFixtureFor(t, 1000)
	configuration := &capsulev1beta2.CapsuleConfiguration{Name: "watch-configuration"}
	if err := f.client.Create(t.Context(), configuration); err != nil {
		t.Fatal(err)
	}
	f.watcher.configurationName = configuration.Name
	f.gets, f.lists = 0, 0
	f.watcher.matchEvent(t.Context(), resourceWatchEvent{resource: configMapsGVR, operation: admissionv1.Create, object: sourceObject("a")})
	if f.gets != 2 || f.lists != 2 {
		t.Fatalf("GET=%d LIST=%d, want one configuration/namespace read and two indexed lists", f.gets, f.lists)
	}
	if targets := f.notifications.take(); len(targets) != 2 {
		t.Fatalf("unexpected targets: %v", targets)
	}
}

func BenchmarkWatchIdentityResolution(b *testing.B) {
	object := &capsulev1beta2.TenantResource{Namespace: "a"}
	props := capsulev1beta2.ServiceAccountClient{TenantDefaultServiceAccount: "reader"}
	b.ReportAllocs()
	for b.Loop() {
		if identity := watchIdentity(object, props); identity.account.Name != "reader" {
			b.Fatal("wrong execution identity")
		}
	}
}

func BenchmarkWatchConfigurationProjection(b *testing.B) {
	f := watchFixtureFor(b, 1000)
	object := &capsulev1beta2.CapsuleConfiguration{Name: "watch-configuration", Spec: capsulev1beta2.CapsuleConfigurationSpec{Impersonation: capsulev1beta2.ServiceAccountClient{TenantDefaultServiceAccount: "reader"}}}
	if err := f.client.Create(b.Context(), object); err != nil {
		b.Fatal(err)
	}
	f.watcher.configurationName = object.Name
	event := resourceWatchEvent{resource: configMapsGVR, operation: admissionv1.Create, object: sourceObject("a"), identity: resourceWatchIdentity{account: cache.Key{Namespace: "a", Name: "reader"}}}
	b.ReportAllocs()
	for b.Loop() {
		f.watcher.matchEvent(b.Context(), event)
		f.notifications.take()
	}
	b.ReportMetric(float64(f.gets)/float64(b.N), "GET/op")
	b.ReportMetric(float64(f.lists)/float64(b.N), "LIST/op")
}

type watchHTTPConfiguration struct {
	watchConfiguration
	endpoint string
	insecure bool
	reads    *atomic.Int64
}

func (c watchHTTPConfiguration) ServiceAccountClient(context.Context) (*rest.Config, error) {
	if c.reads != nil {
		c.reads.Add(1)
	}
	return &rest.Config{Host: c.endpoint, Insecure: c.insecure}, nil
}

func TestImpersonatedWatchRejectsForbiddenReadsWithoutControllerFallback(t *testing.T) {
	var mu sync.Mutex
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api":
			fmt.Fprint(w, `{"kind":"APIVersions","apiVersion":"v1","versions":["v1"]}`)
		case "/apis":
			fmt.Fprint(w, `{"kind":"APIGroupList","apiVersion":"v1","groups":[]}`)
		case "/api/v1":
			fmt.Fprint(w, `{"kind":"APIResourceList","groupVersion":"v1","resources":[{"name":"secrets","kind":"Secret","namespaced":true,"verbs":["list","watch"]}]}`)
		case "/api/v1/namespaces/a/secrets":
			mu.Lock()
			requests = append(requests, r.Header.Get("Impersonate-User"))
			mu.Unlock()
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Forbidden","code":403,"message":"execution identity cannot read secrets"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	f := watchFixtureFor(t, 0)
	f.watcher.configuration = watchHTTPConfiguration{endpoint: server.URL}
	f.watcher.baseREST = &rest.Config{Host: server.URL}
	key := resourceWatchKey{resource: schema.GroupVersionResource{Version: "v1", Resource: "secrets"}, namespace: "a", identity: resourceWatchIdentity{account: cache.Key{Namespace: "a", Name: "restricted"}}}
	informer, err := f.watcher.resourceInformer(t.Context(), key, schema.GroupVersionKind{Version: "v1", Kind: "Secret"})
	if err != nil {
		t.Fatal(err)
	}
	failure := make(chan error, 1)
	if err := informer.SetWatchErrorHandlerWithContext(func(_ context.Context, _ *clientcache.Reflector, err error) {
		select {
		case failure <- err:
		default:
		}
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); informer.RunWithContext(ctx) }()
	select {
	case <-failure:
	case <-time.After(5 * time.Second):
		t.Error("forbidden LIST did not fail")
	}
	cancel()
	<-done
	mu.Lock()
	defer mu.Unlock()
	if len(requests) == 0 {
		t.Fatal("no impersonated source request")
	}
	for _, identity := range requests {
		if identity != "system:serviceaccount:a:restricted" {
			t.Fatalf("source requested under %q", identity)
		}
	}
	if informer.HasSynced() || len(informer.GetStore().List()) != 0 {
		t.Fatal("forbidden data entered informer cache")
	}
	if len(f.notifications.take()) != 0 {
		t.Fatal("forbidden watch notified a target")
	}
	if len(f.watcher.dynamic.(*dynamicfake.FakeDynamicClient).Actions()) != 0 {
		t.Fatal("fell back to privileged source client")
	}
}
