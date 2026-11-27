// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta1

import (
	"fmt"
	"os"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

func (in *Tenant) SetupWebhookWithManager(mgr manager.Manager) error {
	certData, err := os.ReadFile("/tmp/k8s-webhook-server/serving-certs/tls.crt")
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read webhook serving cert: %w", err)
	}
	if len(certData) == 0 {
		return nil
	}

	return ctrl.NewWebhookManagedBy(mgr, in).
		Complete()
}
