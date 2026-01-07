// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/selection"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	clt "github.com/projectcapsule/capsule/pkg/client"
	"github.com/projectcapsule/capsule/pkg/configuration"
	tpl "github.com/projectcapsule/capsule/pkg/template"
	"github.com/projectcapsule/capsule/pkg/utils"
)

const (
	finalizer = "capsule.clastix.io/resources"
)

type Processor struct {
	client                       client.Client
	configuration                configuration.Configuration
	factory                      serializer.CodecFactory
	allowCrossNamespaceSelection bool
}

//func (r *Processor) HandlePruning(
//	ctx context.Context,
//	c client.Client,
//	current,
//	desired sets.Set[string],
//) (failedProcess []string, err error) {
//	log := ctrllog.FromContext(ctx)
//
//	diff := current.Difference(desired)
//	// We don't want to trigger a reconciliation of the Status every time,
//	// rather, only in case of a difference between the processed and the actual status.
//	// This can happen upon the first reconciliation, or a removal, or a change, of a resource.
//	reconcile := diff.Len() > 0 || current.Len() != desired.Len()
//
//	if !reconcile {
//		return
//	}
//
//	processed := sets.NewString()
//
//	log.Info("starting processing pruning", "length", diff.Len())
//
//	// The outer resources must be removed, iterating over these to clean-up
//	for item := range diff {
//		or := capsulev1beta2.ObjectReferenceStatus{}
//		if sectionErr := or.ParseFromString(item); sectionErr != nil {
//			processed.Insert(or.String())
//
//			log.Error(sectionErr, "unable to parse resource to prune", "resource", item)
//
//			continue
//		}
//
//		obj := unstructured.Unstructured{}
//		obj.SetNamespace(or.Namespace)
//		obj.SetName(or.Name)
//		obj.SetGroupVersionKind(schema.FromAPIVersionAndKind(or.APIVersion, or.Kind))
//
//		log.V(5).Info("pruning", "resource", obj.GroupVersionKind(), "name", obj.GetName(), "namespace", obj.GetNamespace())
//
//		if sectionErr := c.Delete(ctx, &obj); err != sectionErr {
//			if apierr.IsNotFound(sectionErr) {
//				// Object may have been already deleted, we can ignore this error
//				continue
//			}
//
//			or.Status = metav1.ConditionFalse
//			or.Message = sectionErr.Error()
//			or.Type = meta.ReadyCondition
//			processed.Insert(or.String())
//
//			err = errors.Join(sectionErr)
//
//			continue
//		}
//
//		log.V(5).Info("resource has been pruned", "resource", item)
//	}
//
//	return processed.List(), nil
//}

//nolint:gocognit
func (r *Processor) foreachTenantNamespace(
	ctx context.Context,
	log logr.Logger,
	c client.Client,
	tnt capsulev1beta2.Tenant,
	resource capsulev1beta2.ResourceSpec,
	resourceIndex string,
	tmplContext tpl.ReferenceContext,
	acc Accumulator,
) (err error) {
	// Creating Namespace selector
	var selector labels.Selector

	if resource.NamespaceSelector != nil {
		selector, err = metav1.LabelSelectorAsSelector(resource.NamespaceSelector)
		if err != nil {
			log.Error(err, "cannot create Namespace selector for Namespace filtering and resource replication")

			return err
		}
	} else {
		selector = labels.NewSelector()
	}
	// Resources can be replicated only on Namespaces belonging to the same Global:
	// preventing a boundary cross by enforcing the selection.
	tntRequirement, err := labels.NewRequirement(meta.TenantLabel, selection.Equals, []string{tnt.GetName()})
	if err != nil {
		log.Error(err, "unable to create requirement for Namespace filtering and resource replication")

		return err
	}

	selector = selector.Add(*tntRequirement)
	// Selecting the targeted Namespace according to the TenantResource specification.
	namespaces := corev1.NamespaceList{}
	if err = r.client.List(ctx, &namespaces, client.MatchingLabelsSelector{Selector: selector}); err != nil {
		log.Error(err, "cannot retrieve Namespaces for resource")

		return err
	}

	log.V(5).Info("retrieved namespaces", "size", len(namespaces.Items))

	for _, ns := range namespaces.Items {

		//spec.Context.GatherContext(ctx, c, nil, ns.GetName())
		err = r.handleResources(
			ctx,
			c,
			tnt,
			resourceIndex,
			resource,
			&ns,
			tmplContext,
			acc,
		)
		if err != nil {
			return
		}
	}

	return
}

