// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package pod

import (
	"context"
	"fmt"
	"net/http"
	"slices"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	evt "github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/runtime/workloads"
)

type qosHandler struct {
	configuration configuration.Configuration
}

func QoSClass(configuration configuration.Configuration) handlers.TypedHandlerWithTenantWithRuleset[*corev1.Pod] {
	return &qosHandler{
		configuration: configuration,
	}
}

func (h *qosHandler) OnCreate(
	_ client.Client,
	_ client.Reader,
	pod *corev1.Pod,
	_ admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
	ruleBlocks []*rules.NamespaceRuleBodyNamespace,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.validate(req, pod, tnt, recorder, ruleBlocks)
	}
}

func (h *qosHandler) OnUpdate(
	_ client.Client,
	_ client.Reader,
	_ *corev1.Pod,
	pod *corev1.Pod,
	_ admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
	ruleBlocks []*rules.NamespaceRuleBodyNamespace,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.validate(req, pod, tnt, recorder, ruleBlocks)
	}
}

func (h *qosHandler) OnDelete(
	client.Client,
	client.Reader,
	*corev1.Pod,
	admission.Decoder,
	events.EventRecorder,
	*capsulev1beta2.Tenant,
	[]*rules.NamespaceRuleBodyNamespace,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *qosHandler) validate(
	req admission.Request,
	pod *corev1.Pod,
	tnt *capsulev1beta2.Tenant,
	recorder events.EventRecorder,
	ruleBlocks []*rules.NamespaceRuleBodyNamespace,
) *admission.Response {
	if pod == nil {
		resp := admission.Errored(http.StatusInternalServerError, fmt.Errorf("pod is nil"))

		return &resp
	}

	if len(ruleBlocks) == 0 {
		return nil
	}

	qosClass := workloads.GetPodQoSClass(pod)

	evaluation, err := evaluateQoSClass(ruleBlocks, qosClass)
	if err != nil {
		resp := admission.Errored(http.StatusInternalServerError, err)

		return &resp
	}

	if evaluation == nil {
		return nil
	}

	warnings := make([]string, 0, len(evaluation.Audits))

	for _, audit := range evaluation.Audits {
		msg := fmt.Sprintf(
			"pod %q uses QoS class %q and matched audit QoS rule",
			pod.Name,
			qosClass,
		)

		h.auditWithEvent(recorder, tnt, pod, msg)
		warnings = append(warnings, msg)

		_ = audit
	}

	if evaluation.Decision == nil {
		if len(warnings) > 0 {
			resp := admission.Allowed("QoS class audited")
			resp.Warnings = append(resp.Warnings, warnings...)

			return &resp
		}

		return nil
	}

	switch evaluation.Decision.Action {
	case rules.ActionTypeAllow:
		if len(warnings) > 0 {
			resp := admission.Allowed("QoS class allowed with warnings")
			resp.Warnings = append(resp.Warnings, warnings...)

			return &resp
		}

		return nil

	case rules.ActionTypeDeny:
		msg := fmt.Sprintf(
			"pod %q uses QoS class %q which is denied by namespace rule",
			pod.Name,
			qosClass,
		)

		return h.denyWithEvent(
			recorder,
			tnt,
			pod,
			evt.ReasonForbiddenPodQoSClass,
			msg,
		)

	case rules.ActionTypeAudit:
		msg := fmt.Sprintf(
			"pod %q uses QoS class %q and matched audit QoS rule",
			pod.Name,
			qosClass,
		)

		h.auditWithEvent(recorder, tnt, pod, msg)

		resp := admission.Allowed("QoS class audited")
		resp.Warnings = append(resp.Warnings, append(warnings, msg)...)

		return &resp

	default:
		resp := admission.Errored(
			http.StatusInternalServerError,
			fmt.Errorf("unsupported namespace rule action %q", evaluation.Decision.Action),
		)

		return &resp
	}
}

type qosDecision struct {
	Action rules.ActionType
	Rule   *rules.NamespaceRuleBodyNamespace
	Class  corev1.PodQOSClass
}

type qosEvaluation struct {
	Decision *qosDecision
	Audits   []*qosDecision
}

