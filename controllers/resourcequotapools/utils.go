// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package resourcequotapools

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsuleutils "github.com/projectcapsule/capsule/pkg/utils"
)

// Get all matching namespaces (just names)
func getMatchingGlobalQuotaNamespacesByName(
	ctx context.Context,
	c client.Client,
	quota *capsulev1beta2.ResourceQuotaPool,
) (nsNames []string, err error) {
	namespaces, err := getMatchingGlobalQuotaNamespaces(ctx, c, quota)
	if err != nil {
		return
	}

	nsNames = make([]string, 0, len(namespaces))
	for _, ns := range namespaces {
		nsNames = append(nsNames, ns.Name)
	}

	return
}

// Get all matching namespaces
func getMatchingGlobalQuotaNamespaces(
	ctx context.Context,
	c client.Client,
	quota *capsulev1beta2.ResourceQuotaPool,
) (namespaces []corev1.Namespace, err error) {
	// Collect Namespaces (Matching)
	namespaces = make([]corev1.Namespace, 0)
	seenNamespaces := make(map[string]struct{})

	// Get Item within Resource Quota
	objectLabel, err := capsuleutils.GetTypeLabel(&capsulev1beta2.Tenant{})
	if err != nil {
		return
	}

	for _, selector := range quota.Spec.Selectors {
		selected, err := selector.GetMatchingNamespaces(ctx, c)
		if err != nil {
			continue
		}

		for _, ns := range selected {
			// Skip if namespace is being deleted
			if !ns.ObjectMeta.DeletionTimestamp.IsZero() {
				continue
			}

			if _, exists := seenNamespaces[ns.Name]; exists {
				continue // Skip duplicates
			}

			if selector.MustTenantNamespace {
				if _, ok := ns.Labels[objectLabel]; !ok {
					continue
				}
			}

			seenNamespaces[ns.Name] = struct{}{}
			namespaces = append(namespaces, ns)
		}
	}

	return
}

// Returns for an item it's name as Kubernetes object
func resourceQuotaItemName(quota *capsulev1beta2.ResourceQuotaPool) string {
	// Generate a name using the tenant name and item name
	return fmt.Sprintf("capsule-pool-%s", quota.Name)
}

//func (r *Manager) emitEvent(object runtime.Object, namespace string, res controllerutil.OperationResult, msg string, err error) {
//	eventType := corev1.EventTypeNormal
//
//	if err != nil {
//		eventType = corev1.EventTypeWarning
//		res = "Error"
//	}
//
//	r.Recorder.AnnotatedEventf(object, map[string]string{"OperationResult": string(res)}, eventType, namespace, msg)
//}
