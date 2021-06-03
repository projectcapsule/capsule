// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package webhook

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

type Func func(ctx context.Context, req admission.Request) admission.Response

type Handler interface {
	OnCreate(client client.Client, decoder *admission.Decoder) Func
	OnDelete(client client.Client, decoder *admission.Decoder) Func
	OnUpdate(client client.Client, decoder *admission.Decoder) Func
}
