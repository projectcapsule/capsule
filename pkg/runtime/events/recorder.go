// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package events

import (
	"context"
	"fmt"
	"time"

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

type EventRecorder struct {
	k8sevents.EventRecorder

	client        client.Client
	configuration configuration.Configuration
}

func NewEventRecorder(
	c client.Client,
	recorder k8sevents.EventRecorder,
	configuration configuration.Configuration,
) *EventRecorder {
	return &EventRecorder{
		EventRecorder: recorder,
		client:        c,
		configuration: configuration,
	}
}

func (r *EventRecorder) EmitLabeledEvent(
	ctx context.Context,
	regarding runtime.Object,
	related runtime.Object,
	eventType string,
	reason string,
	action string,
	note string,
	labels map[string]string,
) error {
	if r == nil {
		return fmt.Errorf("event recorder is nil")
	}

	if r.client == nil {
		return fmt.Errorf("event recorder client is nil")
	}

	if reason == "" {
		return fmt.Errorf("event reason is empty")
	}

	if action == "" {
		return fmt.Errorf("event action is empty")
	}

	regardingRef, metaObj, err := objectReference(regarding)
	if err != nil {
		return fmt.Errorf("build regarding reference: %w", err)
	}

	namespace := metaObj.GetNamespace()
	if namespace == "" {
		namespace = "default"
	}

	event := &eventsv1.Event{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: metaObj.GetName() + ".",
			Namespace:    namespace,
			Labels:       labels,
		},
		EventTime:           metav1.MicroTime{Time: time.Now()},
		ReportingController: ReportingController,
		ReportingInstance:   ReportingInstance,
		Action:              action,
		Reason:              reason,
		Regarding:           regardingRef,
		Note:                note,
		Type:                eventType,
	}

	if related != nil {
		relatedRef, _, err := objectReference(related)
		if err != nil {
			return fmt.Errorf("build related reference: %w", err)
		}

		event.Related = &relatedRef
	}

	return r.client.Create(ctx, event)
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
