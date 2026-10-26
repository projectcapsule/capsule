// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"encoding/json"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionv1 "k8s.io/api/admission/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

var _ = Describe("Tenant lifecycle admission", Label("tenant", "tenant-finalization", "lifecycle-admission"), func() {
	It("rejects an unprotected legacy deletion payload and permits deletion after repair", func() {
		ctx := context.Background()
		name := "e2e-legacy-delete-" + rand.String(6)
		tnt := &capsulev1beta2.Tenant{Name: name, Labels: map[string]string{"env": "e2e"}, Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{{Name: name, Kind: rbac.UserOwner}},
		}}
		other := &capsulev1beta2.Tenant{Name: name + "-other", Labels: map[string]string{"env": "e2e"}, Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{{Name: name + "-other", Kind: rbac.UserOwner}},
		}}
		for _, tenant := range []*capsulev1beta2.Tenant{tnt, other} {
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())
			DeferCleanup(func() { EventuallyDeletion(tenant) })
			TenantReady(tenant, metav1.ConditionTrue, defaultTimeoutInterval)
		}
		By("submitting the pre-upgrade DELETE snapshot to the deployed admission chain")
		// Current CREATE/UPDATE mutation repairs finalizers immediately. Submit a
		// legacy OldObject to the actual webhook without disabling shared webhooks
		// or introducing a race with the controller's startup repair.
		configurations := &admissionregistrationv1.ValidatingWebhookConfigurationList{}
		Expect(k8sClient.List(ctx, configurations)).To(Succeed())
		var service *admissionregistrationv1.ServiceReference
		for _, configuration := range configurations.Items {
			for _, hook := range configuration.Webhooks {
				if hook.ClientConfig.Service != nil && hook.ClientConfig.Service.Path != nil && *hook.ClientConfig.Service.Path == "/tenants/validating" {
					service = hook.ClientConfig.Service
				}
			}
		}
		Expect(service).NotTo(BeNil())
		Expect(service.Port).NotTo(BeNil())
		admin := clusterAdminClient()
		check := func(protected bool) {
			current := &capsulev1beta2.Tenant{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tnt), current)).To(Succeed())
			if !protected {
				current.Finalizers = nil
			}
			raw, err := json.Marshal(current)
			Expect(err).NotTo(HaveOccurred())
			review := admissionv1.AdmissionReview{APIVersion: "admission.k8s.io/v1", Kind: "AdmissionReview", Request: &admissionv1.AdmissionRequest{
				UID: types.UID(rand.String(12)), Name: current.Name, Operation: admissionv1.Delete, DryRun: new(true),
				Kind:      metav1.GroupVersionKind{Group: capsulev1beta2.GroupVersion.Group, Version: capsulev1beta2.GroupVersion.Version, Kind: "Tenant"},
				Resource:  metav1.GroupVersionResource{Group: capsulev1beta2.GroupVersion.Group, Version: capsulev1beta2.GroupVersion.Version, Resource: "tenants"},
				OldObject: runtime.RawExtension{Raw: raw}, UserInfo: authenticationv1.UserInfo{Username: name},
			}}
			body, err := json.Marshal(review)
			Expect(err).NotTo(HaveOccurred())
			body, err = admin.CoreV1().RESTClient().Post().Namespace(service.Namespace).Resource("services").
				Name(fmt.Sprintf("https:%s:%d", service.Name, *service.Port)).SubResource("proxy").Suffix(*service.Path).
				SetHeader("Content-Type", "application/json").Body(body).Do(ctx).Raw()
			Expect(err).NotTo(HaveOccurred())
			response := admissionv1.AdmissionReview{}
			Expect(json.Unmarshal(body, &response)).To(Succeed())
			Expect(response.Response).NotTo(BeNil())
			Expect(response.Response.UID).To(Equal(review.Request.UID))
			Expect(response.Response.Allowed).To(Equal(protected))
			if !protected {
				Expect(response.Response.Result.Reason).To(Equal(metav1.StatusReasonForbidden))
				Expect(response.Response.Result.Message).To(ContainSubstring("tenant lifecycle protection is not ready"))
			}
		}
		check(false)
		By("preserving persisted protection and accepting the repaired snapshot")
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tnt), tnt)).To(Succeed())
		Expect(tnt.Finalizers).To(ContainElement(meta.ControllerFinalizer))
		Expect(tnt.DeletionTimestamp).To(BeNil())
		check(true)
		Expect(k8sClient.Delete(ctx, tnt)).To(Succeed())
		Eventually(func() bool {
			return apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(tnt), &capsulev1beta2.Tenant{}))
		}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(other), other)).To(Succeed())
		Expect(other.DeletionTimestamp).To(BeNil())
	})
})
