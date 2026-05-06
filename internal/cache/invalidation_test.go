// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("ShouldInvalidate", func() {
	var now time.Time

	BeforeEach(func() {
		now = time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
	})

	It("returns false when interval is zero", func() {
		Expect(ShouldInvalidate(nil, now, 0)).To(BeFalse())
	})

	It("returns false when interval is negative", func() {
		Expect(ShouldInvalidate(nil, now, -time.Second)).To(BeFalse())
	})

	It("returns true when last is nil and interval is positive", func() {
		Expect(ShouldInvalidate(nil, now, time.Minute)).To(BeTrue())
	})

	It("returns true when last is zero and interval is positive", func() {
		last := &metav1.Time{}

		Expect(ShouldInvalidate(last, now, time.Minute)).To(BeTrue())
	})

	It("returns false when last is after now", func() {
		last := metav1.NewTime(now.Add(time.Minute))

		Expect(ShouldInvalidate(&last, now, time.Minute)).To(BeFalse())
	})

	It("returns false when elapsed time is below interval", func() {
		last := metav1.NewTime(now.Add(-30 * time.Second))

		Expect(ShouldInvalidate(&last, now, time.Minute)).To(BeFalse())
	})

	It("returns true when elapsed time equals interval", func() {
		last := metav1.NewTime(now.Add(-time.Minute))

		Expect(ShouldInvalidate(&last, now, time.Minute)).To(BeTrue())
	})

	It("returns true when elapsed time is greater than interval", func() {
		last := metav1.NewTime(now.Add(-2 * time.Minute))

		Expect(ShouldInvalidate(&last, now, time.Minute)).To(BeTrue())
	})
})
