// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"fmt"
	"strings"

	"golang.org/x/sync/errgroup"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/util/retry"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

func (r *Manager) syncCustomResourceQuotaUsages(ctx context.Context, tenant *capsulev1beta2.Tenant) error {
	type resource struct {
		kind    string
		group   string
		version string
	}
	//nolint:prealloc
	var resourceList []resource

	for k := range tenant.GetAnnotations() {
		if !strings.HasPrefix(k, capsulev1beta2.ResourceQuotaAnnotationPrefix) {
			continue
		}

		parts := strings.Split(k, "/")
		if len(parts) != 2 {
			r.Log.Info("non well-formed Resource Limit annotation", "key", k)

			continue
		}

		parts = strings.Split(parts[1], "_")

		if len(parts) != 2 {
			r.Log.Info("non well-formed Resource Limit annotation, cannot retrieve version", "key", k)

			continue
		}

		groupKindParts := strings.Split(parts[0], ".")
		if len(groupKindParts) < 2 {
			r.Log.Info("non well-formed Resource Limit annotation, cannot retrieve kind and group", "key", k)

			continue
		}

		resourceList = append(resourceList, resource{
			kind:    groupKindParts[0],
			group:   strings.Join(groupKindParts[1:], "."),
			version: parts[1],
		})
	}

	errGroup := new(errgroup.Group)

	usedMap := make(map[string]int)

	defer func() {
		for gvk, used := range usedMap {
			err := retry.RetryOnConflict(retry.DefaultBackoff, func() (retryErr error) {
				tnt := &capsulev1beta2.Tenant{}
				if retryErr = r.Get(ctx, types.NamespacedName{Name: tenant.GetName()}, tnt); retryErr != nil {
					return
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

	for _, item := range resourceList {
		res := item

		errGroup.Go(func() (scopeErr error) {
			dynamicClient := dynamic.NewForConfigOrDie(r.RESTConfig)

			for _, ns := range tenant.Status.Namespaces {
				var list *unstructured.UnstructuredList

				list, scopeErr = dynamicClient.Resource(schema.GroupVersionResource{Group: res.group, Version: res.version, Resource: res.kind}).List(ctx, metav1.ListOptions{
					FieldSelector: fmt.Sprintf("metadata.namespace==%s", ns),
				})
				if scopeErr != nil {
					return scopeErr
				}

				key := fmt.Sprintf("%s.%s_%s", res.kind, res.group, res.version)

				if _, ok := usedMap[key]; !ok {
					usedMap[key] = 0
				}

				var used int

				for _, k := range list.Items {
					if k.GetDeletionTimestamp() != nil {
						continue
					}

					used++
				}

				usedMap[key] += used
			}

			return
		})
	}

	if err := errGroup.Wait(); err != nil {
		return err
	}

	return nil
}
