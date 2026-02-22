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
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	clt "github.com/projectcapsule/capsule/pkg/runtime/client"
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

	failAndContinue := func(i capsulev1beta2.ObjectReferenceStatus, msg string, err error) bool { // replace ItemType
		if err == nil {
			return false
		}

		itemErrors++
		i.Status = metav1.ConditionFalse
		i.Message = msg + err.Error()
		processed.UpdateItem(i)

		return true
	}

	// Prune first, to work on a consistent Status
	var reconErr error

	for _, i := range *processed {
		if _, exists := acc[i.GetKey("")]; !exists {
			obj := &unstructured.Unstructured{}
			obj.SetGroupVersionKind(i.GetGVK())
			obj.SetNamespace(i.GetNamespace())
			obj.SetName(i.GetName())

			if opts.Prune {
				if !i.LastApply.IsZero() {
					log.V(4).Info("pruning resources", "Kind", i.Kind, "Name", i.Name, "Namespace", i.Namespace)

					fieldOwner := opts.FieldOwnerPrefix + "/" + i.FieldOwner("")

					_, reconErr = p.Prune(ctx, c, obj, fieldOwner)
					if failAndContinue(i, "pruning failed for item: ", reconErr) {
						continue
					}
				}
			}

			// Disown item (only when GET succeeded)
			patches, err := p.handleRemoveManagedMetadata(ctx, c, obj, opts.Owner)
			if err != nil {
				if apierrors.IsNotFound(err) {
					processed.RemoveItem(i)

					continue
				}

				if failAndContinue(i, "disowning failed for item: ", err) {
					continue
				}
			}

			if len(patches) > 0 {
				err = clt.ApplyPatches(ctx, c, obj, patches, meta.ResourceControllerFieldOwnerPrefix())
				if err != nil {
					if apierrors.IsNotFound(err) {
						processed.RemoveItem(i)

						continue
					}

					if failAndContinue(i, "removing metdata failed for item: ", err) {
						continue
					}
				}
			}

			processed.RemoveItem(i)
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

			or.Created = created

			if err != nil {
				hadError = true
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
) (deleted bool, err error) {
	actual := &unstructured.Unstructured{}
	actual.SetGroupVersionKind(obj.GroupVersionKind())
	actual.SetNamespace(obj.GetNamespace())
	actual.SetName(obj.GetName())

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
	)
	if err != nil {
		return deletable, err
	}

	if deletable {
		return deletable, nil
	}

	err = clt.PatchApply(ctx, c, obj, fieldOwner, false)
	if apierrors.IsNotFound(err) {
		return true, nil
	}

	return false, err
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

	deletable = meta.HasExactlyCapsuleOwners(actual, meta.FieldManagerCapsulePrefix+"/resource/",
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

// Remove metadata from the controller when an object
// is not pruned
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
	if v, ok := existingObject.GetLabels()[meta.NewManagedByCapsuleLabel]; !ok || v != meta.ValueControllerResources {
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
) (lastApply *metav1.Time, created bool, err error) {
	log := log.FromContext(ctx)

	actual := &unstructured.Unstructured{}
	actual.SetGroupVersionKind(obj.GroupVersionKind())
	actual.SetNamespace(obj.GetNamespace())
	actual.SetName(obj.GetName())
	key := client.ObjectKeyFromObject(actual)

	// We need to mark an item if we create it with our patch to make proper Garbage Collection
	// If it does not yet exist mark it
	patches, created, err := r.handleCreatedMetadata(ctx, c, obj, ownerreference, adopt)
	if err != nil {
		return nil, created, fmt.Errorf("evaluating managed metadata: %w", err)
	}

	if err := clt.PatchApply(ctx, c, obj, fieldOwner, force); err != nil {
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
		labels := existingObject.GetLabels()

		if v, ok := labels[meta.CreatedByCapsuleLabel]; ok && v == meta.ValueControllerResources {
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

		if v, ok := existingObject.GetLabels()[meta.CreatedByCapsuleLabel]; !ok || v != meta.ValueControllerResources {
			patches = append(patches, clt.AddLabelsPatch(existingObject.GetLabels(), map[string]string{
				meta.CreatedByCapsuleLabel: meta.ValueControllerResources,
			})...)

			// Ensure There are labels otherwise the next patch overwrites labels struct
			if existingObject.GetLabels() == nil {
				existingObject.SetLabels(map[string]string{
					meta.CreatedByCapsuleLabel: meta.ValueControllerResources,
				})
			}
		}
	}

	if created || allowAdoption {
		if v, ok := existingObject.GetLabels()[meta.NewManagedByCapsuleLabel]; !ok || v != meta.ValueControllerResources {
			patches = append(patches, clt.AddLabelsPatch(existingObject.GetLabels(), map[string]string{
				meta.NewManagedByCapsuleLabel: meta.ValueControllerResources,
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
