// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package admission

import (
	"encoding/json"
	"strings"

	admissionv1 "k8s.io/api/admissionregistration/v1"

	"github.com/projectcapsule/capsule/pkg/runtime/replication"
)

const (
	maxTriggerGroups            = 128
	maxTriggerRegistrationBytes = 512 * 1024
	maxCombinedWebhookBytes     = 1024 * 1024
)

// Notification configuration must never crowd enforcement hooks out of the
// shared Kubernetes object. Input is sorted for stable admission publication;
// ordinary configured webhooks are retained regardless of the trigger budget.
func boundTriggerRegistrations(hooks []admissionv1.ValidatingWebhook) ([]admissionv1.ValidatingWebhook, int, error) {
	triggerCount := 0

	for _, hook := range hooks {
		if isTriggerRegistration(hook) {
			triggerCount++
		}
	}

	if triggerCount == 0 {
		return hooks, 0, nil
	}

	legacyBytes := 0

	for _, hook := range hooks {
		if isTriggerRegistration(hook) {
			continue
		}

		encoded, err := json.Marshal(hook)
		if err != nil {
			return nil, 0, err
		}

		legacyBytes += len(encoded)
	}

	budget := max(0, min(maxTriggerRegistrationBytes, maxCombinedWebhookBytes-legacyBytes))

	var groups, bytesUsed, omitted int

	out := make([]admissionv1.ValidatingWebhook, 0, len(hooks))

	for _, hook := range hooks {
		if !isTriggerRegistration(hook) {
			out = append(out, hook)

			continue
		}

		encoded, err := json.Marshal(hook)
		if err != nil {
			return nil, 0, err
		}

		if groups >= maxTriggerGroups || bytesUsed+len(encoded) > budget {
			omitted++

			continue
		}

		groups++
		bytesUsed += len(encoded)

		out = append(out, hook)
	}

	return out, omitted, nil
}

func isTriggerRegistration(hook admissionv1.ValidatingWebhook) bool {
	if hook.ClientConfig.Service != nil && hook.ClientConfig.Service.Path != nil {
		return strings.HasPrefix(*hook.ClientConfig.Service.Path, replication.TriggerPath)
	}

	return hook.ClientConfig.URL != nil && strings.Contains(*hook.ClientConfig.URL, replication.TriggerPath)
}
