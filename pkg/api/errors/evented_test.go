// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package errors_test

import (
	"errors"
	"fmt"
	"testing"

	apierrors "github.com/projectcapsule/capsule/pkg/api/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

type fakeEventRecorder struct {
	calls []recordedEvent
	panic bool
}

type recordedEvent struct {
	regarding runtime.Object
	related   runtime.Object
	eventType string
	reason    string
	action    string
	note      string
	args      []interface{}
}

func (f *fakeEventRecorder) Eventf(
	regarding runtime.Object,
	related runtime.Object,
	eventType string,
	reason string,
	action string,
	note string,
	args ...interface{},
) {
	if f.panic {
		panic("recorder failed")
	}

	f.calls = append(f.calls, recordedEvent{
		regarding: regarding,
		related:   related,
		eventType: eventType,
		reason:    reason,
		action:    action,
		note:      note,
		args:      args,
	})
}

type fakeEventedError struct{}

func (fakeEventedError) Error() string  { return "typed failure" }
func (fakeEventedError) Reason() string { return "TypedFailure" }
func (fakeEventedError) Action() string { return "Validating" }

func TestRecordTypedErrorEvent(t *testing.T) {
	t.Parallel()

	regarding := &corev1.ConfigMap{}
	related := &corev1.Secret{}

	for _, tt := range []struct {
		name      string
		recorder  *fakeEventRecorder
		regarding runtime.Object
		err       error
	}{
		{name: "nil recorder", recorder: nil, regarding: regarding, err: fakeEventedError{}},
		{name: "nil regarding", recorder: &fakeEventRecorder{}, regarding: nil, err: fakeEventedError{}},
		{name: "nil error", recorder: &fakeEventRecorder{}, regarding: regarding, err: nil},
		{name: "plain error", recorder: &fakeEventRecorder{}, regarding: regarding, err: errors.New("plain")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			apierrors.RecordTypedErrorEvent(tt.recorder, tt.regarding, related, tt.err)
			if tt.recorder != nil && len(tt.recorder.calls) != 0 {
				t.Fatalf("RecordTypedErrorEvent() recorded %#v, want none", tt.recorder.calls)
			}
		})
	}

	recorder := &fakeEventRecorder{}
	apierrors.RecordTypedErrorEvent(recorder, regarding, related, fmt.Errorf("wrapped: %w", fakeEventedError{}))
	if len(recorder.calls) != 1 {
		t.Fatalf("RecordTypedErrorEvent() calls = %d, want 1", len(recorder.calls))
	}

	call := recorder.calls[0]
	if call.regarding != regarding || call.related != related {
		t.Fatalf("RecordTypedErrorEvent() objects = %#v/%#v", call.regarding, call.related)
	}
	if call.eventType != corev1.EventTypeWarning || call.reason != "TypedFailure" || call.action != "Validating" {
		t.Fatalf("RecordTypedErrorEvent() event fields = %#v", call)
	}
	if call.note != "%s" || len(call.args) != 1 || call.args[0] != "wrapped: typed failure" {
		t.Fatalf("RecordTypedErrorEvent() note/args = %q %#v", call.note, call.args)
	}
}

func TestRecordTypedErrorEventRecoversRecorderPanic(t *testing.T) {
	t.Parallel()

	apierrors.RecordTypedErrorEvent(
		&fakeEventRecorder{panic: true},
		&corev1.ConfigMap{},
		nil,
		fakeEventedError{},
	)
}
