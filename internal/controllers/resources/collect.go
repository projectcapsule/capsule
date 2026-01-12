// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/go-logr/logr"
	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/misc"
	"github.com/projectcapsule/capsule/pkg/api/processor"
	tpl "github.com/projectcapsule/capsule/pkg/template"
	"github.com/projectcapsule/capsule/pkg/utils/tenant"
)

var reservedLabelSet = map[string]struct{}{
	meta.ResourcesLabel:           {},
	meta.CreatedByCapsuleLabel:    {},
	meta.ManagedByCapsuleLabel:    {},
	meta.NewManagedByCapsuleLabel: {},
}

// With this function we are attempting to collect all the unstructured items
// No Interacting is done with the kubernetes regarding applying etc.
//
//nolint:gocognit
func collectResources(
	ctx context.Context,
	c client.Client,
	tnt capsulev1beta2.Tenant,
	resourceIndex string,
	spec capsulev1beta2.ResourceSpec,
	ns *corev1.Namespace,
	tmplContext tpl.ReferenceContext,
	acc processor.Accumulator,
	allowCrossNamespaceSelection bool,
) (err error) {
	log := ctrllog.FromContext(ctx)

	var syncErr error

	fastContext := tenant.ContextForTenantAndNamespace(&tnt, ns)

	labels, annotations := gatherAdditionalMetadata(spec, fastContext)

	log.V(5).Info("using additional metadata", "labels", labels, "annotations", annotations)

	// Run Items
	for nsIndex, item := range spec.NamespacedItems {
		log.V(5).Info("processing namespaced item", "index", nsIndex)

		p, rawError := handleNamespacedItem(ctx, c, nsIndex, item, ns, tnt, allowCrossNamespaceSelection)
		if rawError != nil {
			syncErr = errors.Join(syncErr, rawError)

			continue
		}

		log.V(5).Info("loaded resources", "amount", len(p))

		for i, o := range p {
			rawError = addToAccumulation(tnt, ns, spec, acc, o, resourceIndex+"/namespaced-"+strconv.Itoa(nsIndex)+"-"+strconv.Itoa(i), labels, annotations, allowCrossNamespaceSelection)
			if rawError != nil {
				syncErr = errors.Join(syncErr, rawError)

				continue
			}
		}
	}

	// Run Raw Items
	for rawIndex, item := range spec.RawItems {
		log.V(5).Info("processing raw item", "index", rawIndex)

		p, rawError := handleRawItem(ctx, c, item, ns, fastContext)
		if rawError != nil {
			syncErr = errors.Join(syncErr, rawError)

			continue
		}

		log.V(7).Info("evaluated raw item", "object", p)

		rawError = addToAccumulation(tnt, ns, spec, acc, p, resourceIndex+"/raw-"+strconv.Itoa(rawIndex), labels, annotations, allowCrossNamespaceSelection)
		if rawError != nil {
			syncErr = errors.Join(syncErr, rawError)

			continue
		}
	}

	// Run Generators
	for generatorIndex, item := range spec.Templates {
		log.V(5).Info("processing generator item", "index", generatorIndex)

		p, genError := handleGeneratorItem(ctx, c, generatorIndex, item, ns, tmplContext)
		if genError != nil {
			syncErr = errors.Join(syncErr, genError)

			continue
		}

		log.V(5).Info("loaded resources", "amount", len(p))

		for i, o := range p {

			genError = addToAccumulation(tnt, ns, spec, acc, o, resourceIndex+"/template-"+strconv.Itoa(generatorIndex)+"-"+strconv.Itoa(i), labels, annotations, allowCrossNamespaceSelection)
			if genError != nil {
				syncErr = errors.Join(syncErr, genError)

				continue
			}
		}
	}

	return syncErr
}

// Add an item to the accumulator
// Mainly handles conflicts
func addToAccumulation(
	tnt capsulev1beta2.Tenant,
	ns *corev1.Namespace,
	spec capsulev1beta2.ResourceSpec,
	acc processor.Accumulator,
	obj *unstructured.Unstructured,
	origin string,
	labels map[string]string,
	annotations map[string]string,
	allowCrossNamespaceSelection bool,
) (err error) {
	if obj == nil {
		return
	}

	if allowCrossNamespaceSelection {
		obj.SetNamespace(ns.GetName())
	}

	if len(labels) > 0 {
		dst := obj.GetLabels()
		if dst == nil {
			dst = make(map[string]string, len(labels))
		}

		maps.Copy(dst, labels)
		obj.SetLabels(dst)

		meta.SetFilteredLabels(obj, reservedLabelSet)
	}

	if len(annotations) > 0 {
		dst := obj.GetAnnotations()
		if dst == nil {
			dst = make(map[string]string, len(annotations))
		}

		maps.Copy(dst, annotations)

		obj.SetAnnotations(dst)
	}

	resource := misc.NewResourceID(obj, tnt.GetName(), origin)
	processor.AccumulatorAdd(acc, resource, processor.AccumulatorObject{
		Object: obj,
		Origin: misc.TenantResourceIDWithOrigin{
			TenantResourceID: misc.TenantResourceID{
				Tenant: tnt.GetName(),
			},
			Origin: origin,
		},
	})

	return nil
}

