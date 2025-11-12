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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
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
	acc api.Accumulator,
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
	acc api.Accumulator,
) (err error) {
	var syncErr error

	// Run Raw Items
	for rawIndex, item := range spec.RawItems {
		p, rawError := r.handleRawItem(ctx, c, rawIndex, item, ns, tnt)
		if rawError != nil {
			syncErr = errors.Join(syncErr, rawError)

			continue
		}

		rawError = r.addToAccumulation(tnt, spec, acc, p, resourceIndex+"/gen-"+strconv.Itoa(rawIndex))
		if rawError != nil {
			syncErr = errors.Join(syncErr, rawError)

			continue
		}
	}

	// Run Generators
	for generatorIndex, item := range spec.Generators {
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
	acc api.Accumulator,
	obj *unstructured.Unstructured,
	index string,
) (err error) {
	r.handleResource(spec, obj)

	key := api.NewResourceID(obj, tnt.GetName(), index)

	acc[key] = obj

	return nil
}

// Handles a single generator item
func (r *Processor) handleGeneratorItem(
	ctx context.Context,
	c client.Client,
	index int,
	item capsulev1beta2.GeneratorItemSpec,
	ns *corev1.Namespace,
	tmplContext tpl.ReferenceContext,
) (processed []*unstructured.Unstructured, err error) {
	objs, err := renderGeneratorItem(item, tmplContext)
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
