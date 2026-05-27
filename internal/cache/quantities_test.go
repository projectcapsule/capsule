// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"sync"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("QuantityCache", func() {
	var (
		c     *QuantityCache[string]
		now   time.Time
		key   string
		limit resource.Quantity
		used  resource.Quantity
	)

	reservation := func(id string, qty string) Reservation {
		return Reservation{
			ID:        id,
			Usage:     resource.MustParse(qty),
			UID:       types.UID("uid-" + id),
			Group:     "",
			Version:   "v1",
			Kind:      "Pod",
			Namespace: "default",
			Name:      "pod-" + id,
		}
	}

	BeforeEach(func() {
		now = time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
		c = NewQuantityCache[string]()
		c.clock = func() time.Time { return now }

		key = "quota-a"
		limit = resource.MustParse("5")
		used = resource.MustParse("0")
	})

	It("returns false for missing entries", func() {
		entry, ok := c.Get(key)

		Expect(ok).To(BeFalse())
		Expect(entry.Reservations).To(BeNil())
		Expect(entry.PendingDeletes).To(BeNil())
		Expect(entry.Reserved.IsZero()).To(BeTrue())
	})

	It("creates a reservation when allowed", func() {
		allowed, effectiveUsed, entry := c.UpsertReservation(key, reservation("a", "1"), used, limit)

		Expect(allowed).To(BeTrue())
		Expect(effectiveUsed.Cmp(resource.MustParse("1"))).To(Equal(0))
		Expect(entry.Reserved.Cmp(resource.MustParse("1"))).To(Equal(0))
		Expect(entry.Reservations).To(HaveLen(1))
		Expect(entry.Reservations["a"].CreatedAt).To(Equal(now))
		Expect(entry.Reservations["a"].UpdatedAt).To(Equal(now))
		Expect(c.Len()).To(Equal(1))
	})

	It("rejects a reservation when it would exceed the limit", func() {
		allowed, effectiveUsed, entry := c.UpsertReservation(
			key,
			reservation("a", "2"),
			resource.MustParse("4"),
			resource.MustParse("5"),
		)

		Expect(allowed).To(BeFalse())
		Expect(effectiveUsed.Cmp(resource.MustParse("4"))).To(Equal(0))
		Expect(entry.Reserved.IsZero()).To(BeTrue())
		Expect(c.Len()).To(Equal(0))
	})

	It("keeps existing reservations unchanged when a new reservation is rejected", func() {
		allowed, _, _ := c.UpsertReservation(key, reservation("a", "2"), used, limit)
		Expect(allowed).To(BeTrue())

		allowed, effectiveUsed, entry := c.UpsertReservation(
			key,
			reservation("b", "4"),
			used,
			limit,
		)

		Expect(allowed).To(BeFalse())
		Expect(effectiveUsed.Cmp(resource.MustParse("2"))).To(Equal(0))
		Expect(entry.Reserved.Cmp(resource.MustParse("2"))).To(Equal(0))
		Expect(entry.Reservations).To(HaveLen(1))
		Expect(entry.Reservations).To(HaveKey("a"))

		stored, ok := c.Get(key)
		Expect(ok).To(BeTrue())
		Expect(stored.Reserved.Cmp(resource.MustParse("2"))).To(Equal(0))
		Expect(stored.Reservations).To(HaveLen(1))
	})

	It("is idempotent for the same reservation identity and usage", func() {
		allowed, _, first := c.UpsertReservation(key, reservation("a", "1"), used, limit)
		Expect(allowed).To(BeTrue())

		now = now.Add(time.Minute)

		allowed, effectiveUsed, second := c.UpsertReservation(key, reservation("a", "1"), used, limit)

		Expect(allowed).To(BeTrue())
		Expect(effectiveUsed.Cmp(resource.MustParse("1"))).To(Equal(0))
		Expect(second.Reserved.Cmp(resource.MustParse("1"))).To(Equal(0))
		Expect(second.Reservations).To(HaveLen(1))
		Expect(second.Reservations["a"].CreatedAt).To(Equal(first.Reservations["a"].CreatedAt))
		Expect(second.Reservations["a"].UpdatedAt).To(Equal(first.Reservations["a"].UpdatedAt))
		Expect(second.LastAccess).To(Equal(now))
	})

	It("updates an existing reservation when usage changes", func() {
		allowed, _, first := c.UpsertReservation(key, reservation("a", "1"), used, limit)
		Expect(allowed).To(BeTrue())

		now = now.Add(time.Minute)

		allowed, effectiveUsed, second := c.UpsertReservation(key, reservation("a", "3"), used, limit)

		Expect(allowed).To(BeTrue())
		Expect(effectiveUsed.Cmp(resource.MustParse("3"))).To(Equal(0))
		Expect(second.Reserved.Cmp(resource.MustParse("3"))).To(Equal(0))
		Expect(second.Reservations).To(HaveLen(1))
		Expect(second.Reservations["a"].CreatedAt).To(Equal(first.Reservations["a"].CreatedAt))
		Expect(second.Reservations["a"].UpdatedAt).To(Equal(now))
	})

	It("updates an existing reservation when object identity changes", func() {
		res := reservation("a", "1")

		allowed, _, first := c.UpsertReservation(key, res, used, limit)
		Expect(allowed).To(BeTrue())

		now = now.Add(time.Minute)
		res.Name = "renamed-pod"

		allowed, _, second := c.UpsertReservation(key, res, used, limit)

		Expect(allowed).To(BeTrue())
		Expect(second.Reservations["a"].Name).To(Equal("renamed-pod"))
		Expect(second.Reservations["a"].CreatedAt).To(Equal(first.Reservations["a"].CreatedAt))
		Expect(second.Reservations["a"].UpdatedAt).To(Equal(now))
	})

	It("returns defensive copies from Get", func() {
		allowed, _, _ := c.UpsertReservation(key, reservation("a", "1"), used, limit)
		Expect(allowed).To(BeTrue())

		entry, ok := c.Get(key)
		Expect(ok).To(BeTrue())

		entry.Reserved = resource.MustParse("999")
		entry.Reservations["a"] = reservation("mutated", "999")
		entry.PendingDeletes = append(entry.PendingDeletes, PendingDeleteHint{UID: "fake"})

		again, ok := c.Get(key)
		Expect(ok).To(BeTrue())
		Expect(again.Reserved.Cmp(resource.MustParse("1"))).To(Equal(0))
		Expect(again.Reservations).To(HaveKey("a"))
		Expect(again.Reservations).NotTo(HaveKey("mutated"))
		Expect(again.PendingDeletes).To(BeEmpty())
	})

	It("returns defensive copies from Snapshot", func() {
		allowed, _, _ := c.UpsertReservation(key, reservation("a", "1"), used, limit)
		Expect(allowed).To(BeTrue())

		snap := c.Snapshot()
		snap[key] = QuantityEntry{
			Reserved: resource.MustParse("999"),
		}

		entry, ok := c.Get(key)
		Expect(ok).To(BeTrue())
		Expect(entry.Reserved.Cmp(resource.MustParse("1"))).To(Equal(0))
	})

	It("deletes entries", func() {
		allowed, _, _ := c.UpsertReservation(key, reservation("a", "1"), used, limit)
		Expect(allowed).To(BeTrue())

		Expect(c.Delete(key)).To(BeTrue())
		Expect(c.Delete(key)).To(BeFalse())
		Expect(c.Len()).To(Equal(0))
	})

	It("deletes reservations and removes empty entries", func() {
		allowed, _, _ := c.UpsertReservation(key, reservation("a", "1"), used, limit)
		Expect(allowed).To(BeTrue())

		Expect(c.DeleteReservation(key, "a")).To(BeTrue())
		Expect(c.Len()).To(Equal(0))

		_, ok := c.Get(key)
		Expect(ok).To(BeFalse())
	})

	It("deletes only the selected reservation and recomputes reserved", func() {
		Expect(c.UpsertReservation(key, reservation("a", "1"), used, limit)).To(ReceiveAllowed())
		Expect(c.UpsertReservation(key, reservation("b", "2"), used, limit)).To(ReceiveAllowed())

		Expect(c.DeleteReservation(key, "a")).To(BeTrue())

		entry, ok := c.Get(key)
		Expect(ok).To(BeTrue())
		Expect(entry.Reserved.Cmp(resource.MustParse("2"))).To(Equal(0))
		Expect(entry.Reservations).To(HaveKey("b"))
		Expect(entry.Reservations).NotTo(HaveKey("a"))
	})

	It("returns false when deleting a missing reservation", func() {
		Expect(c.DeleteReservation(key, "missing")).To(BeFalse())

		allowed, _, _ := c.UpsertReservation(key, reservation("a", "1"), used, limit)
		Expect(allowed).To(BeTrue())

		Expect(c.DeleteReservation(key, "missing")).To(BeFalse())
	})

	It("purges reservations for a key", func() {
		Expect(c.UpsertReservation(key, reservation("a", "1"), used, limit)).To(ReceiveAllowed())
		Expect(c.UpsertReservation(key, reservation("b", "2"), used, limit)).To(ReceiveAllowed())
		Expect(c.UpsertReservation(key, reservation("c", "1"), used, limit)).To(ReceiveAllowed())

		deleted := c.PurgeReservationsForKey(key, func(r Reservation) bool {
			return r.ID == "a" || r.ID == "c"
		})

		Expect(deleted).To(Equal(2))

		entry, ok := c.Get(key)
		Expect(ok).To(BeTrue())
		Expect(entry.Reserved.Cmp(resource.MustParse("2"))).To(Equal(0))
		Expect(entry.Reservations).To(HaveLen(1))
		Expect(entry.Reservations).To(HaveKey("b"))
	})

	It("purges all reservations and removes entry when no pending deletes remain", func() {
		Expect(c.UpsertReservation(key, reservation("a", "1"), used, limit)).To(ReceiveAllowed())

		deleted := c.PurgeReservationsForKey(key, func(r Reservation) bool {
			return true
		})

		Expect(deleted).To(Equal(1))
		Expect(c.Len()).To(Equal(0))
	})

	It("does not remove entry after purging all reservations if pending deletes remain", func() {
		Expect(c.UpsertReservation(key, reservation("a", "1"), used, limit)).To(ReceiveAllowed())
		c.AddPendingDelete(key, "uid-a")

		deleted := c.PurgeReservationsForKey(key, func(r Reservation) bool {
			return true
		})

		Expect(deleted).To(Equal(1))

		entry, ok := c.Get(key)
		Expect(ok).To(BeTrue())
		Expect(entry.Reserved.IsZero()).To(BeTrue())
		Expect(entry.PendingDeletes).To(HaveLen(1))
	})

	It("returns zero when purging a missing key or no reservation matches", func() {
		Expect(c.PurgeReservationsForKey("missing", func(r Reservation) bool { return true })).To(Equal(0))

		Expect(c.UpsertReservation(key, reservation("a", "1"), used, limit)).To(ReceiveAllowed())

		Expect(c.PurgeReservationsForKey(key, func(r Reservation) bool { return false })).To(Equal(0))
	})

	It("adds pending deletes idempotently", func() {
		c.AddPendingDelete(key, "uid-a")
		c.AddPendingDelete(key, "uid-a")

		Expect(c.HasPendingDeletes(key)).To(BeTrue())
		Expect(c.PendingDeletes(key)).To(Equal([]types.UID{"uid-a"}))

		entry, ok := c.Get(key)
		Expect(ok).To(BeTrue())
		Expect(entry.PendingDeletes[0].CreatedAt).To(Equal(now))
	})

	It("ignores empty pending delete UIDs", func() {
		c.AddPendingDelete(key, "")

		Expect(c.Len()).To(Equal(0))
		Expect(c.PendingDeletes(key)).To(BeNil())
	})

	It("removes pending deletes and removes empty entry", func() {
		c.AddPendingDelete(key, "uid-a")

		Expect(c.RemovePendingDelete(key, "uid-a")).To(BeTrue())
		Expect(c.RemovePendingDelete(key, "uid-a")).To(BeFalse())
		Expect(c.Len()).To(Equal(0))
	})

	It("removes pending delete but keeps entry when reservations remain", func() {
		Expect(c.UpsertReservation(key, reservation("a", "1"), used, limit)).To(ReceiveAllowed())
		c.AddPendingDelete(key, "uid-a")

		Expect(c.RemovePendingDelete(key, "uid-a")).To(BeTrue())

		entry, ok := c.Get(key)
		Expect(ok).To(BeTrue())
		Expect(entry.Reserved.Cmp(resource.MustParse("1"))).To(Equal(0))
		Expect(entry.PendingDeletes).To(BeEmpty())
	})

	It("returns nil pending deletes for missing keys", func() {
		Expect(c.PendingDeletes("missing")).To(BeNil())
		Expect(c.HasPendingDeletes("missing")).To(BeFalse())
	})

	It("clamps negative reserved totals to zero", func() {
		Expect(c.UpsertReservation(key, reservation("a", "-5"), used, limit)).To(ReceiveAllowed())

		entry, ok := c.Get(key)
		Expect(ok).To(BeTrue())
		Expect(entry.Reserved.IsZero()).To(BeTrue())
	})

	It("is safe under concurrent reservation upserts", func() {
		const workers = 32

		c.clock = time.Now
		limit := resource.MustParse("100")
		var allowedCount atomic.Int32

		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(workers)

		for i := 0; i < workers; i++ {
			i := i
			go func() {
				defer GinkgoRecover()
				defer wg.Done()

				<-start

				allowed, _, _ := c.UpsertReservation(
					key,
					reservation(string(rune('a'+i)), "1"),
					resource.MustParse("0"),
					limit,
				)
				if allowed {
					allowedCount.Add(1)
				}
			}()
		}

		close(start)
		wg.Wait()

		Expect(allowedCount.Load()).To(Equal(int32(workers)))

		entry, ok := c.Get(key)
		Expect(ok).To(BeTrue())
		Expect(entry.Reservations).To(HaveLen(workers))
		Expect(entry.Reserved.Cmp(resource.MustParse("32"))).To(Equal(0))
	})

	It("enforces limit under concurrent reservation upserts", func() {
		const workers = 16

		c.clock = time.Now
		limit := resource.MustParse("5")
		var allowedCount atomic.Int32

		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(workers)

		for i := 0; i < workers; i++ {
			i := i
			go func() {
				defer GinkgoRecover()
				defer wg.Done()

				<-start

				allowed, _, _ := c.UpsertReservation(
					key,
					reservation(string(rune('a'+i)), "1"),
					resource.MustParse("0"),
					limit,
				)
				if allowed {
					allowedCount.Add(1)
				}
			}()
		}

		close(start)
		wg.Wait()

		Expect(allowedCount.Load()).To(Equal(int32(5)))

		entry, ok := c.Get(key)
		Expect(ok).To(BeTrue())
		Expect(entry.Reservations).To(HaveLen(5))
		Expect(entry.Reserved.Cmp(resource.MustParse("5"))).To(Equal(0))
	})
})

type quantityResult struct {
	allowed       bool
	effectiveUsed resource.Quantity
	entry         QuantityEntry
}

func ReceiveAllowed() OmegaMatcher {
	return WithTransform(func(in any) bool {
		switch v := in.(type) {
		case quantityResult:
			return v.allowed
		default:
			return false
		}
	}, BeTrue())
}