func handleNamespacedItem(
	ctx context.Context,
	c client.Client,
	index int,
	item misc.ResourceReference,
	ns *corev1.Namespace,
	tnt capsulev1beta2.Tenant,
	allowCrossNamespaceSelection bool,

) (processed []*unstructured.Unstructured, err error) {
	tntNamespaces := sets.NewString(tnt.Status.Namespaces...)

	// A TenantResource is created by a TenantOwner, and potentially, they could point to a resource in a non-owned
	// Namespace: this must be blocked by checking it this is the case.
	if !allowCrossNamespaceSelection && !tntNamespaces.Has(string(item.Namespace)) {
		err = fmt.Errorf("cross-namespace selection is not allowed. Referring a Namespace that is not part of the given Tenant")

		return nil, err
	}

	namespace := ""
	if ns != nil {
		namespace = ns.GetName()
	}

	return item.LoadResources(ctx, c, namespace)
}

// Handles a single generator item
func handleGeneratorItem(
	ctx context.Context,
	c client.Client,
	index int,
	item capsulev1beta2.TemplateItemSpec,
	ns *corev1.Namespace,
	tmplContext tpl.ReferenceContext,
) (processed []*unstructured.Unstructured, err error) {
	objs, err := tpl.RenderUnstructuredItems(tmplContext, item.MissingKey, item.Template)
	if err != nil {
		return nil, fmt.Errorf("error running generator: %w", err, "hello")
	}

	for _, obj := range objs {
		if ns != nil {
			obj.SetNamespace(ns.Name)
		}

		processed = append(processed, obj)
	}

	return
}

func handleRawItem(
	ctx context.Context,
	c client.Client,
	item capsulev1beta2.RawExtension,
	ns *corev1.Namespace,
	fastContext map[string]string,
) (processed *unstructured.Unstructured, err error) {
	tmplString := tpl.TemplateForTenantAndNamespace(string(item.Raw), fastContext)

	obj := &unstructured.Unstructured{}
	if _, _, err := unstructured.UnstructuredJSONScheme.Decode([]byte(tmplString), nil, obj); err != nil {
		return nil, fmt.Errorf("decode unstructured: %w", err)
	}

	if ns != nil {
		obj.SetNamespace(ns.Name)
	}

	return obj, nil
}

// Allows templating in
func gatherAdditionalMetadata(
	spec capsulev1beta2.ResourceSpec,
	fastContext map[string]string,
) (labels map[string]string, annotations map[string]string) {
	labels = make(map[string]string)
	annotations = make(map[string]string)

	md := spec.AdditionalMetadata
	if md == nil {
		return labels, annotations
	}

	if md.Labels != nil {
		labels = tpl.TemplateForTenantAndNamespaceMap(maps.Clone(md.Labels), fastContext)
	}

	if md.Annotations != nil {
		annotations = tpl.TemplateForTenantAndNamespaceMap(maps.Clone(md.Annotations), fastContext)
	}

	return labels, annotations
}

//nolint:gocognit
func foreachTenantNamespace(
	ctx context.Context,
	log logr.Logger,
	c client.Client,
	gatherClient client.Client,
	tnt capsulev1beta2.Tenant,
	resource capsulev1beta2.ResourceSpec,
	resourceIndex string,
	tmplContext tpl.ReferenceContext,
	acc processor.Accumulator,
	allowCrossNamespaceSelection bool,
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
	if err = gatherClient.List(ctx, &namespaces, client.MatchingLabelsSelector{Selector: selector}); err != nil {
		log.Error(err, "cannot retrieve Namespaces for resource")

		return err
	}

	log.V(5).Info("retrieved namespaces", "size", len(namespaces.Items))

	for _, ns := range namespaces.Items {

		//spec.Context.GatherContext(ctx, c, nil, ns.GetName())
		err = collectResources(
			ctx,
			c,
			tnt,
			resourceIndex,
			resource,
			&ns,
			tmplContext,
			acc,
			allowCrossNamespaceSelection,
		)
		if err != nil {
			return
		}
	}

	return
}
