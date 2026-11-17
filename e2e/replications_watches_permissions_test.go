// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

// Give a test actor explicit access to subscriptions, without access to source
// Secrets. Its execution identity is independently configured on each resource.
func bindWatchSubscriptionAccess(namespace, account string, verbs []string) {
	GinkgoHelper()
	name := "watch-subscriptions-" + account
	role := &rbacv1.Role{Name: name, Namespace: namespace, Rules: []rbacv1.PolicyRule{{APIGroups: []string{capsulev1beta2.GroupVersion.Group}, Resources: []string{"tenantresources"}, Verbs: verbs}}}
	Expect(k8sClient.Create(context.Background(), role)).To(Succeed())
	Expect(k8sClient.Create(context.Background(), &rbacv1.RoleBinding{Name: name, Namespace: namespace, RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: name}, Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Namespace: namespace, Name: account}}})).To(Succeed())
}

var _ = Describe("Replication watch execution permissions", Label("replications", "replication-watches", "watch-permissions"), func() {
	It("does not expose unreadable Secret predicates through privileged or another identity's streams", func() {
		ctx := context.Background()
		prefix := "watch-permissions-" + rand.String(6)
		owners, namespaces := watchNamespaces(prefix)
		namespace, otherNamespace := namespaces[0].Name, namespaces[2].Name
		for _, account := range []string{"restricted", "reader"} {
			ensureServiceAccount(namespace, account)
		}
		ensureServiceAccount(otherNamespace, "reader")
		bindWatchSubscriptionAccess(namespace, "restricted", []string{"create", "get", "list", "watch", "update", "patch", "delete"})
		bindServiceAccountToSecretReader(namespace, "reader", namespace)
		bindServiceAccountToSecretReader(otherNamespace, "reader", otherNamespace)
		actor := impersonationClient(serviceAccountUsername(namespace, "restricted"), serviceAccountGroups(namespace))
		owner := impersonationClient(owners[0].Name, withDefaultGroups(nil))
		secret := &corev1.Secret{Name: prefix, Namespace: namespace, Labels: map[string]string{watchSourceLabel: prefix}, Data: map[string][]byte{"probe": []byte("initial")}}
		Expect(k8sClient.Create(ctx, secret)).To(Succeed())
		Expect(apierrors.IsForbidden(actor.Get(ctx, client.ObjectKeyFromObject(secret), &corev1.Secret{}))).To(BeTrue(), "the observer must not be able to read the fixture Secret")
		common := func() capsulev1beta2.TenantResourceCommonSpec {
			trigger := watchRule(prefix)
			trigger.Resources = []string{"secrets"}
			trigger.Operations = []admissionv1.OperationType{admissionv1.Update}
			trigger.MatchConditions[0].Expression = fmt.Sprintf("object.data.probe == '%s'", base64.StdEncoding.EncodeToString([]byte("observed")))
			return capsulev1beta2.TenantResourceCommonSpec{Triggers: []capsulev1beta2.TenantResourceTrigger{trigger}, TriggerMinInterval: metav1.Duration{Duration: time.Second}, ResyncPeriod: metav1.Duration{Duration: 24 * time.Hour}, Resources: []capsulev1beta2.ResourceSpec{}}
		}
		hidden := &capsulev1beta2.TenantResource{Name: prefix, Namespace: namespace, Spec: capsulev1beta2.TenantResourceSpec{ServiceAccount: &meta.LocalRFC1123ObjectReference{Name: "restricted"}, TenantResourceCommonSpec: common()}}
		Expect(actor.Create(ctx, hidden)).To(Succeed())
		waitWatchReady(hidden)
		reader := &capsulev1beta2.TenantResource{Name: prefix + "-reader", Namespace: namespace, Spec: capsulev1beta2.TenantResourceSpec{ServiceAccount: &meta.LocalRFC1123ObjectReference{Name: "reader"}, TenantResourceCommonSpec: common()}}
		Expect(owner.Create(ctx, reader)).To(Succeed())
		waitWatchReady(reader)
		other := &capsulev1beta2.TenantResource{Name: prefix, Namespace: otherNamespace, Spec: capsulev1beta2.TenantResourceSpec{ServiceAccount: &meta.LocalRFC1123ObjectReference{Name: "reader"}, TenantResourceCommonSpec: common()}}
		Expect(impersonationClient(owners[1].Name, withDefaultGroups(nil)).Create(ctx, other)).To(Succeed())
		waitWatchReady(other)
		global := &capsulev1beta2.GlobalTenantResource{Name: prefix, Spec: capsulev1beta2.GlobalTenantResourceSpec{Scope: "Namespace", TenantSelector: metav1.LabelSelector{MatchLabels: map[string]string{"projectcapsule.dev/watch-tenant": "0" + prefix}}, TenantResourceCommonSpec: common()}}
		global.Spec.Triggers[0].NamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": namespace}}
		Expect(k8sClient.Create(ctx, global)).To(Succeed())
		DeferCleanup(DeleteInBackground, global)
		waitWatchReady(global)
		change := func() error {
			fresh := &corev1.Secret{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(secret), fresh); err != nil {
				return err
			}
			fresh.Data["probe"] = []byte("observed")
			fresh.Data["revision"] = []byte(rand.String(8))
			return k8sClient.Update(ctx, fresh)
		}
		By("keeping an unreadable predicate quiet while permitted observers see real changes")
		Eventually(func() error {
			if watchToken(reader) != "" && watchToken(global) != "" {
				return nil
			}
			if err := change(); err != nil {
				return err
			}
			return fmt.Errorf("waiting for permitted observers")
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Consistently(func() []string { return []string{watchToken(hidden), watchToken(other)} }, 3*time.Second, 200*time.Millisecond).Should(Equal([]string{"", ""}))
		fresh := &capsulev1beta2.TenantResource{}
		Expect(actor.Get(ctx, client.ObjectKeyFromObject(hidden), fresh)).To(Succeed())
		Expect(fresh.Annotations[meta.ReplicationTriggerAnnotation]).To(BeEmpty())
		By("retiring a permitted stream when its subscription changes execution identity")
		Eventually(func() error {
			fresh := &capsulev1beta2.TenantResource{}
			if err := owner.Get(ctx, client.ObjectKeyFromObject(reader), fresh); err != nil {
				return err
			}
			fresh.Spec.ServiceAccount.Name = "restricted"
			return owner.Update(ctx, fresh)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		waitWatchReady(reader)
		before := watchToken(reader)
		globalBefore := watchToken(global)
		Eventually(func() error {
			if watchToken(global) != globalBefore {
				return nil
			}
			if err := change(); err != nil {
				return err
			}
			return fmt.Errorf("waiting for privileged observer")
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Consistently(func() []string { return []string{watchToken(hidden), watchToken(reader), watchToken(other)} }, 3*time.Second, 200*time.Millisecond).Should(Equal([]string{"", before, ""}))
		By("activating the restricted watch only after source permissions are granted")
		bindServiceAccountToSecretReader(namespace, "restricted", namespace)
		Eventually(func() error {
			if watchToken(hidden) != "" && watchToken(reader) != before {
				return nil
			}
			if err := change(); err != nil {
				return err
			}
			return fmt.Errorf("waiting for newly authorized watch")
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Consistently(func() string { return watchToken(other) }, 2*time.Second, 200*time.Millisecond).Should(BeEmpty())
	})
})
