// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"

	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// A single informer listener records conditions in status-event order. Recording
// from either reconcile queue can publish an older gauge after a newer status
// write. The listener also restores metrics on startup and removes deleted series.
func replicationConditionHandler[T client.Object](record func(T), remove func(T)) handler.EventHandler {
	return handler.Funcs{
		CreateFunc: func(_ context.Context, e event.CreateEvent, _ workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			if obj, ok := e.Object.(T); ok {
				record(obj)
			}
		},
		UpdateFunc: func(_ context.Context, e event.UpdateEvent, _ workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			if obj, ok := e.ObjectNew.(T); ok {
				record(obj)
			}
		},
		DeleteFunc: func(_ context.Context, e event.DeleteEvent, _ workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			if obj, ok := e.Object.(T); ok {
				remove(obj)
			}
		},
	}
}
