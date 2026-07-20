// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/util/retry"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	caperrors "github.com/projectcapsule/capsule/pkg/api/errors"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func (r *Manager) syncCustomResourceQuotaUsages(ctx context.Context, tenant *capsulev1beta2.Tenant) error {
	if tenant.DeletionTimestamp != nil {
		return nil
	}

	type resource struct {
		kind    string
		group   string
		version string
	}

	resourceList := make([]resource, 0, len(tenant.GetAnnotations()))

	for k := range tenant.GetAnnotations() {
		if !strings.HasPrefix(k, meta.ResourceQuotaAnnotationPrefix) {
			continue
		}

		parts := strings.Split(k, "/")
		if len(parts) != 2 {
			r.Log.V(4).Info("non well-formed Resource Limit annotation", "key", k)

			continue
		}

		parts = strings.Split(parts[1], "_")

		if len(parts) != 2 {
			r.Log.V(4).Info("non well-formed Resource Limit annotation, cannot retrieve version", "key", k)

			continue
		}

		groupKindParts := strings.Split(parts[0], ".")
		if len(groupKindParts) < 2 {
			r.Log.V(4).Info("non well-formed Resource Limit annotation, cannot retrieve kind and group", "key", k)

			continue
		}

		resourceList = append(resourceList, resource{
			kind:    groupKindParts[0],
			group:   strings.Join(groupKindParts[1:], "."),
			version: parts[1],
		})
	}

	if len(resourceList) == 0 {
		return nil
	}

	errGroup := new(errgroup.Group)

	usedMap := make(map[string]int)
	usedMapMu := sync.Mutex{}

	defer func() {
		for gvk, used := range usedMap {
			err := retry.RetryOnConflict(retry.DefaultBackoff, func() (retryErr error) {
				tnt := &capsulev1beta2.Tenant{}
				if retryErr = r.Get(ctx, types.NamespacedName{Name: tenant.GetName()}, tnt); retryErr != nil {
					return retryErr
				}

				if tnt.GetAnnotations() == nil {
					tnt.Annotations = make(map[string]string)
				}

				tnt.Annotations[capsulev1beta2.UsedAnnotationForResource(gvk)] = fmt.Sprintf("%d", used)

				return r.Update(ctx, tnt)
			})
			if err != nil {
				r.Log.Error(err, "cannot update custom Resource Quota", "GVK", gvk)
			}
		}
	}()

	dynamicClient := r.DynamicClient
	if dynamicClient == nil {
		dynamicClient = dynamic.NewForConfigOrDie(r.RESTConfig)
	}

	namespaces := readyTenantNamespaces(tenant)

	for _, item := range resourceList {
		res := item
		key := fmt.Sprintf("%s.%s_%s", res.kind, res.group, res.version)

		errGroup.Go(func() (scopeErr error) {
			var used int

			for _, ns := range namespaces {
				var list *unstructured.UnstructuredList

				list, scopeErr = dynamicClient.
					Resource(schema.GroupVersionResource{
						Group:    res.group,
						Version:  res.version,
						Resource: res.kind,
					}).
					Namespace(ns).
					List(ctx, metav1.ListOptions{})
				if scopeErr != nil {
					if caperrors.IgnoreGone(scopeErr) || apierrors.HasStatusCause(scopeErr, corev1.NamespaceTerminatingCause) {
						scopeErr = nil

						continue
					}

					return scopeErr
				}

				for _, k := range list.Items {
					if k.GetDeletionTimestamp() != nil {
						continue
					}

					used++
				}
			}

			usedMapMu.Lock()
			usedMap[key] += used
			usedMapMu.Unlock()

			return scopeErr
		})
	}

	if err := errGroup.Wait(); err != nil {
		return err
	}

	return nil
}
