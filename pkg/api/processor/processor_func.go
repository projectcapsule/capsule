// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package processor

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/ssa"
)

func (p *Processor) Reconcile(
	ctx context.Context,
	log logr.Logger,
	c client.Client,
	processed *meta.ProcessedItems,
	acc Accumulator,
	opts ProcessorOptions,
) (err error) {
	log.V(5).Info("starting pruning items", "present", len(*processed))

	if itemErrors := p.pruneProcessedItems(ctx, log, c, processed, acc, opts); itemErrors > 0 {
		return fmt.Errorf("pruning of %d resources failed", itemErrors)
	}

	log.V(5).Info("accumulation after pruning", "items", len(acc))

	if itemErrors := p.applyAccumulatedItems(ctx, log, c, processed, acc, opts); itemErrors > 0 {
		return fmt.Errorf("applying of %d resources failed", itemErrors)
	}

	// Running Healthchecks

	log.V(4).Info("processing completed")

	return nil
}

// ReconcileNamespace applies the accumulated items targeting a single Namespace of a
// single Tenant, returning the processed items of that scope only.
//
// Contrarily to Reconcile, the given Accumulator is not authoritative for the whole
// Tenant: nothing is pruned, nor disowned, since the items missing from it may just
// belong to a Namespace this run knows nothing about. Pruning remains a duty of the
// full reconciliation.
//
// The returned items are meant to be grafted on the persisted status through
// meta.ProcessedItems.ReplaceScope, which leaves the other Namespaces untouched.
// They are returned along an error too, since they carry the outcome of the single
// items which have been processed.
func (p *Processor) ReconcileNamespace(
	ctx context.Context,
	log logr.Logger,
	c client.Client,
	current meta.ProcessedItems,
	acc Accumulator,
	opts ProcessorOptions,
	scope Scope,
) (meta.ProcessedItems, error) {
	if scope.Namespace == "" {
		return nil, fmt.Errorf("cannot process a Namespace scope without a Namespace")
	}

	log = log.WithValues("tenant", scope.Tenant, "namespace", scope.Namespace)

	processed := current.InScope(scope.Tenant, scope.Namespace)

	scoped, skipped := scopedAccumulator(acc, scope)
	if skipped > 0 {
		log.V(5).Info("ignored accumulated items out of the processed scope", "ignored", skipped)
	}

	log.V(5).Info("starting scoped processing", "present", len(processed), "items", len(scoped))

	if itemErrors := p.applyAccumulatedItems(ctx, log, c, &processed, scoped, opts); itemErrors > 0 {
		return processed, fmt.Errorf("applying of %d resources failed", itemErrors)
	}

	log.V(4).Info("scoped processing completed")

	return processed, nil
}

// Retains the accumulated items belonging to the given scope only, along with the
// amount of the ignored ones.
func scopedAccumulator(acc Accumulator, scope Scope) (Accumulator, int) {
	scoped := make(Accumulator, len(acc))
	ignored := 0

	for key, item := range acc {
		if item == nil {
			continue
		}

		if !scope.Matches(item.Resource) {
			ignored++

			continue
		}

		scoped[key] = item
	}

	return scoped, ignored
}

func (p *Processor) pruneProcessedItems(
	ctx context.Context,
	log logr.Logger,
	c client.Client,
	processed *meta.ProcessedItems,
	acc Accumulator,
	opts ProcessorOptions,
) int {
	itemErrors := 0

	for _, item := range *processed {
		if _, exists := acc[item.GetKey("")]; exists {
			continue
		}

		if item.LastApply.IsZero() {
			processed.RemoveItem(item)

			continue
		}

		obj, err := p.objectForProcessedItem(item)
		if failAndRecord(processed, &itemErrors, item, "resolving resource scope failed: ", err) {
			continue
		}

		if p.pruneProcessedItem(ctx, log, c, processed, opts, item, obj, &itemErrors) {
			continue
		}

		p.disownProcessedItem(ctx, c, processed, opts, item, obj, &itemErrors)
	}

	return itemErrors
}

func (p *Processor) pruneProcessedItem(
	ctx context.Context,
	log logr.Logger,
	c client.Client,
	processed *meta.ProcessedItems,
	opts ProcessorOptions,
	item meta.ObjectReferenceStatus,
	obj *unstructured.Unstructured,
	itemErrors *int,
) bool {
	if !opts.Prune {
		return false
	}

	log.V(4).Info("pruning resources", "Kind", item.Kind, "Name", item.Name, "Namespace", item.Namespace)

	fieldOwner := opts.FieldOwnerPrefix + "/" + item.FieldOwner("")

	deleted, err := p.Prune(ctx, c, obj, fieldOwner, &item)
	if failAndRecord(processed, itemErrors, item, "pruning failed for item: ", err) {
		return true
	}

	if deleted {
		processed.RemoveItem(item)

		return true
	}

	return false
}

