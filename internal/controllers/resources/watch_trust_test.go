// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/rest"
	clientcache "k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
)

func watchTrustFixture(tb testing.TB, unrelated int) (*watchFixture, *corev1.Secret, *capsulev1beta2.CapsuleConfiguration) {
	tb.Helper()
	f := watchFixtureFor(tb, unrelated)
	secret := &corev1.Secret{Name: "watch-ca", Namespace: "capsule-system", Data: map[string][]byte{"ca.crt": []byte("first-ca")}}
	object := &capsulev1beta2.CapsuleConfiguration{Name: "watch-ca", Spec: capsulev1beta2.CapsuleConfigurationSpec{Impersonation: capsulev1beta2.ServiceAccountClient{
		TenantDefaultServiceAccount: "reader", CASecretName: "watch-ca", CASecretNamespace: "capsule-system", CASecretKey: "ca.crt",
	}}}
	for _, obj := range []client.Object{secret, object} {
		if err := f.client.Create(tb.Context(), obj); err != nil {
			tb.Fatal(err)
		}
	}
	f.watcher.configuration = configuration.NewCapsuleConfiguration(tb.Context(), f.client, f.client, &rest.Config{Host: "https://unused.example"}, object.Name)
	f.watcher.configurationName = object.Name
	return f, secret, object
}

func TestWatchTrustRotationReplacesOnlyAffectedStreams(t *testing.T) {
	f, secret, _ := watchTrustFixture(t, 0)
	starts := 0
	f.watcher.newStream = func(_ context.Context, key resourceWatchKey, _ schema.GroupVersionKind) (resourceEventSource, error) {
		starts++
		return dynamicinformer.NewFilteredDynamicInformer(f.watcher.dynamic, key.resource, key.namespace, 0, clientcache.Indexers{}, nil).Informer(), nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	running := make(map[resourceWatchKey]runningResourceWatch)
	t.Cleanup(func() {
		cancel()
		for _, watch := range running {
			<-watch.done
		}
	})
	plan := func() {
		t.Helper()
		if _, err := f.watcher.Reconcile(t.Context(), reconcile.Request{}); err != nil {
			t.Fatal(err)
		}
		f.watcher.syncWatches(ctx, running)
	}
	plan()
	global := resourceWatchKey{resource: configMapsGVR}
	globalDone := running[global].done
	if len(running) != 3 || starts != 3 || globalDone == nil {
		t.Fatalf("initial streams=%d starts=%d", len(running), starts)
	}
	secret.Annotations = map[string]string{"projectcapsule.dev/unrelated": "changed"}
	secret.Data["unrelated"] = []byte("other-data")
	if err := f.client.Update(t.Context(), secret); err != nil {
		t.Fatal(err)
	}
	plan()
	if starts != 3 {
		t.Fatal("unrelated Secret edits restarted streams")
	}
	var previousDone <-chan struct{}
	for key, watch := range running {
		if key.namespace == "a" {
			previousDone = watch.done
		}
	}
	secret.Data["ca.crt"] = []byte("second-ca")
	if err := f.client.Update(t.Context(), secret); err != nil {
		t.Fatal(err)
	}
	plan()
	if len(running) != 3 || starts != 5 || running[global].done != globalDone {
		t.Fatalf("rotation changed unrelated stream or retained old trust: streams=%d starts=%d", len(running), starts)
	}
	select {
	case <-previousDone:
	default:
		t.Fatal("old trust stream was not cancelled and joined")
	}
	delete(secret.Data, "ca.crt")
	if err := f.client.Update(t.Context(), secret); err != nil {
		t.Fatal(err)
	}
	plan()
	if len(running) != 1 || running[global].done != globalDone {
		t.Fatal("missing CA retained impersonated streams or retired controller stream")
	}
	if err := f.client.Delete(t.Context(), secret); err != nil {
		t.Fatal(err)
	}
	plan()
	if len(running) != 1 {
		t.Fatal("deleted CA enabled a fallback stream")
	}
	secret.ResourceVersion = ""
	secret.Data["ca.crt"] = []byte("third-ca")
	if err := f.client.Create(t.Context(), secret); err != nil {
		t.Fatal(err)
	}
	plan()
	if len(running) != 3 || starts != 7 || running[global].done != globalDone {
		t.Fatal("recreated CA did not restore only affected streams")
	}
}

func TestWatchTrustReadsAreBoundedAndSkippedWhenUnused(t *testing.T) {
	for _, unrelated := range []int{0, 1000} {
		t.Run(fmt.Sprint(unrelated), func(t *testing.T) {
			f, _, object := watchTrustFixture(t, unrelated)
			for _, skipTLS := range []bool{false, true} {
				object.Spec.Impersonation.SkipTLSVerify = skipTLS
				if err := f.client.Update(t.Context(), object); err != nil {
					t.Fatal(err)
				}
				f.gets, f.lists = 0, 0
				if _, err := f.watcher.Reconcile(t.Context(), reconcile.Request{}); err != nil {
					t.Fatal(err)
				}
				wantGets := 4 // configuration, two namespace snapshots, one pinned CA Secret
				if skipTLS {
					wantGets = 3
				}
				if f.gets != wantGets || f.lists != 2 {
					t.Fatalf("skipTLS=%t GET=%d LIST=%d", skipTLS, f.gets, f.lists)
				}
			}
			object.Spec.Impersonation.SkipTLSVerify = false
			object.Spec.Impersonation.TenantDefaultServiceAccount = ""
			if err := f.client.Update(t.Context(), object); err != nil {
				t.Fatal(err)
			}
			f.gets = 0
			if _, err := f.watcher.Reconcile(t.Context(), reconcile.Request{}); err != nil {
				t.Fatal(err)
			}
			if f.gets != 3 {
				t.Fatal("controller streams read an unused CA Secret")
			}
		})
	}
}

func TestWatchConstructionRejectsTrustChangedAfterPlanning(t *testing.T) {
	f, _, _ := watchTrustFixture(t, 0)
	key := resourceWatchKey{resource: configMapsGVR, namespace: "a", identity: resourceWatchIdentity{account: cache.Key{Namespace: "a", Name: "reader"}, transport: capsulev1beta2.ServiceAccountClient{CASecretName: "watch-ca", CASecretNamespace: "capsule-system", CASecretKey: "ca.crt"}}, caBundle: sha256.Sum256([]byte("old-ca"))}
	if _, err := f.watcher.resourceStream(t.Context(), key, corev1.SchemeGroupVersion.WithKind("ConfigMap")); err == nil || !strings.Contains(err.Error(), "CA changed since planning") {
		t.Fatalf("expected changed-trust rejection, got %v", err)
	}
}

func TestEmptyPinnedCARejectsWatchPlanningAndConstruction(t *testing.T) {
	f, secret, _ := watchTrustFixture(t, 0)
	secret.Data["ca.crt"] = nil
	if err := f.client.Update(t.Context(), secret); err != nil {
		t.Fatal(err)
	}
	if _, err := f.watcher.Reconcile(t.Context(), reconcile.Request{}); err != nil {
		t.Fatal(err)
	}
	if len(f.watcher.desired) != 1 {
		t.Error("empty pinned CA retained impersonated streams")
	}
	key := resourceWatchKey{resource: configMapsGVR, namespace: "a", identity: resourceWatchIdentity{account: cache.Key{Namespace: "a", Name: "reader"}, transport: capsulev1beta2.ServiceAccountClient{CASecretName: "watch-ca", CASecretNamespace: "capsule-system", CASecretKey: "ca.crt"}}, caBundle: sha256.Sum256(nil)}
	if _, err := f.watcher.resourceStream(t.Context(), key, corev1.SchemeGroupVersion.WithKind("ConfigMap")); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("empty pinned CA allowed system-trust fallback: %v", err)
	}
}

