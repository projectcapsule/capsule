// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package generic

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
)

func allowTerminatingNamespaceDeletion(ctx context.Context, reader client.Reader, req admission.Request, response *admission.Response) *admission.Response {
	return ad.AllowTerminatingNamespaceDeletion(ctx, reader, req, response)
}
