// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package events

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sevents "k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
)

const (
	ReportingController = "controller.projectcapsule.dev"
	ReportingInstance   = "capsule-admission"
)

type EventRecorder interface {
	k8sevents.EventRecorder
	LabeledEvent(regarding runtime.Object, eventType string, reason string, action string, note string) LabeledEvent
}

type eventEmitter interface {
	Emit(ctx context.Context, e LabeledEvent)
}

type eventRecorder struct {
	k8sevents.EventRecorder

	client        client.Client
	configuration configuration.Configuration
	log           logr.Logger
}

func NewEventRecorder(
	c client.Client,
	log logr.Logger,
	recorder k8sevents.EventRecorder,
	configuration configuration.Configuration,
) EventRecorder {
	return &eventRecorder{
		EventRecorder: recorder,
		client:        c,
		log:           log.WithName("event-recorder"),
		configuration: configuration,
	}
}

func (r *eventRecorder) Emit(ctx context.Context, e LabeledEvent) {
	if r == nil {
		return
	}

	if r.client == nil {
		r.log.Error(nil, "cannot emit labeled event: client is nil")

		return
	}

	if e == nil {
		r.log.Error(nil, "cannot emit labeled event: event is nil")

		return
	}

	if e.Reason() == "" {
		r.log.Error(nil, "cannot emit labeled event: reason is empty")

		return
	}

	if e.Action() == "" {
		r.log.Error(nil, "cannot emit labeled event: action is empty")

		return
	}

	regardingRef, metaObj, err := objectReference(e.Regarding())
	if err != nil {
		r.log.Error(err, "cannot emit labeled event: build regarding reference")

		return
	}

	namespace := metaObj.GetNamespace()
	if namespace == "" {
		namespace = r.configuration.Events().ClusterEventNamespace
	}

	if namespace == "" {
		r.log.Error(nil, "cannot emit labeled event: namespace is empty")

		return
	}

	event := &eventsv1.Event{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: metaObj.GetName(),
			Namespace:    namespace,
			Labels:       e.Labels(),
			Annotations:  e.Annotations(),
		},
		EventTime:           metav1.MicroTime{Time: time.Now()},
		ReportingController: ReportingController,
		ReportingInstance:   ReportingInstance,
		Action:              e.Action(),
		Reason:              e.Reason(),
		Regarding:           regardingRef,
		Note:                e.Note(),
		Type:                e.EventType(),
	}

	if e.Related() != nil {
		relatedRef, _, err := objectReference(e.Related())
		if err != nil {
			r.log.Error(err, "cannot emit labeled event: build related reference")

			return
		}

		event.Related = &relatedRef
	}

	if err := r.client.Create(ctx, event); err != nil {
		r.log.Error(
			err,
			"cannot emit labeled event",
			"reason", e.Reason(),
			"action", e.Action(),
			"type", e.EventType(),
			"regarding", regardingRef.Name,
			"namespace", namespace,
		)

		return
	}
}

func objectReference(obj runtime.Object) (corev1.ObjectReference, metav1.Object, error) {
	if obj == nil {
		return corev1.ObjectReference{}, nil, fmt.Errorf("object is nil")
	}

	metaObj, ok := obj.(metav1.Object)
	if !ok {
		return corev1.ObjectReference{}, nil, fmt.Errorf("%T does not implement metav1.Object", obj)
	}

	gvk := obj.GetObjectKind().GroupVersionKind()

	return corev1.ObjectReference{
		APIVersion: gvk.GroupVersion().String(),
		Kind:       gvk.Kind,
		Namespace:  metaObj.GetNamespace(),
		Name:       metaObj.GetName(),
		UID:        metaObj.GetUID(),
	}, metaObj, nil
}
