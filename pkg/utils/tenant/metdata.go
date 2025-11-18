// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	corev1 "k8s.io/api/core/v1"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func AddTenantLabelForNamespace(ns *corev1.Namespace, tnt *capsulev1beta2.Tenant) error {
	if ns.Labels == nil {
		ns.Labels = make(map[string]string)
	}

	ns.Labels[meta.TenantLabel] = tnt.GetName()

	return nil
}
