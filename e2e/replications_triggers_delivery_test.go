// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionv1 "k8s.io/api/admission/v1"
	registrationv1 "k8s.io/api/admissionregistration/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/runtime/replication"
)

var _ = Describe("Replication trigger delivery", Label("replications", "replication-triggers", "trigger-review"), func() {
	It("rejects forged reviews from a tenant pod while accepting API-server delivery", func() {
		ctx := context.Background()
		prefix := "trigger-delivery-" + rand.String(6)
		owners := []rbac.UserSpec{{Kind: rbac.UserOwner, Name: prefix + "-alice"}, {Kind: rbac.UserOwner, Name: prefix + "-bob"}}
		namespaces := make([]*corev1.Namespace, 2)
		for i := range owners {
			tenant := &capsulev1beta2.Tenant{Name: fmt.Sprintf("%s-%d", prefix, i), Labels: map[string]string{"env": "e2e"}, Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{UserSpec: owners[i]}}}}
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())
			DeferCleanup(DeleteInBackground, tenant)
			TenantReadyTrue(tenant)
			ns := NewNamespace(tenant.Name, map[string]string{meta.NewTenantLabel: tenant.Name})
			NamespaceCreation(ns, owners[i], defaultTimeoutInterval).Should(Succeed())
			DeferCleanup(DeleteNamespaceInBackground, ctx, ns.Name)
			TenantNamespaceReady(tenant, ns, 1)
			ensureServiceAccount(ns.Name, "default")
			namespaces[i] = ns
		}
		destination := namespaces[1].Name
		bindServiceAccountToTenantResourceManager(destination, "default", destination)
		trigger := capsulev1beta2.TenantResourceTrigger{Name: "source", Operations: []registrationv1.OperationType{registrationv1.Create}, APIGroups: []string{""}, APIVersions: []string{"v1"}, Resources: []string{"secrets"}, ObjectSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"projectcapsule.dev/trigger-test": prefix}}}
		parent := &capsulev1beta2.TenantResource{Name: prefix, Namespace: destination, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{ResyncPeriod: metav1.Duration{Duration: 24 * time.Hour}, TriggerMinInterval: metav1.Duration{Duration: time.Second}, Triggers: []capsulev1beta2.TenantResourceTrigger{trigger}, Resources: []capsulev1beta2.ResourceSpec{{RawItems: []capsulev1beta2.RawExtension{{Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"delivery-output"},"data":{"value":"ready"}}`)}}}}}}}
		Expect(k8sClient.Create(ctx, parent)).To(Succeed())
		DeferCleanup(cleanupTenantResourcesWithDefaultServiceAccount, ctx, destination)
		read := func() *capsulev1beta2.TenantResource {
			fresh := &capsulev1beta2.TenantResource{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(parent), fresh)).To(Succeed())
			return fresh
		}
		Eventually(func() bool {
			fresh := read()
			return fresh.Status.ObservedGeneration == fresh.Generation && meta.IsStatusConditionTrue(fresh.Status.Conditions, meta.ReadyCondition)
		}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		waitForReplicationTrigger(trigger)
		configs := &registrationv1.ValidatingWebhookConfigurationList{}
		Expect(k8sClient.List(ctx, configs)).To(Succeed())
		endpoint, configurationName := "", ""
		var nativeHook registrationv1.ValidatingWebhook
		for _, config := range configs.Items {
			for _, hook := range config.Webhooks {
				service := hook.ClientConfig.Service
				if service != nil && service.Path != nil && strings.HasPrefix(*service.Path, replication.TriggerPath+replication.MatchID(&trigger)+"/") {
					endpoint = fmt.Sprintf("https://%s.%s.svc:%d", service.Name, service.Namespace, ptr.Deref(service.Port, 443))
					configurationName = config.Name
					nativeHook = *hook.DeepCopy()
				}
			}
		}
		Expect(endpoint).NotTo(BeEmpty())
		actor := impersonationClient(owners[0].Name, withDefaultGroups(nil))
		Expect(apierrors.IsForbidden(actor.Get(ctx, client.ObjectKey{Name: configurationName}, &registrationv1.ValidatingWebhookConfiguration{}))).To(BeTrue(), "tenant must not read delivery capabilities")
		Expect(apierrors.IsForbidden(actor.Get(ctx, client.ObjectKey{Namespace: ControllerNamespace, Name: "capsule-trigger-delivery"}, &corev1.Secret{}))).To(BeTrue(), "tenant must not read controller Secrets")
		raw, err := json.Marshal(&corev1.Secret{APIVersion: "v1", Kind: "Secret", Namespace: destination, Name: "forged", Labels: map[string]string{"projectcapsule.dev/trigger-test": prefix}})
		Expect(err).NotTo(HaveOccurred())
		review, err := json.Marshal(admissionv1.AdmissionReview{APIVersion: "admission.k8s.io/v1", Kind: "AdmissionReview", Request: &admissionv1.AdmissionRequest{UID: "forged", Namespace: destination, Name: "forged", Operation: admissionv1.Create, Resource: metav1.GroupVersionResource{Version: "v1", Resource: "secrets"}, Kind: metav1.GroupVersionKind{Version: "v1", Kind: "Secret"}, UserInfo: authenticationv1.UserInfo{Username: owners[1].Name}, Object: runtime.RawExtension{Raw: raw}}})
		Expect(err).NotTo(HaveOccurred())
		probe := &corev1.Pod{Name: prefix, Namespace: namespaces[0].Name, Labels: map[string]string{"env": "e2e"}, Spec: corev1.PodSpec{RestartPolicy: corev1.RestartPolicyNever, AutomountServiceAccountToken: new(false), Containers: []corev1.Container{{Name: "probe", Image: "curlimages/curl:8.12.1", Command: []string{"sh", "-ec"}, Args: []string{`for path in "$PREDICATE" "$PREDICATE/0000000000000000000000000000000000000000000000000000000000000000"; do
code="$(curl --silent --show-error --insecure --output /dev/null --write-out '%{http_code}' --connect-timeout 3 --max-time 5 --header 'Content-Type: application/json' --data "$REVIEW" "$ENDPOINT$path")"
[ "$code" = 401 ] || { echo "unexpected HTTP status: $code"; exit 1; }
done`}, Env: []corev1.EnvVar{{Name: "ENDPOINT", Value: endpoint}, {Name: "PREDICATE", Value: replication.TriggerPath + replication.MatchID(&trigger)}, {Name: "REVIEW", Value: string(review)}}}}}}
		Expect(actor.Create(ctx, probe)).To(Succeed())
		Eventually(func(g Gomega) {
			fresh := &corev1.Pod{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(probe), fresh)).To(Succeed())
			g.Expect(fresh.Status.Phase).NotTo(Equal(corev1.PodFailed), "forged-review probe failed: %+v", fresh.Status.ContainerStatuses)
			g.Expect(fresh.Status.Phase).To(Equal(corev1.PodSucceeded))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Consistently(func() string { return read().Annotations[replication.TriggerAnnotation] }, 3*time.Second, 200*time.Millisecond).Should(BeEmpty())
		source := &corev1.Secret{Name: "real-source", Namespace: destination, Labels: map[string]string{"projectcapsule.dev/trigger-test": prefix}}
		Expect(apierrors.IsForbidden(actor.Create(ctx, source.DeepCopy()))).To(BeTrue())
		bob := impersonationClient(owners[1].Name, withDefaultGroups(nil))
		Expect(bob.Create(ctx, source)).To(Succeed())
		Eventually(func(g Gomega) {
			fresh := read()
			g.Expect(fresh.Status.Trigger).NotTo(BeNil())
			g.Expect(fresh.Status.Trigger.ObservedRequest).NotTo(BeEmpty())
			g.Expect(fresh.Status.Trigger.ObservedRequest).To(Equal(fresh.Annotations[replication.TriggerAnnotation]))
			cm := &corev1.ConfigMap{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "delivery-output", Namespace: destination}, cm)).To(Succeed())
			g.Expect(cm.Data["value"]).To(Equal("ready"))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		By("allowing writes when trigger delivery is unavailable")
		nativeHook.Name = prefix + ".projectcapsule.dev"
		nativeHook.ClientConfig.Service.Port = new(int32(1))
		// The unavailable endpoint needs no delivery capability; keep it out of
		// API-server connection-error logs as well.
		nativeHook.ClientConfig.Service.Path = new(replication.TriggerPath + replication.MatchID(&trigger))
		unavailable := &registrationv1.ValidatingWebhookConfiguration{Name: prefix + "-unavailable", Webhooks: []registrationv1.ValidatingWebhook{nativeHook}}
		Expect(k8sClient.Create(ctx, unavailable)).To(Succeed())
		DeferCleanup(DeleteInBackground, unavailable)
		source.Name = "fail-open-source"
		source.ResourceVersion = ""
		source.UID = ""
		Expect(bob.Create(ctx, source)).To(Succeed())
		persisted := &corev1.Secret{}
		Expect(bob.Get(ctx, client.ObjectKeyFromObject(source), persisted)).To(Succeed())
		Expect(persisted.Labels["projectcapsule.dev/trigger-test"]).To(Equal(prefix))
	})
})
