// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	resources "k8s.io/api/resource/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

var _ = Describe("DeviceClass request authorization", Label("tenant", "classes", "deviceclass", "dra-requests"), func() {
	DescribeTable("validates every request and fallback within its tenant", func(kind, policyType string) {
		ctx := context.Background()
		prefix := "e2e-dra-" + rand.String(8)
		classes := []string{prefix + "-a", prefix + "-a2", prefix + "-b"}
		for i, name := range classes {
			owner := "a"
			if i == 2 {
				owner = "b"
			}
			dc := &resources.DeviceClass{
				Name:   name,
				Labels: map[string]string{"env": "e2e", "dra-tenant": prefix + "-" + owner},
				Spec: resources.DeviceClassSpec{Selectors: []resources.DeviceSelector{
					{CEL: &resources.CELDeviceSelector{Expression: "device.driver == 'dra.example.com'"}},
				}},
			}
			Expect(k8sClient.Create(ctx, dc)).To(Succeed())
			DeferCleanup(EventuallyDeletion, dc)
		}

		policies := []*api.SelectorAllowedListSpec{{}, {}}
		switch policyType {
		case "names":
			policies[0].Exact = classes[:2]
			policies[1].Exact = classes[2:]
		case "regex":
			policies[0].Regex = "^" + prefix + "-a2?$"
			policies[1].Regex = "^" + prefix + "-b$"
		case "labels":
			policies[0].MatchLabels = map[string]string{"dra-tenant": prefix + "-a"}
			policies[1].MatchLabels = map[string]string{"dra-tenant": prefix + "-b"}
		}

		var namespaces []string
		var owners []client.Client
		for i, policy := range policies {
			name := fmt.Sprintf("%s-%d", prefix, i)
			tnt := &capsulev1beta2.Tenant{
				Name: name, Labels: map[string]string{"env": "e2e"},
				Spec: capsulev1beta2.TenantSpec{
					Owners:        []rbac.OwnerSpec{{Kind: "User", Name: name}},
					DeviceClasses: policy,
				},
			}
			Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
			DeferCleanup(EventuallyDeletion, tnt)
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
			ns := NewNamespace(name+"-ns", map[string]string{meta.TenantLabel: name})
			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())
			TenantNamespaceReady(tnt, ns, 1)
			namespaces = append(namespaces, ns.Name)
			owners = append(owners, impersonationClient(name, withDefaultGroups(nil)))
		}

		for _, tc := range []struct {
			name     string
			requests []resources.DeviceRequest
			owner    int
			denied   string
		}{
			{name: "allowed-exact", requests: draExactRequests(classes[0], classes[1])},
			{name: "allowed-alternatives", requests: draAlternativeRequests(classes[0], classes[1])},
			{name: "repeated-class", requests: draExactRequests(classes[0], classes[0])},
			{name: "forbidden-second-request", requests: draExactRequests(classes[0], classes[2]), denied: "Device Class " + classes[2] + " is forbidden"},
			{name: "forbidden-first-request", requests: draExactRequests(classes[2], classes[0]), denied: "Device Class " + classes[2] + " is forbidden"},
			{name: "forbidden-fallback", requests: draAlternativeRequests(classes[0], classes[2]), denied: "Device Class " + classes[2] + " is forbidden"},
			{name: "forbidden-first-alternative", requests: draAlternativeRequests(classes[2], classes[0]), denied: "Device Class " + classes[2] + " is forbidden"},
			{name: "missing-second-class", requests: draExactRequests(classes[0], prefix+"-missing"), denied: "the selected device class does not exist"},
			{name: "missing-fallback", requests: draAlternativeRequests(classes[0], prefix+"-missing"), denied: "the selected device class does not exist"},
			{name: "mixed-requests", requests: append(draExactRequests(classes[0]), draAlternativeRequests(classes[1], classes[2])...), denied: "Device Class " + classes[2] + " is forbidden"},
			{name: "tenant-b-allowed", owner: 1, requests: draAlternativeRequests(classes[2])},
			{name: "tenant-b-cannot-use-a", owner: 1, requests: draExactRequests(classes[2], classes[0]), denied: "Device Class " + classes[0] + " is forbidden"},
		} {
			By(tc.name)
			obj := draClaimObject(kind, tc.name, namespaces[tc.owner], tc.requests)
			if tc.denied == "" {
				Eventually(func() error {
					return owners[tc.owner].Create(ctx, obj)
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
				persisted := obj.DeepCopyObject().(client.Object)
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), persisted)).To(Succeed())
			} else {
				err := owners[tc.owner].Create(ctx, obj)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring(tc.denied))
				Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), obj))).To(BeTrue())
			}
		}

		By("rejecting claims in another tenant's namespace")
		crossTenant := draClaimObject(kind, "cross-tenant", namespaces[1], draExactRequests(classes[2]))
		Expect(apierrors.IsForbidden(owners[0].Create(ctx, crossTenant))).To(BeTrue())
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(crossTenant), crossTenant))).To(BeTrue())

		By("honoring changed class labels on subsequent admissions")
		if policyType == "labels" {
			dc := &resources.DeviceClass{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: classes[1]}, dc)).To(Succeed())
			dc.Labels["dra-tenant"] = prefix + "-b"
			Expect(k8sClient.Update(ctx, dc)).To(Succeed())
			obj := draClaimObject(kind, "changed-class", namespaces[0], draAlternativeRequests(classes[0], classes[1]))
			Eventually(func(g Gomega) {
				err := owners[0].Create(ctx, obj, client.DryRunAll)
				g.Expect(err).To(HaveOccurred())
				g.Expect(err.Error()).To(ContainSubstring("Device Class " + classes[1] + " is forbidden"))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), obj))).To(BeTrue())
			obj = draClaimObject(kind, "changed-class", namespaces[1], draExactRequests(classes[1]))
			Eventually(func() error { return owners[1].Create(ctx, obj) }, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), obj)).To(Succeed())
		}
	},
		Entry("ResourceClaim with label selectors", "ResourceClaim", "labels"),
		Entry("ResourceClaimTemplate with label selectors", "ResourceClaimTemplate", "labels"),
		Entry("ResourceClaim with name allowlists", "ResourceClaim", "names"),
		Entry("ResourceClaimTemplate with name allowlists", "ResourceClaimTemplate", "names"),
		Entry("ResourceClaim with regex allowlists", "ResourceClaim", "regex"),
		Entry("ResourceClaimTemplate with regex allowlists", "ResourceClaimTemplate", "regex"),
	)
})