func (p *Processor) disownProcessedItem(
	ctx context.Context,
	c client.Client,
	processed *meta.ProcessedItems,
	opts ProcessorOptions,
	item meta.ObjectReferenceStatus,
	obj *unstructured.Unstructured,
	itemErrors *int,
) {
	err := p.resourceManager().Disown(ctx, c, obj, opts.Owner)
	if err != nil {
		if apierrors.IsNotFound(err) {
			processed.RemoveItem(item)

			return
		}

		if failAndRecord(processed, itemErrors, item, "disowning failed for item: ", err) {
			return
		}
	}

	processed.RemoveItem(item)
}

func (p *Processor) applyAccumulatedItems(
	ctx context.Context,
	log logr.Logger,
	c client.Client,
	processed *meta.ProcessedItems,
	acc Accumulator,
	opts ProcessorOptions,
) int {
	itemErrors := 0
	terminatingNamespaces := map[string]bool{}

	for _, item := range acc {
		if p.applyAccumulatedItem(ctx, log, c, processed, item, opts, terminatingNamespaces) {
			itemErrors++
		}
	}

	return itemErrors
}

func (p *Processor) applyAccumulatedItem(
	ctx context.Context,
	log logr.Logger,
	c client.Client,
	processed *meta.ProcessedItems,
	item *AccumulatorItem,
	opts ProcessorOptions,
	terminatingNamespaces map[string]bool,
) bool {
	or := meta.ObjectReferenceStatus{
		ResourceID: item.Resource,
		ObjectReferenceStatusCondition: meta.ObjectReferenceStatusCondition{
			Type: meta.ReadyCondition,
		},
	}

	clusterScoped, err := p.isClusterScoped(item.Resource.GetGVK())
	if err != nil {
		or.Status = metav1.ConditionFalse
		or.Message = "resolving resource scope failed: " + err.Error()
		processed.UpdateItem(or)

		return true
	}

	or.ClusterScoped = clusterScoped

	hadError := false

	for _, obj := range *item.Objects {
		if p.applyAccumulatorObject(ctx, log, c, processed, item, obj, opts, terminatingNamespaces, &or) {
			hadError = true
		}
	}

	return hadError
}

func (p *Processor) applyAccumulatorObject(
	ctx context.Context,
	log logr.Logger,
	c client.Client,
	processed *meta.ProcessedItems,
	item *AccumulatorItem,
	obj AccumulatorObject,
	opts ProcessorOptions,
	terminatingNamespaces map[string]bool,
	or *meta.ObjectReferenceStatus,
) bool {
	fieldOwner := opts.FieldOwnerPrefix + "/" + item.Resource.FieldOwner("")

	terminating, namespace, err := p.isNamespaceTerminatingForObject(ctx, obj.Object, terminatingNamespaces)
	if err != nil {
		or.Status = metav1.ConditionFalse
		or.Message = "checking namespace termination failed for item " + obj.Origin.Origin + ": " + err.Error()

		processed.UpdateItem(*or)

		return true
	}

	if terminating {
		log.V(4).Info(
			"skipping apply because namespace is terminating",
			"item", obj.Origin.Origin,
			"namespace", namespace,
			"Kind", obj.Object.GetKind(),
			"Name", obj.Object.GetName(),
		)

		processed.RemoveItem(*or)

		return false
	}

	ver, created, err := p.Apply(
		ctx,
		c,
		obj.Object,
		fieldOwner,
		opts.Force,
		opts.Adopt,
		opts.Owner,
		processed.GetItem(item.Resource),
	)

	or.Created = created

	if err != nil {
		or.Status = metav1.ConditionFalse
		or.Message = "apply failed for item " + obj.Origin.Origin + ": " + err.Error()

		log.V(4).Info("failed to apply item", "item", obj.Origin.Origin)
	} else {
		if ver != nil {
			or.LastApply = *ver
		}

		or.Status = metav1.ConditionTrue

		log.V(4).Info("successfully applied item", "item", obj.Origin.Origin, "version", ver)
	}

	processed.UpdateItem(*or)

	return err != nil
}

func (p *Processor) objectForProcessedItem(item meta.ObjectReferenceStatus) (*unstructured.Unstructured, error) {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(item.GetGVK())
	obj.SetName(item.GetName())

	clusterScoped := item.ClusterScoped
	if !clusterScoped {
		var err error

		clusterScoped, err = p.isClusterScoped(item.GetGVK())
		if err != nil {
			return nil, err
		}
	}

	ns := item.GetNamespace()
	if ns != "" && !clusterScoped {
		obj.SetNamespace(ns)
	}

	return obj, nil
}

