// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package admission

import (
	"strings"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"

	"github.com/projectcapsule/capsule/pkg/api/meta"
)

// +kubebuilder:object:generate=true
type DynamicAdmissionConfig struct {
	// Name the Admission Webhook
	Name meta.RFC1123Name `json:"name,omitempty"`
	// Labels added to the Admission Webhook
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
	// Annotations added to the Admission Webhook
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
	// From the upstram struct
	Client admissionregistrationv1.WebhookClientConfig `json:"client"`
}

func DynamicClientWithPath(
	in admissionregistrationv1.WebhookClientConfig,
	webhookPath string,
) admissionregistrationv1.WebhookClientConfig {
	out := in

	cleanPath := normalizePath(webhookPath)
	if cleanPath == "" {
		return out
	}

	if out.URL != nil && *out.URL != "" {
		base := strings.TrimRight(*out.URL, "/")

		if base == strings.TrimRight(cleanPath, "/") {
			u := cleanPath
			out.URL = &u

			return out
		}

		if strings.HasSuffix(base, cleanPath) {
			u := base
			out.URL = &u

			return out
		}

		u := base + cleanPath
		out.URL = &u

		return out
	}

	if out.Service != nil {
		svc := *out.Service
		svc.Path = &cleanPath
		out.Service = &svc
	}

	return out
}
