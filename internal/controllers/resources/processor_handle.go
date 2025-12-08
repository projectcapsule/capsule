// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/valyala/fasttemplate"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/misc"
	tpl "github.com/projectcapsule/capsule/pkg/template"
)

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
	var syncErr error

	// Run Items
	for nsIndex, item := range spec.NamespacedItems {
		p, rawError := r.handleNamespacedItem(ctx, c, nsIndex, item, ns, tnt)
		if rawError != nil {
			syncErr = errors.Join(syncErr, rawError)

			continue
		}

		for i, o := range p {
			rawError = r.addToAccumulation(tnt, spec, acc, &o, resourceIndex+"/gen-"+strconv.Itoa(nsIndex)+"-"+strconv.Itoa(i))
			if rawError != nil {
				syncErr = errors.Join(syncErr, rawError)

				continue
			}
		}
	}

	// Run Raw Items
	for rawIndex, item := range spec.RawItems {
		p, rawError := r.handleRawItem(ctx, c, rawIndex, item, ns, tnt)
		if rawError != nil {
			syncErr = errors.Join(syncErr, rawError)

			continue
		}

		rawError = r.addToAccumulation(tnt, spec, acc, p, resourceIndex+"/raw-"+strconv.Itoa(rawIndex))
		if rawError != nil {
			syncErr = errors.Join(syncErr, rawError)

			continue
		}
	}

	// Run Generators
	for generatorIndex, item := range spec.Templates {
		p, genError := r.handleGeneratorItem(ctx, c, generatorIndex, item, ns, tmplContext)
		if genError != nil {
			syncErr = errors.Join(syncErr, genError)

			continue
		}

		for i, o := range p {
			genError = r.addToAccumulation(tnt, spec, acc, o, resourceIndex+"/gen-"+strconv.Itoa(generatorIndex)+"-"+strconv.Itoa(i))
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
) (err error) {
	r.handleResource(spec, obj)

	key := capsulev1beta2.ResourceIDWithOptions{
		ResourceID:           misc.NewResourceID(obj, tnt.GetName(), index),
		ResourceSpecSettings: &spec.ResourceSpecSettings,
	}

	acc[key] = obj

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
	index int,
	item capsulev1beta2.RawExtension,
	ns *corev1.Namespace,
	tnt capsulev1beta2.Tenant,
) (processed *unstructured.Unstructured, err error) {
	template := string(item.Raw)

	t := fasttemplate.New(template, "{{ ", " }}")

	tContext := map[string]interface{}{
		"tenant.name": tnt.Name,
	}
	if ns != nil {
		tContext["namespace"] = ns.Name
	}

	tmplString := t.ExecuteString(tContext)

	obj := &unstructured.Unstructured{}
	if _, _, decodeErr := r.factory.UniversalDeserializer().Decode([]byte(tmplString), nil, obj); decodeErr != nil {
		return nil, fmt.Errorf("error rendering raw: %w", err, "hello")
	}

	if ns != nil {
		obj.SetNamespace(ns.Name)
	}

	return obj, nil
}

func (r *Processor) handleResource(
	spec capsulev1beta2.ResourceSpec,
	obj *unstructured.Unstructured,
) {
	if spec.AdditionalMetadata != nil {
		obj.SetAnnotations(spec.AdditionalMetadata.Annotations)
		obj.SetLabels(spec.AdditionalMetadata.Labels)
	}
}
