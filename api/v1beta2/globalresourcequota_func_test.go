package v1beta2_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("GlobalResourceQuota", func() {

	Context("GetQuotaSpace", func() {
		var grq *capsulev1beta2.GlobalResourceQuota

		BeforeEach(func() {
			grq = &capsulev1beta2.GlobalResourceQuota{
				Spec: capsulev1beta2.GlobalResourceQuotaSpec{
					Items: map[api.Name]corev1.ResourceQuotaSpec{
						"compute": {
							Hard: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("8"),
								corev1.ResourceMemory: resource.MustParse("16Gi"),
							},
						},
					},
				},
				Status: capsulev1beta2.GlobalResourceQuotaStatus{
					Quota: map[api.Name]*corev1.ResourceQuotaStatus{
						"compute": {
							Hard: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("10"),
								corev1.ResourceMemory: resource.MustParse("32Gi"),
							},
							Used: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("4"),
								corev1.ResourceMemory: resource.MustParse("10Gi"),
							},
						},
					},
				},
			}
		})

		It("should calculate available quota correctly when status exists", func() {
			expected := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("6"),    // 10 - 4
				corev1.ResourceMemory: resource.MustParse("22Gi"), // 32Gi - 10Gi
			}

			quotaSpace, _ := grq.GetQuotaSpace("compute")
			Expect(quotaSpace).To(Equal(expected))
		})

		It("should return spec quota if status does not exist", func() {
			expected := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("8"),
				corev1.ResourceMemory: resource.MustParse("16Gi"),
			}

			quotaSpace, _ := grq.GetQuotaSpace("network") // "network" is not in Status
			Expect(quotaSpace).To(Equal(expected))
		})

		It("should handle cases where used quota is missing (default to 0)", func() {
			grq.Status.Quota["compute"].Used = nil

			expected := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10"),   // 10 - 0
				corev1.ResourceMemory: resource.MustParse("32Gi"), // 32Gi - 0
			}

			quotaSpace, _ := grq.GetQuotaSpace("compute")
			Expect(quotaSpace).To(Equal(expected))
		})

		It("should return 0 quota if used exceeds hard limit", func() {
			grq.Status.Quota["compute"].Used = corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("12"),
				corev1.ResourceMemory: resource.MustParse("40Gi"),
			}

			expected := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("0"), // Hard 10, Used 12 → should be 0
				corev1.ResourceMemory: resource.MustParse("0"), // Hard 32, Used 40 → should be 0
			}

			quotaSpace, _ := grq.GetQuotaSpace("compute")
			Expect(quotaSpace).To(Equal(expected))
		})
	})

	Context("AssignNamespaces", func() {
		var grq *capsulev1beta2.GlobalResourceQuota

		BeforeEach(func() {
			grq = &capsulev1beta2.GlobalResourceQuota{}
		})

		It("should assign only active namespaces and update status", func() {
			namespaces := []corev1.Namespace{
				{ObjectMeta: metav1.ObjectMeta{Name: "dev"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "staging"}, Status: corev1.NamespaceStatus{Phase: corev1.NamespaceActive}},
				{ObjectMeta: metav1.ObjectMeta{Name: "prod"}, Status: corev1.NamespaceStatus{Phase: corev1.NamespaceActive}},
			}

			grq.AssignNamespaces(namespaces)

			Expect(grq.Status.Namespaces).To(Equal([]string{"prod", "staging"})) // Sorted order
			Expect(grq.Status.Size).To(Equal(uint(2)))
		})

		It("should handle empty namespace list", func() {
			grq.AssignNamespaces([]corev1.Namespace{})

			Expect(grq.Status.Namespaces).To(BeEmpty())
			Expect(grq.Status.Size).To(Equal(uint(0)))
		})

		It("should ignore inactive namespaces", func() {
			namespaces := []corev1.Namespace{
				{ObjectMeta: metav1.ObjectMeta{Name: "inactive"}, Status: corev1.NamespaceStatus{Phase: corev1.NamespaceTerminating}},
				{ObjectMeta: metav1.ObjectMeta{Name: "active"}, Status: corev1.NamespaceStatus{Phase: corev1.NamespaceActive}},
			}

			grq.AssignNamespaces(namespaces)

			Expect(grq.Status.Namespaces).To(Equal([]string{"active"})) // Only active namespaces are assigned
			Expect(grq.Status.Size).To(Equal(uint(1)))
		})

		It("should sort namespaces alphabetically", func() {
			namespaces := []corev1.Namespace{
				{ObjectMeta: metav1.ObjectMeta{Name: "zeta"}, Status: corev1.NamespaceStatus{Phase: corev1.NamespaceActive}},
				{ObjectMeta: metav1.ObjectMeta{Name: "alpha"}, Status: corev1.NamespaceStatus{Phase: corev1.NamespaceActive}},
				{ObjectMeta: metav1.ObjectMeta{Name: "beta"}, Status: corev1.NamespaceStatus{Phase: corev1.NamespaceActive}},
			}

			grq.AssignNamespaces(namespaces)

			Expect(grq.Status.Namespaces).To(Equal([]string{"alpha", "beta", "zeta"}))
			Expect(grq.Status.Size).To(Equal(uint(3)))
		})
	})
})
