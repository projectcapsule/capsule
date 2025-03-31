// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package globalquota

import (
	"context"
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/go-logr/logr"
	"github.com/projectcapsule/capsule/pkg/api"
	capsuleutils "github.com/projectcapsule/capsule/pkg/utils"
	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
	"github.com/projectcapsule/capsule/pkg/webhook/utils"
)

type statusHandler struct {
	log logr.Logger
}

func StatusHandler(log logr.Logger) capsulewebhook.Handler {
	return &statusHandler{log: log}
}

func (h *statusHandler) OnCreate(c client.Client, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return nil
	}
}

func (h *statusHandler) OnDelete(c client.Client, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return nil
	}
}

func (h *statusHandler) OnUpdate(c client.Client, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.calculate(ctx, c, decoder, recorder, req)
	}
}

func (h *statusHandler) calculate(ctx context.Context, c client.Client, decoder admission.Decoder, recorder record.EventRecorder, req admission.Request) *admission.Response {
	h.log.V(3).Info("loggign request", "REQUEST", req)

	return utils.ErroredResponse(fmt.Errorf("meowie"))

	// Decode the incoming object
	quota := &corev1.ResourceQuota{}
	if err := decoder.Decode(req, quota); err != nil {
		return utils.ErroredResponse(fmt.Errorf("failed to decode new ResourceQuota object: %w", err))
	}

	// Decode the old object (previous state before update)
	oldQuota := &corev1.ResourceQuota{}
	if err := decoder.DecodeRaw(req.OldObject, oldQuota); err != nil {
		return utils.ErroredResponse(fmt.Errorf("failed to decode old ResourceQuota object: %w", err))
	}

	h.log.V(3).Info("loggign request", "REQUEST", req)

	// Get Item within Resource Quota
	indexLabel := capsuleutils.GetGlobalResourceQuotaTypeLabel()
	item, ok := quota.GetLabels()[indexLabel]

	if !ok || item == "" {
		return nil
	}

	// Get Item within Resource Quota
	globalQuota, err := GetGlobalQuota(ctx, c, quota)
	if err != nil {
		return utils.ErroredResponse(err)
	}

	if globalQuota == nil {
		return nil
	}

	// Skip if quota not active
	if !globalQuota.Spec.Active {
		h.log.V(3).Info("GlobalQuota is not active", "quota", globalQuota.Name)

		return nil
	}

	// Skip Directly when the Status has not changed
	//if quota.Status.Hard == oldQuota.Status.Hard {
	//	return nil
	//}

	h.log.V(7).Info("selected quota", "quota", globalQuota.Name, "item", item)

	zero := resource.MustParse("0")

	// Fetch the latest tenant quota status
	tenantQuota, exists := globalQuota.Status.Quota[api.Name(item)]
	if !exists {
		h.log.V(5).Info("No quota entry found in tenant status; initializing", "item", api.Name(item))

		return nil
	}

	// Calculate remaining available space for this item
	availableSpace, _ := globalQuota.GetQuotaSpace(api.Name(item))
	if availableSpace == nil {
		availableSpace = corev1.ResourceList{}
	}

	// Fetch current used quota
	tenantUsed := tenantQuota.Used
	if tenantUsed == nil {
		tenantUsed = corev1.ResourceList{}
	}

	h.log.V(3).Info("Available space calculated", "space", availableSpace)

	// Process each resource and enforce allocation limits
	for resourceName, avail := range availableSpace {
		rlog := h.log.WithValues("resource", resourceName)

		rlog.V(3).Info("AVAILABLE", "avail", avail, "USED", tenantUsed[resourceName], "HARD", tenantQuota.Hard[resourceName])

		if avail.Cmp(zero) == 0 {
			rlog.V(3).Info("NO SPACE AVAILABLE")
			quota.Status.Hard[resourceName] = oldQuota.Status.Hard[resourceName]
			continue
		}

		// Get From the status whet's currently Used
		var globalUsage resource.Quantity
		if currentUsed, exists := tenantUsed[resourceName]; exists {
			globalUsage = currentUsed.DeepCopy()
		} else {
			globalUsage = resource.MustParse("0")
		}

		// Calculate Ingestion Size
		oldAllocated, exists := oldQuota.Status.Used[resourceName]
		if !exists {
			oldAllocated = resource.Quantity{} // default to zero
		}
		//
		//// Get the newly requested limit from the updated quota
		newRequested, exists := quota.Status.Used[resourceName]
		if !exists {
			quota.Status.Hard[resourceName] = resource.Quantity{}
			newRequested = oldAllocated.DeepCopy() // assume no change if missing
		}

		// Calculate Difference in Usage
		diff := newRequested.DeepCopy()
		diff.Sub(oldAllocated)

		rlog.V(3).Info("calculate ingestion", "diff", diff, "old", oldAllocated, "new", newRequested)

		// Compare how the newly ingested resources compare against empty resources
		// This is the quickest way to find out, how the status must be updated
		stat := diff.Cmp(zero)

		switch {
		// Resources are eual
		case stat == 0:
			continue
		// Resource Consumtion Increased
		case stat > 0:
			rlog.V(3).Info("increase")
			// Validate Space
			// Overprovisioned, allocate what's left
			if avail.Cmp(diff) < 0 {
				// Overprovisioned, allocate what's left
				globalUsage.Add(avail)

				// Here we cap overprovisioning, we add what's left to the
				// old status and update the item status. For the other operations that's ensured
				// because of this webhook.

				//oldAllocated.Add(avail)
				rlog.V(5).Info("PREVENT OVERPROVISING", "allocation", oldAllocated)
				quota.Status.Hard[resourceName] = oldQuota.Status.Hard[resourceName]

			} else {
				// Adding, since requested resources have space
				globalUsage.Add(diff)

				oldAllocated.Add(diff)
				quota.Status.Hard[resourceName] = oldAllocated

			}
		// Resource Consumption decreased
		default:
			rlog.V(3).Info("negate")
			// SUbstract Difference from available
			// Negative values also combine correctly with the Add() operation
			globalUsage.Add(diff)

			// Prevent Usage from going to negative
			stat := globalUsage.Cmp(zero)
			if stat < 0 {
				globalUsage = zero
			}
		}

		rlog.V(3).Info("caclulated total usage", "global", globalUsage, "diff", diff, "usage", avail, "hard", quota.Status.Hard[resourceName], "usage", quota.Status.Used[resourceName])
		tenantUsed[resourceName] = globalUsage
	}

	// Persist the updated usage in globalQuota.Status.Quota
	tenantQuota.Used = tenantUsed.DeepCopy()
	globalQuota.Status.Quota[api.Name(item)] = tenantQuota

	//  Ensure the status is updated immediately
	if err := c.Status().Update(ctx, globalQuota); err != nil {
		if apierrors.IsConflict(err) {
			h.log.Info("GlobalQuota status update conflict detected: object was updated concurrently", "error", err.Error())
		}

		h.log.Info("failed to update GlobalQuota status", "error", err.Error(), "global", globalQuota.Name, "quota", api.Name(item), "namespace", quota.Namespace)

		return utils.ErroredResponse(err)
	}

	marshaled, err := json.Marshal(quota)
	if err != nil {
		h.log.Error(err, "Failed to marshal mutated ResourceQuota object")

		return utils.ErroredResponse(err)
	}

	response := admission.PatchResponseFromRaw(req.Object.Raw, marshaled)

	return &response
}
