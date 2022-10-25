// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"fmt"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
	"github.com/hashicorp/go-multierror"
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
)

const (
	finalizer = "capsule.clastix.io/resources"
)

type Processor struct {
	client client.Client
}

func (r *Processor) HandlePruning(ctx context.Context, current, desired sets.String) (updateStatus bool) {
	log := ctrllog.FromContext(ctx)

	diff := current.Difference(desired)
	// We don't want to trigger a reconciliation of the Status every time,
	// rather, only in case of a difference between the processed and the actual status.
	// This can happen upon the first reconciliation, or a removal, or a change, of a resource.
	updateStatus = diff.Len() > 0 || current.Len() != desired.Len()

	if diff.Len() > 0 {
		log.Info("starting processing pruning", "length", diff.Len())
	}

	// The outer resources must be removed, iterating over these to clean-up
	for item := range diff {
		or := capsulev1beta2.ObjectReferenceStatus{}
		if err := or.ParseFromString(item); err != nil {
			log.Error(err, "unable to parse resource to prune", "resource", item)

			continue
		}

		obj := unstructured.Unstructured{}
		obj.SetNamespace(or.Namespace)
		obj.SetName(or.Name)
		obj.SetGroupVersionKind(schema.FromAPIVersionAndKind(or.APIVersion, or.Kind))

		if err := r.client.Delete(ctx, &obj); err != nil {
			if apierr.IsNotFound(err) {
				// Object may have been already deleted, we can ignore this error
				continue
			}

			log.Error(err, "unable to prune resource", "resource", item)

			continue
		}

		log.Info("resource has been pruned", "resource", item)
	}

	return updateStatus
}

func (r *Processor) HandleSection(ctx context.Context, tnt capsulev1beta2.Tenant, allowCrossNamespaceSelection bool, tenantLabel string, resourceIndex int, spec capsulev1beta2.ResourceSpec) ([]string, error) {
	log := ctrllog.FromContext(ctx)

	var err error
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
	// Generating additional metadata
	objAnnotations, objLabels := map[string]string{}, map[string]string{}

	if spec.AdditionalMetadata != nil {
		objAnnotations = spec.AdditionalMetadata.Annotations
		objLabels = spec.AdditionalMetadata.Labels
	}

	objAnnotations[tenantLabel] = tnt.GetName()

	objLabels["capsule.clastix.io/resources"] = fmt.Sprintf("%d", resourceIndex)
	objLabels[tenantLabel] = tnt.GetName()
	// processed will contain the sets of resources replicated, both for the raw and the Namespaced ones:
	// these are required to perform a final pruning once the replication has been occurred.
	processed := sets.NewString()

	tntNamespaces := sets.NewString(tnt.Status.Namespaces...)

	syncErr := new(multierror.Error)

	for nsIndex, item := range spec.NamespacedItems {
		keysAndValues := []interface{}{"index", nsIndex, "namespace", item.Namespace}
		// A TenantResource is created by a TenantOwner, and potentially, they could point to a resource in a non-owned
		// Namespace: this must be blocked by checking it this is the case.
		if !allowCrossNamespaceSelection && !tntNamespaces.Has(item.Namespace) {
			log.Info("skipping processing of namespacedItem, referring a Namespace that is not part of the given Global", keysAndValues...)

			continue
		}
		// Namespaced Items are relying on selecting resources, rather than specifying a specific name:
		// creating it to get used by the client List action.
		itemSelector, selectorErr := metav1.LabelSelectorAsSelector(&item.Selector)
		if err != nil {
			log.Error(selectorErr, "cannot create Selector for namespacedItem", keysAndValues...)

			continue
		}

		objs := unstructured.UnstructuredList{}
		objs.SetGroupVersionKind(schema.FromAPIVersionAndKind(item.APIVersion, fmt.Sprintf("%sList", item.Kind)))

		if clientErr := r.client.List(ctx, &objs, client.InNamespace(item.Namespace), client.MatchingLabelsSelector{Selector: itemSelector}); clientErr != nil {
			log.Error(clientErr, "cannot retrieve object for namespacedItem", keysAndValues...)

			syncErr = multierror.Append(syncErr, clientErr)

			continue
		}

		multiErr := new(multierror.Group)
		// Iterating over all the retrieved objects from the resource spec to get replicated in all the selected Namespaces:
		// in case of error during the create or update function, this will be appended to the list of errors.
		for _, o := range objs.Items {
			obj := o

			multiErr.Go(func() error {
				nsItems, nsErr := r.createOrUpdate(ctx, &obj, objLabels, objAnnotations, namespaces)
				if nsErr != nil {
					log.Error(err, "unable to sync namespacedItems", keysAndValues...)

					return nsErr
				}

				processed.Insert(nsItems...)

				return nil
			})
		}

		if objsErr := multiErr.Wait(); objsErr != nil {
			syncErr = multierror.Append(syncErr, objsErr)
		}
	}

	codecFactory := serializer.NewCodecFactory(r.client.Scheme())

	for rawIndex, item := range spec.RawItems {
		obj, keysAndValues := unstructured.Unstructured{}, []interface{}{"index", rawIndex}

		if _, _, decodeErr := codecFactory.UniversalDeserializer().Decode(item.Raw, nil, &obj); decodeErr != nil {
			log.Error(decodeErr, "unable to deserialize rawItem", keysAndValues...)

			syncErr = multierror.Append(syncErr, decodeErr)

			continue
		}

		syncedRaw, rawErr := r.createOrUpdate(ctx, &obj, objLabels, objAnnotations, namespaces)
		if rawErr != nil {
			log.Info("unable to sync rawItem", keysAndValues...)
			// In case of error processing an item in one of any selected Namespaces, storing it to report it lately
			// to the upper call to ensure a partial sync that will be fixed by a subsequent reconciliation.
			syncErr = multierror.Append(syncErr, rawErr)
		} else {
			processed.Insert(syncedRaw...)
		}
	}

	return processed.List(), syncErr.ErrorOrNil()
}

