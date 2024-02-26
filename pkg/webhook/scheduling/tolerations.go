package scheduling

import (
	"context"
	"encoding/json"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/webhook/utils"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func mutateTenantTolerations(pod *corev1.Pod, tnt capsulev1beta2.Tenant, ctx context.Context, req admission.Request) *admission.Response {

	tolerations := tnt.Spec.PodOptions.Tolerations
	if tolerations == nil {
		return nil
	}

	pod.Spec.Tolerations = tolerations

	// Marshal Pod
	marshaled, err := json.Marshal(pod)
	if err != nil {
		return utils.ErroredResponse(err)
	}

	response := admission.PatchResponseFromRaw(req.Object.Raw, marshaled)

	return &response

}
