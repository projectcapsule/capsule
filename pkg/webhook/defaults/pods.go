// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package defaults

import (
	"context"
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	schedulev1 "k8s.io/api/scheduling/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
	"github.com/clastix/capsule/pkg/webhook/utils"
)

func mutatePodDefaults(ctx context.Context, req admission.Request, c client.Client, decoder *admission.Decoder, recorder record.EventRecorder, namespace string) *admission.Response {
	var err error

	pod := &corev1.Pod{}
	if err = decoder.Decode(req, pod); err != nil {
		return utils.ErroredResponse(err)
	}

	pod.SetNamespace(namespace)

	var tnt *capsulev1beta2.Tenant

	tnt, err = utils.TenantByStatusNamespace(ctx, c, pod.Namespace)
	if err != nil {
		return utils.ErroredResponse(err)
	}

	if tnt == nil {
		return nil
	}

	allowed := tnt.Spec.PriorityClasses

	if allowed == nil || allowed.Default == "" {
		return nil
	}

	priorityClassPod := pod.Spec.PriorityClassName

	var mutate bool

	var cpc *schedulev1.PriorityClass
	// PriorityClass name is empty, if no GlobalDefault is set and no PriorityClass was given on pod
	if len(priorityClassPod) > 0 && priorityClassPod != allowed.Default {
		cpc, err = utils.GetPriorityClassByName(ctx, c, priorityClassPod)
		// Should not happen, since API already checks if PC present
		if err != nil {
			response := admission.Denied(NewPriorityClassError(priorityClassPod, err).Error())

			return &response
		}
	} else {
		mutate = true
	}

	if mutate = mutate || (utils.IsDefaultPriorityClass(cpc) && cpc.GetName() != allowed.Default); !mutate {
		return nil
	}

	pc, err := utils.GetPriorityClassByName(ctx, c, allowed.Default)
	if err != nil {
		return utils.ErroredResponse(fmt.Errorf("failed to assign tenant default Priority Class: %w", err))
	}

	pod.Spec.PreemptionPolicy = pc.PreemptionPolicy
	pod.Spec.Priority = &pc.Value
	pod.Spec.PriorityClassName = pc.Name
	// Marshal Pod
	marshaled, err := json.Marshal(pod)
	if err != nil {
		return utils.ErroredResponse(err)
	}

	recorder.Eventf(tnt, corev1.EventTypeNormal, "TenantDefault", "Assigned Tenant default Priority Class %s to %s/%s", allowed.Default, pod.Namespace, pod.Name)

	response := admission.PatchResponseFromRaw(req.Object.Raw, marshaled)

	return &response
}
