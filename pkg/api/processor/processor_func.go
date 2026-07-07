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
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/projectcapsule/capsule/pkg/api/meta"
	clt "github.com/projectcapsule/capsule/pkg/runtime/client"
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
	patches, err := p.handleRemoveManagedMetadata(ctx, c, obj, opts.Owner)
	if err != nil {
		if apierrors.IsNotFound(err) {
			processed.RemoveItem(item)

			return
		}

		if failAndRecord(processed, itemErrors, item, "disowning failed for item: ", err) {
			return
		}
	}

	//nolint:nestif
	if len(patches) > 0 {
		err = clt.ApplyPatches(ctx, c, obj, patches, meta.ResourceControllerFieldOwnerPrefix())
		if err != nil {
			if apierrors.IsNotFound(err) {
				processed.RemoveItem(item)

				return
			}

			if failAndRecord(processed, itemErrors, item, "removing metdata failed for item: ", err) {
				return
			}
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

// Prune by reverting the patch by the given fieldOwner
// If the item was created by the controller and has no more field-managers we are going to delete.
func (r *Processor) Prune(
	ctx context.Context,
	c client.Client,
	obj *unstructured.Unstructured,
	fieldOwner string,
	current *meta.ObjectReferenceStatus,
) (deleted bool, err error) {
	actual := &unstructured.Unstructured{}
	actual.SetGroupVersionKind(obj.GroupVersionKind())
	actual.SetName(obj.GetName())

	mapping, err := r.Mapper.RESTMapping(obj.GroupVersionKind().GroupKind(), obj.GroupVersionKind().Version)
	if err != nil {
		return false, err
	}

	// Handles the case where the namespace was already deleted
	if mapping.Scope.Name() == k8smeta.RESTScopeNameNamespace {
		namespace := obj.GetNamespace()
		actual.SetNamespace(namespace)

		ns := &corev1.Namespace{}
		if err := r.GatherClient.Get(ctx, types.NamespacedName{Name: namespace}, ns); err != nil {
			if apierrors.IsNotFound(err) {
				return true, nil
			}

			return false, err
		}
	}

	err = c.Get(ctx, client.ObjectKeyFromObject(actual), actual)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return true, nil
		}

		return false, err
	}

	deletable, err := r.handlePruneDeletion(
		ctx,
		c,
		actual,
		fieldOwner,
		current,
	)
	if err != nil {
		return deletable, err
	}

	if deletable {
		err = c.Delete(ctx, actual)
		if apierrors.IsNotFound(err) {
			return deletable, nil
		}

		return deletable, err
	}

	err = clt.PatchApply(ctx, c, obj, fieldOwner, false)
	if apierrors.IsNotFound(err) {
		return true, nil
	}

	return false, err
}

func (r *Processor) isClusterScoped(gvk schema.GroupVersionKind) (bool, error) {
	mapping, err := r.Mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return false, err
	}

	return mapping.Scope.Name() == k8smeta.RESTScopeNameRoot, nil
}

// Completely prune the resource when there's no more managers and the resource was created by the controller.
func (r *Processor) handlePruneDeletion(
	ctx context.Context,
	c client.Client,
	actual *unstructured.Unstructured,
	fieldOwner string,
	current *meta.ObjectReferenceStatus,
) (bool, error) {
	if current != nil && current.Created {
		return true, nil
	}

	labels := actual.GetLabels()
	if _, ok := labels[meta.CreatedByCapsuleLabel]; !ok {
		return false, nil
	}

	return meta.HasExactlyCapsuleOwners(actual, meta.FieldManagerCapsulePrefix+"/resource/", []string{
		fieldOwner,
		meta.ResourceControllerFieldOwnerPrefix(),
	}), nil
}

