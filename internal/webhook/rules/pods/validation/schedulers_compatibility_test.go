// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

func TestSchedulerFieldsCompose(t *testing.T) {
	for _, action := range []rules.ActionType{rules.ActionTypeAllow, rules.ActionTypeDeny, rules.ActionTypeAudit} {
		for _, scheduler := range []string{"preferred", "legacy", "other"} {
			t.Run(string(action)+"/"+scheduler, func(t *testing.T) {
				// Spare capacity detects accidental append into a cache-owned slice.
				backing := []runtime.ExpressionMatch{schedulerExactForTest("preferred"), schedulerExactForTest("sentinel")}
				body := schedulerEnforceForTest(action)
				body.Workloads.Placement.Schedulers = backing[:1]
				body.Workloads.Schedulers = []runtime.ExpressionMatch{schedulerExactForTest("legacy")}
				before := body.DeepCopy()
				evaluation, err := podRulesForTest().validateSchedulers(schedulerPodForTest(scheduler), []*rules.NamespaceRuleEnforceBody{body})
				require.NoError(t, err)
				matched := scheduler != "other"
				blocked := (action == rules.ActionTypeDeny && matched) || (action == rules.ActionTypeAllow && !matched)
				require.Equal(t, blocked, evaluation.Blocking != nil)
				if action == rules.ActionTypeAudit && matched {
					require.Len(t, evaluation.Audits, 1)
				} else {
					require.Empty(t, evaluation.Audits)
				}
				if action == rules.ActionTypeAllow && !matched {
					require.Contains(t, evaluation.Blocking.Message, "preferred")
					require.Contains(t, evaluation.Blocking.Message, "legacy")
				}
				require.Equal(t, before, body)
				require.Equal(t, "sentinel", backing[1].Exact[0])
			})
		}
	}
}

func TestLegacySchedulerAdmissionConditionsAndOrder(t *testing.T) {
	compiler, err := cache.NewCELCache()
	require.NoError(t, err)
	h := PodRules(nil, nil, compiler)
	pod := schedulerPodForTest("legacy")
	body := &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{
		Action: rules.ActionTypeDeny,
		Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
			Targets:    []rules.WorkloadValidationTarget{rules.ValidatePod},
			Schedulers: []runtime.ExpressionMatch{schedulerExactForTest("legacy")},
		},
	}}
	recorder := events.NewEventRecorder(nil, logr.Discard(), nil, nil)
	tnt := &capsulev1beta2.Tenant{}
	for _, condition := range []string{"true", "false", "object.spec.missing == 'x'"} {
		body.Enforce.Conditions = []rules.AdmissionCondition{{Expression: condition}}
		for _, subresource := range []string{"", "ephemeralcontainers"} {
			req := admission.Request{}
			req.SubResource = subresource
			bodies := []*rules.NamespaceRuleBodyNamespace{body}
			for _, response := range []*admission.Response{
				h.OnCreate(nil, nil, pod, nil, recorder, tnt, bodies)(t.Context(), req),
				h.OnUpdate(nil, nil, pod.DeepCopy(), pod, nil, recorder, tnt, bodies)(t.Context(), req),
			} {
				if condition == "false" {
					require.Nil(t, response)
				} else {
					require.NotNil(t, response)
					require.False(t, response.Allowed)
					if condition == "true" {
						require.Contains(t, response.Result.Message, "spec.schedulerName")
					} else {
						require.Contains(t, response.Result.Message, "conditions[0]")
					}
				}
			}
		}
	}
	body.Enforce.Conditions = nil
	allow := &rules.NamespaceRuleBodyNamespace{Enforce: schedulerEnforceForTest(rules.ActionTypeAllow, schedulerExactForTest("legacy"))}
	require.Nil(t, h.OnCreate(nil, nil, pod, nil, recorder, tnt, []*rules.NamespaceRuleBodyNamespace{body, allow})(t.Context(), admission.Request{}))
	response := h.OnCreate(nil, nil, pod, nil, recorder, tnt, []*rules.NamespaceRuleBodyNamespace{allow, body})(t.Context(), admission.Request{})
	require.NotNil(t, response)
	require.False(t, response.Allowed)
	require.Nil(t, h.OnDelete(nil, nil, pod, nil, recorder, tnt, []*rules.NamespaceRuleBodyNamespace{body})(t.Context(), admission.Request{}))
}
