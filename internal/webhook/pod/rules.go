package pod

import (
	"context"
	"errors"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	apirules "github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/rules"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/runtime/workloads"
)

type podRuleValidator func(*corev1.Pod, []*apirules.NamespaceRuleEnforceBody) (*rules.Evaluation, error)

type podRules struct {
	rules []podRuleValidator
}

func PodRules() handlers.TypedHandlerWithTenantWithRuleset[*corev1.Pod] {
	return &podRules{
		rules: []podRuleValidator{
			validateSchedulers,
			validateQoSClasses,
		},
	}
}

func (h *podRules) OnCreate(
	c client.Client,
	_ client.Reader,
	pod *corev1.Pod,
	_ admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
	bodies []*apirules.NamespaceRuleBodyNamespace,
) handlers.Func {
	return func(ctx context.Context, _ admission.Request) *admission.Response {
		enforceBodies := rules.EnforceBodiesFromNamespaceRules(bodies)

		if err := h.validatePodRules(ctx, c, pod, tnt, recorder, enforceBodies); err != nil {
			return ad.Deny(err.Error())
		}

		return nil
	}
}

func (h *podRules) OnUpdate(
	c client.Client,
	_ client.Reader,
	pod *corev1.Pod,
	_ *corev1.Pod,
	_ admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
	bodies []*apirules.NamespaceRuleBodyNamespace,
) handlers.Func {
	return func(ctx context.Context, _ admission.Request) *admission.Response {
		enforceBodies := rules.EnforceBodiesFromNamespaceRules(bodies)

		if err := h.validatePodRules(ctx, c, pod, tnt, recorder, enforceBodies); err != nil {
			return ad.Deny(err.Error())
		}

		return nil
	}
}

func (h *podRules) OnDelete(
	_ client.Client,
	_ client.Reader,
	_ *corev1.Pod,
	_ admission.Decoder,
	_ events.EventRecorder,
	_ *capsulev1beta2.Tenant,
	_ []*apirules.NamespaceRuleBodyNamespace,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *podRules) validatePodRules(
	ctx context.Context,
	c client.Client,
	pod *corev1.Pod,
	tnt *capsulev1beta2.Tenant,
	recorder events.EventRecorder,
	enforceBodies []*apirules.NamespaceRuleEnforceBody,
) error {
	log := log.FromContext(ctx)

	for _, evaluate := range h.rules {
		evaluation, err := evaluate(pod, enforceBodies)
		if err != nil {
			return err
		}

		if evaluation == nil {
			continue
		}

		for _, audit := range evaluation.Audits {
			recorder.Eventf(
				pod,
				tnt,
				corev1.EventTypeNormal,
				events.ReasonNamespaceRuleAudit,
				events.ActionRuleAudit,
				audit.Message,
			)
		}

		if err := evaluation.BlockingError(); err != nil {
			var decisionErr *rules.DecisionError
			if errors.As(err, &decisionErr) && decisionErr.Decision != nil {
				err = recorder.EmitLabeledEvent(
					ctx,
					tnt,
					pod,
					corev1.EventTypeNormal,
					decisionErr.Decision.EventReason,
					events.ActionValidationDenied,
					decisionErr.Decision.Message,
					map[string]string{
						"projectcapsule.dev/tenant": tnt.Name,
					},
				)

				log.Error(err, "emiting event sad")
			}
			return err
		}
	}

	return nil
}

func validateSchedulers(
	pod *corev1.Pod,
	enforceBodies []*apirules.NamespaceRuleEnforceBody,
) (*rules.Evaluation, error) {
	return evaluatePodRules[api.ExpressionMatch](
		pod,
		enforceBodies,
		podRuleSet[api.ExpressionMatch]{
			EventReason: events.ReasonForbiddenPodQoSClass,
			Name:        "scheduler",
			Values: func(pod *corev1.Pod) []rules.Value {
				return []rules.Value{
					{
						Value: strings.TrimSpace(pod.Spec.SchedulerName),
						Path:  "spec.schedulerName",
					},
				}
			},
			Rules: func(enforce *apirules.NamespaceRuleEnforceBody) []api.ExpressionMatch {
				if enforce == nil {
					return nil
				}

				return enforce.Workloads.Schedulers
			},
			Matches: func(match api.ExpressionMatch, value rules.Value) (bool, error) {
				return match.Matches(value.Value)
			},
		},
	)
}

func validateQoSClasses(
	pod *corev1.Pod,
	enforceBodies []*apirules.NamespaceRuleEnforceBody,
) (*rules.Evaluation, error) {
	return evaluatePodRules[corev1.PodQOSClass](
		pod,
		enforceBodies,
		podRuleSet[corev1.PodQOSClass]{
			Name: "QoS class",
			Values: func(pod *corev1.Pod) []rules.Value {
				return []rules.Value{
					{
						Value: string(workloads.GetPodQoSClass(pod)),
						Path:  "status.qosClass",
					},
				}
			},
			Rules: func(enforce *apirules.NamespaceRuleEnforceBody) []corev1.PodQOSClass {
				if enforce == nil {
					return nil
				}

				return enforce.Workloads.QoSClasses
			},
			Matches: func(match corev1.PodQOSClass, value rules.Value) (bool, error) {
				return string(match) == value.Value, nil
			},
		},
	)
}