// Remove metadata from the controller when an object
// is not pruned.
func (r *Processor) handleRemoveManagedMetadata(
	ctx context.Context,
	c client.Client,
	obj *unstructured.Unstructured,
	ownerreference *metav1.OwnerReference,
) (patches []clt.JSONPatch, err error) {
	existingObject := obj.DeepCopy()

	err = c.Get(ctx, client.ObjectKeyFromObject(existingObject), existingObject)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}

		return nil, err
	}

	// Remove Ownerreference if given
	if ownerreference != nil {
		patches = append(patches, clt.RemoveOwnerReferencePatch(existingObject.GetOwnerReferences(), ownerreference)...)
	}

	// Remove Managed Labels
	if v, ok := existingObject.GetLabels()[meta.NewManagedByCapsuleLabel]; !ok || v != meta.ValueControllerReplications {
		return patches, nil
	}

	patches = append(patches, clt.PatchRemoveLabels(existingObject.GetLabels(), []string{
		meta.NewManagedByCapsuleLabel,
	})...)

	return patches, nil
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
	log := log.FromContext(ctx)

	actual := &unstructured.Unstructured{}
	actual.SetGroupVersionKind(obj.GroupVersionKind())
	actual.SetName(obj.GetName())

	ns := obj.GetNamespace()
	if ns != "" {
		actual.SetNamespace(ns)
	}

	key := client.ObjectKeyFromObject(actual)

	// We need to mark an item if we create it with our patch to make proper Garbage Collection
	// If it does not yet exist mark it
	patches, created, err := r.handleCreatedMetadata(ctx, c, obj, ownerreference, adopt, current)
	if err != nil {
		return nil, created, fmt.Errorf("evaluating managed metadata: %w", err)
	}

	err = retry.OnError(
		retry.DefaultBackoff,
		apierrors.IsConflict,
		func() error {
			return clt.PatchApply(ctx, c, obj, fieldOwner, force)
		},
	)
	if err != nil {
		return nil, created, fmt.Errorf("applying object failed: %w", err)
	}

	err = retry.OnError(
		retry.DefaultBackoff,
		apierrors.IsNotFound,
		func() error {
			return c.Get(ctx, key, actual)
		},
	)
	if err != nil {
		return nil, created, fmt.Errorf("failed to get object after apply: %w", err)
	}

	// Apply metadata patches if needed
	log.V(4).Info("applying patches", "items", len(patches))

	if len(patches) > 0 {
		if err := clt.ApplyPatches(ctx, c, actual, patches, meta.ResourceControllerFieldOwnerPrefix()); err != nil {
			return nil, created, err
		}
	}

	return clt.LastApplyTimeForManager(actual, fieldOwner), created, nil
}

func (r *Processor) handleCreatedMetadata(
	ctx context.Context,
	c client.Client,
	obj *unstructured.Unstructured,
	ownerreference *metav1.OwnerReference,
	allowAdoption bool,
	current *meta.ObjectReferenceStatus,
) (patches []clt.JSONPatch, created bool, err error) {
	created = false

	existingObject := obj.DeepCopy()

	err = c.Get(ctx, client.ObjectKeyFromObject(existingObject), existingObject)

	switch {
	case apierrors.IsNotFound(err):
		created = true
		err = nil
	case err != nil:
		return nil, created, err
	default:
		if current != nil {
			if current.Created {
				created = true
			}
		}

		labels := existingObject.GetLabels()

		if v, ok := labels[meta.CreatedByCapsuleLabel]; ok && v == meta.ValueControllerReplications {
			created = true
		}

		if _, ok := labels[meta.ResourcesLabel]; ok {
			created = true

			patches = append(patches, clt.PatchRemoveLabels(existingObject.GetLabels(), []string{
				meta.ResourcesLabel,
			})...,
			)
		}
	}

	if created {
		if ownerreference != nil {
			patches = append(patches, clt.AddOwnerReferencePatch(existingObject.GetOwnerReferences(), ownerreference)...)
		}

		if v, ok := existingObject.GetLabels()[meta.CreatedByCapsuleLabel]; !ok || v != meta.ValueControllerReplications {
			patches = append(patches, clt.AddLabelsPatch(existingObject.GetLabels(), map[string]string{
				meta.CreatedByCapsuleLabel: meta.ValueControllerReplications,
			})...)

			// Ensure There are labels otherwise the next patch overwrites labels struct
			if existingObject.GetLabels() == nil {
				existingObject.SetLabels(map[string]string{
					meta.CreatedByCapsuleLabel: meta.ValueControllerReplications,
				})
			}
		}
	}

	if created || allowAdoption {
		if v, ok := existingObject.GetLabels()[meta.NewManagedByCapsuleLabel]; !ok || v != meta.ValueControllerReplications {
			patches = append(patches, clt.AddLabelsPatch(existingObject.GetLabels(), map[string]string{
				meta.NewManagedByCapsuleLabel: meta.ValueControllerReplications,
			})...)
		}

		return patches, created, err
	}

	return nil, created, fmt.Errorf(
		"object %s/%s %s/%s exists and cannot be adopted",
		existingObject.GetAPIVersion(),
		existingObject.GetKind(),
		existingObject.GetNamespace(),
		existingObject.GetName(),
	)
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
