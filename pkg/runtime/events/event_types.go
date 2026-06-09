// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package events

import (
	"context"
	"maps"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

type LabeledEvent struct {
	recorder *eventRecorder

	regarding runtime.Object
	related   runtime.Object

	eventType string
	reason    string
	action    string
	note      string

	labels      map[string]string
	annotations map[string]string
}

func (r *eventRecorder) LabeledEvent(
	regarding runtime.Object,
	eventType string,
	reason string,
	action string,
	note string,
) *LabeledEvent {
	return &LabeledEvent{
		recorder:    r,
		regarding:   regarding,
		eventType:   eventType,
		reason:      reason,
		action:      action,
		note:        note,
		labels:      map[string]string{},
		annotations: map[string]string{},
	}
}

func (e *LabeledEvent) Emit(ctx context.Context) {
	if e == nil || e.recorder == nil {
		return
	}

	e.recorder.emitLabeledEvent(ctx, e)
}

func (e *LabeledEvent) WithRelated(obj runtime.Object) *LabeledEvent {
	e.related = obj

	return e
}

func (e *LabeledEvent) WithLabels(labels map[string]string) *LabeledEvent {
	maps.Copy(e.labels, labels)

	return e
}

func (e *LabeledEvent) WithAnnotations(annotations map[string]string) *LabeledEvent {
	maps.Copy(e.annotations, annotations)

	return e
}

func (e *LabeledEvent) WithTenantLabel(tnt *capsulev1beta2.Tenant) *LabeledEvent {
	if tnt == nil {
		return e
	}

	e.labels[meta.NewTenantLabel] = tnt.Name

	return e
}

func (e *LabeledEvent) WithRequestAnnotations(req admission.Request) *LabeledEvent {
	if req.UID != "" {
		e.annotations[meta.AuditRequestUID] = string(req.UID)
	}

	if req.UserInfo.Username != "" {
		e.annotations[meta.AuditUsername] = req.UserInfo.Username
	}

	return e
}
