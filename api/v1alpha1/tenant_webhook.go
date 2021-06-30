// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// log is for logging in this package.
var tenantlog = logf.Log.WithName("tenant-resource")

func (t *Tenant) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(t).
		Complete()
}

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
