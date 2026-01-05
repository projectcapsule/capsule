// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	ssa "github.com/fluxcd/pkg/ssa"
	"github.com/go-logr/logr"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
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

	if err = utils.CreateOrPatch(
		ctx,
		c,
		obj,
		fieldOwner,
		false,
	); err != nil {
		return
	}

	return r.handlePruneDeletion(
		ctx,
		c,
		obj,
	)
}

// Completely prune the resource when there's no more managers and the resource was created by the controller
func (r *Processor) handlePruneDeletion(
	ctx context.Context,
	c client.Client,
	obj *unstructured.Unstructured,
) (err error) {
	if len(obj.GetManagedFields()) > 0 {
		return
	}

	labels := obj.GetLabels()
	if _, ok := labels[meta.CreatedByCapsuleLabel]; !ok {
		return
	}

	return c.Delete(ctx, obj)
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
	_, _, err = r.isAdoptable(ctx, c, obj)
	if err != nil {
		return fmt.Errorf("resource adoption failed", err)
	}

	//if !present {
	//	return nil
	//}
	//
	//if !adopt && !adoptable {
	//	return fmt.Errorf("resource exists and can not be adopted")
	//}

	b, err := obj.MarshalJSON()
	if err != nil {
		return fmt.Errorf("marshal obj: %w", err)
	}
	log.Info("OBJ_JSON", "obj", string(b))

	return utils.CreateOrPatch(
		ctx,
		c,
		obj,
		fieldOwner,
		force,
	)

	// Handle Adoption
}

func (r *Processor) isAdoptable(
	ctx context.Context,
	c client.Client,
	obj *unstructured.Unstructured,
) (present bool, adoptable bool, err error) {
	adoptable = false
	present = true

	existingObject := obj.DeepCopy()

	err = c.Get(ctx, client.ObjectKeyFromObject(existingObject), existingObject)
	switch {
	case apierrors.IsNotFound(err):
		present = false
	case err != nil:
		return
	default:
		labels := existingObject.GetLabels()

		if _, ok := labels[meta.ResourceCapsuleLabel]; ok {
			adoptable = true

			return
		}
	}

	if !adoptable {
		return
	}

	return

	//patch := []map[string]any{
	//	{
	//		"op":    "add",
	//		"path":  "/metadata/labels/capsule.clastix.io~1created-by",
	//		"value": "controller",
	//	},
	//}
	//
	//rawPatch, err := json.Marshal(patch)
	//if err != nil {
	//	return false, false, err
	//}
	//
	//return false, true, c.Patch(ctx, existingObject, client.RawPatch(types.JSONPatchType, rawPatch))
}

func (r *Processor) handlePatching(
	ctx context.Context,
	c client.Client,
	obj *unstructured.Unstructured,
	manager string,
) (err error) {
	existingObject := obj.DeepCopy()
	var patches []ssa.JSONPatch

	if len(patches) == 0 {
		return nil
	}

	rawPatch, err := json.Marshal(patches)
	if err != nil {
		return err
	}

	patch := client.RawPatch(types.JSONPatchType, rawPatch)

	return c.Patch(ctx, existingObject, patch, client.FieldOwner(manager))
}

func patchAddLabels(object *unstructured.Unstructured, keys []string) []ssa.JSONPatch {
	var patches []ssa.JSONPatch
	labels := object.GetLabels()
	for _, key := range keys {
		if _, ok := labels[key]; ok {
			path := fmt.Sprintf("/metadata/labels/%s", strings.ReplaceAll(key, "/", "~1"))
			patches = append(patches, ssa.JSONPatch{
				Operation: "replace",
				Path:      path,
			})
		}
	}
	return patches
}
