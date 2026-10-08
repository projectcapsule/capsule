// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package events

import "k8s.io/apimachinery/pkg/runtime"

// NewDiscardRecorder returns a recorder that never publishes Events. It is safe
// to share across requests; each labeled event retains its own metadata.
func NewDiscardRecorder() EventRecorder {
	return discardRecorder{}
}

type discardRecorder struct{}

func (discardRecorder) Eventf(_, _ runtime.Object, _, _, _, _ string, _ ...any) {}

func (discardRecorder) LabeledEvent(regarding runtime.Object, eventType, reason, action, note string) LabeledEvent {
	return newLabeledEvent(nil, regarding, eventType, reason, action, note)
}
