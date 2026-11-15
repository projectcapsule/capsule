// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"maps"
	"slices"
	"strings"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clientcache "k8s.io/client-go/tools/cache"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/replication"
	"github.com/projectcapsule/capsule/pkg/runtime/ssa"
)

type resourceWatchEvent struct {
	done              <-chan struct{}
	resource          schema.GroupVersionResource
	operation         admissionv1.OperationType
	oldObject, object *unstructured.Unstructured
	identity          resourceWatchIdentity
	bytes             int64
}

const maxWatchEventBytes = 8 * 1024 * 1024

func watchObject(value any) *unstructured.Unstructured {
	if tombstone, ok := value.(clientcache.DeletedFinalStateUnknown); ok {
		value = tombstone.Obj
	}

	object, _ := value.(*unstructured.Unstructured)

	return object
}

func watchManaged(object *unstructured.Unstructured) bool {
	if object == nil {
		return false
	}

	metadata := &metav1.PartialObjectMetadata{Labels: object.GetLabels(), OwnerReferences: object.GetOwnerReferences()}
	if replication.Managed(metadata) || len(ssa.ReplicationOwnerPrefixes(object, "")) > 0 {
		return true
	}

	for _, field := range object.GetManagedFields() {
		if strings.HasPrefix(field.Manager, meta.FieldManagerCapsulePrefix+"/") {
			return true
		}
	}

	return false
}

func bookkeepingEvent(resource schema.GroupVersionResource, oldObject, object *unstructured.Unstructured) bool {
	if resource.Group != capsulev1beta2.GroupVersion.Group || (resource.Resource != "tenantresources" && resource.Resource != "globaltenantresources") || oldObject == nil || object == nil {
		return false
	}

	if oldObject.GetGeneration() != object.GetGeneration() || !maps.Equal(oldObject.GetLabels(), object.GetLabels()) {
		return false
	}

	if !oldObject.GetDeletionTimestamp().Equal(object.GetDeletionTimestamp()) {
		return false
	}

	if !slices.EqualFunc(oldObject.GetOwnerReferences(), object.GetOwnerReferences(), func(a, b metav1.OwnerReference) bool {
		return a.APIVersion == b.APIVersion && a.Kind == b.Kind && a.Name == b.Name && a.UID == b.UID &&
			ptr.Equal(a.Controller, b.Controller) && ptr.Equal(a.BlockOwnerDeletion, b.BlockOwnerDeletion)
	}) {
		return false
	}

	oldAnnotations, newAnnotations := oldObject.GetAnnotations(), object.GetAnnotations()
	for _, key := range []string{replication.TriggerAnnotation, meta.ReconcileAnnotation} {
		delete(oldAnnotations, key)
		delete(newAnnotations, key)
	}
	// Watch events have no actor identity. Ignore parent status/finalizer and
	// internal request-token changes by their contents, while preserving spec,
	// label, ownership and other annotation changes from any writer.
	return maps.Equal(oldAnnotations, newAnnotations)
}

func (r *ResourceWatches) enqueueEvent(ctx context.Context, resource schema.GroupVersionResource, identity resourceWatchIdentity, operation admissionv1.OperationType, oldValue, value any) {
	if ctx.Err() != nil {
		return
	}

	oldObject, object := watchObject(oldValue), watchObject(value)
	if oldObject != nil && object != nil && oldObject.GetUID() != object.GetUID() {
		// Informer relists can report a same-name recreation as an update.
		// These are independent objects, including for managed exclusion.
		r.enqueueEvent(ctx, resource, identity, admissionv1.Delete, oldObject, nil)
		r.enqueueEvent(ctx, resource, identity, admissionv1.Create, nil, object)

		return
	}

	source := object

	if source == nil {
		source = oldObject
	}

	if source == nil || source.GetNamespace() == "" {
		return
	}

	if oldObject != nil && object != nil && (oldObject.GetResourceVersion() == object.GetResourceVersion() || bookkeepingEvent(resource, oldObject, object)) {
		return
	}

	oldBytes, objectBytes := cache.WatchObjectBytes(oldObject), cache.WatchObjectBytes(object)
	if oldBytes > cache.MaxWatchObjectBytes || objectBytes > cache.MaxWatchObjectBytes {
		r.metrics.Events.WithLabelValues("object-too-large").Inc()

		return
	}

	if watchManaged(oldObject) || watchManaged(object) {
		return
	}

	event := resourceWatchEvent{done: ctx.Done(), resource: resource, identity: identity, operation: operation, oldObject: oldObject, object: object, bytes: int64(oldBytes + objectBytes)}
	if !r.reserveEvent(event.bytes) {
		r.metrics.Events.WithLabelValues("dropped").Inc()

		return
	}

	select {
	case r.events <- event:
		r.metrics.Events.WithLabelValues("queued").Inc()
	case <-ctx.Done():
		r.releaseEvent(event)
	default:
		r.releaseEvent(event)
		r.metrics.Events.WithLabelValues("dropped").Inc()
	}
}

