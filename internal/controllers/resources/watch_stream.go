// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	clientcache "k8s.io/client-go/tools/cache"

	"github.com/projectcapsule/capsule/internal/cache"
)

const (
	watchListPageSize    = 32
	maxWatchListObjects  = 2048
	watchListPageTimeout = 15 * time.Second
)

// resourceEventSource is the event lifecycle used by the watch runnable. Production
// sources use a client-go reflector directly, avoiding informer stores, DeltaFIFO
// queues and asynchronous handler buffers that retain unbounded full objects.
type resourceEventSource interface {
	RunWithContext(context.Context)
	HasSynced() bool
	AddEventHandler(clientcache.ResourceEventHandler) (clientcache.ResourceEventHandlerRegistration, error)
	SetWatchErrorHandlerWithContext(clientcache.WatchErrorHandlerWithContext) error
}

type resourceWatchStream struct {
	mu       sync.Mutex
	started  bool
	handler  clientcache.ResourceEventHandler
	onError  clientcache.WatchErrorHandlerWithContext
	synced   atomic.Bool
	store    *cache.ReplicationWatchSnapshots
	page     func(context.Context, metav1.ListOptions) (*unstructured.UnstructuredList, error)
	watch    func(context.Context, metav1.ListOptions) (watch.Interface, error)
	slots    chan struct{}
	owner    *ResourceWatches
	stamp    uint64
	complete bool
}

// The reflector's streaming-list implementation accumulates a temporary unbounded
// store. This source supplies its own bounded, paginated baseline instead.
type boundedWatchList struct{ *clientcache.ListWatch }

func (boundedWatchList) IsWatchListSemanticsUnSupported() bool { return true }

func (s *resourceWatchStream) HasSynced() bool { return s.synced.Load() }

func (s *resourceWatchStream) GetStore() *cache.ReplicationWatchSnapshots { return s.store }

func (s *resourceWatchStream) AddEventHandler(handler clientcache.ResourceEventHandler) (clientcache.ResourceEventHandlerRegistration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if handler == nil {
		return nil, fmt.Errorf("resource watch handler is required")
	}

	if s.started || s.handler != nil {
		return nil, fmt.Errorf("resource watch handler is already configured")
	}

	s.handler = handler

	return nil, nil
}

func (s *resourceWatchStream) SetWatchErrorHandlerWithContext(handler clientcache.WatchErrorHandlerWithContext) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return fmt.Errorf("resource watch already started")
	}

	s.onError = handler

	return nil
}

func (s *resourceWatchStream) RunWithContext(ctx context.Context) {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()

		return
	}

	s.started = true
	s.mu.Unlock()

	defer func() { s.owner.metrics.SnapshotBytes.Add(float64(s.store.Close())) }()

	lw := boundedWatchList{&clientcache.ListWatch{
		ListWithContextFunc: s.list,
		WatchFuncWithContext: func(ctx context.Context, options metav1.ListOptions) (watch.Interface, error) {
			stream, err := s.watch(ctx, options)
			if err != nil && s.onError != nil {
				s.onError(ctx, nil, err)
			}

			return stream, err
		},
	}}
	reflector := clientcache.NewReflectorWithOptions(lw, &unstructured.Unstructured{}, &resourceWatchStore{stream: s, done: ctx.Done()}, clientcache.ReflectorOptions{Logger: &s.owner.log})
	reflector.RunWithContext(ctx)
}

func (s *resourceWatchStream) list(ctx context.Context, options metav1.ListOptions) (runtime.Object, error) {
	result, err := s.listPages(ctx, options)
	if err != nil && s.onError != nil {
		s.onError(ctx, nil, err)
	}

	return result, err
}

