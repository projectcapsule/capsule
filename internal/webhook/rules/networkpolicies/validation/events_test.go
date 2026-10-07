// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

type recordingEvents struct {
	events.EventRecorder
	emitted []events.LabeledEvent
}

func (r *recordingEvents) LabeledEvent(obj runtime.Object, eventType, reason, action, note string) events.LabeledEvent {
	return &recordedEvent{LabeledEvent: r.EventRecorder.LabeledEvent(obj, eventType, reason, action, note), recorder: r}
}

type recordedEvent struct {
	events.LabeledEvent
	recorder *recordingEvents
}

func (e *recordedEvent) Emit(context.Context) {
	e.recorder.emitted = append(e.recorder.emitted, e.LabeledEvent)
}

func (e *recordedEvent) WithRelated(obj runtime.Object) events.LabeledEvent {
	e.LabeledEvent.WithRelated(obj)
	return e
}

func (e *recordedEvent) WithTenantLabel(tnt *capsulev1beta2.Tenant) events.LabeledEvent {
	e.LabeledEvent.WithTenantLabel(tnt)
	return e
}

func (e *recordedEvent) WithRequestAnnotations(req admission.Request) events.LabeledEvent {
	e.LabeledEvent.WithRequestAnnotations(req)
	return e
}

func TestNetworkPolicyDryRunEvents(t *testing.T) {
	no, yes := false, true
	for _, action := range []rules.ActionType{rules.ActionTypeAudit, rules.ActionTypeDeny, rules.ActionTypeAllow} {
		for _, operation := range []admissionv1.Operation{admissionv1.Create, admissionv1.Update} {
			for _, dryRun := range []*bool{nil, &no, &yes} {
				name := "omitted"
				if dryRun != nil {
					name = fmt.Sprint(*dryRun)
				}
				t.Run(fmt.Sprintf("%s/%s/dryRun=%s", action, operation, name), func(t *testing.T) {
					cl, decoder, requests := admissionFixture(t, 2, true, func(i int) []*rules.NamespaceRuleBodyNamespace {
						if i == 1 {
							return nil
						}
						return []*rules.NamespaceRuleBodyNamespace{{Enforce: cidrRule(action, "10.0.0.0/8")}}
					})
					for i, req := range requests {
						recorder := &recordingEvents{EventRecorder: events.NewEventRecorder(nil, logr.Discard(), nil, nil)}
						req.DryRun, req.Operation, req.OldObject = dryRun, operation, req.Object
						handler := Handler(nil, nil)
						handle := handler.OnCreate(cl, cl, decoder, recorder)
						if operation == admissionv1.Update {
							handle = handler.OnUpdate(cl, cl, decoder, recorder)
						}
						response := handle(t.Context(), req)
						if i == 0 && action == rules.ActionTypeDeny {
							require.NotNil(t, response)
							require.False(t, response.Allowed)
							require.Contains(t, response.Result.Message, "networkPolicy egress CIDR")
						} else {
							require.Nil(t, response, "successful checks must continue the admission chain")
						}
						if i == 1 || action == rules.ActionTypeAllow || (dryRun != nil && *dryRun) {
							require.Empty(t, recorder.emitted)
						} else {
							require.Len(t, recorder.emitted, 1)
							reason := events.ReasonNamespaceRuleAudit
							if action == rules.ActionTypeDeny {
								reason = events.ReasonForbiddenNetworkPolicyEgressCIDR
							}
							require.Equal(t, reason, recorder.emitted[0].Reason())
							require.Equal(t, "tenant-0", recorder.emitted[0].Labels()[meta.NewTenantLabel])
						}
					}
				})
			}
		}
	}
}
