// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"maps"
	"sync"
	"time"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/client-go/dynamic/dynamicinformer"
	clientcache "k8s.io/client-go/tools/cache"
)

type runningResourceWatch struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func (r *ResourceWatches) Start(ctx context.Context) error {
	running := make(map[resourceWatchKey]runningResourceWatch)

	var workers sync.WaitGroup
	for range 2 {
		workers.Go(func() {
			for {
				select {
				case <-ctx.Done():
					return
				case event := <-r.events:
					select {
					case <-event.done:
						continue
					default:
					}

					eventCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
					r.matchEvent(eventCtx, event)

					if eventCtx.Err() != nil {
						r.metrics.Events.WithLabelValues("timed-out").Inc()
					}

					cancel()
				}
			}
		})
	}

	defer func() {
		for _, watch := range running {
			watch.cancel()
		}

		for _, watch := range running {
			<-watch.done
		}

		workers.Wait()
		r.metrics.ActiveWatches.Set(0)
	}()
	// Reconcile may have prepared subscriptions before the leader runnable starts.
	r.syncWatches(ctx, running)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-r.changed:
			r.syncWatches(ctx, running)
		}
	}
}

func (r *ResourceWatches) syncWatches(ctx context.Context, running map[resourceWatchKey]runningResourceWatch) {
	r.mu.Lock()
	desired := maps.Clone(r.desired)
	r.mu.Unlock()

	for key, watch := range running {
		if _, ok := desired[key]; !ok {
			watch.cancel()
			<-watch.done
			delete(running, key)
		}
	}

	for key := range desired {
		if _, ok := running[key]; ok {
			continue
		}

		watchCtx, cancel := context.WithCancel(ctx)
		resource := key.resource

		informer := dynamicinformer.NewFilteredDynamicInformer(r.dynamic, resource, key.namespace, 0, clientcache.Indexers{}, nil).Informer()
		if r.newInformer != nil {
			informer = r.newInformer(resource, key.namespace)
		}

		if err := informer.SetWatchErrorHandlerWithContext(func(_ context.Context, _ *clientcache.Reflector, err error) {
			if watchCtx.Err() == nil {
				r.metrics.Events.WithLabelValues("watch-error").Inc()
				r.log.Error(err, "resource watch failed; periodic reconciliation remains active", "resource", resource.String())
			}
		}); err != nil {
			cancel()
			r.log.Error(err, "cannot configure resource watch")

			continue
		}

		if _, err := informer.AddEventHandler(clientcache.ResourceEventHandlerDetailedFuncs{
			AddFunc: func(obj any, initial bool) {
				if !initial {
					r.enqueueEvent(watchCtx, resource, admissionv1.Create, nil, obj)
				}
			},
			UpdateFunc: func(oldObject, object any) { r.enqueueEvent(watchCtx, resource, admissionv1.Update, oldObject, object) },
			DeleteFunc: func(obj any) { r.enqueueEvent(watchCtx, resource, admissionv1.Delete, obj, nil) },
		}); err != nil {
			cancel()
			r.log.Error(err, "cannot attach resource watch")

			continue
		}

		done := make(chan struct{})

		running[key] = runningResourceWatch{cancel: cancel, done: done}

		go func() { defer close(done); informer.RunWithContext(watchCtx) }()
	}

	r.metrics.ActiveWatches.Set(float64(len(running)))
}
