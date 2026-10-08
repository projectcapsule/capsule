// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package webhook

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sevents "k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

// Capture writes from the production recorder's queue, including Events created
// without request annotations. The final marker drains earlier writes FIFO.
type eventWrites struct {
	client.Client
	writes chan *eventsv1.Event
}

func (c *eventWrites) Create(_ context.Context, obj client.Object, _ ...client.CreateOption) error {
	c.writes <- obj.(*eventsv1.Event).DeepCopy()
	return nil
}

func testRecorder() (events.EventRecorder, *eventWrites, *k8sevents.FakeRecorder) {
	writes := &eventWrites{writes: make(chan *eventsv1.Event, 1024)}
	plain := k8sevents.NewFakeRecorder(1024)
	return events.NewEventRecorder(writes, logr.Discard(), plain, nil), writes, plain
}

func drainEvents(t *testing.T, recorder events.EventRecorder, writes *eventWrites) []*eventsv1.Event {
	t.Helper()
	recorder.LabeledEvent(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "marker", Namespace: "test"}}, "Normal", "Marker", "Test", "drain").Emit(t.Context())
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	var got []*eventsv1.Event
	for {
		select {
		case event := <-writes.writes:
			if event.Reason == "Marker" {
				return got
			}
			got = append(got, event)
		case <-timeout.C:
			t.Fatal("event recorder did not drain")
			return nil
		}
	}
}

type eventHandler struct {
	calls    *[]string
	name     string
	response *admission.Response
}

func (h eventHandler) handle(recorder events.EventRecorder, operation admissionv1.Operation) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		if h.calls != nil {
			*h.calls = append(*h.calls, h.name+":"+string(operation))
		}
		obj := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: req.Name, Namespace: req.Namespace}}
		recorder.LabeledEvent(obj, "Normal", "Audit", "Validate", h.name).Emit(ctx)
		recorder.Eventf(obj, nil, "Warning", "Denied", "Validate", "%s", req.Name)
		return h.response
	}
}
func (h eventHandler) OnCreate(_ client.Client, _ client.Reader, _ admission.Decoder, recorder events.EventRecorder) handlers.Func {
	return h.handle(recorder, admissionv1.Create)
}
func (h eventHandler) OnUpdate(_ client.Client, _ client.Reader, _ admission.Decoder, recorder events.EventRecorder) handlers.Func {
	return h.handle(recorder, admissionv1.Update)
}
func (h eventHandler) OnDelete(_ client.Client, _ client.Reader, _ admission.Decoder, recorder events.EventRecorder) handlers.Func {
	return h.handle(recorder, admissionv1.Delete)
}

func TestRouterDryRunEventsAndResponses(t *testing.T) {
	no, yes := false, true
	denied, allowed := admission.Denied("policy denied"), admission.Allowed("explicit allow")
	patched := admission.PatchResponseFromRaw([]byte(`{"metadata":{}}`), []byte(`{"metadata":{"labels":{"profile":"test"}}}`))
	patched.Warnings = []string{"audit warning"}
	patched.AuditAnnotations = map[string]string{"policy": "matched"}
	for _, operation := range []admissionv1.Operation{admissionv1.Create, admissionv1.Update, admissionv1.Delete, admissionv1.Connect} {
		for _, dryRun := range []*bool{nil, &no, &yes} {
			for _, response := range []*admission.Response{nil, &denied, &allowed, &patched} {
				name := "omitted"
				if dryRun != nil {
					name = fmt.Sprint(*dryRun)
				}
				t.Run(fmt.Sprintf("%s/dryRun=%s/response=%v", operation, name, response), func(t *testing.T) {
					recorder, writes, plain := testRecorder()
					var calls []string
					router := &handlerRouter{recorder: recorder, handlers: []handlers.Handler{
						eventHandler{calls: &calls, name: "first"},
						eventHandler{calls: &calls, name: "second", response: response},
						eventHandler{calls: &calls, name: "last"},
					}}
					req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{Operation: operation, Namespace: "tenant-a", Name: "request", DryRun: dryRun, SubResource: "status"}}
					got := router.Handle(t.Context(), req)
					wantCalls := []string{"first:" + string(operation), "second:" + string(operation)}
					wantResponse := admission.Allowed("")
					if response == nil {
						wantCalls = append(wantCalls, "last:"+string(operation))
					} else {
						wantResponse = *response
					}
					if operation == admissionv1.Connect {
						wantCalls = nil
						wantResponse = admission.Allowed("")
					}
					require.Equal(t, wantCalls, calls)
					require.Equal(t, wantResponse, got, "decisions, patches, warnings and audit annotations must survive dry runs")
					count := len(wantCalls)
					if dryRun != nil && *dryRun {
						count = 0
					}
					require.Len(t, drainEvents(t, recorder, writes), count)
					require.Len(t, plain.Events, count)
				})
			}
		}
	}
}

func TestRouterConcurrentDryRunIsolation(t *testing.T) {
	recorder, writes, plain := testRecorder()
	router := &handlerRouter{recorder: recorder, handlers: []handlers.Handler{eventHandler{name: "audit"}}}
	var wg sync.WaitGroup
	for i := range 64 {
		wg.Go(func() {
			dryRun := i%2 == 0
			namespace := "tenant-live"
			if dryRun {
				namespace = "tenant-dry"
			}
			req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{Operation: admissionv1.Create, Namespace: namespace, Name: fmt.Sprintf("request-%d", i), DryRun: &dryRun}}
			got := router.Handle(t.Context(), req)
			if !got.Allowed {
				t.Errorf("request %d unexpectedly denied", i)
			}
		})
	}
	wg.Wait()
	// A controller using the original recorder must also still publish Events.
	recorder.LabeledEvent(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "controller", Namespace: "tenant-live"}}, "Normal", "Reconciled", "Reconcile", "done").Emit(t.Context())
	got := drainEvents(t, recorder, writes)
	require.Len(t, got, 33)
	for _, event := range got {
		require.Equal(t, "tenant-live", event.Namespace)
	}
	require.Len(t, plain.Events, 32)
}
