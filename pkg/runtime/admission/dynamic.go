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

	out := in // shallow copy (safe)

	cleanPath := normalizePath(webhookPath)
	if cleanPath == "" {
		return out
	}

	// URL mode
	if out.URL != nil && *out.URL != "" {
		u := strings.TrimRight(*out.URL, "/") + cleanPath
		out.URL = &u
		return out
	}

	// Service mode
	if out.Service != nil {
		svc := *out.Service // copy to avoid mutating original
		svc.Path = &cleanPath
		out.Service = &svc
	}

	return out
}
