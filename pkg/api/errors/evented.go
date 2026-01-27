// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package errors

import (
	"errors"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
)

type EventedError interface {
	error
	Reason() string // UpperCamelCase, short
	Action() string // UpperCamelCase, short (<=128)
}

func RecordTypedErrorEvent(
	recorder events.EventRecorder,
	regarding runtime.Object,
	related runtime.Object,
	err error,
) {
	if recorder == nil || regarding == nil || err == nil {
		return
	}

	var ee EventedError
	if !errors.As(err, &ee) {
		return
	}

	defer func() { _ = recover() }()

	recorder.Eventf(
		regarding,
		related,
		corev1.EventTypeWarning,
		ee.Reason(),
		ee.Action(),
		"%s", // note
		err.Error(),
	)
}
