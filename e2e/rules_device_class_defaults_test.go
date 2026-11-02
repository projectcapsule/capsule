// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	resources "k8s.io/api/resource/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/gvk"
)

var _ = Describe("DeviceClass default namespace profiles", Label("tenant", "rules", "mutation", "deviceclass", "device-class-defaults"), func() {
	DescribeTable("defaults empty references without replacing explicit classes", func(kind string) {
		ctx := context.Background()
		mapping, err := gvk.PreferredRESTMapping(k8sClient.RESTMapper(), resources.SchemeGroupVersion.WithKind("DeviceClass").GroupKind(), "v1", "v1beta2")
		Expect(err).NotTo(HaveOccurred())
		version := mapping.GroupVersionKind.Version
		prefix := "e2e-device-defaults-" + rand.String(8)
		condition := []rules.AdmissionCondition{{Expression: `object.metadata.labels['default-stage'] == 'metadata-first' && !('skip-default' in object.metadata.labels)`}}
		profile := func(name string, defaults ...string) *rules.NamespaceRuleBodyTenant {
			body := &rules.NamespaceRuleBodyTenant{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"device-profile": name}}, NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{}}
			for _, class := range defaults {
				body.Mutate = append(body.Mutate, rules.NamespaceRuleMutation{Conditions: condition, Devices: rules.DeviceMutation{Class: class}})
			}
			return body
		}
		var tenants []*capsulev1beta2.Tenant
		var owners []client.Client
		for _, suffix := range []string{"a", "b"} {
			name := prefix + "-" + suffix
			for _, device := range []string{"gpu", "cpu"} {
				class := draObjectForVersion(&resources.DeviceClass{Name: name + "-" + device, Labels: map[string]string{"env": "e2e"}, Spec: resources.DeviceClassSpec{Selectors: []resources.DeviceSelector{{CEL: &resources.CELDeviceSelector{Expression: "device.driver == 'dra.example.com'"}}}}}, version)
				Expect(k8sClient.Create(ctx, class)).To(Succeed())
				DeferCleanup(DeleteInBackground, class)
			}
			tnt := &capsulev1beta2.Tenant{Name: name, Labels: map[string]string{"env": "e2e"}, Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{Kind: "User", Name: name}}, Rules: []*rules.NamespaceRuleBodyTenant{
				{NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeAllow, Metadata: []rules.MetadataRule{{APIGroups: []string{resources.GroupName}, Kinds: []string{"ResourceClaim", "ResourceClaimTemplate"}, Labels: map[string]rules.MetadataValueRule{"default-stage": {Default: new("metadata-first"), Values: []apiruntime.ExpressionMatch{{Exact: []string{"metadata-first"}}}}}}}}}},
				profile("gpu", "{{ .tenant.metadata.name }}-gpu", "{{ .tenant.metadata.name }}-cpu"),
				profile("cpu", "{{ .tenant.metadata.name }}-cpu"),
				{NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeAllow, Devices: rules.NamespaceRuleEnforceDevicesBody{Classes: []rules.DeviceClassMatch{{Exact: []string{name + "-gpu", name + "-cpu"}}}}}}},
			}}}
			Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
			DeferCleanup(DeleteInBackground, tnt)
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
			tenants = append(tenants, tnt)
			owners = append(owners, impersonationClient(name, withDefaultGroups(nil)))
		}
		waitProfile := func(ns *corev1.Namespace, defaults ...string) {
			Eventually(func(g Gomega) {
				status := &capsulev1beta2.RuleStatus{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: meta.NameForManagedRuleStatus()}, status)).To(Succeed())
				g.Expect(status.Status.ObservedGeneration).To(Equal(status.Generation))
				ready := status.Status.Conditions.GetConditionByType(meta.ReadyCondition)
				g.Expect(ready).NotTo(BeNil())
				g.Expect(ready.Status).To(Equal(metav1.ConditionTrue))
				var actual []string
				for _, body := range status.Status.Rules {
					for _, mutation := range body.Mutate {
						if mutation.Devices.Class != "" {
							actual = append(actual, mutation.Devices.Class)
						}
					}
				}
				g.Expect(actual).To(Equal(defaults))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		newNS := func(tenant int, name string, defaults ...string) *corev1.Namespace {
			ns := NewNamespace("", map[string]string{meta.TenantLabel: tenants[tenant].Name, "device-profile": name})
			NamespaceCreation(ns, tenants[tenant].Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tenants[tenant], ns).Should(Succeed())
			DeferCleanup(DeleteNamespaceInBackground, ctx, ns.Name)
			waitProfile(ns, defaults...)
			return ns
		}
		gpuA, cpuA, gpuB := tenants[0].Name+"-gpu", tenants[0].Name+"-cpu", tenants[1].Name+"-gpu"
		gpuNS := newNS(0, "gpu", gpuA, cpuA)
		cpuNS := newNS(0, "cpu", cpuA)
		otherNS := newNS(0, "other")
		tenantBNS := newNS(1, "gpu", gpuB, tenants[1].Name+"-cpu")
		sequence := 0
		claim := func(ns *corev1.Namespace, requests []resources.DeviceRequest) client.Object {
			sequence++
			obj := draClaimObject(version, kind, fmt.Sprintf("claim-%d", sequence), ns.Name, requests)
			obj.SetLabels(map[string]string{"env": "e2e"})
			return obj
		}
		expectNames := func(obj client.Object, wanted ...string) {
			Eventually(func(g Gomega) {
				stored := &unstructured.Unstructured{}
				stored.SetGroupVersionKind(obj.GetObjectKind().GroupVersionKind())
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), stored)).To(Succeed())
				path := []string{"spec", "devices", "requests"}
				if kind == "ResourceClaimTemplate" {
					path = append([]string{"spec"}, path...)
				}
				requests, _, err := unstructured.NestedSlice(stored.Object, path...)
				g.Expect(err).NotTo(HaveOccurred())
				var actual []string
				for _, raw := range requests {
					request := raw.(map[string]any)
					if exact, found := request["exactly"]; found {
						actual = append(actual, exact.(map[string]any)["deviceClassName"].(string))
					}
					if alternatives, found := request["firstAvailable"]; found {
						for _, alternative := range alternatives.([]any) {
							actual = append(actual, alternative.(map[string]any)["deviceClassName"].(string))
						}
					}
				}
				g.Expect(actual).To(Equal(wanted))
				g.Expect(stored.GetLabels()).To(HaveKeyWithValue("default-stage", "metadata-first"))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		create := func(owner client.Client, obj client.Object, wanted ...string) {
			Eventually(func() error { return owner.Create(ctx, obj) }, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			expectNames(obj, wanted...)
		}
		deny := func(owner client.Client, obj client.Object, reason string) {
			err := owner.Create(ctx, obj)
			Expect(err).To(HaveOccurred(), "a forbidden create must never succeed")
			Expect(err.Error()).To(ContainSubstring(reason))
			Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), obj))).To(BeTrue())
		}
		By("defaulting both request forms, preserving explicit classes, and composing namespace profiles")
		original := claim(gpuNS, draExactRequests(""))
		create(owners[0], original, gpuA)
		create(owners[0], claim(gpuNS, draExactRequests(cpuA)), cpuA)
		create(owners[0], claim(gpuNS, draAlternativeRequests("", cpuA, "")), gpuA, cpuA, gpuA)
		create(owners[0], claim(cpuNS, draExactRequests("")), cpuA)
		create(owners[1], claim(tenantBNS, draExactRequests("")), gpuB)
		create(owners[0], claim(otherNS, draExactRequests(gpuA)), gpuA)
		deny(owners[0], claim(otherNS, draExactRequests("")), "deviceClassName")
		By("retaining condition skips, native validation, and class enforcement after mutation")
		skipped := claim(gpuNS, draExactRequests(""))
		skipped.SetLabels(map[string]string{"env": "e2e", "skip-default": "true"})
		deny(owners[0], skipped, "deviceClassName")
		deny(owners[0], claim(gpuNS, []resources.DeviceRequest{{Name: "missing-form"}}), "spec.devices.requests")
		deny(owners[0], claim(gpuNS, draExactRequests(gpuB)), "not allowed")
		deny(owners[0], claim(gpuNS, draAlternativeRequests("", gpuB)), "not allowed")
		By("using the destination profile even for delegated cross-tenant writes")
		cross := claim(tenantBNS, draExactRequests(""))
		err = owners[0].Create(ctx, cross)
		Expect(apierrors.IsForbidden(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("cannot create resource"))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(cross), cross))).To(BeTrue())
		role := &rbacv1.Role{Name: "claim-writer", Namespace: tenantBNS.Name, Rules: []rbacv1.PolicyRule{{APIGroups: []string{resources.GroupName}, Resources: []string{"resourceclaims", "resourceclaimtemplates"}, Verbs: []string{"create"}}}}
		Expect(k8sClient.Create(ctx, role)).To(Succeed())
		binding := &rbacv1.RoleBinding{Name: role.Name, Namespace: role.Namespace, RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: role.Name}, Subjects: []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: "User", Name: tenants[0].Name}}}
		Expect(k8sClient.Create(ctx, binding)).To(Succeed())
		create(owners[0], claim(tenantBNS, draExactRequests("")), gpuB)
		deny(owners[0], claim(tenantBNS, draExactRequests(gpuA)), "not allowed")

		By("converging after namespace selection changes")
		nsRole := &rbacv1.ClusterRole{Name: prefix + "-namespace-update", Labels: map[string]string{"env": "e2e"}, Rules: []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"namespaces"}, ResourceNames: []string{cpuNS.Name}, Verbs: []string{"get", "update"}}}}
		Expect(k8sClient.Create(ctx, nsRole)).To(Succeed())
		DeferCleanup(DeleteInBackground, nsRole)
		nsBinding := &rbacv1.ClusterRoleBinding{Name: nsRole.Name, Labels: map[string]string{"env": "e2e"}, RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: nsRole.Name}, Subjects: []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: "User", Name: tenants[0].Name}}}
		Expect(k8sClient.Create(ctx, nsBinding)).To(Succeed())
		DeferCleanup(DeleteInBackground, nsBinding)
		Eventually(func() error {
			current := &corev1.Namespace{}
			if err := owners[0].Get(ctx, client.ObjectKeyFromObject(cpuNS), current); err != nil {
				return err
			}
			current.Labels["device-profile"] = "gpu"
			return owners[0].Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		waitProfile(cpuNS, gpuA, cpuA)
		create(owners[0], claim(cpuNS, draExactRequests("")), gpuA)
		By("converging after a default policy changes without reconciling existing resources")
		Eventually(func() error {
			current := &capsulev1beta2.Tenant{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(tenants[0]), current); err != nil {
				return err
			}
			current.Spec.Rules[1].Mutate[0].Devices.Class = cpuA
			return k8sClient.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		waitProfile(gpuNS, cpuA, cpuA)
		create(owners[0], claim(gpuNS, draExactRequests("")), cpuA)
		expectNames(original, gpuA)
		Eventually(func() error {
			current := &unstructured.Unstructured{}
			current.SetGroupVersionKind(original.GetObjectKind().GroupVersionKind())
			if err := owners[0].Get(ctx, client.ObjectKeyFromObject(original), current); err != nil {
				return err
			}
			labels := current.GetLabels()
			labels["reviewed"] = "true"
			current.SetLabels(labels)
			return owners[0].Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		expectNames(original, gpuA)

		if kind == "ResourceClaimTemplate" {
			By("preserving the old template class on a controller-generated claim after the default changes")
			pod := MakePod(gpuNS.Name, "controller-claim", map[string]string{"env": "e2e"}, nil, "registry.k8s.io/pause:3.10", "", "")
			pod.Spec.ResourceClaims = []corev1.PodResourceClaim{{Name: "device", ResourceClaimTemplateName: new(original.GetName())}}
			pod.Spec.Containers[0].Resources.Claims = []corev1.ResourceClaim{{Name: "device"}}
			Expect(owners[0].Create(ctx, pod)).To(Succeed())
			Eventually(func(g Gomega) {
				current := &corev1.Pod{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(pod), current)).To(Succeed())
				g.Expect(current.Status.ResourceClaimStatuses).To(HaveLen(1))
				g.Expect(current.Status.ResourceClaimStatuses[0].ResourceClaimName).NotTo(BeNil())
				generated := draObjectForVersion(&resources.ResourceClaim{}, version)
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: *current.Status.ResourceClaimStatuses[0].ResourceClaimName, Namespace: gpuNS.Name}, generated)).To(Succeed())
				g.Expect(generated.GetOwnerReferences()).To(ContainElement(HaveField("UID", pod.UID)))
				requests, _, err := unstructured.NestedSlice(generated.Object, "spec", "devices", "requests")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(requests).To(HaveLen(1))
				g.Expect(requests[0].(map[string]any)["exactly"].(map[string]any)["deviceClassName"]).To(Equal(gpuA))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}

		By("rejecting invalid policies and a default that is outside the active allow-list")
		for _, replace := range []bool{false, true} {
			Eventually(func(g Gomega) {
				current := &capsulev1beta2.Tenant{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tenants[0]), current)).To(Succeed())
				mutation := &current.Spec.Rules[1].Mutate[0]
				if replace {
					mutation.Action = rules.MutationActionReplace
				} else {
					mutation.Devices.Class = "bad/name"
				}
				err := k8sClient.Update(ctx, current)
				if err == nil {
					StopTrying("an invalid DeviceClass mutation was accepted").Now()
				}
				g.Expect(err).To(MatchError(ContainSubstring("devices.class")))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		storedTenant := &capsulev1beta2.Tenant{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tenants[0]), storedTenant)).To(Succeed())
		Expect(storedTenant.Spec.Rules[1].Mutate[0].Devices.Class).To(Equal(cpuA))
		Expect(storedTenant.Spec.Rules[1].Mutate[0].Action).To(Equal(rules.MutationActionMerge))
		Eventually(func() error {
			current := &capsulev1beta2.Tenant{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(tenants[0]), current); err != nil {
				return err
			}
			current.Spec.Rules[1].Mutate[0].Devices.Class = gpuB
			return k8sClient.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		waitProfile(gpuNS, gpuB, cpuA)
		deny(owners[0], claim(gpuNS, draExactRequests("")), "not allowed")
		expectNames(original, gpuA)
		create(owners[1], claim(tenantBNS, draExactRequests("")), gpuB)
	}, Entry("ResourceClaim", "ResourceClaim"), Entry("ResourceClaimTemplate", "ResourceClaimTemplate"))
})
