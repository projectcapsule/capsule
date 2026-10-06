// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

var _ = Describe("Service forbidden regex admission", Label("tenant", "networking", "service", "metadata", "forbidden"), func() {
	DescribeTable("rejects malformed policies and preserves tenant-scoped Service enforcement", func(field string) {
		ctx := context.Background()
		prefix := "e2e-service-regex-" + rand.String(8)
		setRegex := func(tnt *capsulev1beta2.Tenant, expression string) {
			if tnt.Spec.ServiceOptions == nil {
				tnt.Spec.ServiceOptions = &api.ServiceOptions{}
			}
			if field == "forbiddenLabels" {
				tnt.Spec.ServiceOptions.ForbiddenLabels.Regex = expression
			} else {
				tnt.Spec.ServiceOptions.ForbiddenAnnotations.Regex = expression
			}
		}
		getRegex := func(tnt *capsulev1beta2.Tenant) string {
			if field == "forbiddenLabels" {
				return tnt.Spec.ServiceOptions.ForbiddenLabels.Regex
			}
			return tnt.Spec.ServiceOptions.ForbiddenAnnotations.Regex
		}
		setMetadata := func(svc *corev1.Service, key string) {
			if field == "forbiddenLabels" {
				svc.Labels = map[string]string{"env": "e2e", key: "value"}
			} else {
				svc.Annotations = map[string]string{key: "value"}
			}
		}
		expectPersistedMetadata := func(svc *corev1.Service, key string) {
			stored := &corev1.Service{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(svc), stored)).To(Succeed())
			if field == "forbiddenLabels" {
				Expect(stored.Labels).To(HaveKeyWithValue(key, "value"))
			} else {
				Expect(stored.Annotations).To(HaveKeyWithValue(key, "value"))
			}
		}
		var tenants []*capsulev1beta2.Tenant
		var owners []client.Client
		for _, suffix := range []string{"a", "b"} {
			name := prefix + "-" + suffix
			tnt := &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"env": "e2e"}}, Spec: capsulev1beta2.TenantSpec{
				Owners: rbac.OwnerListSpec{{Kind: "User", Name: name}},
			}}
			if suffix == "a" {
				setRegex(tnt, "^blocked-")
			}
			Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
			DeferCleanup(EventuallyDeletion, tnt)
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
			tenants = append(tenants, tnt)
			owners = append(owners, impersonationClient(name, withDefaultGroups(nil)))
		}

		By("denying malformed Tenant creation while valid tenants are present")
		invalid := &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: prefix + "-invalid", Labels: map[string]string{"env": "e2e"}}, Spec: *tenants[0].Spec.DeepCopy()}
		setRegex(invalid, "[")
		err := k8sClient.Create(ctx, invalid)
		if err == nil {
			DeferCleanup(EventuallyDeletion, invalid)
			Fail("malformed Service regex was accepted on Tenant creation")
		}
		path := "spec.serviceOptions." + field + ".deniedRegex"
		Expect(err).To(MatchError(And(ContainSubstring("unable to compile regex"), ContainSubstring(path))))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(invalid), invalid))).To(BeTrue())

		By("denying malformed Tenant updates without persisting the pattern")
		Eventually(func(g Gomega) {
			current := &capsulev1beta2.Tenant{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tenants[0]), current)).To(Succeed())
			setRegex(current, "[")
			err := k8sClient.Update(ctx, current)
			if err == nil {
				Fail("malformed Service regex was accepted on Tenant update")
			}
			g.Expect(err).To(MatchError(And(ContainSubstring("unable to compile regex"), ContainSubstring(path))))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		storedTenant := &capsulev1beta2.Tenant{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tenants[0]), storedTenant)).To(Succeed())
		Expect(getRegex(storedTenant)).To(Equal("^blocked-"))

		By("preserving Service create and update enforcement in both namespaces of tenant A")
		var selectedNamespace string
		for i, ownerIndex := range []int{0, 0, 1} {
			tnt := tenants[ownerIndex]
			ns := NewNamespace("", map[string]string{meta.TenantLabel: tnt.Name})
			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())
			if i == 0 {
				selectedNamespace = ns.Name
			}
			svc := NewService(types.NamespacedName{Namespace: ns.Name, Name: "allowed"})
			svc.Labels = map[string]string{"env": "e2e"}
			setMetadata(svc, "safe-key")
			Eventually(func() error { return owners[ownerIndex].Create(ctx, svc) }, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			expectPersistedMetadata(svc, "safe-key")
			blocked := NewService(types.NamespacedName{Namespace: ns.Name, Name: "blocked"})
			blocked.Labels = map[string]string{"env": "e2e"}
			setMetadata(blocked, "blocked-key")
			err := owners[ownerIndex].Create(ctx, blocked)
			if ownerIndex == 1 {
				Expect(err).NotTo(HaveOccurred(), "tenant A's policy must not affect tenant B")
				expectPersistedMetadata(blocked, "blocked-key")
				continue
			}
			Expect(err).To(MatchError(ContainSubstring("blocked-key is forbidden")))
			Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(blocked), blocked))).To(BeTrue())
			Eventually(func(g Gomega) {
				fresh := &corev1.Service{}
				g.Expect(owners[ownerIndex].Get(ctx, client.ObjectKeyFromObject(svc), fresh)).To(Succeed())
				setMetadata(fresh, "blocked-key")
				err := owners[ownerIndex].Update(ctx, fresh)
				if err == nil {
					Fail("forbidden Service metadata update unexpectedly succeeded")
				}
				g.Expect(err).To(MatchError(ContainSubstring("blocked-key is forbidden")))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			stored := &corev1.Service{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(svc), stored)).To(Succeed())
			Expect(stored.Labels).NotTo(HaveKey("blocked-key"))
			Expect(stored.Annotations).NotTo(HaveKey("blocked-key"))
		}

		By("allowing a valid policy change to clear the restriction")
		Eventually(func() error {
			current := &capsulev1beta2.Tenant{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(tenants[0]), current); err != nil {
				return err
			}
			setRegex(current, "")
			return k8sClient.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		relaxed := NewService(types.NamespacedName{Namespace: selectedNamespace, Name: "after-policy-change"})
		relaxed.Labels = map[string]string{"env": "e2e"}
		setMetadata(relaxed, "blocked-key")
		Eventually(func() error { return owners[0].Create(ctx, relaxed) }, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		expectPersistedMetadata(relaxed, "blocked-key")
	}, Entry("forbidden labels", "forbiddenLabels"), Entry("forbidden annotations", "forbiddenAnnotations"))
})
