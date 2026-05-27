// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package admission

import (
	"gomodules.xyz/jsonpatch/v2"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func AccumulateAdmissionResponse(
	accumulated []jsonpatch.JsonPatchOperation,
	response *admission.Response,
) ([]jsonpatch.JsonPatchOperation, *admission.Response) {
	if response == nil {
		return accumulated, nil
	}

	// Denied or errored responses must stop immediately.
	if !response.Allowed {
		return accumulated, response
	}

	if len(response.Patches) > 0 {
		accumulated = append(accumulated, response.Patches...)
	}

	return accumulated, nil
}