func (r *ResourceWatches) reserveEvent(size int64) bool {
	for {
		used := r.eventBytes.Load()
		if size > maxWatchEventBytes-used {
			return false
		}

		if r.eventBytes.CompareAndSwap(used, used+size) {
			r.metrics.QueuedBytes.Add(float64(size))

			return true
		}
	}
}

func (r *ResourceWatches) releaseEvent(event resourceWatchEvent) {
	r.eventBytes.Add(-event.bytes)
	r.metrics.QueuedBytes.Sub(float64(event.bytes))
}

func (r *ResourceWatches) matchEvent(ctx context.Context, event resourceWatchEvent) {
	if r.configuration == nil {
		return
	}

	source := event.object
	if source == nil {
		source = event.oldObject
	}

	key := replication.ResourceKey(event.resource, event.operation)
	local := &capsulev1beta2.TenantResourceList{}
	global := &capsulev1beta2.GlobalTenantResourceList{}

	filter := client.MatchingFields{replication.TriggerIndexerField: key}
	if err := r.client.List(ctx, local, filter, client.InNamespace(source.GetNamespace()), client.UnsafeDisableDeepCopy); err != nil {
		r.log.Error(err, "cannot resolve local watch subscriptions")

		return
	}

	if err := r.client.List(ctx, global, filter, client.UnsafeDisableDeepCopy); err != nil {
		r.log.Error(err, "cannot resolve global watch subscriptions")

		return
	}

	var namespaceLabels map[string]string

	namespaceRead, namespaceFailed := false, false

	if len(local.Items)+len(global.Items) == 0 {
		return
	}

	props, err := r.watchProperties(ctx)
	if err != nil {
		r.log.Error(err, "cannot resolve watch execution configuration")

		return
	}

	match := func(object client.Object, spec *capsulev1beta2.TenantResourceCommonSpec) {
		if ctx.Err() != nil || spec.IsCordoned() || !object.GetDeletionTimestamp().IsZero() {
			return
		}

		// A privileged or differently impersonated stream must never supply
		// object contents to another replication's selectors or CEL programs.
		if watchIdentity(object, props) != event.identity {
			return
		}

		prepared, ok := r.prepared.Get(object.GetUID(), object.GetGeneration())
		if !ok {
			r.metrics.Events.WithLabelValues("unprepared").Inc()

			return
		}

		if prepared.NeedsNamespace() && !namespaceRead {
			namespaceRead = true

			namespace := &corev1.Namespace{}
			if err := r.client.Get(ctx, client.ObjectKey{Name: source.GetNamespace()}, namespace); err != nil {
				namespaceFailed = true
			} else {
				namespaceLabels = namespace.Labels
				namespaceFailed = !namespace.DeletionTimestamp.IsZero()
			}
		}

		matched, err := prepared.MatchesWithNamespace(ctx, key, event.operation, namespaceLabels, !namespaceFailed, event.oldObject, event.object)
		if err != nil {
			r.metrics.Events.WithLabelValues("condition-error").Inc()

			return
		}

		if matched {
			r.notifier.Notify(replication.Target{Name: object.GetName(), Namespace: object.GetNamespace(), UID: object.GetUID()}, replication.Interval(spec))
		}
	}

	for i := range local.Items {
		object := &local.Items[i]
		match(object, &object.Spec.TenantResourceCommonSpec)
	}

	for i := range global.Items {
		object := &global.Items[i]
		match(object, &object.Spec.TenantResourceCommonSpec)
	}
}
