// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"

	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

var _ = Describe("changing Tenant managed Kubernetes resources", Label("tenant", "managed", "current"), func() {
	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-resources-changes",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: api.OwnerListSpec{
				{
					CoreOwnerSpec: api.CoreOwnerSpec{
						UserSpec: api.UserSpec{
							Name: "laura",
							Kind: "User",
						},
					},
				},
			},
			LimitRanges: api.LimitRangesSpec{Items: []corev1.LimitRangeSpec{
				{
					Limits: []corev1.LimitRangeItem{
						{
							Type: corev1.LimitTypePod,
							Min: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceCPU:    resource.MustParse("50m"),
								corev1.ResourceMemory: resource.MustParse("5Mi"),
							},
							Max: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceCPU:    resource.MustParse("1"),
								corev1.ResourceMemory: resource.MustParse("1Gi"),
							},
						},
						{
							Type: corev1.LimitTypeContainer,
							Default: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceCPU:    resource.MustParse("200m"),
								corev1.ResourceMemory: resource.MustParse("100Mi"),
							},
							DefaultRequest: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("10Mi"),
							},
							Min: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceCPU:    resource.MustParse("50m"),
								corev1.ResourceMemory: resource.MustParse("5Mi"),
							},
							Max: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceCPU:    resource.MustParse("1"),
								corev1.ResourceMemory: resource.MustParse("1Gi"),
							},
						},
						{
							Type: corev1.LimitTypePersistentVolumeClaim,
							Min: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceStorage: resource.MustParse("1Gi"),
							},
							Max: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceStorage: resource.MustParse("10Gi"),
							},
						},
					},
				},
			},
			},
			NetworkPolicies: api.NetworkPolicySpec{Items: []networkingv1.NetworkPolicySpec{
				{
					Ingress: []networkingv1.NetworkPolicyIngressRule{
						{
							From: []networkingv1.NetworkPolicyPeer{
								{
									NamespaceSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											"capsule.clastix.io/tenant": "tenant-resources",
										},
									},
								},
								{
									PodSelector: &metav1.LabelSelector{},
								},
								{
									IPBlock: &networkingv1.IPBlock{
										CIDR: "192.168.0.0/12",
									},
								},
							},
						},
					},
					Egress: []networkingv1.NetworkPolicyEgressRule{
						{
							To: []networkingv1.NetworkPolicyPeer{
								{
									IPBlock: &networkingv1.IPBlock{
										CIDR: "0.0.0.0/0",
										Except: []string{
											"192.168.0.0/12",
										},
									},
								},
							},
						},
					},
					PodSelector: metav1.LabelSelector{},
					PolicyTypes: []networkingv1.PolicyType{
						networkingv1.PolicyTypeIngress,
						networkingv1.PolicyTypeEgress,
					},
				},
			},
			},
			NodeSelector: map[string]string{
				"kubernetes.io/os": "linux",
			},
			ResourceQuota: api.ResourceQuotaSpec{Items: []corev1.ResourceQuotaSpec{
				{
					Hard: map[corev1.ResourceName]resource.Quantity{
						corev1.ResourceLimitsCPU:      resource.MustParse("8"),
						corev1.ResourceLimitsMemory:   resource.MustParse("16Gi"),
						corev1.ResourceRequestsCPU:    resource.MustParse("8"),
						corev1.ResourceRequestsMemory: resource.MustParse("16Gi"),
					},
					Scopes: []corev1.ResourceQuotaScope{
						corev1.ResourceQuotaScopeNotTerminating,
					},
				},
				{
					Hard: map[corev1.ResourceName]resource.Quantity{
						corev1.ResourcePods: resource.MustParse("10"),
					},
				},
				{
					Hard: map[corev1.ResourceName]resource.Quantity{
						corev1.ResourceRequestsStorage: resource.MustParse("100Gi"),
					},
				},
			},
			},
		},
	}
	nsl := []string{"fire", "walk", "with", "me"}
	JustBeforeEach(func() {
		EventuallyCreation(func() error {
			tnt.ResourceVersion = ""
			return k8sClient.Create(context.TODO(), tnt)
		}).Should(Succeed())
		By("creating the Namespaces", func() {
			for _, i := range nsl {
				ns := NewNamespace(i)
				NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
				TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))
			}
		})
	})
	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), tnt)).Should(Succeed())
	})
	It("should reapply the original resources upon third party change", func() {

		sampleUser := "test@user.com"

		for _, ns := range nsl {
			By("changing Limit Range", func() {

				ensureTamperRoleBinding(
					context.TODO(),
					k8sClient,
					ns,
					sampleUser,
					"allow-tamper-limitrange",
					"",
					"limitranges",
				)

				for i, s := range tnt.Spec.LimitRanges.Items {
					n := fmt.Sprintf("capsule-%s-%d", tnt.GetName(), i)
					lr := &corev1.LimitRange{}
					Eventually(func() error {
						return k8sClient.Get(context.TODO(), types.NamespacedName{Name: n, Namespace: ns}, lr)
					}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

					// Delete As owner in the namespace should fails
					cs := impersonationClient(tnt.Spec.Owners[0].UserSpec.Name, withDefaultGroups(nil))

					By(fmt.Sprintf("owner cannot delete limitrange"), func() {
						obj := &corev1.LimitRange{ObjectMeta: metav1.ObjectMeta{Name: n, Namespace: ns}}
						err := cs.Delete(context.TODO(), obj)
						Expect(err).To(HaveOccurred())
					})

					By(fmt.Sprintf("owner cannot update limitrange"), func() {
						current := &corev1.LimitRange{}
						Expect(cs.Get(context.TODO(), types.NamespacedName{Name: n, Namespace: ns}, current)).To(Succeed())

						mut := current.DeepCopy()
						mut.Spec.Limits = []corev1.LimitRangeItem{}
						err := cs.Update(context.TODO(), mut)
						Expect(err).To(HaveOccurred())
					})

					// Delete As any user in the namespace should fails
					csu := impersonationClient(sampleUser, nil)

					By(fmt.Sprintf("user cannot delete limitrange"), func() {
						obj := &corev1.LimitRange{ObjectMeta: metav1.ObjectMeta{Name: n, Namespace: ns}}
						err := csu.Delete(context.TODO(), obj)
						Expect(err).To(HaveOccurred())
					})

					By(fmt.Sprintf("user cannot update limitrange"), func() {
						current := &corev1.LimitRange{}
						Expect(csu.Get(context.TODO(), types.NamespacedName{Name: n, Namespace: ns}, current)).To(Succeed())

						mut := current.DeepCopy()
						mut.Spec.Limits = []corev1.LimitRangeItem{}
						err := csu.Update(context.TODO(), mut)
						Expect(err).To(HaveOccurred())
					})

					c := lr.DeepCopy()
					c.Spec.Limits = []corev1.LimitRangeItem{}
					Expect(k8sClient.Update(context.TODO(), c, &client.UpdateOptions{})).Should(Succeed())

					Eventually(func() corev1.LimitRangeSpec {
						Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: n, Namespace: ns}, lr)).Should(Succeed())
						return lr.Spec
					}, defaultTimeoutInterval, defaultPollInterval).Should(Equal(s))
				}
			})
			By("changing Network Policy", func() {
				for i, s := range tnt.Spec.NetworkPolicies.Items {
					n := fmt.Sprintf("capsule-%s-%d", tnt.GetName(), i)
					np := &networkingv1.NetworkPolicy{}
					Eventually(func() error {
						return k8sClient.Get(context.TODO(), types.NamespacedName{Name: n, Namespace: ns}, np)
					}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
					Expect(np.Spec).Should(Equal(s))

					// Delete As owner in the namespace should fails
					cs := impersonationClient(tnt.Spec.Owners[0].UserSpec.Name, withDefaultGroups(nil))

					By(fmt.Sprintf("owner cannot delete netpol"), func() {
						obj := &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: n, Namespace: ns}}
						err := cs.Delete(context.TODO(), obj)
						Expect(err).To(HaveOccurred())
					})

					By(fmt.Sprintf("owner cannot update netpol"), func() {
						current := &networkingv1.NetworkPolicy{}
						Expect(cs.Get(context.TODO(), types.NamespacedName{Name: n, Namespace: ns}, current)).To(Succeed())

						mut := current.DeepCopy()
						mut.Spec.PodSelector = metav1.LabelSelector{
							MatchLabels: map[string]string{
								"something": "custom",
							},
						}
						err := cs.Update(context.TODO(), mut)
						Expect(err).To(HaveOccurred())
					})

					// Delete As owner in the namespace should fails
					csu := impersonationClient(sampleUser, nil)

					By(fmt.Sprintf("user cannot delete netpol"), func() {
						obj := &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: n, Namespace: ns}}
						err := csu.Delete(context.TODO(), obj)
						Expect(err).To(HaveOccurred())
					})

					By(fmt.Sprintf("user cannot update netpol"), func() {
						current := &networkingv1.NetworkPolicy{}
						Expect(csu.Get(context.TODO(), types.NamespacedName{Name: n, Namespace: ns}, current)).To(Succeed())

						mut := current.DeepCopy()
						mut.Spec.PodSelector = metav1.LabelSelector{
							MatchLabels: map[string]string{
								"something": "custom",
							},
						}
						err := csu.Update(context.TODO(), mut)
						Expect(err).To(HaveOccurred())
					})

					c := np.DeepCopy()
					c.Spec.Egress = []networkingv1.NetworkPolicyEgressRule{}
					c.Spec.Ingress = []networkingv1.NetworkPolicyIngressRule{}
					Expect(k8sClient.Update(context.TODO(), c, &client.UpdateOptions{})).Should(Succeed())

					Eventually(func() networkingv1.NetworkPolicySpec {
						Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: n, Namespace: ns}, np)).Should(Succeed())
						return np.Spec
					}, defaultTimeoutInterval, defaultPollInterval).Should(Equal(s))
				}
			})
			By("changing Resource Quota", func() {
				for i, s := range tnt.Spec.ResourceQuota.Items {
					n := fmt.Sprintf("capsule-%s-%d", tnt.GetName(), i)
					rq := &corev1.ResourceQuota{}
					Eventually(func() error {
						return k8sClient.Get(context.TODO(), types.NamespacedName{Name: n, Namespace: ns}, rq)
					}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

					// Delete As owner in the namespace should fails
					cs := impersonationClient(tnt.Spec.Owners[0].UserSpec.Name, withDefaultGroups(nil))

					By(fmt.Sprintf("owner cannot delete resourcequota"), func() {
						obj := &corev1.ResourceQuota{ObjectMeta: metav1.ObjectMeta{Name: n, Namespace: ns}}
						err := cs.Delete(context.TODO(), obj)
						Expect(err).To(HaveOccurred())
					})

					By(fmt.Sprintf("owner cannot update resourcequota"), func() {
						current := &corev1.ResourceQuota{}
						Expect(cs.Get(context.TODO(), types.NamespacedName{Name: n, Namespace: ns}, current)).To(Succeed())

						mut := current.DeepCopy()
						mut.SetLabels(map[string]string{
							meta.ManagedByCapsuleLabel: "someone-else",
						})

						err := cs.Update(context.TODO(), mut)
						Expect(err).To(HaveOccurred())
					})

					// Delete As owner in the namespace should fails
					csu := impersonationClient(sampleUser, nil)

					By(fmt.Sprintf("user cannot delete resourcequota"), func() {
						obj := &corev1.ResourceQuota{ObjectMeta: metav1.ObjectMeta{Name: n, Namespace: ns}}
						err := csu.Delete(context.TODO(), obj)
						Expect(err).To(HaveOccurred())
					})

					By(fmt.Sprintf("user cannot update resourcequota"), func() {
						current := &corev1.ResourceQuota{}
						Expect(csu.Get(context.TODO(), types.NamespacedName{Name: n, Namespace: ns}, current)).To(Succeed())

						mut := current.DeepCopy()
						mut.SetLabels(map[string]string{
							meta.ManagedByCapsuleLabel: "someone-else",
						})

						err := csu.Update(context.TODO(), mut)
						Expect(err).To(HaveOccurred())
					})

					c := rq.DeepCopy()
					c.Spec.Hard = map[corev1.ResourceName]resource.Quantity{}
					Expect(k8sClient.Update(context.TODO(), c, &client.UpdateOptions{})).Should(Succeed())

					Eventually(func() corev1.ResourceQuotaSpec {
						Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: n, Namespace: ns}, rq)).Should(Succeed())
						return rq.Spec
					}, defaultTimeoutInterval, defaultPollInterval).Should(Equal(s))
				}
			})
		}
	})
})

func ensureTamperRoleBinding(
	ctx context.Context,
	k8sClient client.Client,
	ns string,
	user string,
	roleName string,
	apiGroup string,
	resource string,
) {
	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      roleName,
			Namespace: ns,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{apiGroup},
				Resources: []string{resource},
				Verbs:     []string{"get", "list", "watch", "update", "patch", "delete"},
			},
		},
	}

	Expect(k8sClient.Create(ctx, role)).To(SatisfyAny(Succeed(), WithTransform(apierrors.IsAlreadyExists, BeTrue())))

	rb := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      roleName,
			Namespace: ns,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "Role",
			Name:     roleName,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:     rbacv1.UserKind,
				APIGroup: rbacv1.GroupName,
				Name:     user,
			},
		},
	}

	Expect(k8sClient.Create(ctx, rb)).To(SatisfyAny(Succeed(), WithTransform(apierrors.IsAlreadyExists, BeTrue())))
}
