// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	"io/ioutil"

	ctrl "sigs.k8s.io/controller-runtime"
)

func (t *Tenant) SetupWebhookWithManager(mgr ctrl.Manager) error {
	certData, _ := ioutil.ReadFile("/tmp/k8s-webhook-server/serving-certs/tls.crt")
	if len(certData) == 0 {
		return nil
	}

	return ctrl.NewWebhookManagedBy(mgr).
		For(t).
		Complete()
}
