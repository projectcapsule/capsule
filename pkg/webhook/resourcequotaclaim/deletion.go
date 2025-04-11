// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package globalquota

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/go-logr/logr"
	"github.com/projectcapsule/capsule/pkg/api"
	capsuleutils "github.com/projectcapsule/capsule/pkg/utils"
	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
	"github.com/projectcapsule/capsule/pkg/webhook/utils"
)

type deletionHandler struct {
	log logr.Logger
}

func DeletionHandler(log logr.Logger) capsulewebhook.Handler {
	return &deletionHandler{log: log}
}

func (h *deletionHandler) OnCreate(c client.Client, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return nil
	}
}

// Substract a ResourceQuota (Usage) when it's deleted
// In normal operations this covers the case, when a namespace no longer get's selected and therefor
// The quota is being terminated /Maybe not working on status subresource
func (h *deletionHandler) OnDelete(c client.Client, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		h.log.V(5).Info("loggign request", "REQUEST", req)

		// Decode the incoming object (Always old object)
		quota := &corev1.ResourceQuota{}
		if err := decoder.DecodeRaw(req.OldObject, quota); err != nil {
			return utils.ErroredResponse(fmt.Errorf("failed to decode new ResourceQuota object: %w", err))
		}

		// Get Item within Resource Quota
		indexLabel := capsuleutils.GetGlobalResourceQuotaTypeLabel()
		item, ok := quota.GetLabels()[indexLabel]

		if !ok || item == "" {
			return nil
		}

		// Get Item within Resource Quota
		globalQuota, err := GetGlobalQuota(ctx, c, quota)
		// Just delete the quopta when the globalquota was delete
		if apierrors.IsNotFound(err) {
			return nil
		}

		if err != nil {
			return utils.ErroredResponse(err)
		}

		if globalQuota == nil {
			return nil
		}

		zero := resource.MustParse("0")

		// Use retry to handle concurrent updates
		err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			// Re-fetch the tenant to get the latest status
			if err := c.Get(ctx, client.ObjectKey{Name: globalQuota.Name}, globalQuota); err != nil {
				h.log.Error(err, "Failed to fetch globalquota during retry", "quota", globalQuota.Name)

				return err
			}
			// Fetch the latest tenant quota status
			tenantQuota, exists := globalQuota.Status.Quota[api.Name(item)]
			if !exists {
				h.log.V(5).Info("No quota entry found in tenant status; initializing", "item", api.Name(item))

				return nil
			}

			// Fetch current used quota
			tenantUsed := tenantQuota.Used
			if tenantUsed == nil {
				tenantUsed = corev1.ResourceList{}
			}

			// Remove all resources from the used property on the global quota
			for resourceName, used := range quota.Status.Used {
				rlog := h.log.WithValues("resource", resourceName)

				// Get From the status whet's currently Used
				var globalUsage resource.Quantity
				if currentUsed, exists := tenantUsed[resourceName]; exists {
					globalUsage = currentUsed.DeepCopy()
				} else {
					continue
				}

				// Remove
				globalUsage.Sub(used)

				// Avoid being below 0 (negative)
				stat := globalUsage.Cmp(zero)
				if stat < 0 {
					globalUsage = zero
				}

				rlog.V(7).Info("decreasing global usage", "decrease", used, "status", globalUsage)

				tenantUsed[resourceName] = globalUsage

			}

			h.log.V(7).Info("calculated status", "used", tenantUsed)

			// Persist the updated usage in globalQuota.Status.Qcuota
			globalQuota.Status.Quota[api.Name(item)].Used = tenantUsed.DeepCopy()

			//  Ensure the status is updated immediately
			if err := c.Status().Update(ctx, globalQuota); err != nil {
				h.log.Info("Failed to update GlobalQuota status", "error", err.Error())

				return fmt.Errorf("failed to update GlobalQuota status: %w", err)
			}

			return nil
		})

		if err != nil {
			h.log.Error(err, "Failed to process ResourceQuota update", "quota", quota.Name)

			return utils.ErroredResponse(err)
		}

		return nil
	}
}

func (h *deletionHandler) OnUpdate(c client.Client, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return nil
	}
}