func evaluateQoSClass(
	ruleBlocks []*rules.NamespaceRuleBodyNamespace,
	qosClass corev1.PodQOSClass,
) (*qosEvaluation, error) {
	evaluation := &qosEvaluation{}

	for _, rule := range ruleBlocks {
		if rule == nil {
			continue
		}

		if len(rule.Enforce.Workloads.QoSClasses) == 0 {
			continue
		}

		if !rule.Enforce.WorkloadTargetsAny(
			rules.ValidateInitContainers,
			rules.ValidateEphemeralContainers,
			rules.ValidateContainers,
			rules.ValidateVolumes,
		) {
			continue
		}

		if !qosClassMatches(rule.Enforce.Workloads.QoSClasses, qosClass) {
			continue
		}

		action := rule.Enforce.Action.OrDefault()

		decision := &qosDecision{
			Action: action,
			Rule:   rule,
			Class:  qosClass,
		}

		switch action {
		case rules.ActionTypeAllow, rules.ActionTypeDeny:
			// Last matching allow/deny wins.
			evaluation.Decision = decision

		case rules.ActionTypeAudit:
			evaluation.Audits = append(evaluation.Audits, decision)

		default:
			return nil, fmt.Errorf("unsupported namespace rule action %q", action)
		}
	}

	return evaluation, nil
}

func qosClassMatches(classes []corev1.PodQOSClass, got corev1.PodQOSClass) bool {
	return slices.Contains(classes, got)
}

func ComputePodQoSClass(pod *corev1.Pod) corev1.PodQOSClass {
	if pod == nil {
		return corev1.PodQOSBestEffort
	}

	containers := make([]corev1.Container, 0,
		len(pod.Spec.InitContainers)+
			len(pod.Spec.Containers)+
			len(pod.Spec.EphemeralContainers),
	)

	containers = append(containers, pod.Spec.InitContainers...)
	containers = append(containers, pod.Spec.Containers...)

	for _, c := range pod.Spec.EphemeralContainers {
		containers = append(containers, corev1.Container{
			Name:      c.Name,
			Resources: c.Resources,
		})
	}

	if len(containers) == 0 {
		return corev1.PodQOSBestEffort
	}

	requestsOrLimitsFound := false
	guaranteed := true

	for _, c := range containers {
		resources := c.Resources

		if len(resources.Requests) > 0 || len(resources.Limits) > 0 {
			requestsOrLimitsFound = true
		}

		cpuRequest, hasCPURequest := resources.Requests[corev1.ResourceCPU]
		memoryRequest, hasMemoryRequest := resources.Requests[corev1.ResourceMemory]
		cpuLimit, hasCPULimit := resources.Limits[corev1.ResourceCPU]
		memoryLimit, hasMemoryLimit := resources.Limits[corev1.ResourceMemory]

		if !hasCPURequest || !hasMemoryRequest || !hasCPULimit || !hasMemoryLimit {
			guaranteed = false

			continue
		}

		if !quantityEqual(cpuRequest, cpuLimit) || !quantityEqual(memoryRequest, memoryLimit) {
			guaranteed = false
		}
	}

	if guaranteed && requestsOrLimitsFound {
		return corev1.PodQOSGuaranteed
	}

	if !requestsOrLimitsFound {
		return corev1.PodQOSBestEffort
	}

	return corev1.PodQOSBurstable
}

func quantityEqual(a, b resource.Quantity) bool {
	return a.Cmp(b) == 0
}

func (h *qosHandler) auditWithEvent(
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
	pod *corev1.Pod,
	msg string,
) {
	recorder.Eventf(
		pod,
		tnt,
		corev1.EventTypeWarning,
		evt.ReasonForbiddenPodQoSClass,
		evt.ActionValidationDenied,
		msg,
	)
}

func (h *qosHandler) denyWithEvent(
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
	pod *corev1.Pod,
	reason string,
	msg string,
) *admission.Response {
	recorder.Eventf(
		pod,
		tnt,
		corev1.EventTypeWarning,
		reason,
		evt.ActionValidationDenied,
		msg,
	)

	return ad.Deny(msg)
}
