// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"os"
	"strings"
	"time"

	"filippo.io/age"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/ssa"
)

func exerciseGlobalAgeRotation(tenantA, tenantB, selectedNamespace, otherNamespace, excludedNamespace string) {
	ctx := context.Background()
	data, err := os.ReadFile("../playground/platform/globaltenantresources/gtr-age-keys.yaml")
	Expect(err).NotTo(HaveOccurred())
	parent := &capsulev1beta2.GlobalTenantResource{}
	Expect(yaml.UnmarshalStrict(data, parent)).To(Succeed())
	// Exercise the playground's retention template on a bounded test interval.
	parent.Spec.ResyncPeriod = metav1.Duration{Duration: 10 * time.Second}
	parent.Spec.Resources[0].Policy.Condition = strings.ReplaceAll(parent.Spec.Resources[0].Policy.Condition, "duration('720h')", "duration('5m')")
	parent.Spec.Resources[0].Policy.Deletion = apiruntime.ResourceDeletionPolicyRemove
	parent.Name = "e2e-gtr-age-rotation"
	parent.Spec.TenantSelector = metav1.LabelSelector{MatchLabels: map[string]string{"energy": "solar"}}
	parent.Spec.ServiceAccount.Name = "gtr-age-rotation"
	ensureServiceAccount("capsule-system", "gtr-age-rotation")
	bindServiceAccountToNamespacedResource("capsule-system", "gtr-age-rotation", selectedNamespace, []string{"secrets"}, []string{"get", "list", "watch", "create", "update", "patch", "delete"})
	DeferCleanup(func() {
		ignoreNotFound(k8sClient.Delete(ctx, &corev1.ServiceAccount{Name: "gtr-age-rotation", Namespace: "capsule-system"}))
	})
	for _, ns := range []string{selectedNamespace, excludedNamespace} {
		Expect(k8sClient.Patch(ctx, &corev1.Namespace{Name: ns}, client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"labels":{"projectcapsule.dev/age-keys":"enabled"}}}`)))).To(Succeed())
	}
	secretKey := client.ObjectKey{Name: tenantA + "-age-keys", Namespace: selectedNamespace}
	By("rejecting invalid conditions on both create and update")
	invalid := parent.DeepCopy()
	invalid.Name += "-invalid"
	invalid.Spec.Resources[0].Policy.Condition = "42"
	Expect(k8sClient.Create(ctx, invalid, client.DryRunAll)).To(MatchError(ContainSubstring("spec.resources[0].policy.condition")))
	Expect(k8sClient.Create(ctx, parent)).To(Succeed())
	DeferCleanup(func() {
		ignoreNotFound(k8sClient.Delete(ctx, parent))
		Eventually(func() bool {
			return apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(parent), &capsulev1beta2.GlobalTenantResource{}))
		}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
	})
	invalid = parent.DeepCopy()
	invalid.Spec.Resources[0].Policy.Condition = "object..invalid"
	Expect(k8sClient.Update(ctx, invalid, client.DryRunAll)).To(MatchError(ContainSubstring("spec.resources[0].policy.condition")))

	By("creating the first key from an optional, fast-templated context reference")
	secret := &corev1.Secret{}
	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, secretKey, secret)).To(Succeed())
		g.Expect(ageIdentityCount(secret)).To(Equal(1))
		g.Expect(secret.Labels).To(HaveKeyWithValue(meta.ProtectedByCapsuleLabel, meta.ValueControllerReplications))
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	first := secret.DeepCopy().Data
	rotated, err := time.Parse(time.RFC3339, secret.Annotations["keys.example.org/rotated-at"])
	Expect(err).NotTo(HaveOccurred())

	By("leaving another parent's existing target untouched when a condition is false")
	skipped := &capsulev1beta2.GlobalTenantResource{Name: parent.Name + "-skipped", Spec: *parent.Spec.DeepCopy()}
	skipped.Spec.Resources[0].Policy.Condition = "false"
	Expect(k8sClient.Create(ctx, skipped)).To(Succeed())
	DeferCleanup(func() { ignoreNotFound(k8sClient.Delete(ctx, skipped)) })
	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(skipped), skipped)).To(Succeed())
		g.Expect(skipped.Status.ProcessedItems).To(HaveLen(1))
		g.Expect(skipped.Status.ProcessedItems[0].Message).To(Equal(ssa.ConditionNotMet))
		g.Expect(skipped.Status.ProcessedItems[0].Created).To(BeFalse())
		g.Expect(skipped.Status.ProcessedItems[0].LastApply.IsZero()).To(BeTrue())
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	Expect(k8sClient.Delete(ctx, skipped)).To(Succeed())
	Eventually(func() bool {
		return apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(skipped), &capsulev1beta2.GlobalTenantResource{}))
	}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
	Expect(k8sClient.Get(ctx, secretKey, secret)).To(Succeed())
	Expect(maps.EqualFunc(secret.Data, first, bytes.Equal)).To(BeTrue())
	Expect(secret.Labels).To(HaveKeyWithValue(meta.ProtectedByCapsuleLabel, meta.ValueControllerReplications))

	By("skipping repeated reconciliations until the actual five-minute interval elapses")
	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(parent), parent)).To(Succeed())
		g.Expect(parent.Status.ProcessedItems).To(HaveLen(1))
		g.Expect(parent.Status.ProcessedItems[0].Message).To(Equal(ssa.ConditionNotMet))
		g.Expect(parent.Status.ProcessedItems[0].LastApply.IsZero()).To(BeFalse())
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	before := parent.Status.ProcessedItems[0].LastApply
	// CEL and the rotation annotation use wall time. Observe the timestamp of
	// each change instead of assuming a monotonic test timer stays aligned with
	// the cluster clock across host sleep or clock adjustments.
	wait := max(time.Until(rotated.Add(5*time.Minute)), 0) + 45*time.Second
	Eventually(func(g Gomega) bool {
		g.Expect(k8sClient.Get(ctx, secretKey, secret)).To(Succeed())
		g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(parent), parent)).To(Succeed())
		for _, key := range []client.ObjectKey{{Namespace: otherNamespace, Name: tenantA + "-age-keys"}, {Namespace: excludedNamespace, Name: tenantB + "-age-keys"}, {Namespace: selectedNamespace, Name: tenantB + "-age-keys"}} {
			Expect(apierrors.IsNotFound(k8sClient.Get(ctx, key, &corev1.Secret{}))).To(BeTrue())
		}
		if maps.EqualFunc(secret.Data, first, bytes.Equal) {
			Expect(secret.Annotations["keys.example.org/rotated-at"]).To(Equal(rotated.Format(time.RFC3339)))
			if parent.Status.ProcessedItems[0].LastApply.Equal(&before) {
				return false
			}
			// The target may rotate between the Secret and status reads. A new
			// apply timestamp must correspond to an actual content rotation.
			g.Expect(k8sClient.Get(ctx, secretKey, secret)).To(Succeed())
			Expect(maps.EqualFunc(secret.Data, first, bytes.Equal)).To(BeFalse(), "apply timestamp changed without rotating keys")
		}
		rotatedAgain, err := time.Parse(time.RFC3339, secret.Annotations["keys.example.org/rotated-at"])
		Expect(err).NotTo(HaveOccurred())
		// Rendering precedes condition evaluation and formats whole seconds.
		Expect(rotatedAgain.Before(rotated.Add(5*time.Minute-time.Second))).To(BeFalse(), "key entries changed before rotation was due")
		Expect(ageIdentityCount(secret)).To(Equal(2))
		Expect(ageEntriesPreserved(secret, first)).To(BeTrue(), "first key entry was removed, renamed, or changed")
		return true
	}, wait, defaultPollInterval).Should(BeTrue())
	second := secret.DeepCopy().Data

	By("retaining every old key on a third rotation")
	writer := impersonationClient("system:serviceaccount:capsule-system:gtr-age-rotation", nil)
	setMarker := func(value string) {
		patch, err := json.Marshal(map[string]any{"metadata": map[string]any{"annotations": map[string]string{"keys.example.org/rotated-at": value}}})
		Expect(err).NotTo(HaveOccurred())
		Expect(writer.Patch(ctx, &corev1.Secret{Name: secretKey.Name, Namespace: secretKey.Namespace}, client.RawPatch(types.MergePatchType, patch))).To(Succeed())
	}
	setMarker(time.Now().UTC().Add(-6 * time.Minute).Format(time.RFC3339))
	By("respecting SSA conflicts after an external writer changes the rotation marker")
	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(parent), parent)).To(Succeed())
		g.Expect(parent.Status.ProcessedItems).To(HaveLen(1))
		g.Expect(parent.Status.ProcessedItems[0].Message).To(ContainSubstring("conflict"))
		g.Expect(k8sClient.Get(ctx, secretKey, secret)).To(Succeed())
		g.Expect(maps.EqualFunc(secret.Data, second, bytes.Equal)).To(BeTrue())
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	By("explicitly reclaiming the changed field before forcing the third rotation")
	Eventually(func() error {
		if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(parent), parent); err != nil {
			return err
		}
		parent.Spec.Resources[0].Policy.Force = true
		return k8sClient.Update(ctx, parent)
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, secretKey, secret)).To(Succeed())
		g.Expect(ageIdentityCount(secret)).To(Equal(3))
		g.Expect(ageEntriesPreserved(secret, second)).To(BeTrue(), "an old key entry was removed, renamed, or changed")
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	third := secret.DeepCopy().Data
	currentEntry := string(secret.Data["recipient"]) + ".agekey"
	_, wasPrevious := second[currentEntry]
	Expect(wasPrevious).To(BeFalse(), "current recipient must point to the newest key entry")
	identity, err := age.ParseX25519Identity(string(secret.Data[currentEntry]))
	Expect(err).NotTo(HaveOccurred())
	Expect(string(secret.Data["recipient"])).To(Equal(identity.Recipient().String()))

	By("blocking writes on evaluation errors without losing history or lifecycle tracking")
	setMarker("invalid-timestamp")
	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(parent), parent)).To(Succeed())
		g.Expect(parent.Status.ProcessedItems).To(HaveLen(1))
		g.Expect(parent.Status.ProcessedItems[0].Message).To(ContainSubstring("ConditionEvaluationFailed"))
		g.Expect(parent.Status.ProcessedItems[0].Created).To(BeTrue())
		g.Expect(k8sClient.Get(ctx, secretKey, secret)).To(Succeed())
		g.Expect(maps.EqualFunc(secret.Data, third, bytes.Equal)).To(BeTrue())
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	By("pruning on parent deletion independently of a failing apply condition")
	Expect(k8sClient.Delete(ctx, parent)).To(Succeed())
	Eventually(func() bool { return apierrors.IsNotFound(k8sClient.Get(ctx, secretKey, &corev1.Secret{})) }, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
}

func ageIdentityCount(secret *corev1.Secret) int {
	count := 0
	for field, value := range secret.Data {
		if !strings.HasSuffix(field, ".agekey") {
			continue
		}
		identity, err := age.ParseX25519Identity(string(value))
		if err != nil || field != identity.Recipient().String()+".agekey" {
			return -1
		}
		count++
	}
	return count
}

func ageEntriesPreserved(secret *corev1.Secret, previous map[string][]byte) bool {
	for field, value := range previous {
		if strings.HasSuffix(field, ".agekey") && !bytes.Equal(secret.Data[field], value) {
			return false
		}
	}
	return true
}

func exerciseTenantResourceConditions(tenantName, baseNamespace, targetNamespace, excludedNamespace string) {
	ctx := context.Background()
	parent := &capsulev1beta2.TenantResource{Name: "e2e-tr-condition", Namespace: baseNamespace, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{
		ResyncPeriod: metav1.Duration{Duration: 2 * time.Second},
		Resources: []capsulev1beta2.ResourceSpec{{
			NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": targetNamespace}},
			Policy:            &apiruntime.ResourceTemplatePolicy{Condition: "false"},
			RawItems:          []capsulev1beta2.RawExtension{{Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"conditional"},"data":{"value":"first"}}`)}},
		}},
	}}}
	Expect(k8sClient.Create(ctx, parent)).To(Succeed())
	key := client.ObjectKeyFromObject(parent)
	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, key, parent)).To(Succeed())
		g.Expect(parent.Status.ProcessedItems).To(HaveLen(1))
		g.Expect(parent.Status.ProcessedItems[0].Message).To(Equal(ssa.ConditionNotMet))
		g.Expect(parent.Status.ProcessedItems[0].Tenant).To(Equal(tenantName))
		g.Expect(parent.Status.ProcessedItems[0].LastApply.IsZero()).To(BeTrue())
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	expectConfigMapAbsent(targetNamespace, "conditional")
	Eventually(func() error {
		if err := k8sClient.Get(ctx, key, parent); err != nil {
			return err
		}
		parent.Spec.Resources[0].Policy.Condition = "object == null"
		return k8sClient.Update(ctx, parent)
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	expectConfigMapData(targetNamespace, "conditional", map[string]string{"value": "first"})
	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, key, parent)).To(Succeed())
		g.Expect(parent.Status.ProcessedItems[0].Message).To(Equal(ssa.ConditionNotMet))
		g.Expect(parent.Status.ProcessedItems[0].LastApply.IsZero()).To(BeFalse())
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	expectConfigMapAbsent(excludedNamespace, "conditional")
	Expect(k8sClient.Delete(ctx, parent)).To(Succeed())
	Eventually(func() bool {
		return apierrors.IsNotFound(k8sClient.Get(ctx, key, &capsulev1beta2.TenantResource{}))
	}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
	Eventually(func() bool {
		return apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: targetNamespace, Name: "conditional"}, &corev1.ConfigMap{}))
	}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
	expectConfigMapAbsent(targetNamespace, "conditional")
}