func draExactRequests(classes ...string) []resources.DeviceRequest {
	requests := make([]resources.DeviceRequest, 0, len(classes))
	for i, class := range classes {
		requests = append(requests, resources.DeviceRequest{
			Name: fmt.Sprintf("request-%d", i), Exactly: &resources.ExactDeviceRequest{DeviceClassName: class},
		})
	}
	return requests
}

func draAlternativeRequests(classes ...string) []resources.DeviceRequest {
	request := resources.DeviceRequest{Name: "alternatives"}
	for i, class := range classes {
		request.FirstAvailable = append(request.FirstAvailable, resources.DeviceSubRequest{
			Name: fmt.Sprintf("alternative-%d", i), DeviceClassName: class,
		})
	}
	return []resources.DeviceRequest{request}
}

func draClaimObject(kind, name, namespace string, requests []resources.DeviceRequest) client.Object {
	spec := resources.ResourceClaimSpec{Devices: resources.DeviceClaim{Requests: requests}}
	if kind == "ResourceClaim" {
		return &resources.ResourceClaim{Name: name, Namespace: namespace, Spec: spec}
	}
	return &resources.ResourceClaimTemplate{Name: name, Namespace: namespace, Spec: resources.ResourceClaimTemplateSpec{Spec: spec}}
}
