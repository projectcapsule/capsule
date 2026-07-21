// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package indexers_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/go-logr/logr"
	"github.com/projectcapsule/capsule/pkg/runtime/indexers"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/events"
	"k8s.io/client-go/tools/record"
	ctrlcache "sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	ctrlconfig "sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/conversion"
)

func TestAddToManagerRegistersIndexers(t *testing.T) {
	t.Parallel()

	mgr := &fakeManager{indexer: &recordingFieldIndexer{}}

	if err := indexers.AddToManager(context.Background(), logr.Discard(), mgr); err != nil {
		t.Fatalf("AddToManager() unexpected error: %v", err)
	}

	if got, want := len(mgr.indexer.calls), 22; got != want {
		t.Fatalf("registered indexers = %d, want %d", got, want)
	}

	fields := map[string]bool{}
	for _, call := range mgr.indexer.calls {
		if call.object == nil {
			t.Fatalf("registered nil object for field %q", call.field)
		}
		if call.field == "" {
			t.Fatalf("registered empty field for object %T", call.object)
		}
		if call.fn == nil {
			t.Fatalf("registered nil indexer func for field %q", call.field)
		}
		fields[call.field] = true
	}

	for _, field := range []string{
		".spec.name",
		".status.namespaces",
		"spec.serviceaccount",
		"hostnamePathPair",
		".spec.dependsOn.global",
		".spec.dependsOn.namespaced",
	} {
		if !fields[field] {
			t.Fatalf("expected field %q to be registered; got %#v", field, fields)
		}
	}
}

func TestAddToManagerReturnsRegistrationError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("index failed")
	mgr := &fakeManager{indexer: &recordingFieldIndexer{err: wantErr}}

	if err := indexers.AddToManager(context.Background(), logr.Discard(), mgr); !errors.Is(err, wantErr) {
		t.Fatalf("AddToManager() error = %v, want %v", err, wantErr)
	}
}

type indexCall struct {
	object client.Object
	field  string
	fn     client.IndexerFunc
}

type recordingFieldIndexer struct {
	calls []indexCall
	err   error
}

func (r *recordingFieldIndexer) IndexField(_ context.Context, obj client.Object, field string, fn client.IndexerFunc) error {
	r.calls = append(r.calls, indexCall{object: obj, field: field, fn: fn})

	return r.err
}

type fakeManager struct {
	indexer *recordingFieldIndexer
}

func (f *fakeManager) GetFieldIndexer() client.FieldIndexer { return f.indexer }
func (f *fakeManager) GetEventRecorderFor(string) record.EventRecorder {
	return nil
}
func (f *fakeManager) GetEventRecorder(string) events.EventRecorder { return nil }
func (f *fakeManager) GetHTTPClient() *http.Client                  { return nil }
func (f *fakeManager) GetConfig() *rest.Config                      { return nil }
func (f *fakeManager) GetCache() ctrlcache.Cache                    { return nil }
func (f *fakeManager) GetScheme() *runtime.Scheme                   { return nil }
func (f *fakeManager) GetClient() client.Client                     { return nil }
func (f *fakeManager) GetRESTMapper() meta.RESTMapper               { return nil }
func (f *fakeManager) GetAPIReader() client.Reader                  { return nil }
func (f *fakeManager) Start(context.Context) error                  { return nil }
func (f *fakeManager) Add(manager.Runnable) error                   { return nil }
func (f *fakeManager) Elected() <-chan struct{}                     { return nil }
func (f *fakeManager) AddMetricsServerExtraHandler(string, http.Handler) error {
	return nil
}
func (f *fakeManager) AddHealthzCheck(string, healthz.Checker) error { return nil }
func (f *fakeManager) AddReadyzCheck(string, healthz.Checker) error  { return nil }
func (f *fakeManager) GetWebhookServer() webhook.Server              { return nil }
func (f *fakeManager) GetLogger() logr.Logger                        { return logr.Discard() }
func (f *fakeManager) GetControllerOptions() ctrlconfig.Controller {
	return ctrlconfig.Controller{}
}
func (f *fakeManager) GetConverterRegistry() conversion.Registry { return nil }

var _ manager.Manager = (*fakeManager)(nil)
var _ client.FieldIndexer = (*recordingFieldIndexer)(nil)
var _ cluster.Cluster = (*fakeManager)(nil)
