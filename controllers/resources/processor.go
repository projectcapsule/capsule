// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"errors"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	apierr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/configuration"
	"github.com/projectcapsule/capsule/pkg/meta"
	tpl "github.com/projectcapsule/capsule/pkg/template"
	"github.com/projectcapsule/capsule/pkg/utils"
)

const (
	finalizer = "capsule.clastix.io/resources"
)

type Processor struct {
	client        client.Client
	configuration configuration.Configuration
}

func prepareAdditionalMetadata(m map[string]string) map[string]string {
	if m == nil {
		return make(map[string]string)
	}

	// we need to create a new map to avoid modifying the original one
	copied := make(map[string]string, len(m))
	for k, v := range m {
		copied[k] = v
	}

	return copied
}

func (r *Processor) HandlePruning(
	ctx context.Context,
	c client.Client,
	current,
	desired sets.Set[string],
) (failedProcess []string, err error) {
	log := ctrllog.FromContext(ctx)

	diff := current.Difference(desired)
	// We don't want to trigger a reconciliation of the Status every time,
	// rather, only in case of a difference between the processed and the actual status.
	// This can happen upon the first reconciliation, or a removal, or a change, of a resource.
	reconcile := diff.Len() > 0 || current.Len() != desired.Len()

	if !reconcile {
		return
	}

	processed := sets.NewString()

	log.Info("starting processing pruning", "length", diff.Len())

	// The outer resources must be removed, iterating over these to clean-up
	for item := range diff {
		or := capsulev1beta2.ObjectReferenceStatus{}
		if sectionErr := or.ParseFromString(item); sectionErr != nil {
			processed.Insert(or.String())

			log.Error(sectionErr, "unable to parse resource to prune", "resource", item)

			continue
		}

		obj := unstructured.Unstructured{}
		obj.SetNamespace(or.Namespace)
		obj.SetName(or.Name)
		obj.SetGroupVersionKind(schema.FromAPIVersionAndKind(or.APIVersion, or.Kind))

		log.V(5).Info("pruning", "resource", obj.GroupVersionKind(), "name", obj.GetName(), "namespace", obj.GetNamespace())

		if sectionErr := c.Delete(ctx, &obj); err != sectionErr {
			if apierr.IsNotFound(sectionErr) {
				// Object may have been already deleted, we can ignore this error
				continue
			}

			or.Status = metav1.ConditionFalse
			or.Message = sectionErr.Error()
			or.Type = meta.ReadyCondition
			processed.Insert(or.String())

			err = errors.Join(sectionErr)

			continue
		}

		log.V(5).Info("resource has been pruned", "resource", item)
	}

	return processed.List(), nil
}

//nolint:gocognit
func (r *Processor) HandleSectionPreflight(
	ctx context.Context,
	c client.Client,
	tnt capsulev1beta2.Tenant,
	allowCrossNamespaceSelection bool,
	tenantLabel string,
	resourceIndex int,
	spec capsulev1beta2.ResourceSpec,
	fieldOwner string,
	scope api.ResourceScope,
) (processed []string, err error) {
	log := ctrllog.FromContext(ctx)

	tplContext := loadTenantToContext(&tnt)

	switch scope {
	case api.ResourceScopeTenant:
		tplContext, _ = spec.Context.GatherContext(ctx, c, nil, "")
		tplContext["Tenant"] = tnt

		owner := fieldOwner + "/" + tnt.Name + "/" + strconv.Itoa(resourceIndex)

		return r.reconcile(
			ctx,
			c,
			tnt,
			allowCrossNamespaceSelection,
			tenantLabel,
			resourceIndex,
			spec,
			owner,
			capsulev1beta2.ObjectReferenceStatusOwner{
				Name:  tnt.GetName(),
				UID:   tnt.GetUID(),
				Scope: api.ResourceScopeTenant,
			},
			nil,
			tplContext,
		)
	default:

		// Creating Namespace selector
		var selector labels.Selector

		if spec.NamespaceSelector != nil {
			selector, err = metav1.LabelSelectorAsSelector(spec.NamespaceSelector)
			if err != nil {
				log.Error(err, "cannot create Namespace selector for Namespace filtering and resource replication", "index", resourceIndex)

				return nil, err
			}
		} else {
			selector = labels.NewSelector()
		}
		// Resources can be replicated only on Namespaces belonging to the same Global:
		// preventing a boundary cross by enforcing the selection.
		tntRequirement, err := labels.NewRequirement(tenantLabel, selection.Equals, []string{tnt.GetName()})
		if err != nil {
			log.Error(err, "unable to create requirement for Namespace filtering and resource replication", "index", resourceIndex)

			return nil, err
		}

		selector = selector.Add(*tntRequirement)
		// Selecting the targeted Namespace according to the TenantResource specification.
		namespaces := corev1.NamespaceList{}
		if err = r.client.List(ctx, &namespaces, client.MatchingLabelsSelector{Selector: selector}); err != nil {
			log.Error(err, "cannot retrieve Namespaces for resource", "index", resourceIndex)

			return nil, err
		}

		for _, ns := range namespaces.Items {

			//spec.Context.GatherContext(ctx, c, nil, ns.GetName())

			owner := fieldOwner + "/" + tnt.Name + "/" + ns.Name + "/" + strconv.Itoa(resourceIndex)

			p, perr := r.reconcile(
				ctx,
				c,
				tnt,
				allowCrossNamespaceSelection,
				tenantLabel,
				resourceIndex,
				spec,
				owner,
				capsulev1beta2.ObjectReferenceStatusOwner{
					Name:  ns.GetName(),
					UID:   ns.GetUID(),
					Scope: api.ResourceScopeNamespace,
				},
				&ns,
				tplContext)
			if perr != nil {
				err = errors.Join(err, perr)
			}

			processed = append(processed, p...)
		}
	}

	return
}

