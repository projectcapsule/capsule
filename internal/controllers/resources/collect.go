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
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/go-logr/logr"
	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/misc"
	"github.com/projectcapsule/capsule/pkg/api/processor"
	"github.com/projectcapsule/capsule/pkg/runtime/gvk"
	"github.com/projectcapsule/capsule/pkg/runtime/sanitize"
	"github.com/projectcapsule/capsule/pkg/template"
	tpl "github.com/projectcapsule/capsule/pkg/template"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

type Collector struct {
	gatherClient           client.Client
	mapper                 k8smeta.RESTMapper
	contextSanitizeOptions sanitize.SanitizeOptions
	objectSanitizeOptions  sanitize.SanitizeOptions
	reservedLabelSet       map[string]struct{}
}

type CollectorOptions struct {
	AllowCrossNamespaceSelection bool
	Accumulator                  processor.Accumulator
	Iterator                     CollectorIteratorOptions
}

type CollectorIteratorOptions struct {
	Labels      map[string]string
	Annotations map[string]string
	FastContext map[string]string
}

func NewCollectorIteratorOptions(
	tnt *capsulev1beta2.Tenant,
	ns *corev1.Namespace,
	spec capsulev1beta2.ResourceSpec,
) CollectorIteratorOptions {
	opts := CollectorIteratorOptions{}

	opts.FastContext = tenant.ContextForTenantAndNamespace(tnt, ns)

	labels, annotations := GatherAdditionalMetadata(spec, opts.FastContext)
	opts.Labels = labels
	opts.Annotations = annotations

	return opts
}

func NewCollector(c client.Client, mapper k8smeta.RESTMapper) Collector {
	return Collector{
		gatherClient: c,
		mapper:       mapper,
		contextSanitizeOptions: sanitize.SanitizeOptions{
			StripUID:           false,
			StripManagedFields: true,
			StripLastApplied:   true,
			StripStatus:        false,
		},
		objectSanitizeOptions: sanitize.DefaultSanitizeOptions(),
		reservedLabelSet: map[string]struct{}{
			meta.ResourcesLabel:           {},
			meta.CreatedByCapsuleLabel:    {},
			meta.ManagedByCapsuleLabel:    {},
			meta.NewManagedByCapsuleLabel: {},
		},
	}
}

