// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package predicates_test

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/projectcapsule/capsule/pkg/runtime/predicates"
)

func TestCreatedAfterPredicate_Create(t *testing.T) {
	t.Parallel()

	since := time.Date(2026, time.August, 11, 10, 0, 0, 0, time.UTC)
	p := predicates.NewCreatedAfterPredicate(since)

	namespace := func(created time.Time) *corev1.Namespace {
		return &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "example",
				CreationTimestamp: metav1.NewTime(created),
			},
		}
	}

	for _, tc := range []struct {
		name string
		obj  *corev1.Namespace
		want bool
	}{
		{
			name: "created afterwards",
			obj:  namespace(since.Add(time.Second)),
			want: true,
		},
		{
			name: "created within the very same second",
			obj:  namespace(since),
			want: true,
		},
		{
			name: "already existing",
			obj:  namespace(since.Add(-time.Hour)),
			want: false,
		},
		{
			name: "no creation timestamp",
			obj:  &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "example"}},
			want: false,
		},
	} {
		if got := p.Create(event.CreateEvent{Object: tc.obj}); got != tc.want {
			t.Fatalf("%s: expected %v, got %v", tc.name, tc.want, got)
		}
	}

	if p.Create(event.CreateEvent{}) {
		t.Fatal("expected a nil object to be discarded")
	}
}

func TestCreatedAfterPredicate_TruncatesToSecond(t *testing.T) {
	t.Parallel()

	// The API Server tracks the creation timestamp with a second granularity: a Namespace
	// created right after the start up must not be discarded because of the sub-second
	// remainder of the lower bound.
	since := time.Date(2026, time.August, 11, 10, 0, 0, 500_000_000, time.UTC)
	p := predicates.NewCreatedAfterPredicate(since)

	created := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			CreationTimestamp: metav1.NewTime(since.Truncate(time.Second)),
		},
	}

	if !p.Create(event.CreateEvent{Object: created}) {
		t.Fatal("expected the object created within the same second to be passed")
	}
}

func TestCreatedAfterPredicate_IgnoresAnyOtherEvent(t *testing.T) {
	t.Parallel()

	p := predicates.NewCreatedAfterPredicate(time.Date(2026, time.August, 11, 10, 0, 0, 0, time.UTC))

	recent := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			CreationTimestamp: metav1.NewTime(time.Date(2026, time.August, 11, 11, 0, 0, 0, time.UTC)),
		},
	}

	if p.Update(event.UpdateEvent{ObjectOld: recent, ObjectNew: recent}) {
		t.Fatal("expected update events to be discarded")
	}

	if p.Delete(event.DeleteEvent{Object: recent}) {
		t.Fatal("expected delete events to be discarded")
	}

	if p.Generic(event.GenericEvent{Object: recent}) {
		t.Fatal("expected generic events to be discarded")
	}
}
