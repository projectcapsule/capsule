// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"errors"
	"fmt"

	"github.com/valyala/fasttemplate"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/meta"
	tpl "github.com/projectcapsule/capsule/pkg/template"
)

// With this function we are attempting to collect all the unstructured items
// No Interacting is done with the kubernetes regarding applying etc.
//
//nolint:gocognit
func (r *Processor) handleResources(
	ctx context.Context,
	c client.Client,
	tnt capsulev1beta2.Tenant,
	allowCrossNamespaceSelection bool,
	tenantLabel string,
	resourceIndex int,
	spec capsulev1beta2.ResourceSpec,
	owner capsulev1beta2.ObjectReferenceStatusOwner,
	ns *corev1.Namespace,
	tmplContext tpl.ReferenceContext,
) (processed []*unstructured.Unstructured, err error) {
	//log := ctrllog.FromContext(ctx)

	// Generating additional metadata
	objAnnotations, objLabels := map[string]string{}, map[string]string{}

	if spec.AdditionalMetadata != nil {
		objAnnotations = prepareAdditionalMetadata(spec.AdditionalMetadata.Annotations)
		objLabels = prepareAdditionalMetadata(spec.AdditionalMetadata.Labels)
	}

	objAnnotations[tenantLabel] = tnt.GetName()

	objLabels[meta.ResourcesLabel] = fmt.Sprintf("%d", resourceIndex)
	objLabels[tenantLabel] = tnt.GetName()

	var syncErr error

	codecFactory := serializer.NewCodecFactory(r.client.Scheme())

	// Run Raw Items
	for rawIndex, item := range spec.RawItems {
		p, rawError := r.handleRawItem(ctx, c, codecFactory, rawIndex, item, ns, tnt)
		if rawError != nil {
			syncErr = errors.Join(syncErr, rawError)

			continue
		}

		processed = append(processed, p)
	}

	// Run Generators
	for generatorIndex, item := range spec.Generators {
		p, genError := r.handleGeneratorItem(ctx, c, generatorIndex, item, ns, tmplContext)
		if genError != nil {
			syncErr = errors.Join(syncErr, genError)

			continue
		}

		processed = append(processed, p...)
	}

	return processed, syncErr
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
	codecFactory serializer.CodecFactory,
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
	if _, _, decodeErr := codecFactory.UniversalDeserializer().Decode([]byte(tmplString), nil, obj); decodeErr != nil {
		return nil, fmt.Errorf("error rendering raw: %w", err, "hello")
	}

	if ns != nil {
		obj.SetNamespace(ns.Name)
	}

	return obj, nil
}
