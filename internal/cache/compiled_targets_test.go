// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"errors"
	"sync"
	"sync/atomic"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

var _ = Describe("CompiledTargetsCache", func() {
	var c *CompiledTargetsCache[string]

	target := func(kind string) CompiledTarget {
		return CompiledTarget{
			CustomQuotaStatusTarget: capsulev1beta2.CustomQuotaStatusTarget{
				CustomQuotaSpecSource: capsulev1beta2.CustomQuotaSpecSource{
					GroupVersionKind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    kind,
					},
				},
			},
		}
	}

	BeforeEach(func() {
		c = NewCompiledTargetsCache[string]()
	})

	It("returns false when key is missing", func() {
		value, ok := c.Get("missing")

		Expect(ok).To(BeFalse())
		Expect(value).To(BeNil())
	})

	It("sets and gets values", func() {
		c.Set("quota-a", []CompiledTarget{target("Pod"), target("Service")})

		value, ok := c.Get("quota-a")

		Expect(ok).To(BeTrue())
		Expect(value).To(HaveLen(2))
		Expect(value[0].Kind).To(Equal("Pod"))
		Expect(value[1].Kind).To(Equal("Service"))
		Expect(c.Stats()).To(Equal(1))
	})

	It("returns a copy from Get", func() {
		c.Set("quota-a", []CompiledTarget{target("Pod")})

		value, ok := c.Get("quota-a")
		Expect(ok).To(BeTrue())

		value[0].Kind = "Mutated"
		value = append(value, target("Service"))

		again, ok := c.Get("quota-a")
		Expect(ok).To(BeTrue())
		Expect(again).To(HaveLen(1))
		Expect(again[0].Kind).To(Equal("Pod"))
	})

	It("stores a copy from Set", func() {
		original := []CompiledTarget{target("Pod")}

		c.Set("quota-a", original)

		original[0].Kind = "Mutated"
		original = append(original, target("Service"))

		value, ok := c.Get("quota-a")
		Expect(ok).To(BeTrue())
		Expect(value).To(HaveLen(1))
		Expect(value[0].Kind).To(Equal("Pod"))
	})

	It("builds missing values with GetOrBuild", func() {
		var calls atomic.Int32

		value, err := c.GetOrBuild("quota-a", func() ([]CompiledTarget, error) {
			calls.Add(1)
			return []CompiledTarget{target("Pod")}, nil
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(value).To(HaveLen(1))
		Expect(value[0].Kind).To(Equal("Pod"))
		Expect(calls.Load()).To(Equal(int32(1)))

		cached, ok := c.Get("quota-a")
		Expect(ok).To(BeTrue())
		Expect(cached).To(HaveLen(1))
	})

	It("does not call build when key already exists", func() {
		c.Set("quota-a", []CompiledTarget{target("Pod")})

		value, err := c.GetOrBuild("quota-a", func() ([]CompiledTarget, error) {
			return nil, errors.New("should not be called")
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(value).To(HaveLen(1))
		Expect(value[0].Kind).To(Equal("Pod"))
	})

	It("does not store value when build fails", func() {
		expectedErr := errors.New("compile failed")

		value, err := c.GetOrBuild("quota-a", func() ([]CompiledTarget, error) {
			return nil, expectedErr
		})

		Expect(err).To(MatchError(expectedErr))
		Expect(value).To(BeNil())

		_, ok := c.Get("quota-a")
		Expect(ok).To(BeFalse())
		Expect(c.Stats()).To(Equal(0))
	})

	It("returns a copy from GetOrBuild", func() {
		value, err := c.GetOrBuild("quota-a", func() ([]CompiledTarget, error) {
			return []CompiledTarget{target("Pod")}, nil
		})
		Expect(err).NotTo(HaveOccurred())

		value[0].Kind = "Mutated"
		value = append(value, target("Service"))

		again, err := c.GetOrBuild("quota-a", func() ([]CompiledTarget, error) {
			return []CompiledTarget{target("ShouldNotBuild")}, nil
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(again).To(HaveLen(1))
		Expect(again[0].Kind).To(Equal("Pod"))
	})

	It("deletes existing values", func() {
		c.Set("quota-a", []CompiledTarget{target("Pod")})

		Expect(c.Delete("quota-a")).To(BeTrue())
		Expect(c.Delete("quota-a")).To(BeFalse())

		_, ok := c.Get("quota-a")
		Expect(ok).To(BeFalse())
		Expect(c.Stats()).To(Equal(0))
	})

	It("resets all values", func() {
		c.Set("quota-a", []CompiledTarget{target("Pod")})
		c.Set("quota-b", []CompiledTarget{target("Service")})

		Expect(c.Stats()).To(Equal(2))

		c.Reset()

		Expect(c.Stats()).To(Equal(0))

		_, ok := c.Get("quota-a")
		Expect(ok).To(BeFalse())
	})

	It("prunes inactive keys", func() {
		c.Set("quota-a", []CompiledTarget{target("Pod")})
		c.Set("quota-b", []CompiledTarget{target("Service")})
		c.Set("quota-c", []CompiledTarget{target("PersistentVolumeClaim")})

		pruned := c.PruneActive(map[string]struct{}{
			"quota-a": {},
			"quota-c": {},
		})

		Expect(pruned).To(Equal(1))
		Expect(c.Stats()).To(Equal(2))

		_, ok := c.Get("quota-a")
		Expect(ok).To(BeTrue())

		_, ok = c.Get("quota-b")
		Expect(ok).To(BeFalse())

		_, ok = c.Get("quota-c")
		Expect(ok).To(BeTrue())
	})

	It("prunes all keys when active set is empty", func() {
		c.Set("quota-a", []CompiledTarget{target("Pod")})
		c.Set("quota-b", []CompiledTarget{target("Service")})

		Expect(c.PruneActive(map[string]struct{}{})).To(Equal(2))
		Expect(c.Stats()).To(Equal(0))
	})

	It("is safe under concurrent GetOrBuild calls for the same key", func() {
		var calls atomic.Int32
		const workers = 32

		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(workers)

		for i := 0; i < workers; i++ {
			go func() {
				defer GinkgoRecover()
				defer wg.Done()

				<-start

				value, err := c.GetOrBuild("quota-a", func() ([]CompiledTarget, error) {
					calls.Add(1)
					return []CompiledTarget{target("Pod")}, nil
				})

				Expect(err).NotTo(HaveOccurred())
				Expect(value).To(HaveLen(1))
				Expect(value[0].Kind).To(Equal("Pod"))
			}()
		}

		close(start)
		wg.Wait()

		Expect(calls.Load()).To(Equal(int32(1)))
		Expect(c.Stats()).To(Equal(1))
	})
})
