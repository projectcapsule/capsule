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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/misc"
	tpl "github.com/projectcapsule/capsule/pkg/template"
	"github.com/projectcapsule/capsule/pkg/utils/tenant"
)

var reservedLabelSet = map[string]struct{}{
	meta.ResourcesLabel:           {},
	meta.CreatedByCapsuleLabel:    {},
	meta.ManagedByCapsuleLabel:    {},
	meta.NewManagedByCapsuleLabel: {},
}

func (r *Processor) handleResources(
	ctx context.Context,
	c client.Client,
	tnt capsulev1beta2.Tenant,
	resourceIndex string,
	spec capsulev1beta2.ResourceSpec,
	ns *corev1.Namespace,
	tmplContext tpl.ReferenceContext,
	acc Accumulator,
) (err error) {
	return r.collectResources(ctx, c, tnt, resourceIndex, spec, ns, tmplContext, acc)
}

// With this function we are attempting to collect all the unstructured items
// No Interacting is done with the kubernetes regarding applying etc.
//
//nolint:gocognit
func (r *Processor) collectResources(
	ctx context.Context,
	c client.Client,
	tnt capsulev1beta2.Tenant,
	resourceIndex string,
	spec capsulev1beta2.ResourceSpec,
	ns *corev1.Namespace,
	tmplContext tpl.ReferenceContext,
	acc Accumulator,
) (err error) {
	log := ctrllog.FromContext(ctx)

	var syncErr error

	fastContext := tenant.ContextForTenantAndNamespace(&tnt, ns)

	labels, annotations := r.gatherAdditionalMetadata(spec, fastContext)

	log.V(5).Info("using additional metadata", "labels", labels, "annotations", annotations)

	// Run Items
	for nsIndex, item := range spec.NamespacedItems {
		log.V(5).Info("processing namespaced item", "index", nsIndex)

		p, rawError := r.handleNamespacedItem(ctx, c, nsIndex, item, ns, tnt)
		if rawError != nil {
			syncErr = errors.Join(syncErr, rawError)

			continue
		}

		log.V(5).Info("loaded resources", "amount", len(p))

		for i, o := range p {
			rawError = r.addToAccumulation(tnt, spec, acc, &o, resourceIndex+"/gen-"+strconv.Itoa(nsIndex)+"-"+strconv.Itoa(i), labels, annotations)
			if rawError != nil {
				syncErr = errors.Join(syncErr, rawError)

				continue
			}
		}
	}

	// Run Raw Items
	for rawIndex, item := range spec.RawItems {
		log.V(5).Info("processing raw item", "index", rawIndex)

		p, rawError := r.handleRawItem(ctx, c, item, ns, fastContext)
		if rawError != nil {
			syncErr = errors.Join(syncErr, rawError)

			continue
		}

		log.V(7).Info("evaluated raw item", "object", p)

		rawError = r.addToAccumulation(tnt, spec, acc, p, resourceIndex+"/raw-"+strconv.Itoa(rawIndex), labels, annotations)
		if rawError != nil {
			syncErr = errors.Join(syncErr, rawError)

			continue
		}
	}

	// Run Generators
	for generatorIndex, item := range spec.Templates {
		log.V(5).Info("processing generator item", "index", generatorIndex)

		p, genError := r.handleGeneratorItem(ctx, c, generatorIndex, item, ns, tmplContext)
		if genError != nil {
			syncErr = errors.Join(syncErr, genError)

			continue
		}

		log.V(5).Info("loaded resources", "amount", len(p))

		for i, o := range p {

			genError = r.addToAccumulation(tnt, spec, acc, o, resourceIndex+"/gen-"+strconv.Itoa(generatorIndex)+"-"+strconv.Itoa(i), labels, annotations)
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
func (r *Processor) addToAccumulation(
	tnt capsulev1beta2.Tenant,
	spec capsulev1beta2.ResourceSpec,
	acc Accumulator,
	obj *unstructured.Unstructured,
	index string,
	labels map[string]string,
	annotations map[string]string,

) (err error) {
	r.handleMetadata(obj, labels, annotations)

	resource := misc.NewResourceID(obj, tnt.GetName(), index)

	acc[resource.GetKey()] = &AccumulatorItem{
		Object: obj,
		Options: capsulev1beta2.ResourceIDWithOptions{
			ResourceID:           resource,
			ResourceSpecSettings: &spec.ResourceSpecSettings,
		},
	}

	return nil
}

func (r *Processor) handleNamespacedItem(
	ctx context.Context,
	c client.Client,
	index int,
	item misc.ResourceReference,
	ns *corev1.Namespace,
	tnt capsulev1beta2.Tenant,
) (processed []unstructured.Unstructured, err error) {
	tntNamespaces := sets.NewString(tnt.Status.Namespaces...)

	// A TenantResource is created by a TenantOwner, and potentially, they could point to a resource in a non-owned
	// Namespace: this must be blocked by checking it this is the case.
	if !r.allowCrossNamespaceSelection && !tntNamespaces.Has(item.Namespace) {
		err = fmt.Errorf("cross-namespace selection is not allowed. Referring a Namespace that is not part of the given Tenant")

		return nil, err
	}
	// Namespaced Items are relying on selecting resources, rather than specifying a specific name:
	// creating it to get used by the client List action.
	objSelector := item.Selector

	itemSelector, err := metav1.LabelSelectorAsSelector(objSelector)
	if err != nil {
		return nil, err
	}

	objs := unstructured.UnstructuredList{}
	objs.SetGroupVersionKind(schema.FromAPIVersionAndKind(item.APIVersion, fmt.Sprintf("%sList", item.Kind)))

	if err = r.client.List(ctx, &objs, client.InNamespace(item.Namespace), client.MatchingLabelsSelector{Selector: itemSelector}); err != nil {
		if item.Optional && apierrors.IsNotFound(err) {
			return objs.Items, nil
		}

		return nil, err
	}

	return objs.Items, nil
}

// Handles a single generator item
func (r *Processor) handleGeneratorItem(
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

func (r *Processor) handleRawItem(
	ctx context.Context,
	c client.Client,
	item capsulev1beta2.RawExtension,
	ns *corev1.Namespace,
	fastContext map[string]string,
) (processed *unstructured.Unstructured, err error) {
	tmplString := tpl.TemplateForTenantAndNamespace(string(item.Raw), fastContext)

	log := log.FromContext(ctx)

	log.Info("TEMP", "template", tmplString)

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
func (r *Processor) gatherAdditionalMetadata(
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

func (r *Processor) handleMetadata(
	obj *unstructured.Unstructured,
	labels map[string]string,
	annotations map[string]string,
) {
	if obj == nil {
		return
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
}
