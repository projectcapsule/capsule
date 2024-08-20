// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package defaults

import (
	"context"
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	schedulev1 "k8s.io/api/scheduling/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/webhook/utils"
)

func mutatePodDefaults(ctx context.Context, req admission.Request, c client.Client, decoder admission.Decoder, recorder record.EventRecorder, namespace string) *admission.Response {
	var pod corev1.Pod
	if err := decoder.Decode(req, &pod); err != nil {
		return utils.ErroredResponse(err)
	}

	pod.SetNamespace(namespace)

	tnt, tErr := utils.TenantByStatusNamespace(ctx, c, pod.Namespace)
	if tErr != nil {
		return utils.ErroredResponse(tErr)
	} else if tnt == nil {
		return nil
	}

	var err error

	pcMutated, pcErr := handlePriorityClassDefault(ctx, c, tnt.Spec.PriorityClasses, &pod)
	if pcErr != nil {
		return utils.ErroredResponse(pcErr)
	} else if pcMutated {
		defer func() {
			if err == nil {
				recorder.Eventf(tnt, corev1.EventTypeNormal, "TenantDefault", "Assigned Tenant default Priority Class %s to %s/%s", tnt.Spec.PriorityClasses.Default, pod.Namespace, pod.Name)
			}
		}()
	}

	rcMutated := handleRuntimeClassDefault(tnt.Spec.RuntimeClasses, &pod)
	if rcMutated {
		defer func() {
			if err == nil {
				recorder.Eventf(tnt, corev1.EventTypeNormal, "TenantDefault", "Assigned Tenant default Runtime Class %s to %s/%s", tnt.Spec.RuntimeClasses.Default, pod.Namespace, pod.Name)
			}
		}()
	}

	if !rcMutated && !pcMutated {
		return nil
	}

	var marshaled []byte

	if marshaled, err = json.Marshal(pod); err != nil {
		return utils.ErroredResponse(err)
	}

	return ptr.To(admission.PatchResponseFromRaw(req.Object.Raw, marshaled))
}

func handleRuntimeClassDefault(allowed *api.DefaultAllowedListSpec, pod *corev1.Pod) (mutated bool) {
	if allowed == nil || allowed.Default == "" {
		return false
	}

	runtimeClass := pod.Spec.RuntimeClassName

	switch {
	case allowed.Default == "":
		return false
	case runtimeClass != nil && *runtimeClass != "":
		return false
	case runtimeClass != nil && *runtimeClass != allowed.Default:
		return false
	default:
		pod.Spec.RuntimeClassName = &allowed.Default

		return true
	}
}

func handlePriorityClassDefault(ctx context.Context, c client.Client, allowed *api.DefaultAllowedListSpec, pod *corev1.Pod) (mutated bool, err error) {
	if allowed == nil || allowed.Default == "" {
		return false, nil
	}

	priorityClassPod := pod.Spec.PriorityClassName

	var cpc *schedulev1.PriorityClass
	// PriorityClass name is empty, if no GlobalDefault is set and no PriorityClass was given on pod
	if len(priorityClassPod) > 0 && priorityClassPod != allowed.Default {
		cpc, err = utils.GetPriorityClassByName(ctx, c, priorityClassPod)
		// Should not happen, since API already checks if PC present
		if err != nil {
			return false, NewPriorityClassError(priorityClassPod, err)
		}
	} else {
		mutated = true
	}

	if mutated = mutated || (utils.IsDefaultPriorityClass(cpc) && cpc.GetName() != allowed.Default); !mutated {
		return false, nil
	}

	pc, err := utils.GetPriorityClassByName(ctx, c, allowed.Default)
	if err != nil {
		return false, fmt.Errorf("failed to assign tenant default Priority Class: %w", err)
	}

	pod.Spec.PreemptionPolicy = pc.PreemptionPolicy
	pod.Spec.Priority = &pc.Value
	pod.Spec.PriorityClassName = pc.Name

	return true, nil
}
