// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package events_test

import (
	"context"
	"testing"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2/klogr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func TestLabeledEventAccessorsAndCopies(t *testing.T) {
	t.Parallel()

	recorder := events.NewEventRecorder(nil, klogr.New(), nil, nil)
	pod := podObject("tenant-a", "api")
	related := podObject("tenant-a", "sidecar")

	event := recorder.LabeledEvent(pod, corev1.EventTypeWarning, events.ReasonForbiddenMetadata, events.ActionValidationDenied, "denied").
		WithRelated(related).
		WithLabels(map[string]string{"existing": "label"}).
		WithAnnotations(map[string]string{"existing": "annotation"}).
		WithTenantLabel(&capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: "tenant-a"}}).
		WithRequestAnnotations(admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
			UID: types.UID("request-uid"),
			UserInfo: authenticationv1.UserInfo{
				Username: "alice",
			},
		}})

	if event.Reason() != events.ReasonForbiddenMetadata || event.Action() != events.ActionValidationDenied {
		t.Fatalf("unexpected reason/action: %s/%s", event.Reason(), event.Action())
	}
	if event.Regarding() != pod || event.Related() != related {
		t.Fatalf("unexpected regarding/related objects")
	}
	if event.Note() != "denied" || event.EventType() != corev1.EventTypeWarning {
		t.Fatalf("unexpected note/type: %q/%q", event.Note(), event.EventType())
	}

	labels := event.Labels()
	labels["existing"] = "changed"
	if event.Labels()["existing"] != "label" {
		t.Fatalf("Labels() did not return a copy")
	}
	if event.Labels()[meta.NewTenantLabel] != "tenant-a" {
		t.Fatalf("WithTenantLabel() did not set tenant label")
	}

	annotations := event.Annotations()
	annotations["existing"] = "changed"
	if event.Annotations()["existing"] != "annotation" {
		t.Fatalf("Annotations() did not return a copy")
	}
	if event.Annotations()[meta.AuditRequestUID] != "request-uid" || event.Annotations()[meta.AuditUsername] != "alice" {
		t.Fatalf("WithRequestAnnotations() annotations = %#v", event.Annotations())
	}
}

func TestLabeledEventEmitCreatesEvent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cl := eventsFakeClient(t,
		&capsulev1beta2.CapsuleConfiguration{
			ObjectMeta: metav1.ObjectMeta{Name: "capsule"},
			Spec: capsulev1beta2.CapsuleConfigurationSpec{
				Events: capsulev1beta2.EventsConfiguration{ClusterEventNamespace: "audit"},
			},
		},
	)
	cfg := configuration.NewCapsuleConfiguration(ctx, cl, cl, &rest.Config{}, "capsule")
	recorder := events.NewEventRecorder(cl, klogr.New(), nil, cfg)

	recorder.LabeledEvent(
		podObject("", "cluster-object"),
		corev1.EventTypeWarning,
		events.ReasonAdmissionFailure,
		events.ActionValidationDenied,
		"blocked",
	).WithLabels(map[string]string{"capsule.clastix.io/test": "true"}).Emit(ctx)

	var eventList eventsv1.EventList
	if err := cl.List(ctx, &eventList, client.InNamespace("audit")); err != nil {
		t.Fatalf("listing emitted events: %v", err)
	}
	if len(eventList.Items) != 1 {
		t.Fatalf("emitted events = %d, want 1", len(eventList.Items))
	}

	got := eventList.Items[0]
	if got.ReportingController != events.ReportingController || got.ReportingInstance != events.ReportingInstance {
		t.Fatalf("reporting fields = %q/%q", got.ReportingController, got.ReportingInstance)
	}
	if got.Reason != events.ReasonAdmissionFailure || got.Action != events.ActionValidationDenied || got.Note != "blocked" {
		t.Fatalf("event payload = reason %q action %q note %q", got.Reason, got.Action, got.Note)
	}
	if got.Namespace != "audit" || got.Regarding.Name != "cluster-object" || got.Labels["capsule.clastix.io/test"] != "true" {
		t.Fatalf("event metadata = %#v", got)
	}
}

func TestLabeledEventEmitNoopsForInvalidInputs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	recorder := events.NewEventRecorder(nil, klogr.New(), nil, nil)
	recorder.LabeledEvent(podObject("tenant-a", "api"), corev1.EventTypeWarning, "", events.ActionValidationDenied, "missing reason").Emit(ctx)
}

func eventsFakeClient(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("adding core scheme: %v", err)
	}
	if err := eventsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("adding events scheme: %v", err)
	}
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatalf("adding capsule scheme: %v", err)
	}

	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}

func podObject(namespace, name string) *corev1.Pod {
	return &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Pod",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			UID:       types.UID(name + "-uid"),
		},
	}
}
