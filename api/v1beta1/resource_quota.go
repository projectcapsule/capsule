// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package v1beta1

import corev1 "k8s.io/api/core/v1"

type ResourceQuotaSpec struct {
	Items []corev1.ResourceQuotaSpec `json:"items,omitempty"`
}
