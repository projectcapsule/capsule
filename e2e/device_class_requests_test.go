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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/runtime/gvk"
)

var _ = Describe("DeviceClass request authorization", Label("tenant", "classes", "deviceclass", "dra-requests"), func() {
	DescribeTable("validates every request and fallback within its tenant", func(kind, policyType string) {
		ctx := context.Background()
		mapping, err := gvk.PreferredRESTMapping(k8sClient.RESTMapper(),
			resources.SchemeGroupVersion.WithKind("DeviceClass").GroupKind(), "v1", "v1beta2",
		)
		Expect(err).NotTo(HaveOccurred())
		version := mapping.GroupVersionKind.Version
		By("using resource.k8s.io/" + version)

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
			class := draObjectForVersion(dc, version)
			Expect(k8sClient.Create(ctx, class)).To(Succeed())
			DeferCleanup(EventuallyDeletion, class)
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
			if policyType != "regex" {
				want := classes[:2]
				if i == 1 {
					want = classes[2:]
				}
				Eventually(func(g Gomega) {
					current := &capsulev1beta2.Tenant{}
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tnt), current)).To(Succeed())
					g.Expect(current.Status.Classes.DeviceClasses).To(ConsistOf(want))
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}
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
			obj := draClaimObject(version, kind, tc.name, namespaces[tc.owner], tc.requests)
			if tc.denied == "" {
				Eventually(func() error {
					return owners[tc.owner].Create(ctx, obj)
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
				persisted := obj.DeepCopyObject().(client.Object)
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), persisted)).To(Succeed())
				path := []string{"spec", "devices", "requests"}
				if kind == "ResourceClaimTemplate" {
					path = append([]string{"spec"}, path...)
				}
				stored, found, err := unstructured.NestedSlice(persisted.(*unstructured.Unstructured).Object, path...)
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())
				Expect(stored).To(HaveLen(len(tc.requests)))
				for i, request := range tc.requests {
					fields := stored[i].(map[string]any)
					if request.Exactly != nil {
						name, _, err := unstructured.NestedString(fields, "exactly", "deviceClassName")
						Expect(err).NotTo(HaveOccurred())
						Expect(name).To(Equal(request.Exactly.DeviceClassName))
					} else {
						alternatives, _, err := unstructured.NestedSlice(fields, "firstAvailable")
						Expect(err).NotTo(HaveOccurred())
						Expect(alternatives).To(HaveLen(len(request.FirstAvailable)), "DRAPrioritizedList must be enabled")
						for j, alternative := range request.FirstAvailable {
							Expect(alternatives[j].(map[string]any)["deviceClassName"]).To(Equal(alternative.DeviceClassName))
						}
					}
				}
			} else {
				err := owners[tc.owner].Create(ctx, obj)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring(tc.denied))
				Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), obj))).To(BeTrue())
			}
		}

		By("rejecting claims in another tenant's namespace")
		crossTenant := draClaimObject(version, kind, "cross-tenant", namespaces[1], draExactRequests(classes[2]))
		Expect(apierrors.IsForbidden(owners[0].Create(ctx, crossTenant))).To(BeTrue())
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(crossTenant), crossTenant))).To(BeTrue())

		By("honoring changed class labels on subsequent admissions")
		if policyType == "labels" {
			dc := draObjectForVersion(&resources.DeviceClass{}, version)
			Eventually(func() error {
				if err := k8sClient.Get(ctx, client.ObjectKey{Name: classes[1]}, dc); err != nil {
					return err
				}
				labels := dc.GetLabels()
				labels["dra-tenant"] = prefix + "-b"
				dc.SetLabels(labels)
				return k8sClient.Update(ctx, dc)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			obj := draClaimObject(version, kind, "changed-class", namespaces[0], draAlternativeRequests(classes[0], classes[1]))
			Eventually(func(g Gomega) {
				err := owners[0].Create(ctx, obj, client.DryRunAll)
				g.Expect(err).To(HaveOccurred())
				g.Expect(err.Error()).To(ContainSubstring("Device Class " + classes[1] + " is forbidden"))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), obj))).To(BeTrue())
			obj = draClaimObject(version, kind, "changed-class", namespaces[1], draExactRequests(classes[1]))
			Eventually(func() error { return owners[1].Create(ctx, obj) }, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), obj)).To(Succeed())
			Eventually(func(g Gomega) {
				for i, want := range [][]string{{classes[0]}, {classes[1], classes[2]}} {
					current := &capsulev1beta2.Tenant{}
					g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: fmt.Sprintf("%s-%d", prefix, i)}, current)).To(Succeed())
					g.Expect(current.Status.Classes.DeviceClasses).To(ConsistOf(want))
				}
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
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

func draClaimObject(version, kind, name, namespace string, requests []resources.DeviceRequest) client.Object {
	spec := resources.ResourceClaimSpec{Devices: resources.DeviceClaim{Requests: requests}}
	if kind == "ResourceClaim" {
		return draObjectForVersion(&resources.ResourceClaim{Name: name, Namespace: namespace, Spec: spec}, version)
	}
	return draObjectForVersion(&resources.ResourceClaimTemplate{Name: name, Namespace: namespace, Spec: resources.ResourceClaimTemplateSpec{Spec: spec}}, version)
}

// The v1 and v1beta2 DRA objects used here have the same wire representation.
// Use the served GVK so the same scenarios run on both 1.33 and stable-v1 clusters.
func draObjectForVersion(obj client.Object, version string) *unstructured.Unstructured {
	values, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	Expect(err).NotTo(HaveOccurred())
	kind := "DeviceClass"
	switch obj.(type) {
	case *resources.ResourceClaim:
		kind = "ResourceClaim"
	case *resources.ResourceClaimTemplate:
		kind = "ResourceClaimTemplate"
	}
	result := &unstructured.Unstructured{Object: values}
	result.SetGroupVersionKind(resources.SchemeGroupVersion.WithKind(kind))
	result.SetAPIVersion(resources.GroupName + "/" + version)
	return result
}