func (s *resourceWatchStream) listPages(ctx context.Context, options metav1.ListOptions) (runtime.Object, error) {
	s.stamp++
	s.complete = false

	options.Limit, options.Continue = watchListPageSize, ""
	if options.ResourceVersion == "0" {
		options.ResourceVersion = ""
	}

	var resourceVersion string

	count, bytes := 0, 0

	for range maxWatchListObjects / watchListPageSize {
		var next string

		err := s.consumePage(ctx, &options, func(page *unstructured.UnstructuredList) error {
			if resourceVersion == "" {
				resourceVersion = page.GetResourceVersion()
			} else if page.GetResourceVersion() != resourceVersion {
				return fmt.Errorf("resource watch LIST changed snapshot resourceVersion")
			}

			processed := 0

			for i := range page.Items {
				if err := ctx.Err(); err != nil {
					return err
				}

				if count >= maxWatchListObjects || bytes >= 4*cache.MaxWatchSnapshotBytes {
					break
				}
				// Copy the small wrapper so one retained snapshot cannot pin the entire
				// decoded page's backing array and all its otherwise evicted objects.
				object := page.Items[i]
				bytes += s.observe(&object, false)
				count++
				processed++
			}

			next = page.GetContinue()
			s.complete = next == "" && processed == len(page.Items)

			return nil
		})
		if err != nil {
			return nil, err
		}

		if s.complete {
			break
		}

		if next == "" || count >= maxWatchListObjects || bytes >= 4*cache.MaxWatchSnapshotBytes {
			break
		}

		options.Continue, options.ResourceVersion = next, ""
	}

	if !s.complete {
		s.owner.metrics.Events.WithLabelValues("baseline-limited").Inc()
	}
	// Only the snapshot RV reaches the reflector's pager; decoded objects have
	// already passed through the bounded store, with at most two pages in flight.
	return &unstructured.UnstructuredList{Object: map[string]any{"apiVersion": "v1", "kind": "List", "metadata": map[string]any{"resourceVersion": resourceVersion}}}, nil
}

func (s *resourceWatchStream) consumePage(ctx context.Context, options *metav1.ListOptions, consume func(*unstructured.UnstructuredList) error) error {
	select {
	case s.slots <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}

	defer func() { <-s.slots }()

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		exceeded := &atomic.Bool{}
		pageCtx, cancel := context.WithTimeout(ctx, watchListPageTimeout)

		page, err := s.page(context.WithValue(pageCtx, watchListLimitKey{}, exceeded), *options)

		cancel()

		if exceeded.Load() && options.Limit > 1 {
			options.Limit = max(1, options.Limit/2)

			continue
		}

		if err != nil {
			return err
		}

		return consume(page)
	}
}

func (s *resourceWatchStream) observe(object *unstructured.Unstructured, create bool) int {
	key := object.GetNamespace() + "/" + object.GetName()
	old, _, known := s.store.Get(key)

	size := cache.WatchObjectBytes(object)
	if size > cache.MaxWatchObjectBytes || watchManaged(object) {
		s.owner.metrics.SnapshotBytes.Add(float64(s.store.Delete(key)))

		if size > cache.MaxWatchObjectBytes {
			s.owner.metrics.Events.WithLabelValues("object-too-large").Inc()
		}

		return size
	}

	s.owner.metrics.SnapshotBytes.Add(float64(s.store.Put(key, object, size, s.stamp)))

	if s.handler == nil || (!create && !s.synced.Load()) {
		return size
	}

	switch {
	case known:
		if old.GetResourceVersion() != object.GetResourceVersion() || old.GetUID() != object.GetUID() {
			s.handler.OnUpdate(old, object)
		}

	case create:
		s.handler.OnAdd(object, false)
	default:
		s.owner.metrics.Events.WithLabelValues("old-object-unavailable").Inc()
	}

	return size
}

type resourceWatchStore struct {
	stream *resourceWatchStream
	done   <-chan struct{}
}

func (s *resourceWatchStore) Add(value any) error { return s.observe(value, true) }

func (s *resourceWatchStore) Update(value any) error { return s.observe(value, false) }

func (s *resourceWatchStore) Delete(value any) error {
	object, ok := value.(*unstructured.Unstructured)
	if !ok || object == nil {
		return fmt.Errorf("unexpected resource watch object type %T", value)
	}

	key := object.GetNamespace() + "/" + object.GetName()
	if old, _, found := s.stream.store.Get(key); found && old.GetUID() == object.GetUID() {
		s.stream.owner.metrics.SnapshotBytes.Add(float64(s.stream.store.Delete(key)))
	}

	if s.active() && s.stream.handler != nil {
		s.stream.handler.OnDelete(object)
	}

	return nil
}

func (s *resourceWatchStore) Replace(_ []any, _ string) error {
	if s.stream.synced.Load() && s.stream.complete {
		for _, object := range s.stream.store.Missing(s.stream.stamp) {
			if err := s.Delete(object); err != nil {
				return err
			}
		}
	}

	s.stream.synced.Store(true)

	return nil
}

func (*resourceWatchStore) Resync() error { return nil }

func (s *resourceWatchStore) observe(value any, create bool) error {
	object, ok := value.(*unstructured.Unstructured)
	if !ok || object == nil {
		return fmt.Errorf("unexpected resource watch object type %T", value)
	}

	if s.active() {
		s.stream.observe(object, create)
	}

	return nil
}

func (s *resourceWatchStore) active() bool {
	select {
	case <-s.done:
		return false
	default:
		return true
	}
}
