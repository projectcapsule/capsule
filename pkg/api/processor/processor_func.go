// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package processor

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	clt "github.com/projectcapsule/capsule/pkg/client"
)

func (p *Processor) Reconcile(
	ctx context.Context,
	log logr.Logger,
	c client.Client,
	processed *capsulev1beta2.ProcessedItems,
	acc Accumulator,
	opts ProcessorOptions,
) (err error) {
	var itemErrors = 0

	log.V(5).Info("starting pruning items", "present", len(*processed))

	// Prune first, to work on a consistent Status
	for _, i := range *processed {
		if _, exists := acc[i.GetKey("")]; !exists {
			if opts.Prune {
				if !i.LastApply.IsZero() {
					log.V(4).Info("pruning resources", "Kind", i.Kind, "Name", i.Name, "Namespace", i.Namespace)

					obj := &unstructured.Unstructured{}
					obj.SetGroupVersionKind(i.GetGVK())
					obj.SetNamespace(i.GetNamespace())
					obj.SetName(i.GetName())

					fieldOwner := opts.FieldOwnerPrefix + "/" + i.FieldOwner("")

					err := p.Prune(ctx, c, obj, fieldOwner)
					if err != nil {
						itemErrors++

						i.Status = metav1.ConditionFalse
						i.Message = "pruning failed for item: " + err.Error()
						processed.UpdateItem(i)

						continue
					}

				}

				processed.RemoveItem(i)
			}
		}
	}

	if itemErrors > 0 {
		return fmt.Errorf("pruning of %d resources failed", itemErrors)
	}

	log.V(5).Info("accumulation after pruning", "items", len(acc))

	for _, item := range acc {
		or := capsulev1beta2.ObjectReferenceStatus{
			ResourceID: item.Resource,
			ObjectReferenceStatusCondition: capsulev1beta2.ObjectReferenceStatusCondition{
				Type: meta.ReadyCondition,
			},
		}

		hadError := false

		for _, obj := range *item.Objects {
			fieldOwner := opts.FieldOwnerPrefix + "/" + item.Resource.FieldOwner("")

			ver, created, err := p.Apply(
				ctx,
				c,
				obj.Object,
				fieldOwner,
				opts.Force,
				opts.Adopt,
				opts.Owner,
			)

			if err != nil {
				hadError = true
				or.Status = metav1.ConditionFalse
				or.Message = "apply failed for item '" + obj.Origin.Origin + "': " + err.Error()
				or.Created = created

				log.V(4).Info("failed to apply item", "item", obj.Origin.Origin)
			} else {
				if ver != nil {
					or.LastApply = *ver
				}

				or.Status = metav1.ConditionTrue

				log.V(4).Info("successfully applied item", "item", obj.Origin.Origin, "version", ver)
			}

			processed.UpdateItem(or)
		}

		if hadError {
			itemErrors++
		}
	}

	if itemErrors > 0 {
		return fmt.Errorf("applying of %d resources failed", itemErrors)
	}

	log.V(4).Info("processing completed")

	return nil
}

// Prune by reverting the patch by the given fieldOwner
// If the item was created by the controller and has no more field-managers we are going to delete
func (r *Processor) Prune(
	ctx context.Context,
	c client.Client,
	obj *unstructured.Unstructured,
	fieldOwner string,
) (err error) {

	target := &unstructured.Unstructured{}
	target.SetGroupVersionKind(obj.GroupVersionKind())
	target.SetNamespace(obj.GetNamespace())
	target.SetName(obj.GetName())

	actual := &unstructured.Unstructured{}
	actual.SetGroupVersionKind(obj.GroupVersionKind())
	actual.SetNamespace(obj.GetNamespace())
	actual.SetName(obj.GetName())

	err = c.Get(ctx, client.ObjectKeyFromObject(actual), actual)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}

		return err
	}

	deletable, err := r.handlePruneDeletion(
		ctx,
		c,
		actual,
		fieldOwner,
	)
	if err != nil {
		return err
	}

	if deletable {
		return nil
	}

	return clt.CreateOrPatch(
		ctx,
		c,
		obj,
		fieldOwner,
		false,
	)
}