func (r *Processor) reconcile(
	ctx context.Context,
	c client.Client,
	tnt capsulev1beta2.Tenant,
	allowCrossNamespaceSelection bool,
	tenantLabel string,
	resourceIndex int,
	resource capsulev1beta2.ResourceSpec,
	fieldOwner string,
	owner capsulev1beta2.ObjectReferenceStatusOwner,
	ns *corev1.Namespace,
	tmplContext tpl.ReferenceContext,
) ([]string, error) {
	log := ctrllog.FromContext(ctx)

	// Collect Resources to apply
	objects, err := r.handleResources(
		ctx,
		c,
		tnt,
		allowCrossNamespaceSelection,
		tenantLabel,
		resourceIndex,
		resource,
		owner,
		ns,
		tmplContext,
	)
	if err != nil {
		log.Error(err, "some error happend", "here", "here")
		return nil, err
	}

	var syncErr error

	processed := sets.NewString()

	log.V(4).Info("processing items", "items", len(objects))

	// Apply objects and return processed
	for i, obj := range objects {
		replicatedItem := &capsulev1beta2.ObjectReferenceStatus{}
		replicatedItem.Name = obj.GetName()
		replicatedItem.Kind = obj.GetKind()
		replicatedItem.APIVersion = obj.GetAPIVersion()
		replicatedItem.Owner = owner
		replicatedItem.Type = meta.ReadyCondition

		if ns != nil {
			replicatedItem.Namespace = ns.GetName()
		}

		fieldOwnerw := fieldOwner + "/" + tnt.Name + "/" + strconv.Itoa(i)

		if err := r.createOrPatch(ctx, c, obj, resource, fieldOwnerw); err != nil {
			replicatedItem.Status = metav1.ConditionFalse
			replicatedItem.Message = err.Error()
		} else {
			replicatedItem.Status = metav1.ConditionTrue
		}

		processed.Insert(replicatedItem.String())
	}

	return processed.List(), syncErr
}

func (r *Processor) createOrPatch(
	ctx context.Context,
	c client.Client,
	obj *unstructured.Unstructured,
	resource capsulev1beta2.ResourceSpec,
	fieldOwner string,
) error {
	actual := &unstructured.Unstructured{}
	actual.SetGroupVersionKind(obj.GroupVersionKind())
	actual.SetNamespace(obj.GetNamespace())
	actual.SetName(obj.GetName())

	// Fetch current to have a stable mutate func input
	_ = c.Get(ctx, client.ObjectKeyFromObject(actual), actual)

	if resource.AdditionalMetadata != nil {
		obj.SetAnnotations(resource.AdditionalMetadata.Annotations)
		obj.SetLabels(resource.AdditionalMetadata.Labels)
	}

	return utils.CreateOrPatch(
		ctx,
		c,
		obj,
		fieldOwner,
		append(resource.Ignore, r.configuration.ReplicationIgnoreRules()...),
		*resource.Force,
	)
}