func failAndRecord(
	processed *meta.ProcessedItems,
	itemErrors *int,
	item meta.ObjectReferenceStatus,
	msg string,
	err error,
) bool {
	if err == nil {
		return false
	}

	(*itemErrors)++
	item.Status = metav1.ConditionFalse
	item.Message = msg + err.Error()
	processed.UpdateItem(item)

	return true
}

// Prune relinquishes the fields owned by fieldOwner and deletes resources
// which were created exclusively for this processor item.
func (p *Processor) Prune(
	ctx context.Context,
	c client.Client,
	obj *unstructured.Unstructured,
	fieldOwner string,
	current *meta.ObjectReferenceStatus,
) (deleted bool, err error) {
	created := current != nil && current.Created

	return p.resourceManager().Prune(ctx, c, obj, ssa.PruneOptions{
		FieldOwner:        fieldOwner,
		PreviouslyCreated: created,
	})
}

func (p *Processor) resourceManager() ssa.Manager {
	return ssa.Manager{
		Reader: p.GatherClient,
		Mapper: p.Mapper,
		Metadata: ssa.Metadata{
			CreatedByValue:     meta.ValueControllerReplications,
			ManagedByValue:     meta.ValueControllerReplications,
			LegacyCreatedLabel: meta.ResourcesLabel,
		},
	}
}

func (r *Processor) isClusterScoped(gvk schema.GroupVersionKind) (bool, error) {
	mapping, err := r.Mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return false, err
	}

	return mapping.Scope.Name() == k8smeta.RESTScopeNameRoot, nil
}

func (r *Processor) Apply(
	ctx context.Context,
	c client.Client,
	obj *unstructured.Unstructured,
	fieldOwner string,
	force bool,
	adopt bool,
	ownerreference *metav1.OwnerReference,
	current *meta.ObjectReferenceStatus,
) (lastApply *metav1.Time, created bool, err error) {
	previouslyCreated := current != nil && current.Created
	result, err := r.resourceManager().Apply(ctx, c, obj, ssa.ApplyOptions{
		FieldOwner:        fieldOwner,
		Force:             force,
		Adopt:             adopt,
		OwnerReference:    ownerreference,
		PreviouslyCreated: previouslyCreated,
	})

	return result.LastApply, result.Created, err
}

func (r *Processor) isNamespaceTerminatingForObject(
	ctx context.Context,
	obj *unstructured.Unstructured,
	cache map[string]bool,
) (terminating bool, namespace string, err error) {
	// The Namespace object itself is cluster-scoped, but if Capsule is applying
	// a Namespace which is already terminating, we should skip it as well.
	if obj.GroupVersionKind().Group == "" && obj.GetKind() == "Namespace" {
		namespace = obj.GetName()

		ns := &corev1.Namespace{}
		if err := r.GatherClient.Get(ctx, types.NamespacedName{Name: namespace}, ns); err != nil {
			if apierrors.IsNotFound(err) {
				cache[namespace] = false

				return false, namespace, nil
			}

			return false, namespace, err
		}

		terminating = ns.DeletionTimestamp != nil || ns.Status.Phase == corev1.NamespaceTerminating

		cache[namespace] = terminating

		return terminating, namespace, nil
	}

	mapping, err := r.Mapper.RESTMapping(
		obj.GroupVersionKind().GroupKind(),
		obj.GroupVersionKind().Version,
	)
	if err != nil {
		return false, "", err
	}

	if mapping.Scope.Name() != k8smeta.RESTScopeNameNamespace {
		return false, "", nil
	}

	namespace = obj.GetNamespace()
	if namespace == "" {
		return false, "", nil
	}

	return r.isNamespaceTerminating(ctx, namespace, cache)
}

func (r *Processor) isNamespaceTerminating(
	ctx context.Context,
	namespace string,
	cache map[string]bool,
) (bool, string, error) {
	if namespace == "" {
		return false, namespace, nil
	}

	if terminating, ok := cache[namespace]; ok {
		return terminating, namespace, nil
	}

	ns := &corev1.Namespace{}
	if err := r.GatherClient.Get(ctx, types.NamespacedName{Name: namespace}, ns); err != nil {
		if apierrors.IsNotFound(err) {
			cache[namespace] = true

			return true, namespace, nil
		}

		return false, namespace, err
	}

	terminating := ns.DeletionTimestamp != nil || ns.Status.Phase == corev1.NamespaceTerminating
	cache[namespace] = terminating

	return terminating, namespace, nil
}
