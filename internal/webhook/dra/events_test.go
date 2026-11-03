// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package dra

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

type recordingClassEvents struct {
	events.EventRecorder
	built   int
	emitted []events.LabeledEvent
}

func (r *recordingClassEvents) LabeledEvent(obj runtime.Object, eventType, reason, action, note string) events.LabeledEvent {
	r.built++
	return &recordedClassEvent{LabeledEvent: r.EventRecorder.LabeledEvent(obj, eventType, reason, action, note), recorder: r}
}

type recordedClassEvent struct {
	events.LabeledEvent
	recorder *recordingClassEvents
}

func (e *recordedClassEvent) Emit(context.Context) {
	e.recorder.emitted = append(e.recorder.emitted, e.LabeledEvent)
}

func (e *recordedClassEvent) WithRelated(obj runtime.Object) events.LabeledEvent {
	e.LabeledEvent.WithRelated(obj)
	return e
}

func (e *recordedClassEvent) WithTenantLabel(tnt *capsulev1beta2.Tenant) events.LabeledEvent {
	e.LabeledEvent.WithTenantLabel(tnt)
	return e
}

func (e *recordedClassEvent) WithRequestAnnotations(req admission.Request) events.LabeledEvent {
	e.LabeledEvent.WithRequestAnnotations(req)
	return e
}

func TestDeviceClassDryRunEvents(t *testing.T) {
	for _, version := range []string{"v1", "v1beta2"} {
		for _, kind := range []string{"ResourceClaim", "ResourceClaimTemplate"} {
			for _, mode := range []string{"audit", "deny", "allow-miss", "audit-and-deny", "legacy-deny"} {
				t.Run(version+"/"+kind+"/"+mode, func(t *testing.T) {
					cl := newDRAClientVersion(t, version)
					setClassRules(t, cl, "tenant-b")
					name, denial := "gpu-a", ""
					var bodies []*rules.NamespaceRuleBodyTenant
					var reasons []string
					switch mode {
					case "audit":
						bodies = []*rules.NamespaceRuleBodyTenant{classBody(rules.ActionTypeAudit, rules.DeviceClassMatch{Exact: []string{name}})}
						reasons = []string{events.ReasonNamespaceRuleAudit}
					case "deny":
						bodies = []*rules.NamespaceRuleBodyTenant{classBody(rules.ActionTypeDeny, rules.DeviceClassMatch{Exact: []string{name}})}
						denial, reasons = "denied", []string{events.ReasonForbiddenDeviceClass}
					case "allow-miss", "audit-and-deny":
						name, denial = "gpu-other", "not allowed"
						bodies = []*rules.NamespaceRuleBodyTenant{classBody(rules.ActionTypeAllow, rules.DeviceClassMatch{Exact: []string{"gpu-a"}})}
						if mode == "audit-and-deny" {
							bodies = append(bodies, classBody(rules.ActionTypeAudit, rules.DeviceClassMatch{Exact: []string{name}}))
							reasons = append(reasons, events.ReasonNamespaceRuleAudit)
						}
						reasons = append(reasons, events.ReasonForbiddenDeviceClass)
					case "legacy-deny":
						name, denial, reasons = "gpu-other", "Device Class gpu-other is forbidden", []string{events.ReasonForbiddenDeviceClass}
					}
					if mode != "legacy-deny" {
						setClassRules(t, cl, "tenant-a", bodies...)
					}
					for _, namespace := range []string{"ns-a", "ns-b"} {
						var baseline *admission.Response
						for _, dryRun := range []*bool{nil, new(false), new(true)} {
							label := "omitted"
							if dryRun != nil {
								label = fmt.Sprint(*dryRun)
							}
							t.Run(namespace+"/dryRun="+label, func(t *testing.T) {
								req := draAdmissionRequestVersion(t, version, kind, namespace, exactRequests(name))
								req.DryRun, req.UID, req.UserInfo.Username = dryRun, types.UID("admission-request"), "tenant-owner"
								recorder := &recordingClassEvents{EventRecorder: events.NewDiscardRecorder()}
								response := DeviceClass(nil, nil, nil, nil).OnCreate(cl, cl, admission.NewDecoder(cl.Scheme()), recorder)(t.Context(), req)
								if namespace == "ns-a" && denial != "" {
									require.NotNil(t, response)
									require.False(t, response.Allowed)
									require.Contains(t, response.Result.Message, denial)
								} else {
									require.Nil(t, response, "allow must continue the handler chain")
								}
								if dryRun == nil {
									baseline = response
								} else {
									require.Equal(t, baseline, response, "dry-run must preserve the admission decision")
								}
								if namespace == "ns-b" || (dryRun != nil && *dryRun) {
									require.Zero(t, recorder.built, "dry runs must not even construct event payloads")
									require.Empty(t, recorder.emitted)
									return
								}
								var got []string
								for _, event := range recorder.emitted {
									got = append(got, event.Reason())
									require.Equal(t, "tenant-a", event.Labels()[meta.NewTenantLabel])
									require.Equal(t, "admission-request", event.Annotations()[meta.AuditRequestUID])
									require.Equal(t, "tenant-owner", event.Annotations()[meta.AuditUsername])
									require.Equal(t, "tenant-a", event.Related().(*capsulev1beta2.Tenant).Name)
									require.Equal(t, namespace, event.Regarding().(interface{ GetNamespace() string }).GetNamespace())
									if event.Reason() == events.ReasonNamespaceRuleAudit {
										require.Equal(t, corev1.EventTypeNormal, event.EventType())
										require.Equal(t, events.ActionRuleAudit, event.Action())
									} else {
										require.Equal(t, corev1.EventTypeWarning, event.EventType())
										require.Equal(t, events.ActionValidationDenied, event.Action())
									}
								}
								require.Equal(t, reasons, got)
								require.Equal(t, len(reasons), recorder.built)
							})
						}
					}
				})
			}
		}
	}
}
