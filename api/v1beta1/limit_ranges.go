// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package v1beta1

import corev1 "k8s.io/api/core/v1"

type LimitRangesSpec struct {
	Items []corev1.LimitRangeSpec `json:"items,omitempty"`
}