func TestExplicitTLSVerificationSkipStillIgnoresEmptyCA(t *testing.T) {
	f, secret, object := watchTrustFixture(t, 0)
	secret.Data["ca.crt"] = nil
	object.Spec.Impersonation.SkipTLSVerify = true
	for _, value := range []client.Object{secret, object} {
		if err := f.client.Update(t.Context(), value); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.watcher.Reconcile(t.Context(), reconcile.Request{}); err != nil {
		t.Fatal(err)
	}
	if len(f.watcher.desired) != 3 {
		t.Fatal("explicit TLS verification skip was overridden by the unused empty CA")
	}
}

func BenchmarkWatchCAPlanning(b *testing.B) {
	for _, unrelated := range []int{0, 1000} {
		for _, size := range []int{4096, 65536} {
			b.Run(fmt.Sprintf("unrelated=%d/CA=%d", unrelated, size), func(b *testing.B) {
				f, secret, _ := watchTrustFixture(b, unrelated)
				secret.Data["ca.crt"] = bytes.Repeat([]byte("a"), size)
				if err := f.client.Update(b.Context(), secret); err != nil {
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
				b.ReportMetric(float64(f.gets)/float64(b.N), "GET/op")
				b.ReportMetric(float64(f.lists)/float64(b.N), "LIST/op")
			})
		}
	}
}
