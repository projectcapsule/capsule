// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package serviceaccounts

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	serviceaccountindexer "github.com/projectcapsule/capsule/pkg/runtime/indexers/serviceaccount"
)

// ReferenceProtection prevents deletion of execution identities which are
// still required by replication resources or an unexpired BreakRequest.
func ReferenceProtection() handlers.Handler {
	return &referenceProtection{}
}

type referenceProtection struct{}

func (*referenceProtection) OnCreate(
	client.Client,
	client.Reader,
	admission.Decoder,
	events.EventRecorder,
) handlers.Func {
	return allowServiceAccountReferenceOperation
}

func (*referenceProtection) OnUpdate(
	client.Client,
	client.Reader,
	admission.Decoder,
	events.EventRecorder,
) handlers.Func {
	return allowServiceAccountReferenceOperation
}

func (*referenceProtection) OnDelete(
	c client.Client,
	_ client.Reader,
	_ admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		key := serviceaccountindexer.ReferenceKey(req.Namespace, req.Name)
		if key == "" {
			return handlers.ErroredResponse(fmt.Errorf("ServiceAccount deletion request has an empty namespace or name"))
		}

		requests := &capsulev1beta2.BreakRequestList{}
		if err := c.List(ctx, requests, client.MatchingFields{
			serviceaccountindexer.ReferenceFieldName: key,
		}); err != nil {
			return handlers.ErroredResponse(fmt.Errorf("listing BreakRequests for ServiceAccount %s: %w", key, err))
		}

		for index := range requests.Items {
			request := &requests.Items[index]
			if request.Status.Phase != capsulev1beta2.RequestPhaseExpired {
				return ad.Denyf(
					"ServiceAccount %s cannot be deleted because it is used by unexpired BreakRequest %s/%s",
					key,
					request.Namespace,
					request.Name,
				)
			}
		}

		globalResources := &capsulev1beta2.GlobalTenantResourceList{}
		if err := c.List(ctx, globalResources, client.MatchingFields{
			serviceaccountindexer.ReferenceFieldName: key,
		}); err != nil {
			return handlers.ErroredResponse(fmt.Errorf("listing GlobalTenantResources for ServiceAccount %s: %w", key, err))
		}

		if len(globalResources.Items) > 0 {
			return ad.Denyf(
				"ServiceAccount %s cannot be deleted because it is used by GlobalTenantResource %s",
				key,
				globalResources.Items[0].Name,
			)
		}

		tenantResources := &capsulev1beta2.TenantResourceList{}
		if err := c.List(ctx, tenantResources, client.MatchingFields{
			serviceaccountindexer.ReferenceFieldName: key,
		}); err != nil {
			return handlers.ErroredResponse(fmt.Errorf("listing TenantResources for ServiceAccount %s: %w", key, err))
		}

		if len(tenantResources.Items) > 0 {
			resource := tenantResources.Items[0]

			return ad.Denyf(
				"ServiceAccount %s cannot be deleted because it is used by TenantResource %s/%s",
				key,
				resource.Namespace,
				resource.Name,
			)
		}

		return nil
	}
}

func allowServiceAccountReferenceOperation(context.Context, admission.Request) *admission.Response {
	return nil
}
