// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

func TestForKindFiltersBeforeCallingHandler(t *testing.T) {
	t.Parallel()

	spy := &requestSpyHandler{}
	handler := ForKind(schema.GroupKind{Kind: "Pod"}, spy, "ephemeralcontainers")
	handle := handler.OnCreate(nil, nil, nil, nil)

	if response := handle(context.Background(), requestWithKind("", "Service")); response != nil {
		t.Fatalf("service response = %#v, want nil", response)
	}
	if spy.calls != 0 {
		t.Fatalf("handler calls = %d, want 0 for a different kind", spy.calls)
	}

	if response := handle(context.Background(), requestWithKind("", "Pod")); response != nil {
		t.Fatalf("pod response = %#v, want nil", response)
	}
	if spy.calls != 1 {
		t.Fatalf("handler calls = %d, want 1 for Pod", spy.calls)
	}

	statusRequest := requestWithKind("", "Pod")
	statusRequest.SubResource = "status"
	if response := handle(context.Background(), statusRequest); response != nil {
		t.Fatalf("pod status response = %#v, want nil", response)
	}
	if spy.calls != 1 {
		t.Fatalf("handler calls = %d, want Pod status to be filtered", spy.calls)
	}

	ephemeralRequest := requestWithKind("", "Pod")
	ephemeralRequest.SubResource = "ephemeralcontainers"
	if response := handle(context.Background(), ephemeralRequest); response != nil {
		t.Fatalf("pod ephemeral containers response = %#v, want nil", response)
	}
	if spy.calls != 2 {
		t.Fatalf("handler calls = %d, want Pod ephemeral containers to be included", spy.calls)
	}
}

func TestGenericValidatingIncludesResourceHandlers(t *testing.T) {
	t.Parallel()

	webhook := Register(nil, nil, &requestSpyHandler{}, &requestSpyHandler{})
	if got := len(webhook.GetHandlers()); got != 4 {
		t.Fatalf("handlers = %d, want generic metadata, two resource handlers, and ingress", got)
	}
}

func TestMatchesGenericMetadataRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		request     admission.Request
		wantMatched bool
	}{
		{
			name:        "main resource",
			request:     requestWithKind("apps", "Deployment"),
			wantMatched: true,
		},
		{
			name: "namespace status",
			request: func() admission.Request {
				req := requestWithKind("", "Namespace")
				req.SubResource = "status"

				return req
			}(),
			wantMatched: true,
		},
		{
			name: "deployment scale",
			request: func() admission.Request {
				req := requestWithKind("apps", "Deployment")
				req.SubResource = "scale"

				return req
			}(),
			wantMatched: false,
		},
		{
			name: "pod status",
			request: func() admission.Request {
				req := requestWithKind("", "Pod")
				req.SubResource = "status"

				return req
			}(),
			wantMatched: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := matchesGenericMetadataRequest(tt.request); got != tt.wantMatched {
				t.Fatalf("matchesGenericMetadataRequest() = %t, want %t", got, tt.wantMatched)
			}
		})
	}
}

type requestSpyHandler struct {
	calls int
}

func (h *requestSpyHandler) OnCreate(
	client.Client,
	client.Reader,
	admission.Decoder,
	events.EventRecorder,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		h.calls++

		return nil
	}
}

func (h *requestSpyHandler) OnUpdate(
	client.Client,
	client.Reader,
	admission.Decoder,
	events.EventRecorder,
) handlers.Func {
	return h.OnCreate(nil, nil, nil, nil)
}

func (h *requestSpyHandler) OnDelete(
	client.Client,
	client.Reader,
	admission.Decoder,
	events.EventRecorder,
) handlers.Func {
	return h.OnCreate(nil, nil, nil, nil)
}

func requestWithKind(group, kind string) admission.Request {
	return admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Kind: metav1.GroupVersionKind{
			Group:   group,
			Version: "v1",
			Kind:    kind,
		},
	}}
}
