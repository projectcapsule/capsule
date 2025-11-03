// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/valyala/fasttemplate"
	corev1 "k8s.io/api/core/v1"
	apierr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/meta"
	tpl "github.com/projectcapsule/capsule/pkg/template"
)

const (
	finalizer = "capsule.clastix.io/resources"
)

type Processor struct {
	client client.Client
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
	scope api.ResourceScope,
) (processed []string, err error) {
	log := ctrllog.FromContext(ctx)

	tplContext := loadTenantToContext(&tnt)

	switch scope {
	case api.ResourceScopeTenant:

		tplContext, _ := spec.Context.GatherContext(ctx, c, nil, "")

		log.Info("got context", "context", tplContext)

		return r.handleSection(
			ctx,
			c,
			tnt,
			allowCrossNamespaceSelection,
			tenantLabel,
			resourceIndex,
			spec,
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

			p, perr := r.handleSection(
				ctx,
				c,
				tnt,
				allowCrossNamespaceSelection,
				tenantLabel,
				resourceIndex,
				spec,
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

//nolint:gocognit
func (r *Processor) handleSection(
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
) ([]string, error) {
	log := ctrllog.FromContext(ctx)

	// Generating additional metadata
	objAnnotations, objLabels := map[string]string{}, map[string]string{}

	if spec.AdditionalMetadata != nil {
		objAnnotations = prepareAdditionalMetadata(spec.AdditionalMetadata.Annotations)
		objLabels = prepareAdditionalMetadata(spec.AdditionalMetadata.Labels)
	}

	objAnnotations[tenantLabel] = tnt.GetName()

	objLabels[meta.ResourcesLabel] = fmt.Sprintf("%d", resourceIndex)
	objLabels[tenantLabel] = tnt.GetName()
	// processed will contain the sets of resources replicated, both for the raw and the Namespaced ones:
	// these are required to perform a final pruning once the replication has been occurred.
	processed := sets.NewString()

	tntNamespaces := sets.NewString(tnt.Status.Namespaces...)

	var syncErr error

	codecFactory := serializer.NewCodecFactory(r.client.Scheme())

	for nsIndex, item := range spec.NamespacedItems {
		keysAndValues := []any{"index", nsIndex, "namespace", item.Namespace, "tenant", tnt.GetName()}
		// A TenantResource is created by a TenantOwner, and potentially, they could point to a resource in a non-owned
		// Namespace: this must be blocked by checking it this is the case.
		if !allowCrossNamespaceSelection && !tntNamespaces.Has(item.Namespace) {
			log.Info("skipping processing of namespacedItem, referring a Namespace that is not part of the given Tenant", keysAndValues...)

			continue
		}
		// Namespaced Items are relying on selecting resources, rather than specifying a specific name:
		// creating it to get used by the client List action.
		objSelector := item.Selector

		itemSelector, selectorErr := metav1.LabelSelectorAsSelector(&objSelector)
		if selectorErr != nil {
			log.Error(selectorErr, "cannot create Selector for namespacedItem", keysAndValues...)

			syncErr = errors.Join(syncErr, selectorErr)

			continue
		}

		objs := unstructured.UnstructuredList{}
		objs.SetGroupVersionKind(schema.FromAPIVersionAndKind(item.APIVersion, fmt.Sprintf("%sList", item.Kind)))

		if clientErr := c.List(ctx, &objs, client.InNamespace(item.Namespace), client.MatchingLabelsSelector{Selector: itemSelector}); clientErr != nil {
			log.Error(clientErr, "cannot retrieve object for namespacedItem", keysAndValues...)

			syncErr = errors.Join(syncErr, clientErr)

			continue
		}

		var wg sync.WaitGroup

		errorsChan := make(chan error, len(objs.Items))
		// processedRaw is used to avoid concurrent map writes during iteration of namespaced items:
		// the objects will be then added to processed variable if the resulting string is not empty,
		// meaning it has been processed correctly.
		processedRaw := make([]string, len(objs.Items))
		// Iterating over all the retrieved objects from the resource spec to get replicated in all the selected Namespaces:
		// in case of error during the create or update function, this will be appended to the list of errors.
		for i, o := range objs.Items {
			obj := o
			obj.SetNamespace(ns.Name)
			obj.SetOwnerReferences(nil)

			wg.Add(1)

			go func(index int, obj unstructured.Unstructured) {
				defer wg.Done()

				kv := keysAndValues
				kv = append(kv, "resource", fmt.Sprintf("%s/%s", obj.GetNamespace(), obj.GetNamespace()))

				replicatedItem := &capsulev1beta2.ObjectReferenceStatus{}
				replicatedItem.Name = obj.GetName()
				replicatedItem.Kind = obj.GetKind()
				replicatedItem.APIVersion = obj.GetAPIVersion()
				replicatedItem.Type = meta.ReadyCondition
				replicatedItem.Owner = owner

				if ns != nil {
					replicatedItem.Namespace = ns.Name
				}

				if opErr := r.createOrPatch(ctx, c, &obj, objLabels, objAnnotations, spec.Ignore); opErr != nil {
					log.Error(opErr, "unable to sync namespacedItems", kv...)
					errorsChan <- opErr

					replicatedItem.Status = metav1.ConditionFalse
					replicatedItem.Message = opErr.Error()
				} else {
					replicatedItem.Status = metav1.ConditionTrue
				}

				log.Info("resource has been replicated", kv...)

				processedRaw[index] = replicatedItem.String()
			}(i, obj)
		}

		wg.Wait()
		close(errorsChan)

		for err := range errorsChan {
			if err != nil {
				syncErr = errors.Join(syncErr, err)
			}
		}

		for _, p := range processedRaw {
			if p == "" {
				continue
			}

			processed.Insert(p)
		}
	}

	for rawIndex, item := range spec.RawItems {
		template := string(item.Raw)

		t := fasttemplate.New(template, "{{ ", " }}")

		tContext := map[string]interface{}{
			"tenant.name": tnt.Name,
		}
		if ns != nil {
			tContext["namespace"] = ns.Name
		}

		tmplString := t.ExecuteString(tContext)

		obj, keysAndValues := unstructured.Unstructured{}, []interface{}{"index", rawIndex}

		if _, _, decodeErr := codecFactory.UniversalDeserializer().Decode([]byte(tmplString), nil, &obj); decodeErr != nil {
			log.Error(decodeErr, "unable to deserialize rawItem", keysAndValues...)

			syncErr = errors.Join(syncErr, decodeErr)

			continue
		}

		if ns != nil {
			obj.SetNamespace(ns.Name)
		}

		replicatedItem := &capsulev1beta2.ObjectReferenceStatus{}
		replicatedItem.Name = obj.GetName()
		replicatedItem.Kind = obj.GetKind()
		replicatedItem.APIVersion = obj.GetAPIVersion()
		replicatedItem.Type = meta.ReadyCondition
		replicatedItem.Owner = owner

		if ns != nil {
			replicatedItem.Namespace = ns.Name
		}

		if rawErr := r.createOrPatch(ctx, c, &obj, objLabels, objAnnotations, spec.Ignore); rawErr != nil {
			log.Info("unable to sync rawItem", keysAndValues...)

			replicatedItem.Status = metav1.ConditionFalse
			replicatedItem.Message = rawErr.Error()

			// In case of error processing an item in one of any selected Namespaces, storing it to report it lately
			// to the upper call to ensure a partial sync that will be fixed by a subsequent reconciliation.
			syncErr = errors.Join(syncErr, rawErr)
		} else {
			log.Info("resource has been replicated", keysAndValues...)

			replicatedItem.Status = metav1.ConditionTrue
		}

		processed.Insert(replicatedItem.String())
	}

	// Run Generators
	for generatorIndex, item := range spec.Generators {
		keysAndValues := []interface{}{"index", generatorIndex}

		log.V(5).Info("reconciling generator", keysAndValues...)

		objs, err := renderGeneratorItem(item, tmplContext)
		if err != nil {
			syncErr = errors.Join(syncErr, err)

			log.Error(err, "unable to deserialize rawItem", keysAndValues...)

			continue

		}

		log.V(5).Info("obtained objects", "items", len(objs))

		for _, obj := range objs {
			if ns != nil {
				obj.SetNamespace(ns.Name)
			}

			replicatedItem := &capsulev1beta2.ObjectReferenceStatus{}
			replicatedItem.Name = obj.GetName()
			replicatedItem.Kind = obj.GetKind()
			replicatedItem.APIVersion = obj.GetAPIVersion()
			replicatedItem.Type = meta.ReadyCondition
			replicatedItem.Owner = owner

			if ns != nil {
				replicatedItem.Namespace = ns.Name
			}

			if rawErr := r.createOrPatch(ctx, c, &obj, objLabels, objAnnotations, spec.Ignore); rawErr != nil {
				log.Info("unable to sync rawItem", keysAndValues...)

				replicatedItem.Status = metav1.ConditionFalse
				replicatedItem.Message = rawErr.Error()

				// In case of error processing an item in one of any selected Namespaces, storing it to report it lately
				// to the upper call to ensure a partial sync that will be fixed by a subsequent reconciliation.
				syncErr = errors.Join(syncErr, rawErr)
			} else {
				log.Info("resource has been replicated", keysAndValues...)

				replicatedItem.Status = metav1.ConditionTrue
			}

			processed.Insert(replicatedItem.String())
		}
	}

	return processed.List(), syncErr
}

func (r *Processor) createOrPatch(
	ctx context.Context,
	c client.Client,
	obj *unstructured.Unstructured,
	labels, annotations map[string]string,
	ignore []api.IgnoreRule,
) error {
	actual := &unstructured.Unstructured{}
	actual.SetGroupVersionKind(obj.GroupVersionKind())
	actual.SetNamespace(obj.GetNamespace())
	actual.SetName(obj.GetName())

	// Fetch current to have a stable mutate func input
	_ = c.Get(ctx, client.ObjectKeyFromObject(actual), actual) // ignore notfound here

	igPaths := matchIgnorePaths(ignore, obj.GetKind(), obj.GetAPIVersion())

	_, err := controllerutil.CreateOrPatch(ctx, c, actual, func() error {
		// Keep copies
		live := actual.DeepCopy() // current from cluster (may be empty)
		desired := obj.DeepCopy() // what we want

		// Merge controller-managed labels/annotations into desired
		mergeLabelsAnnotations(desired, labels, annotations)

		// Preserve ignored JSON pointers: copy live -> desired at those paths
		if len(igPaths) > 0 {
			preserveIgnoredPaths(desired.Object, live.Object, igPaths)
		}

		// Replace actual content with the prepared desired content
		uid := actual.GetUID()
		rv := actual.GetResourceVersion()

		actual.Object = desired.Object
		actual.SetUID(uid)
		actual.SetResourceVersion(rv)

		return nil
	})
	return err
}

func mergeLabelsAnnotations(u *unstructured.Unstructured, ls, as map[string]string) {
	lbl := u.GetLabels()
	if lbl == nil {
		lbl = map[string]string{}
	}
	for k, v := range ls {
		lbl[k] = v
	}
	u.SetLabels(lbl)

	ann := u.GetAnnotations()
	if ann == nil {
		ann = map[string]string{}
	}
	for k, v := range as {
		ann[k] = v
	}
	u.SetAnnotations(ann)
}

// jsonPointerGet returns (value, true) if JSON pointer p exists.
func jsonPointerGet(obj map[string]any, p string) (any, bool) {
	if p == "" || p == "/" {
		return obj, true
	}
	parts := strings.Split(p, "/")[1:]
	cur := any(obj)
	for _, raw := range parts {
		key := strings.ReplaceAll(strings.ReplaceAll(raw, "~1", "/"), "~0", "~")
		switch node := cur.(type) {
		case map[string]any:
			next, ok := node[key]
			if !ok {
				return nil, false
			}
			cur = next
		case []any:
			idx, err := strconv.Atoi(key)
			if err != nil || idx < 0 || idx >= len(node) {
				return nil, false
			}
			cur = node[idx]
		default:
			return nil, false
		}
	}
	return cur, true
}

func jsonPointerSet(obj map[string]any, p string, val any) error {
	if p == "" || p == "/" {
		return fmt.Errorf("cannot set root with pointer")
	}
	parts := strings.Split(p, "/")[1:]
	cur := obj
	for i, raw := range parts {
		key := strings.ReplaceAll(strings.ReplaceAll(raw, "~1", "/"), "~0", "~")
		last := i == len(parts)-1
		if last {
			cur[key] = val
			return nil
		}
		nxt, ok := cur[key]
		if !ok {
			n := map[string]any{}
			cur[key] = n
			cur = n
			continue
		}
		switch m := nxt.(type) {
		case map[string]any:
			cur = m
		default:
			n := map[string]any{}
			cur[key] = n
			cur = n
		}
	}
	return nil
}

func jsonPointerDelete(obj map[string]any, p string) error {
	if p == "" || p == "/" {
		return fmt.Errorf("cannot delete root with pointer")
	}
	parts := strings.Split(p, "/")[1:]
	cur := obj
	for i, raw := range parts {
		key := strings.ReplaceAll(strings.ReplaceAll(raw, "~1", "/"), "~0", "~")
		last := i == len(parts)-1
		if last {
			delete(cur, key)
			return nil
		}
		nxt, ok := cur[key]
		if !ok {
			return nil
		}
		m, ok := nxt.(map[string]any)
		if !ok {
			return nil
		}
		cur = m
	}
	return nil
}

func preserveIgnoredPaths(desired, live map[string]any, ptrs []string) {
	for _, p := range ptrs {
		if v, ok := jsonPointerGet(live, p); ok {
			_ = jsonPointerSet(desired, p, v)
		} else {
			_ = jsonPointerDelete(desired, p)
		}
	}
}

func matchIgnorePaths(rules []api.IgnoreRule, kind, apiver string) []string {
	var out []string

	for _, r := range rules {
		if r.Target.Kind != "" && r.Target.Kind != kind {
			continue
		}
		if r.Target.Version != "" && r.Target.Version != apiver {
			continue
		}
		out = append(out, r.Paths...)
	}
	return out
}

// createOrUpdate replicates the provided unstructured object to all the provided Namespaces:
// this function mimics the CreateOrUpdate, by retrieving the object to understand if it must be created or updated,
// along adding the additional metadata, if required.
//func (r *Processor) createOrUpdate(
//	ctx context.Context,
//	c client.Client,
//	obj *unstructured.Unstructured,
//	labels map[string]string,
//	annotations map[string]string,
//) (err error) {
//	actual, desired := &unstructured.Unstructured{}, obj.DeepCopy()
//
//	actual.SetAPIVersion(desired.GetAPIVersion())
//	actual.SetKind(desired.GetKind())
//	actual.SetNamespace(desired.GetNamespace())
//	actual.SetName(desired.GetName())
//
//	_, err = controllerutil.CreateOrUpdate(ctx, c, actual, func() error {
//		UID := actual.GetUID()
//		rv := actual.GetResourceVersion()
//		actual.SetUnstructuredContent(desired.Object)
//
//		combinedLabels := obj.GetLabels()
//		if combinedLabels == nil {
//			combinedLabels = make(map[string]string)
//		}
//
//		for key, value := range labels {
//			combinedLabels[key] = value
//		}
//
//		actual.SetLabels(combinedLabels)
//
//		combinedAnnotations := obj.GetAnnotations()
//		if combinedAnnotations == nil {
//			combinedAnnotations = make(map[string]string)
//		}
//
//		for key, value := range annotations {
//			combinedAnnotations[key] = value
//		}
//
//		actual.SetAnnotations(combinedAnnotations)
//		actual.SetResourceVersion(rv)
//		actual.SetUID(UID)
//
//		return nil
//	})
//
//	return err
//}