//func (r *Processor) reconcile(
//	ctx context.Context,
//	c client.Client,
//	resources []capsulev1beta2.ResourceSpec,
//	tnt capsulev1beta2.Tenant,
//	allowCrossNamespaceSelection bool,
//	fieldOwner string,
//	owner capsulev1beta2.ObjectReferenceStatusOwner,
//	ns *corev1.Namespace,
//	tmplContext tpl.ReferenceContext,
//	acc Accumulator,
//) error {
//	log := ctrllog.FromContext(ctx)
//
//	for resourceIndex, resource := range resources {
//		// Collect Resources to apply
//		err := r.handleResources(
//			ctx,
//			c,
//			codecFactory,
//			tnt,
//			allowCrossNamespaceSelection,
//			strconv.Itoa(resourceIndex),
//			resource,
//			owner,
//			ns,
//			tmplContext,
//			acc,
//		)
//
//		log.Error(err, "sadd me")
//	}
//
//	log.Info("ACCUMULATION", "acc", acc)
//
//	return nil, nil
//
//	// Prune First
//
//	// Collect Resources to apply
//	//objects, err := r.handleResources(
//	//	ctx,
//	//	c,
//	//	tnt,
//	//	allowCrossNamespaceSelection,
//	//	tenantLabel,
//	//	resourceIndex,
//	//	resource,
//	//	owner,
//	//	ns,
//	//	tmplContext,
//	//)
//	//if err != nil {
//	//	log.Error(err, "some error happend", "here", "here")
//	//	return nil, err
//	//}
//	//
//	//var syncErr error
//	//
//	//processed := sets.NewString()
//	//
//	//log.V(4).Info("processing items", "items", len(objects))
//	//
//	//// Apply objects and return processed
//	//for i, obj := range objects {
//	//	replicatedItem := &capsulev1beta2.ObjectReferenceStatus{}
//	//	replicatedItem.Name = obj.GetName()
//	//	replicatedItem.Kind = obj.GetKind()
//	//	replicatedItem.APIVersion = obj.GetAPIVersion()
//	//	replicatedItem.Owner = owner
//	//	replicatedItem.Type = meta.ReadyCondition
//	//
//	//	if ns != nil {
//	//		replicatedItem.Namespace = ns.GetName()
//	//	}
//	//
//	//	fieldOwnerw := fieldOwner + "/" + tnt.Name + "/" + strconv.Itoa(i)
//	//
//	//	if err := r.createOrPatch(ctx, c, obj, resource, fieldOwnerw); err != nil {
//	//		replicatedItem.Status = metav1.ConditionFalse
//	//		replicatedItem.Message = err.Error()
//	//	} else {
//	//		replicatedItem.Status = metav1.ConditionTrue
//	//	}
//	//
//	//	processed.Insert(replicatedItem.String())
//	//}
//	//
//	//// Run Garbage Collection
//	//
//	//return processed.List(), syncErr
//}

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

	return utils.CreateOrPatch(
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

	deletable = meta.HasExactlyCapsuleOwners(actual, []string{
		fieldOwner,
		meta.ControllerFieldOwner(),
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
) (err error) {
	log := log.FromContext(ctx)

	actual := &unstructured.Unstructured{}
	actual.SetGroupVersionKind(obj.GroupVersionKind())
	actual.SetNamespace(obj.GetNamespace())
	actual.SetName(obj.GetName())

	// We need to mark an item if we create it with our patch to make proper Garbage Collection
	// If it does not yet exist mark it
	patches, adoptable, err := r.handleControllerMetadata(ctx, c, obj)
	if err != nil {
		return fmt.Errorf("resource adoption failed", err)
	}

	if !adoptable {
		return fmt.Errorf("resource exists and can not be adopted")
	}

	err = utils.CreateOrPatch(
		ctx,
		c,
		obj,
		fieldOwner,
		force,
	)
	if err != nil {
		return fmt.Errorf("applying object failed: %w", err)
	}

	log.V(4).Info("applying patches", "items", len(patches))

	if len(patches) == 0 {
		return
	}

	return clt.ApplyPatches(ctx, c, obj, patches, meta.ControllerFieldOwner())
}

func (r *Processor) handleControllerMetadata(
	ctx context.Context,
	c client.Client,
	obj *unstructured.Unstructured,
) (patches []clt.JSONPatch, adoptable bool, err error) {
	adoptable = false

	existingObject := obj.DeepCopy()

	err = c.Get(ctx, client.ObjectKeyFromObject(existingObject), existingObject)
	switch {
	case apierrors.IsNotFound(err):
		patches = append(patches, clt.AddLabelsPatch(existingObject, map[string]string{
			meta.CreatedByCapsuleLabel: "controller",
		})...)

		return patches, true, nil
	case err != nil:
		return patches, false, err
	default:
		labels := existingObject.GetLabels()

		if v, ok := labels[meta.CreatedByCapsuleLabel]; ok || v == "controller" {
			adoptable = true
		}

		if _, ok := labels[meta.ResourceCapsuleLabel]; ok {
			adoptable = true

			patches = append(patches, clt.PatchRemoveLabels(existingObject, []string{
				meta.ResourceCapsuleLabel,
			})...,
			)

			if v, ok := labels[meta.CreatedByCapsuleLabel]; !ok || v != "controller" {
				patches = append(patches, clt.AddLabelsPatch(existingObject, map[string]string{
					meta.CreatedByCapsuleLabel: "controller",
				})...)
			}
		}
	}

	return patches, adoptable, err
}
