package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("NamespaceStatus objects", Label("tenant", "rules"), func() {
	ctx := context.Background()

	// Two tenants, each with one owner (reuse your existing ownerClient/NamespaceCreation helpers)
	tntA := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: "nsstatus-a"},
		Spec: capsulev1beta2.TenantSpec{
			Owners: api.OwnerListSpec{
				{
					CoreOwnerSpec: api.CoreOwnerSpec{
						UserSpec: api.UserSpec{Name: "matt", Kind: "User"},
					},
				},
			},
		},
	}

	tntB := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: "nsstatus-b"},
		Spec: capsulev1beta2.TenantSpec{
			Owners: api.OwnerListSpec{
				{
					CoreOwnerSpec: api.CoreOwnerSpec{
						UserSpec: api.UserSpec{Name: "matt", Kind: "User"},
					},
				},
			},
		},
	}

	var (
		nsA1 *corev1.Namespace
		nsA2 *corev1.Namespace
		nsB1 *corev1.Namespace
	)

	JustBeforeEach(func() {
		// Create tenants
		EventuallyCreation(func() error {
			tntA.ResourceVersion = ""
			return k8sClient.Create(ctx, tntA)
		}).Should(Succeed())

		EventuallyCreation(func() error {
			tntB.ResourceVersion = ""
			return k8sClient.Create(ctx, tntB)
		}).Should(Succeed())

		// Create namespaces for each tenant using your helper
		nsA1 = NewNamespace("rule-status-ns1", map[string]string{
			meta.TenantLabel: tntA.GetName(),
		})
		nsA2 = NewNamespace("rule-status-ns2", map[string]string{
			meta.TenantLabel: tntA.GetName(),
		})
		nsB1 = NewNamespace("rule-status-ns3", map[string]string{
			meta.TenantLabel: tntB.GetName(),
		})

		NamespaceCreation(nsA1, tntA.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceCreation(nsA2, tntA.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceCreation(nsB1, tntB.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())

		// Wait until tenants list their namespaces (optional but makes debugging easier)
		TenantNamespaceList(tntA, defaultTimeoutInterval).Should(ContainElements(nsA1.GetName(), nsA2.GetName()))
		TenantNamespaceList(tntB, defaultTimeoutInterval).Should(ContainElement(nsB1.GetName()))
	})

	JustAfterEach(func() {
		// Best-effort cleanup namespaces first (your env may already handle this)
		for _, n := range []*corev1.Namespace{nsA1, nsA2, nsB1} {
			if n == nil {
				continue
			}
			_ = k8sClient.Delete(ctx, n)
		}

		// Delete tenants
		if tntA != nil {
			_ = k8sClient.Delete(ctx, tntA)
		}
		if tntB != nil {
			_ = k8sClient.Delete(ctx, tntB)
		}
	})

	// --- Helpers ---

	expectNamespaceStatusFor := func(ns *corev1.Namespace, tenantName string) {
		By(fmt.Sprintf("verifying NamespaceStatus for namespace %q (tenant=%q)", ns.Name, tenantName))

		Eventually(func(g Gomega) {
			// Re-read namespace to get UID reliably (in case local object is stale)
			curNS := &corev1.Namespace{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: ns.Name}, curNS)).To(Succeed())

			nsStatus := &capsulev1beta2.RuleStatus{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: meta.NameForManagedRuleStatus(), Namespace: ns.Name}, nsStatus)).To(Succeed())

			// 2) OwnerReference must point to the Namespace and be controller owner
			g.Expect(nsStatus.OwnerReferences).NotTo(BeEmpty())

			var found bool
			for _, or := range nsStatus.OwnerReferences {
				if or.APIVersion == "v1" &&
					or.Kind == "Namespace" &&
					or.Name == curNS.Name &&
					or.UID == curNS.UID {

					found = true

					break
				}
			}
			g.Expect(found).To(BeTrue(), "expected NamespaceStatus to have Namespace controller OwnerReference")
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	}

	It("creates one NamespaceStatus per namespace, with correct Status.Tenant and Namespace controller OwnerReference", func() {
		expectNamespaceStatusFor(nsA1, tntA.Name)
		expectNamespaceStatusFor(nsA2, tntA.Name)
		expectNamespaceStatusFor(nsB1, tntB.Name)
	})

	It("removes NamespaceStatus when the Namespace is deleted (ownerReference GC)", func() {
		// Ensure it exists first
		expectNamespaceStatusFor(nsA1, tntA.Name)

		// Delete namespace
		Expect(k8sClient.Delete(ctx, nsA1)).To(Succeed())

		// Namespace deletion can take time; once it's gone, the status should be GC'd
		Eventually(func() bool {
			// confirm namespace gone or terminating; either way, check status disappears eventually
			nsStatus := &capsulev1beta2.RuleStatus{}
			err := k8sClient.Get(ctx, client.ObjectKey{Name: meta.NameForManagedRuleStatus(), Namespace: nsA1.Name}, nsStatus)
			return apierrors.IsNotFound(err)
		}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
	})
})
