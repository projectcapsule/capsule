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
	// whats the problem
	Client *admissionregistrationv1.WebhookClientConfig `json:"client"`
}

func DynamicWebhookURL(baseURL *string, webhookPath string) *string {
	cleanPath := normalizePath(webhookPath)
	if cleanPath == "" {
		if baseURL == nil || *baseURL == "" {
			return nil
		}

		u := *baseURL

		return &u
	}

	if baseURL == nil || *baseURL == "" {
		u := cleanPath

		return &u
	}

	base := strings.TrimRight(*baseURL, "/")

	if base == strings.TrimRight(cleanPath, "/") {
		u := cleanPath

		return &u
	}

	if strings.HasSuffix(base, cleanPath) {
		u := base

		return &u
	}

	u := base + cleanPath

	return &u
}

func DynamicClientWithPath(
	in admissionregistrationv1.WebhookClientConfig,
	webhookPath string,
) admissionregistrationv1.WebhookClientConfig {
	out := in

	if out.URL != nil {
		out.URL = DynamicWebhookURL(out.URL, webhookPath)

		return out
	}

	cleanPath := normalizePath(webhookPath)
	if cleanPath == "" {
		return out
	}

	if out.Service != nil {
		svc := *out.Service
		svc.Path = &cleanPath
		out.Service = &svc
	}

	return out
}
