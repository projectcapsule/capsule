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

type LabeledEvent interface {
	Emit(ctx context.Context)
	WithRelated(obj runtime.Object) LabeledEvent
	WithLabels(labels map[string]string) LabeledEvent
	WithAnnotations(annotations map[string]string) LabeledEvent
	WithTenantLabel(tnt *capsulev1beta2.Tenant) LabeledEvent
	WithRequestAnnotations(req admission.Request) LabeledEvent

	Reason() string
	Action() string
	Regarding() runtime.Object
	Labels() map[string]string
	Annotations() map[string]string
	Note() string
	EventType() string
	Related() runtime.Object
}

type labeledEvent struct {
	emitter eventEmitter

	regarding runtime.Object
	related   runtime.Object

	eventType string
	reason    string
	action    string
	note      string

	labels      map[string]string
	annotations map[string]string
}

func (e *labeledEvent) Reason() string {
	return e.reason
}

func (e *labeledEvent) Action() string {
	return e.action
}

func (e *labeledEvent) Regarding() runtime.Object {
	return e.regarding
}

func (e *labeledEvent) Labels() map[string]string {
	return maps.Clone(e.labels)
}

func (e *labeledEvent) Annotations() map[string]string {
	return maps.Clone(e.annotations)
}

func (e *labeledEvent) Note() string {
	return e.note
}

func (e *labeledEvent) EventType() string {
	return e.eventType
}

func (e *labeledEvent) Related() runtime.Object {
	return e.related
}

func (r *eventRecorder) LabeledEvent(
	regarding runtime.Object,
	eventType string,
	reason string,
	action string,
	note string,
) LabeledEvent {
	return &labeledEvent{
		emitter:     r,
		regarding:   regarding,
		eventType:   eventType,
		reason:      reason,
		action:      action,
		note:        note,
		labels:      map[string]string{},
		annotations: map[string]string{},
	}
}

func (e *labeledEvent) Emit(ctx context.Context) {
	if e == nil || e.emitter == nil {
		return
	}

	e.emitter.Emit(ctx, e)
}

func (e *labeledEvent) WithRelated(obj runtime.Object) LabeledEvent {
	e.related = obj

	return e
}

func (e *labeledEvent) WithLabels(labels map[string]string) LabeledEvent {
	maps.Copy(e.labels, labels)

	return e
}

func (e *labeledEvent) WithAnnotations(annotations map[string]string) LabeledEvent {
	maps.Copy(e.annotations, annotations)

	return e
}

func (e *labeledEvent) WithTenantLabel(tnt *capsulev1beta2.Tenant) LabeledEvent {
	if tnt == nil {
		return e
	}

	e.labels[meta.ManagedByCapsuleLabel] = tnt.Name
	e.labels[meta.NewManagedByCapsuleLabel] = tnt.Name
	e.labels[meta.NewTenantLabel] = tnt.Name

	return e
}

func (e *labeledEvent) WithRequestAnnotations(req admission.Request) LabeledEvent {
	if req.UID != "" {
		e.annotations[meta.AuditRequestUID] = string(req.UID)
	}

	if req.UserInfo.Username != "" {
		e.annotations[meta.AuditUsername] = req.UserInfo.Username
	}

	return e
}
