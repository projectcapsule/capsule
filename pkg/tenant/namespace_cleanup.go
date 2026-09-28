// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"

	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var errCleanupNamespaceChanged = errors.New("cleanup namespace disappeared or changed identity")

// NamespacedCascadingCleanup completes cleanup of a terminating namespace after
// Pods have gone. The reader must bypass the informer cache. Namespace identity
// is checked after each nonempty LIST and before destructive writes; object preconditions
// protect replacements between those checks and the writes themselves.
func NamespacedCascadingCleanup(ctx context.Context, reader client.Reader, disco discovery.DiscoveryInterface, resourceCache NamespacedResourceCache, dyn dynamic.Interface, ns *corev1.Namespace) (bool, error) {
	if ns == nil || ns.UID == "" || ns.DeletionTimestamp == nil {
		return false, nil
	}

	check := func(ctx context.Context) error {
		current := &corev1.Namespace{}
		if err := reader.Get(ctx, client.ObjectKey{Name: ns.Name}, current); err != nil {
			if apierrors.IsNotFound(err) {
				return errCleanupNamespaceChanged
			}

			return err
		}

		if current.UID != ns.UID || current.DeletionTimestamp == nil || !reflect.DeepEqual(TenantOwnerReferences(current), TenantOwnerReferences(ns)) || TenanLabelValue(current) != TenanLabelValue(ns) {
			return errCleanupNamespaceChanged
		}

		return nil
	}
	if err := check(ctx); err != nil {
		if errors.Is(err, errCleanupNamespaceChanged) {
			return false, nil
		}

		return false, err
	}

	gvrs, err := resourceCache.Get(disco)
	if err != nil {
		return false, err
	}

	var (
		mu   sync.Mutex
		errs []error
	)

	cleanedAny := false
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(4)

	for _, gvr := range gvrs {
		if groupCtx.Err() != nil {
			break
		}
		// Pods are guarded by their own lifecycle and must never be force-cleaned.
		if gvr.Group == "" && gvr.Resource == "pods" {
			continue
		}

		group.Go(func() error {
			if err := groupCtx.Err(); err != nil {
				return err
			}

			cleaned, err := cleanupResourceType(groupCtx, dyn, gvr, ns.Name, check)
			if errors.Is(err, errCleanupNamespaceChanged) {
				return err
			}

			mu.Lock()
			defer mu.Unlock()

			cleanedAny = cleanedAny || cleaned

			if err != nil {
				errs = append(errs, fmt.Errorf("process %s in namespace %q: %w", gvr, ns.Name, err))
			}

			return nil
		})
	}

	stopErr := group.Wait()
	if errors.Is(stopErr, errCleanupNamespaceChanged) {
		return cleanedAny, nil
	}

	if ctx.Err() != nil {
		return cleanedAny, ctx.Err()
	}

	return cleanedAny, errors.Join(errs...)
}

func cleanupResourceType(ctx context.Context, dyn dynamic.Interface, gvr schema.GroupVersionResource, namespace string, check func(context.Context) error) (bool, error) {
	resources := dyn.Resource(gvr).Namespace(namespace)

	list, err := resources.List(ctx, metav1.ListOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) || apierrors.IsMethodNotSupported(err) {
			return false, nil
		}

		return false, fmt.Errorf("list: %w", err)
	}

	if len(list.Items) == 0 {
		return false, nil
	}
	// A LIST can return objects from a replacement namespace, even if the
	// namespace existed with the expected UID before the request.
	if err := check(ctx); err != nil {
		return false, err
	}

	cleaned := false

	var errs []error

	for i := range list.Items {
		if i > 0 {
			if err := check(ctx); err != nil {
				return cleaned, err
			}
		}

		changed, err := cleanupNamespacedObject(ctx, resources, &list.Items[i], check)
		cleaned = cleaned || changed

		if err != nil {
			if errors.Is(err, errCleanupNamespaceChanged) || ctx.Err() != nil {
				return cleaned, err
			}

			errs = append(errs, err)
		}
	}

	return cleaned, errors.Join(errs...)
}

func cleanupNamespacedObject(ctx context.Context, resources dynamic.ResourceInterface, obj *unstructured.Unstructured, check func(context.Context) error) (bool, error) {
	uid := obj.GetUID()
	if uid == "" {
		return false, fmt.Errorf("refusing cleanup of %q without UID", obj.GetName())
	}

	changed := false

	if obj.GetDeletionTimestamp() == nil {
		if err := resources.Delete(ctx, obj.GetName(), metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}}); err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}

			return false, fmt.Errorf("delete %q: %w", obj.GetName(), err)
		}

		changed = true
	}

	if len(obj.GetFinalizers()) == 0 {
		return changed, nil
	}
	// Deletion changes resourceVersion. Read the exact object again, and only
	// remove the finalizers observed on that version of the original object.
	current, err := resources.Get(ctx, obj.GetName(), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return changed, nil
	}

	if err != nil {
		return changed, fmt.Errorf("get %q before clearing finalizers: %w", obj.GetName(), err)
	}

	if current.GetUID() != uid || len(current.GetFinalizers()) == 0 {
		return changed, nil
	}

	if current.GetResourceVersion() == "" {
		return changed, fmt.Errorf("refusing finalizer cleanup of %q without resourceVersion", obj.GetName())
	}

	if err := check(ctx); err != nil {
		return changed, err
	}

	patch, err := json.Marshal(map[string]any{"metadata": map[string]any{
		"uid": uid, "resourceVersion": current.GetResourceVersion(), "finalizers": []string{},
	}})
	if err != nil {
		return changed, err
	}

	if _, err := resources.Patch(ctx, obj.GetName(), types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
		if apierrors.IsNotFound(err) {
			return changed, nil
		}

		return changed, fmt.Errorf("clear finalizers on %q: %w", obj.GetName(), err)
	}

	return true, nil
}