// createOrUpdate replicates the provided unstructured object to all the provided Namespaces:
// this function mimics the CreateOrUpdate, by retrieving the object to understand if it must be created or updated,
// along adding the additional metadata, if required.
func (r *Processor) createOrUpdate(ctx context.Context, obj *unstructured.Unstructured, labels map[string]string, annotations map[string]string, namespaces corev1.NamespaceList) ([]string, error) {
	log := ctrllog.FromContext(ctx)

	errGroup := new(multierror.Group)

	var items []string

	for _, item := range namespaces.Items {
		ns := item.GetName()

		errGroup.Go(func() (err error) {
			actual, desired := obj.DeepCopy(), obj.DeepCopy()
			// Using a deferred function to properly log the results, and adding the item to the processed set.
			defer func() {
				keysAndValues := []interface{}{"resource", fmt.Sprintf("%s/%s", ns, desired.GetName())}

				if err != nil {
					log.Error(err, "unable to replicate resource", keysAndValues...)

					return
				}

				log.Info("resource has been replicated", keysAndValues...)

				replicatedItem := &capsulev1beta2.ObjectReferenceStatus{
					Name: obj.GetName(),
				}
				replicatedItem.Kind = obj.GetKind()
				replicatedItem.Namespace = ns
				replicatedItem.APIVersion = obj.GetAPIVersion()

				items = append(items, replicatedItem.String())
			}()

			actual.SetNamespace(ns)

			_, err = controllerutil.CreateOrUpdate(ctx, r.client, actual, func() error {
				UID := actual.GetUID()

				actual.SetUnstructuredContent(desired.Object)
				actual.SetNamespace(ns)
				actual.SetLabels(labels)
				actual.SetAnnotations(annotations)
				actual.SetResourceVersion("")
				actual.SetUID(UID)

				return nil
			})

			return
		})
	}
	// Wait returns *multierror.Error that implements stdlib error:
	// the nil check must be performed down here rather than at the caller level to avoid wrong casting.
	if err := errGroup.Wait(); err != nil {
		return items, err
	}

	return items, nil
}
