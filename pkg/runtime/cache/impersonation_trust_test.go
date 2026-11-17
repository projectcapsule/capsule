// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cache_test

import (
	"bytes"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/cache"
)

func TestImpersonationCacheRefreshesTransportTrust(t *testing.T) {
	server := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(server.Close)
	base := &rest.Config{Host: server.URL, CAData: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})}
	sa := makeSA("tenant-a", "reader")
	for _, change := range []string{"ca-data", "endpoint", "tls-server-name", "insecure", "token", "username", "protocols"} {
		t.Run(change, func(t *testing.T) {
			clients := cache.NewImpersonationCache()
			first, err := clients.LoadOrCreate(t.Context(), logr.Discard(), base, scheme.Scheme, sa)
			if err != nil {
				t.Fatal(err)
			}
			rotated := rest.CopyConfig(base)
			switch change {
			case "ca-data":
				rotated.CAData = append(append([]byte(nil), base.CAData...), '\n')
			case "endpoint":
				rotated.Host += "/different"
			case "tls-server-name":
				rotated.ServerName = "different.example"
			case "insecure":
				rotated.CAData = nil
				rotated.Insecure = true
			case "token":
				rotated.BearerToken = "other-token"
			case "username":
				rotated.Username = "other-user"
			case "protocols":
				rotated.NextProtos = []string{"http/1.1"}
			}
			second, err := clients.LoadOrCreate(t.Context(), logr.Discard(), rotated, scheme.Scheme, sa)
			if err != nil {
				t.Fatal(err)
			}
			if first == second {
				t.Fatal("changed trust reused the old execution client")
			}
			again, err := clients.LoadOrCreate(t.Context(), logr.Discard(), rest.CopyConfig(rotated), scheme.Scheme, sa)
			if err != nil || again != second {
				t.Fatalf("identical trust did not reuse replacement: %v", err)
			}
			if clients.Stats() != 1 {
				t.Fatal("rotation retained historical clients")
			}
		})
	}
}

func TestImpersonationCacheRejectsInvalidReplacement(t *testing.T) {
	clients := cache.NewImpersonationCache()
	sa := makeSA("tenant-a", "reader")
	base := &rest.Config{Host: "https://unused.example"}
	first, err := clients.LoadOrCreate(t.Context(), logr.Discard(), base, scheme.Scheme, sa)
	if err != nil {
		t.Fatal(err)
	}
	invalid := rest.CopyConfig(base)
	invalid.CAData = []byte("not a certificate")
	if got, err := clients.LoadOrCreate(t.Context(), logr.Discard(), invalid, scheme.Scheme, sa); err == nil || got != nil {
		t.Fatal("invalid replacement fell back to an older cached client")
	}
	if got, err := clients.LoadOrCreate(t.Context(), logr.Discard(), base, scheme.Scheme, sa); err != nil || got != first {
		t.Fatal("failed build mutated the valid cached snapshot")
	}
	if got, err := clients.LoadOrCreate(t.Context(), logr.Discard(), nil, scheme.Scheme, sa); err == nil || got != nil {
		t.Fatal("missing REST configuration reused a transport-bound client")
	}
	clients.Set("tenant-a", "reader", first)
	if got, err := clients.LoadOrCreate(t.Context(), logr.Discard(), nil, scheme.Scheme, sa); err != nil || got != first {
		t.Fatal("explicit client injection compatibility changed")
	}
}

