// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	cryptorand "crypto/rand"
	"encoding/pem"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/util/cert"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

var _ = Describe("Replication watch CA rotation", Label("config", "replications", "replication-watches", "watch-ca"), func() {
	DescribeTable("recovers after an in-place CA update without leaking across execution identities or tenants", func(emptyCA bool) {
		ctx := context.Background()
		prefix := "watch-ca-" + rand.String(6)
		owners, namespaces := watchNamespaces(prefix)
		namespace, otherNamespace := namespaces[0].Name, namespaces[2].Name
		actor := impersonationClient(owners[0].Name, withDefaultGroups(nil))
		original := &capsulev1beta2.CapsuleConfiguration{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: defaultConfigurationName}, original)).To(Succeed())
		key, err := ecdsa.GenerateKey(elliptic.P256(), cryptorand.Reader)
		Expect(err).NotTo(HaveOccurred())
		untrusted, err := cert.NewSelfSignedCACert(cert.Config{CommonName: prefix}, key)
		Expect(err).NotTo(HaveOccurred())
		secret := &corev1.Secret{Name: prefix, Namespace: ControllerNamespace, Data: map[string][]byte{"ca.crt": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: untrusted.Raw})}}
		if emptyCA {
			secret.Data["ca.crt"] = nil
		}
		Expect(k8sClient.Create(ctx, secret)).To(Succeed())
		DeferCleanup(DeleteInBackground, secret)
		DeferCleanup(func() {
			ModifyCapsuleConfigurationOpts(func(object *capsulev1beta2.CapsuleConfiguration) {
				object.Spec.Impersonation = original.Spec.Impersonation
				object.Spec.CacheInvalidation = original.Spec.CacheInvalidation
			})
		})
		props := original.Spec.Impersonation
		props.Endpoint, props.SkipTLSVerify = "", false
		props.GlobalDefaultServiceAccount, props.GlobalDefaultServiceAccountNamespace = "", ""
		props.CASecretName, props.CASecretNamespace, props.CASecretKey = meta.RFC1123Name(secret.Name), meta.RFC1123SubdomainName(secret.Namespace), "ca.crt"
		ModifyCapsuleConfigurationOpts(func(object *capsulev1beta2.CapsuleConfiguration) {
			object.Spec.Impersonation = props
			object.Spec.CacheInvalidation.Duration = 24 * time.Hour
		})
		common := func() capsulev1beta2.TenantResourceCommonSpec {
			trigger := watchRule(prefix)
			trigger.Operations = []admissionv1.OperationType{admissionv1.Update}
			return capsulev1beta2.TenantResourceCommonSpec{Triggers: []capsulev1beta2.TenantResourceTrigger{trigger}, TriggerMinInterval: metav1.Duration{Duration: time.Second}, ResyncPeriod: metav1.Duration{Duration: 24 * time.Hour}, Resources: []capsulev1beta2.ResourceSpec{}}
		}
		parent := &capsulev1beta2.TenantResource{Name: prefix, Namespace: namespace, Spec: capsulev1beta2.TenantResourceSpec{ServiceAccount: &meta.LocalRFC1123ObjectReference{Name: "default"}, TenantResourceCommonSpec: common()}}
		Expect(actor.Create(ctx, parent)).To(Succeed())
		waitWatchReady(parent)
		other := &capsulev1beta2.TenantResource{Name: prefix, Namespace: otherNamespace, Spec: capsulev1beta2.TenantResourceSpec{ServiceAccount: &meta.LocalRFC1123ObjectReference{Name: "default"}, TenantResourceCommonSpec: common()}}
		Expect(impersonationClient(owners[1].Name, withDefaultGroups(nil)).Create(ctx, other)).To(Succeed())
		waitWatchReady(other)
		global := &capsulev1beta2.GlobalTenantResource{Name: prefix, Spec: capsulev1beta2.GlobalTenantResourceSpec{Scope: "Namespace", TenantSelector: metav1.LabelSelector{MatchLabels: map[string]string{"projectcapsule.dev/watch-tenant": "0" + prefix}}, TenantResourceCommonSpec: common()}}
		global.Spec.Triggers[0].NamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": namespace}}
		Expect(k8sClient.Create(ctx, global)).To(Succeed())
		DeferCleanup(DeleteInBackground, global)
		waitWatchReady(global)
		source := &corev1.ConfigMap{Name: prefix, Namespace: namespace, Labels: map[string]string{watchSourceLabel: prefix, watchInputLabel: prefix}, Data: map[string]string{"value": "initial"}}
		Expect(actor.Create(ctx, source)).To(Succeed())
		By("requiring real reads and applies through the cached impersonated client")
		Eventually(func() error {
			fresh := &capsulev1beta2.TenantResource{}
			if err := actor.Get(ctx, client.ObjectKeyFromObject(parent), fresh); err != nil {
				return err
			}
			fresh.Spec.Resources = watchBody(prefix, namespace, prefix+"-output").Resources
			fresh.Spec.Resources[0].NamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": namespace}}
			return actor.Update(ctx, fresh)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func(g Gomega) {
			fresh := &capsulev1beta2.TenantResource{}
			g.Expect(actor.Get(ctx, client.ObjectKeyFromObject(parent), fresh)).To(Succeed())
			g.Expect(fresh.Status.ObservedGeneration).To(Equal(fresh.Generation))
			condition := fresh.Status.Conditions.GetConditionByType(meta.ReadyCondition)
			g.Expect(condition).NotTo(BeNil())
			g.Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			g.Expect(condition.Message).To(ContainSubstring("x509:"))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: prefix + "-output"}, &corev1.ConfigMap{})).To(Satisfy(apierrors.IsNotFound))
		change := func() error {
			fresh := &corev1.ConfigMap{}
			if err := actor.Get(ctx, client.ObjectKeyFromObject(source), fresh); err != nil {
				return err
			}
			fresh.Data["value"] = rand.String(8)
			return actor.Update(ctx, fresh)
		}
		By("observing persisted writes through the controller stream while invalid trust blocks the impersonated stream")
		Eventually(func() error {
			if watchToken(global) != "" {
				return nil
			}
			if err := change(); err != nil {
				return err
			}
			return fmt.Errorf("waiting for controller observer")
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Consistently(func() []string { return []string{watchToken(parent), watchToken(other)} }, 3*time.Second, 200*time.Millisecond).Should(Equal([]string{"", ""}))
		root := &corev1.ConfigMap{}
		Eventually(func() error {
			if err := k8sClient.Get(ctx, client.ObjectKey{Name: "kube-root-ca.crt", Namespace: namespace}, root); err != nil {
				return err
			}
			if root.Data["ca.crt"] == "" {
				return fmt.Errorf("waiting for cluster CA")
			}
			return nil
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		By("changing only the referenced Secret's CA data and recovering delivery")
		Eventually(func() error {
			fresh := &corev1.Secret{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(secret), fresh); err != nil {
				return err
			}
			fresh.Data["ca.crt"] = []byte(root.Data["ca.crt"])
			return k8sClient.Update(ctx, fresh)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		rotatedAt := time.Now()
		Eventually(func() error {
			if watchToken(parent) != "" {
				return nil
			}
			if err := change(); err != nil {
				return err
			}
			return fmt.Errorf("waiting for CA-rotated source watch")
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		AddReportEntry("ca-rotation-recovery", time.Since(rotatedAt).String())
		waitWatchReady(parent)
		By("updating the applied resource from fresh input after trust recovers")
		changeWatchInput(actor, source, "after-ca-rotation")
		expectConfigMapData(namespace, prefix+"-output", map[string]string{"value": "after-ca-rotation"})
		Consistently(func() string { return watchToken(other) }, 3*time.Second, 200*time.Millisecond).Should(BeEmpty())
		for _, excluded := range namespaces[1:] {
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: excluded.Name, Name: prefix + "-output"}, &corev1.ConfigMap{})).To(Satisfy(apierrors.IsNotFound))
		}
		current := &capsulev1beta2.CapsuleConfiguration{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: defaultConfigurationName}, current)).To(Succeed())
		Expect(current.Spec.Impersonation).To(Equal(props), "recovery must not require editing CapsuleConfiguration")
	}, Entry("untrusted bundle", false), Entry("empty bundle", true))
})
