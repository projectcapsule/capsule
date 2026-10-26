// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package handlers_test

import (
	"context"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type matchingRequestSpy struct {
	calls    int
	response *admission.Response
}

func (h *matchingRequestSpy) OnCreate(client.Client, client.Reader, admission.Decoder, events.EventRecorder) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response { h.calls++; return h.response }
}
func (h *matchingRequestSpy) OnUpdate(c client.Client, r client.Reader, d admission.Decoder, e events.EventRecorder) handlers.Func {
	return h.OnCreate(c, r, d, e)
}
func (h *matchingRequestSpy) OnDelete(c client.Client, r client.Reader, d admission.Decoder, e events.EventRecorder) handlers.Func {
	return h.OnCreate(c, r, d, e)
}

func TestMatchingRequestPreservesChainAndResponses(t *testing.T) {
	for _, allowed := range []bool{true, false} {
		response := admission.Allowed("continue")
		if !allowed {
			response = admission.Denied("required denial")
		}
		spy := &matchingRequestSpy{response: &response}
		h := handlers.MatchingRequest(func(req admission.Request) bool { return req.Name == "selected" }, spy)
		for _, fn := range []handlers.Func{h.OnCreate(nil, nil, nil, nil), h.OnUpdate(nil, nil, nil, nil), h.OnDelete(nil, nil, nil, nil)} {
			if got := fn(t.Context(), admission.Request{}); got != nil {
				t.Fatalf("skip ended admission chain: %+v", got)
			}
			if got := fn(t.Context(), admission.Request{Name: "selected"}); got != &response {
				t.Fatalf("response was replaced: %+v", got)
			}
		}
		if spy.calls != 3 {
			t.Fatalf("inner calls=%d, want 3", spy.calls)
		}
	}
}