// With this function we are attempting to collect all the unstructured items
// No Interacting is done with the kubernetes regarding applying etc.
//
//nolint:gocognit
func (co *Collector) Collect(
	ctx context.Context,
	c client.Client,
	opts CollectorOptions,
	tnt capsulev1beta2.Tenant,
	resourceIndex string,
	spec capsulev1beta2.ResourceSpec,
	ns *corev1.Namespace,
) (err error) {
	log := ctrllog.FromContext(ctx)

	var syncErr error

	tplContext := template.ReferenceContext{}
	//if spec.Context != nil {
	//	tplContext, _ = resource.Context.GatherContext(ctx, c, nil, "", nil)
	//}

	if ns != nil {
		tplContext["Namespace"] = sanitize.SanitizeObject(ns, c.Scheme(), co.contextSanitizeOptions)
	}
	tplContext["Tenant"] = sanitize.SanitizeObject(&tnt, c.Scheme(), co.contextSanitizeOptions)

	// Run Raw Items
	for rawIndex, item := range spec.RawItems {
		log.V(5).Info("processing raw item", "index", rawIndex)

		p, rawError := co.handleRawItem(ctx, c, opts, item, ns)
		if rawError != nil {
			syncErr = errors.Join(syncErr, rawError)

			continue
		}

		log.V(7).Info("evaluated raw item", "object", p)

		rawError = co.AddToAccumulation(tnt, ns, opts, spec, p, resourceIndex+"/raw-"+strconv.Itoa(rawIndex), true)
		if rawError != nil {
			syncErr = errors.Join(syncErr, rawError)

			continue
		}
	}

	// Run Generators
	for generatorIndex, item := range spec.Templates {
		log.V(5).Info("processing generator item", "index", generatorIndex)

		p, genError := co.handleGeneratorItem(ctx, c, generatorIndex, item, ns, tplContext)
		if genError != nil {
			syncErr = errors.Join(syncErr, genError)

			continue
		}

		log.V(5).Info("loaded resources", "amount", len(p))

		for i, o := range p {

			genError = co.AddToAccumulation(tnt, ns, opts, spec, o, resourceIndex+"/template-"+strconv.Itoa(generatorIndex)+"-"+strconv.Itoa(i), true)
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
func (co *Collector) AddToAccumulation(
	tnt capsulev1beta2.Tenant,
	ns *corev1.Namespace,
	opts CollectorOptions,
	spec capsulev1beta2.ResourceSpec,
	obj *unstructured.Unstructured,
	origin string,
	combine bool,
) (err error) {
	if obj == nil {
		return
	}

	resource := misc.NewResourceID(obj, tnt.GetName(), origin)

	if !combine {
		if _, k := opts.Accumulator[resource.GetKey("")]; k {
			return nil
		}
	}

	if !opts.AllowCrossNamespaceSelection && ns != nil {
		obj.SetNamespace(ns.GetName())
	}

	if len(opts.Iterator.Labels) > 0 {
		dst := obj.GetLabels()
		if dst == nil {
			dst = make(map[string]string, len(opts.Iterator.Labels))
		}

		maps.Copy(dst, opts.Iterator.Labels)
		obj.SetLabels(dst)

		meta.SetFilteredLabels(obj, co.reservedLabelSet)
	}

	if len(opts.Iterator.Annotations) > 0 {
		dst := obj.GetAnnotations()
		if dst == nil {
			dst = make(map[string]string, len(opts.Iterator.Annotations))
		}

		maps.Copy(dst, opts.Iterator.Annotations)

		obj.SetAnnotations(dst)
	}

	sanitize.SanitizeUnstructured(obj, co.objectSanitizeOptions)

	processor.AccumulatorAdd(opts.Accumulator, resource, processor.AccumulatorObject{
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

func (co *Collector) CollectNamespacedItems(
	ctx context.Context,
	c client.Client,
	opts CollectorOptions,
	spec capsulev1beta2.ResourceSpec,
	ns *corev1.Namespace,
	tnt capsulev1beta2.Tenant,
) (items map[gvk.ResourceKey]*unstructured.Unstructured, err error) {
	var totalError error

	seen := make(map[gvk.ResourceKey]*unstructured.Unstructured)

	log := log.FromContext(ctx)
	tntNamespaces := sets.NewString(tnt.Status.Namespaces...)

	namespace := ""
	if !opts.AllowCrossNamespaceSelection && ns != nil {
		namespace = ns.GetName()
	}

	// A TenantResource is created by a TenantOwner, and potentially, they could point to a resource in a non-owned
	// Namespace: this must be blocked by checking it this is the case.
	if !opts.AllowCrossNamespaceSelection && !tntNamespaces.Has(string(namespace)) {
		err = fmt.Errorf("cross-namespace selection is not allowed. Referring a Namespace that is not part of the given Tenant")

		return nil, err
	}

	selector, err := getSelectorForCreatedResourcesExclusion()
	if err != nil {
		return nil, err
	}

	for _, item := range spec.NamespacedItems {
		p, err := item.LoadResources(ctx, c, co.mapper, namespace, []labels.Selector{selector}, opts.Iterator.FastContext, false)
		if err != nil {
			totalError = errors.Join(totalError, err)

			continue
		}

		for _, o := range p {
			// Namespaced Items are different. Even if we allow cross namespace loading
			// If a target namespace is given it always is used
			if ns != nil && ns.GetName() != "" {
				o.SetNamespace(ns.GetName())
			}

			k, ok := gvk.KeyFromUnstructured(o)
			if ok {
				if _, already := seen[k]; already {
					log.V(6).Info("skipping duplicate loaded resource",
						"gvk", schema.GroupVersionKind{Group: k.Group, Version: k.Version, Kind: k.Kind}.String(),
						"namespace", k.Namespace,
						"name", k.Name,
					)
					continue
				}
				seen[k] = o
			} else {
				log.V(4).Info("resource missing identity; cannot dedupe reliably",
					"apiVersion", o.GetAPIVersion(), "kind", o.GetKind(), "namespace", o.GetNamespace(), "name", o.GetName(),
				)
			}

		}
	}

	objs := make([]*unstructured.Unstructured, 0, len(seen))
	for _, obj := range seen {
		objs = append(objs, obj)
	}

	return seen, totalError
}

// Handles a single generator item
func (co *Collector) handleGeneratorItem(
	ctx context.Context,
	c client.Client,
	index int,
	item capsulev1beta2.TemplateItemSpec,
	ns *corev1.Namespace,
	tmplContext tpl.ReferenceContext,
) (processed []*unstructured.Unstructured, err error) {
	objs, err := tpl.RenderUnstructuredItems(tmplContext, item.MissingKey, item.Template)
	if err != nil {
		return nil, fmt.Errorf("error running generator: %w", err)
	}

	for _, obj := range objs {
		if ns != nil {
			obj.SetNamespace(ns.Name)
		}

		processed = append(processed, obj)
	}

	return
}

func (co *Collector) handleRawItem(
	ctx context.Context,
	c client.Client,
	opts CollectorOptions,
	item capsulev1beta2.RawExtension,
	ns *corev1.Namespace,
) (processed *unstructured.Unstructured, err error) {
	tmplString := tpl.FastTemplate(string(item.Raw), opts.Iterator.FastContext)

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
func GatherAdditionalMetadata(
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
		labels = tpl.FastTemplateMap(maps.Clone(md.Labels), fastContext)
	}

	if md.Annotations != nil {
		annotations = tpl.FastTemplateMap(maps.Clone(md.Annotations), fastContext)
	}

	return labels, annotations
}

//nolint:gocognit
func (co *Collector) foreachTenantNamespace(
	ctx context.Context,
	log logr.Logger,
	c client.Client,
	tnt capsulev1beta2.Tenant,
	resource capsulev1beta2.ResourceSpec,
	resourceIndex string,
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
	if err = co.gatherClient.List(ctx, &namespaces, client.MatchingLabelsSelector{Selector: selector}); err != nil {
		log.Error(err, "cannot retrieve Namespaces for resource")

		return err
	}

	log.V(5).Info("retrieved namespaces", "size", len(namespaces.Items))

	for _, ns := range namespaces.Items {
		log.V(5).Info("reconciling for", "namespace", ns.Name)

		err = co.Collect(
			ctx,
			c,
			CollectorOptions{},
			tnt,
			resourceIndex,
			resource,
			&ns,
		)
		if err != nil {
			return
		}
	}

	return
}