// Completely prune the resource when there's no more managers and the resource was created by the controller
func (r *Processor) handlePruneDeletion(
	ctx context.Context,
	c client.Client,
	actual *unstructured.Unstructured,
	fieldOwner string,
) (deletable bool, err error) {
	labels := actual.GetLabels()
	if _, ok := labels[meta.CreatedByCapsuleLabel]; !ok {
		return false, nil
	}

	deletable = meta.HasExactlyCapsuleOwners(actual, meta.CapsuleFieldOwnerPrefix+"/resource/",
		[]string{
			fieldOwner,
			meta.ResourceControllerFieldOwnerPrefix(),
		})

	if !deletable {
		return
	}

	err = c.Delete(ctx, actual)
	if apierrors.IsNotFound(err) {
		return deletable, nil
	}

	return deletable, err
}

func (r *Processor) Apply(
	ctx context.Context,
	c client.Client,
	obj *unstructured.Unstructured,
	fieldOwner string,
	force bool,
	adopt bool,
	ownerreference *metav1.OwnerReference,
) (lastApply *metav1.Time, created bool, err error) {
	log := log.FromContext(ctx)

	obj.GetOwnerReferences()

	actual := &unstructured.Unstructured{}
	actual.SetGroupVersionKind(obj.GroupVersionKind())
	actual.SetNamespace(obj.GetNamespace())
	actual.SetName(obj.GetName())

	// We need to mark an item if we create it with our patch to make proper Garbage Collection
	// If it does not yet exist mark it
	patches, created, err := r.handleControllerMetadata(ctx, c, obj, ownerreference)
	if err != nil {
		return nil, created, fmt.Errorf("resource adoption failed", err)
	}

	if !created {
		return nil, created, fmt.Errorf("resource exists and can not be adopted")
	}

	if err := clt.CreateOrPatch(ctx, c, obj, fieldOwner, force); err != nil {
		return nil, created, fmt.Errorf("applying object failed: %w", err)
	}

	// Apply metadata patches if needed
	log.V(4).Info("applying patches", "items", len(patches))
	if len(patches) > 0 {
		if err := clt.ApplyPatches(ctx, c, actual, patches, meta.ResourceControllerFieldOwnerPrefix()); err != nil {
			return nil, created, err
		}
	}

	// Fetch live object to get the updated generation
	if err := c.Get(ctx, client.ObjectKeyFromObject(actual), actual); err != nil {
		return nil, created, fmt.Errorf("failed to get object after apply: %w", err)
	}

	return clt.LastApplyTimeForManager(actual, fieldOwner), created, nil
}

func (r *Processor) handleControllerMetadata(
	ctx context.Context,
	c client.Client,
	obj *unstructured.Unstructured,
	ownerreference *metav1.OwnerReference,
) (patches []clt.JSONPatch, adoptable bool, err error) {
	adoptable = false

	existingObject := obj.DeepCopy()

	err = c.Get(ctx, client.ObjectKeyFromObject(existingObject), existingObject)
	switch {
	case apierrors.IsNotFound(err):
		patches = append(patches, clt.AddLabelsPatch(existingObject.GetLabels(), map[string]string{
			meta.CreatedByCapsuleLabel: "controller",
		})...)

		if ownerreference != nil {
			patches = append(patches, clt.AddOwnerReferencePatch(existingObject.GetOwnerReferences(), ownerreference)...)
		}

		return patches, true, nil
	case err != nil:
		return patches, false, err
	default:
		labels := existingObject.GetLabels()

		if v, ok := labels[meta.CreatedByCapsuleLabel]; ok || v == "controller" {
			adoptable = true
		}

		if ownerreference != nil {
			patches = append(patches, clt.AddOwnerReferencePatch(existingObject.GetOwnerReferences(), ownerreference)...)
		}

		if _, ok := labels[meta.ResourceCapsuleLabel]; ok {
			adoptable = true

			patches = append(patches, clt.PatchRemoveLabels(existingObject.GetLabels(), []string{
				meta.ResourceCapsuleLabel,
			})...,
			)

			if v, ok := labels[meta.CreatedByCapsuleLabel]; !ok || v != "controller" {
				patches = append(patches, clt.AddLabelsPatch(existingObject.GetLabels(), map[string]string{
					meta.CreatedByCapsuleLabel: "controller",
				})...)
			}
		}
	}

	return patches, adoptable, err
}
