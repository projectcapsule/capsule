// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cfg

import (
	"testing"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	runtimeadmission "github.com/projectcapsule/capsule/pkg/runtime/admission"
)

func TestValidateAdmissionClients(t *testing.T) {
	t.Parallel()

	webhookURL := "https://capsule.example.com"
	service := &admissionregistrationv1.ServiceReference{
		Name:      "capsule-webhook-service",
		Namespace: "capsule-system",
	}

	tests := []struct {
		name    string
		config  capsulev1beta2.DynamicAdmission
		wantErr bool
	}{
		{name: "not configured"},
		{
			name: "validating url",
			config: capsulev1beta2.DynamicAdmission{
				Validating: dynamicValidatingConfig(&admissionregistrationv1.WebhookClientConfig{URL: &webhookURL}),
			},
		},
		{
			name: "mutating service",
			config: capsulev1beta2.DynamicAdmission{
				Mutating: dynamicMutatingConfig(&admissionregistrationv1.WebhookClientConfig{Service: service}),
			},
		},
		{
			name: "validating missing client",
			config: capsulev1beta2.DynamicAdmission{
				Validating: dynamicValidatingConfig(nil),
			},
			wantErr: true,
		},
		{
			name: "mutating has neither",
			config: capsulev1beta2.DynamicAdmission{
				Mutating: dynamicMutatingConfig(&admissionregistrationv1.WebhookClientConfig{}),
			},
			wantErr: true,
		},
		{
			name: "mutating has both",
			config: capsulev1beta2.DynamicAdmission{
				Mutating: dynamicMutatingConfig(&admissionregistrationv1.WebhookClientConfig{
					URL:     &webhookURL,
					Service: service,
				}),
			},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := validateAdmissionClients(test.config)
			if (err != nil) != test.wantErr {
				t.Fatalf("validateAdmissionClients() error = %v, wantErr %t", err, test.wantErr)
			}
		})
	}
}

func dynamicValidatingConfig(
	client *admissionregistrationv1.WebhookClientConfig,
) *capsulev1beta2.DynamicValidatingAdmissionConfig {
	return &capsulev1beta2.DynamicValidatingAdmissionConfig{
		DynamicAdmissionConfig: runtimeadmission.DynamicAdmissionConfig{Client: client},
	}
}

func dynamicMutatingConfig(
	client *admissionregistrationv1.WebhookClientConfig,
) *capsulev1beta2.DynamicMutatingAdmissionConfig {
	return &capsulev1beta2.DynamicMutatingAdmissionConfig{
		DynamicAdmissionConfig: runtimeadmission.DynamicAdmissionConfig{Client: client},
	}
}
