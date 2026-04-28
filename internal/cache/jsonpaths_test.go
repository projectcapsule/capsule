// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"sync"
	"sync/atomic"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("JSONPathCache", func() {
	var c *JSONPathCache

	BeforeEach(func() {
		c = NewJSONPathCache()
	})

	It("returns false when path is missing", func() {
		value, ok := c.Get(".spec.missing")

		Expect(ok).To(BeFalse())
		Expect(value).To(BeNil())
	})

	It("compiles and stores a JSONPath", func() {
		compiled, err := c.GetOrCompile(".spec.containers[*].resources.requests.cpu")

		Expect(err).NotTo(HaveOccurred())
		Expect(compiled).NotTo(BeNil())
		Expect(c.Stats()).To(Equal(1))

		cached, ok := c.Get(".spec.containers[*].resources.requests.cpu")
		Expect(ok).To(BeTrue())
		Expect(cached).To(BeIdenticalTo(compiled))
	})

	It("returns the cached instance on repeated GetOrCompile calls", func() {
		first, err := c.GetOrCompile(".spec.resources.requests.storage")
		Expect(err).NotTo(HaveOccurred())

		second, err := c.GetOrCompile(".spec.resources.requests.storage")
		Expect(err).NotTo(HaveOccurred())

		Expect(second).To(BeIdenticalTo(first))
		Expect(c.Stats()).To(Equal(1))
	})

	It("does not store invalid JSONPath expressions", func() {
		compiled, err := c.GetOrCompile(".spec.containers[?(")

		Expect(err).To(HaveOccurred())
		Expect(compiled).To(BeNil())
		Expect(c.Stats()).To(Equal(0))

		_, ok := c.Get(".spec.containers[?(")
		Expect(ok).To(BeFalse())
	})

	It("deletes an existing path", func() {
		_, err := c.GetOrCompile(".spec.replicas")
		Expect(err).NotTo(HaveOccurred())

		Expect(c.Delete(".spec.replicas")).To(BeTrue())
		Expect(c.Delete(".spec.replicas")).To(BeFalse())

		_, ok := c.Get(".spec.replicas")
		Expect(ok).To(BeFalse())
		Expect(c.Stats()).To(Equal(0))
	})

	It("deletes many paths and ignores missing or empty expressions", func() {
		_, err := c.GetOrCompile(".spec.a")
		Expect(err).NotTo(HaveOccurred())

		_, err = c.GetOrCompile(".spec.b")
		Expect(err).NotTo(HaveOccurred())

		_, err = c.GetOrCompile(".spec.c")
		Expect(err).NotTo(HaveOccurred())

		deleted := c.DeleteMany(".spec.a", "", ".spec.missing", ".spec.c")

		Expect(deleted).To(Equal(2))
		Expect(c.Stats()).To(Equal(1))

		_, ok := c.Get(".spec.a")
		Expect(ok).To(BeFalse())

		_, ok = c.Get(".spec.b")
		Expect(ok).To(BeTrue())

		_, ok = c.Get(".spec.c")
		Expect(ok).To(BeFalse())
	})

	It("resets all compiled paths", func() {
		_, err := c.GetOrCompile(".spec.a")
		Expect(err).NotTo(HaveOccurred())

		_, err = c.GetOrCompile(".spec.b")
		Expect(err).NotTo(HaveOccurred())

		Expect(c.Stats()).To(Equal(2))

		c.Reset()

		Expect(c.Stats()).To(Equal(0))

		_, ok := c.Get(".spec.a")
		Expect(ok).To(BeFalse())
	})

	It("prunes inactive paths", func() {
		_, err := c.GetOrCompile(".spec.a")
		Expect(err).NotTo(HaveOccurred())

		_, err = c.GetOrCompile(".spec.b")
		Expect(err).NotTo(HaveOccurred())

		_, err = c.GetOrCompile(".spec.c")
		Expect(err).NotTo(HaveOccurred())

		pruned := c.PruneActive(map[string]struct{}{
			".spec.a": {},
			".spec.c": {},
		})

		Expect(pruned).To(Equal(1))
		Expect(c.Stats()).To(Equal(2))

		_, ok := c.Get(".spec.a")
		Expect(ok).To(BeTrue())

		_, ok = c.Get(".spec.b")
		Expect(ok).To(BeFalse())

		_, ok = c.Get(".spec.c")
		Expect(ok).To(BeTrue())
	})

	It("prunes all paths when active set is empty", func() {
		_, err := c.GetOrCompile(".spec.a")
		Expect(err).NotTo(HaveOccurred())

		_, err = c.GetOrCompile(".spec.b")
		Expect(err).NotTo(HaveOccurred())

		Expect(c.PruneActive(map[string]struct{}{})).To(Equal(2))
		Expect(c.Stats()).To(Equal(0))
	})

	It("is safe under concurrent GetOrCompile calls for the same path", func() {
		const workers = 32

		var successes atomic.Int32
		start := make(chan struct{})

		var wg sync.WaitGroup
		wg.Add(workers)

		for i := 0; i < workers; i++ {
			go func() {
				defer GinkgoRecover()
				defer wg.Done()

				<-start

				compiled, err := c.GetOrCompile(".spec.containers[*].resources.requests.cpu")
				Expect(err).NotTo(HaveOccurred())
				Expect(compiled).NotTo(BeNil())

				successes.Add(1)
			}()
		}

		close(start)
		wg.Wait()

		Expect(successes.Load()).To(Equal(int32(workers)))
		Expect(c.Stats()).To(Equal(1))
	})
})