func TestImpersonationCacheInvalidationDuringConstruction(t *testing.T) {
	for _, action := range []string{"reset", "invalidate", "set", "publish-new-trust"} {
		t.Run(action, func(t *testing.T) {
			clients := cache.NewImpersonationCache()
			sa := makeSA("tenant-a", "reader")
			started, released, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var startOnce, releaseOnce sync.Once
			defer releaseOnce.Do(func() { close(released) })
			base := &rest.Config{Host: "https://unused.example", WrapTransport: func(transport http.RoundTripper) http.RoundTripper {
				startOnce.Do(func() { close(started) })
				<-released
				return transport
			}}
			var loadErr error
			go func() {
				defer close(done)
				_, loadErr = clients.LoadOrCreate(t.Context(), logr.Discard(), base, scheme.Scheme, sa)
			}()
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("client construction did not reach transport wrapper")
			}
			var replacement client.Client = fake.NewClientBuilder().Build()
			switch action {
			case "reset":
				clients.Reset()
			case "invalidate":
				clients.Invalidate("tenant-a", "reader")
			case "set":
				clients.Set("tenant-a", "reader", replacement)
			case "publish-new-trust":
				updated := rest.CopyConfig(base)
				updated.Host, updated.WrapTransport = "https://new.example", nil
				var err error
				replacement, err = clients.LoadOrCreate(t.Context(), logr.Discard(), updated, scheme.Scheme, sa)
				if err != nil {
					t.Fatal(err)
				}
			}
			releaseOnce.Do(func() { close(released) })
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("client construction did not finish")
			}
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			current, found := clients.Get("tenant-a", "reader")
			if action == "set" || action == "publish-new-trust" {
				if !found || current != replacement {
					t.Fatal("in-flight construction overwrote the explicit replacement")
				}
			} else if found || clients.Stats() != 0 {
				t.Fatal("in-flight construction repopulated an invalidated entry")
			}
		})
	}
}

func TestImpersonationCacheTrustIsTenantScoped(t *testing.T) {
	clients := cache.NewImpersonationCache()
	base := &rest.Config{Host: "https://unused.example"}
	a, b := makeSA("tenant-a", "reader"), makeSA("tenant-b", "reader")
	firstA, err := clients.LoadOrCreate(t.Context(), logr.Discard(), base, scheme.Scheme, a)
	if err != nil {
		t.Fatal(err)
	}
	firstB, err := clients.LoadOrCreate(t.Context(), logr.Discard(), base, scheme.Scheme, b)
	if err != nil || firstB == firstA {
		t.Fatalf("different namespaces shared an execution client: %v", err)
	}
	updated := rest.CopyConfig(base)
	updated.Host = "https://new.example"
	secondA, err := clients.LoadOrCreate(t.Context(), logr.Discard(), updated, scheme.Scheme, a)
	if err != nil || secondA == firstA || secondA == firstB {
		t.Fatalf("changed transport did not replace only tenant A: %v", err)
	}
	secondB, err := clients.LoadOrCreate(t.Context(), logr.Discard(), base, scheme.Scheme, b)
	if err != nil || secondB != firstB || clients.Stats() != 2 {
		t.Fatalf("tenant A's replacement disturbed tenant B: %v", err)
	}
}

func BenchmarkImpersonationCacheTransport(b *testing.B) {
	server := httptest.NewTLSServer(http.NotFoundHandler())
	b.Cleanup(server.Close)
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	for _, bundles := range []int{1, 32} {
		for _, identities := range []int{1, 64} {
			for _, mode := range []string{"cold", "warm", "parallel"} {
				b.Run(fmt.Sprintf("ca=%d/identities=%d/%s", bundles, identities, mode), func(b *testing.B) {
					base := &rest.Config{Host: server.URL, CAData: bytes.Repeat(ca, bundles)}
					clients := cache.NewImpersonationCache()
					accounts := make([]meta.NamespacedRFC1123ObjectReferenceWithNamespace, identities)
					for i := range identities {
						accounts[i] = makeSA(fmt.Sprint(i), "reader")
						if _, err := clients.LoadOrCreate(b.Context(), logr.Discard(), base, scheme.Scheme, accounts[i]); err != nil {
							b.Fatal(err)
						}
					}
					load := func(i int) {
						sa := accounts[i%identities]
						if mode == "cold" {
							clients.Invalidate(string(sa.Namespace), string(sa.Name))
						}
						if _, err := clients.LoadOrCreate(b.Context(), logr.Discard(), base, scheme.Scheme, sa); err != nil {
							b.Error(err)
						}
					}
					b.ReportAllocs()
					if mode == "parallel" {
						b.ResetTimer()
						b.RunParallel(func(pb *testing.PB) {
							for i := 0; pb.Next(); i++ {
								load(i)
							}
						})
					} else {
						for i := 0; b.Loop(); i++ {
							load(i)
						}
					}
				})
			}
		}
	}
}
