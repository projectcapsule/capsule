// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"time"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/ssa"
)

func exerciseTenantResourceConditions(tenantName, baseNamespace, targetNamespace, excludedNamespace string) {
	ctx := context.Background()
	parent := &capsulev1beta2.TenantResource{Name: "e2e-tr-condition", Namespace: baseNamespace, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{
		ResyncPeriod: metav1.Duration{Duration: 2 * time.Second},
		Resources: []capsulev1beta2.ResourceSpec{{
			NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": targetNamespace}},
			Policy:            &apiruntime.ResourceReplicationPolicy{Condition: "false"},
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
