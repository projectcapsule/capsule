package scheduling

import (
	"context"
	"encoding/json"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/webhook/utils"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func mutateTenantAffinity(pod *corev1.Pod, tnt capsulev1beta2.Tenant, ctx context.Context, req admission.Request) *admission.Response {

	affinity := tnt.Spec.PodOptions.Affinity
	if affinity == nil {
		return nil
	}

	pod.Spec.Affinity = &affinity

	// Marshal Pod
	marshaled, err := json.Marshal(pod)
	if err != nil {
		return utils.ErroredResponse(err)
	}

	response := admission.PatchResponseFromRaw(req.Object.Raw, marshaled)

	return &response

}
