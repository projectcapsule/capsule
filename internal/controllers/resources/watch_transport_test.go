// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"
	clientcache "k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func watchEndpoint(name string, sourceRequests *atomic.Int64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api":
			fmt.Fprint(w, `{"kind":"APIVersions","apiVersion":"v1","versions":["v1"]}`)
		case "/apis":
			fmt.Fprint(w, `{"kind":"APIGroupList","apiVersion":"v1","groups":[]}`)
		case "/api/v1":
			fmt.Fprint(w, `{"kind":"APIResourceList","groupVersion":"v1","resources":[{"name":"configmaps","kind":"ConfigMap","namespaced":true,"verbs":["list","watch"]}]}`)
		case "/api/v1/namespaces/a/configmaps":
			sourceRequests.Add(1)
			object := fmt.Sprintf(`{"kind":"ConfigMap","apiVersion":"v1","metadata":{"name":"input","namespace":"a","uid":%q,"resourceVersion":"1","labels":{"source":"yes"}},"data":{"value":%q}}`, name, name)
			if r.URL.Query().Get("watch") == "true" {
				if r.URL.Query().Get("sendInitialEvents") == "true" {
					fmt.Fprintf(w, "{\"type\":\"ADDED\",\"object\":%s}\n", object)
					fmt.Fprintln(w, `{"type":"BOOKMARK","object":{"kind":"ConfigMap","apiVersion":"v1","metadata":{"resourceVersion":"1","annotations":{"k8s.io/initial-events-end":"true"}}}}`)
				}
				w.(http.Flusher).Flush()
				<-r.Context().Done()
				return
			}
			fmt.Fprintf(w, `{"kind":"ConfigMapList","apiVersion":"v1","metadata":{"resourceVersion":"1"},"items":[%s]}`, object)
		default:
			http.NotFound(w, r)
		}
	})
}

func runWatchUntilReadyOrError(t *testing.T, informer resourceEventSource, inspect ...func()) error {
	t.Helper()
	failures := make(chan error, 1)
	if err := informer.SetWatchErrorHandlerWithContext(func(_ context.Context, _ *clientcache.Reflector, err error) {
		select {
		case failures <- err:
		default:
		}
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); informer.RunWithContext(ctx) }()
	defer func() { cancel(); <-done }()
	var failure error
	if err := wait.PollUntilContextTimeout(t.Context(), time.Millisecond, 5*time.Second, true, func(context.Context) (bool, error) {
		if informer.HasSynced() {
			return true, nil
		}
		select {
		case failure = <-failures:
			return true, nil
		default:
			return false, nil
		}
	}); err != nil {
		t.Fatal(err)
	}
	if failure == nil {
		for _, check := range inspect {
			check()
		}
	}
	return failure
}

func TestWatchEndpointUsesIdentitySnapshotInsteadOfLiveTransport(t *testing.T) {
	var aRequests, bRequests, liveReads atomic.Int64
	a := httptest.NewServer(watchEndpoint("A", &aRequests))
	b := httptest.NewServer(watchEndpoint("B-private", &bRequests))
	defer a.Close()
	defer b.Close()
	f := watchFixtureFor(t, 0)
	props := capsulev1beta2.ServiceAccountClient{Endpoint: a.URL}
	f.watcher.configuration = watchHTTPConfiguration{properties: props, endpoint: b.URL, reads: &liveReads}
	local := &capsulev1beta2.TenantResource{}
	if err := f.client.Get(t.Context(), client.ObjectKey{Name: "a", Namespace: "a"}, local); err != nil {
		t.Fatal(err)
	}
	local.Spec.ServiceAccount = &meta.LocalRFC1123ObjectReference{Name: "reader"}
	local.Spec.Triggers[0].MatchConditions = []capsulev1beta2.TenantResourceTriggerCondition{{Name: "private-probe", Expression: "object.data.value == 'B-private'"}}
	local.Generation++
	if err := f.client.Update(t.Context(), local); err != nil {
		t.Fatal(err)
	}
	if _, err := f.watcher.Reconcile(t.Context(), reconcile.Request{}); err != nil {
		t.Fatal(err)
	}
	identity := watchIdentity(local, props)
	key := resourceWatchKey{resource: configMapsGVR, namespace: "a", identity: identity}
	informer, err := f.watcher.resourceStream(t.Context(), key, corev1.SchemeGroupVersion.WithKind("ConfigMap"))
	if err != nil {
		t.Fatal(err)
	}
	var objects []any
	if err := runWatchUntilReadyOrError(t, informer, func() { objects = informer.GetStore().List() }); err != nil {
		t.Fatal(err)
	}
	if len(objects) != 1 {
		t.Fatalf("source objects=%d", len(objects))
	}
	object := objects[0].(*unstructured.Unstructured)
	if aRequests.Load() == 0 || bRequests.Load() != 0 || liveReads.Load() != 0 {
		t.Errorf("transport snapshot diverged: A=%d B=%d live-config=%d", aRequests.Load(), bRequests.Load(), liveReads.Load())
	}
	f.watcher.matchEvent(t.Context(), resourceWatchEvent{resource: configMapsGVR, identity: identity, operation: admissionv1.Create, object: object})
	if targets := f.notifications.take(); len(targets) != 0 {
		t.Fatalf("another endpoint's data reached identity A's CEL: %v", targets)
	}
}

func TestWatchTLSVerificationUsesIdentitySnapshot(t *testing.T) {
	for _, skip := range []bool{false, true} {
		t.Run(fmt.Sprint(skip), func(t *testing.T) {
			var requests, liveReads atomic.Int64
			server := httptest.NewTLSServer(watchEndpoint("A", &requests))
			defer server.Close()
			f := watchFixtureFor(t, 0)
			props := capsulev1beta2.ServiceAccountClient{Endpoint: server.URL, SkipTLSVerify: skip, TenantDefaultServiceAccount: "reader"}
			f.watcher.configuration = watchHTTPConfiguration{properties: props, endpoint: server.URL, insecure: !skip, reads: &liveReads}
			identity := watchIdentity(&capsulev1beta2.TenantResource{Namespace: "a"}, props)
			informer, err := f.watcher.resourceStream(t.Context(), resourceWatchKey{resource: configMapsGVR, namespace: "a", identity: identity}, corev1.SchemeGroupVersion.WithKind("ConfigMap"))
			if err != nil {
				t.Fatal(err)
			}
			err = runWatchUntilReadyOrError(t, informer)
			if liveReads.Load() != 0 || (skip && (err != nil || !informer.HasSynced())) || (!skip && (err == nil || !strings.Contains(err.Error(), "certificate"))) {
				t.Fatalf("skip=%t live-config=%d synced=%t err=%v", skip, liveReads.Load(), informer.HasSynced(), err)
			}
		})
	}
}
