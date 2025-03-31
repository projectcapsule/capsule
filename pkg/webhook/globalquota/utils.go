// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package globalquota

import (
	"context"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsuleutils "github.com/projectcapsule/capsule/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func GetGlobalQuota(ctx context.Context, c client.Client, quota *corev1.ResourceQuota) (q *capsulev1beta2.GlobalResourceQuota, err error) {
	q = &capsulev1beta2.GlobalResourceQuota{}

	// Get Item within Resource Quota
	objectLabel, err := capsuleutils.GetTypeLabel(&capsulev1beta2.GlobalResourceQuota{})
	if err != nil {
		return
	}

	// Not a global quota resourcequota
	labels := quota.GetLabels()

	globalQuotaName, ok := labels[objectLabel]
	if !ok {
		return
	}

	if err = c.Get(ctx, types.NamespacedName{Name: globalQuotaName}, q); err != nil {
		return
	}

	return
}
