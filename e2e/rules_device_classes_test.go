// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"
	"strings"

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
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/gvk"
)

var _ = Describe("DeviceClass namespace profiles", Label("tenant", "rules", "deviceclass", "dra-class-rules"), func() {
	DescribeTable("enforces composed profiles, changes, and tenant boundaries", func(kind string) {
		ctx := context.Background()
		mapping, err := gvk.PreferredRESTMapping(k8sClient.RESTMapper(), resources.SchemeGroupVersion.WithKind("DeviceClass").GroupKind(), "v1", "v1beta2")
		Expect(err).NotTo(HaveOccurred())
		version := mapping.GroupVersionKind.Version
		prefix := "e2e-dra-rules-" + rand.String(8)
		classes := []string{prefix + "-a", prefix + "-a2", prefix + "-b"}
		for i, name := range classes {
			pool := "a"
			if i == 2 {
				pool = "b"
			}
			obj := draObjectForVersion(&resources.DeviceClass{Name: name, Labels: map[string]string{"env": "e2e", "dra-pool": prefix + "-" + pool}, Spec: resources.DeviceClassSpec{Selectors: []resources.DeviceSelector{{CEL: &resources.CELDeviceSelector{Expression: "device.driver == 'dra.example.com'"}}}}}, version)
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(DeleteInBackground, obj)
		}
		policy := func(profile string, action rules.ActionType, matches ...rules.DeviceClassMatch) *rules.NamespaceRuleBodyTenant {
			body := &rules.NamespaceRuleBodyTenant{NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Action: action, Devices: rules.NamespaceRuleEnforceDevicesBody{Classes: matches}}}}
			if profile != "" {
				body.NamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"dra-profile": profile}}
			}
			return body
		}
		a := &capsulev1beta2.Tenant{Name: prefix + "-a", Labels: map[string]string{"env": "e2e"}, Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{Kind: "User", Name: prefix + "-a"}}, Rules: []*rules.NamespaceRuleBodyTenant{
			policy("named", rules.ActionTypeDeny, rules.DeviceClassMatch{Expression: "^" + prefix + "-"}),
			policy("named", rules.ActionTypeAllow, rules.DeviceClassMatch{Exact: classes[:1]}),
			policy("selected", rules.ActionTypeAllow, rules.DeviceClassMatch{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"dra-pool": prefix + "-a"}}}),
			policy("named", rules.ActionTypeAudit, rules.DeviceClassMatch{Exact: classes[2:]}),
			{NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeDeny, Metadata: []rules.MetadataRule{{APIGroups: []string{resources.GroupName}, Kinds: []string{kind}, Labels: map[string]rules.MetadataValueRule{"dra-denied": {Values: []apiruntime.ExpressionMatch{{Exact: []string{"true"}}}}}}}}}},
		}}}
		b := &capsulev1beta2.Tenant{Name: prefix + "-b", Labels: map[string]string{"env": "e2e"}, Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{Kind: "User", Name: prefix + "-b"}}, Rules: []*rules.NamespaceRuleBodyTenant{policy("", rules.ActionTypeAllow, rules.DeviceClassMatch{Exact: classes[2:]})}}}
		for _, tnt := range []*capsulev1beta2.Tenant{a, b} {
			Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
			DeferCleanup(DeleteInBackground, tnt)
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
		}
		ownerA := impersonationClient(a.Name, withDefaultGroups(nil))
		ownerB := impersonationClient(b.Name, withDefaultGroups(nil))
		waitProfile := func(ns *corev1.Namespace, wanted ...*rules.NamespaceRuleBodyTenant) {
			expected := make([]*rules.NamespaceRuleBodyNamespace, 0, len(wanted))
			for _, body := range wanted {
				expected = append(expected, body.NamespaceRuleBodyNamespace)
			}
			Eventually(func(g Gomega) {
				status := &capsulev1beta2.RuleStatus{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: meta.NameForManagedRuleStatus(), Namespace: ns.Name}, status)).To(Succeed())
				g.Expect(status.Status.ObservedGeneration).To(Equal(status.Generation))
				g.Expect(status.Status.Rules).To(Equal(expected))
				condition := status.Status.Conditions.GetConditionByType(meta.ReadyCondition)
				g.Expect(condition).NotTo(BeNil())
				g.Expect(condition.Status).To(Equal(metav1.ConditionTrue))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		newNS := func(tnt *capsulev1beta2.Tenant, profile string, wanted ...*rules.NamespaceRuleBodyTenant) *corev1.Namespace {
			ns := NewNamespace("", map[string]string{meta.TenantLabel: tnt.Name, "dra-profile": profile})
			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())
			DeferCleanup(DeleteNamespaceInBackground, ctx, ns.Name)
			waitProfile(ns, wanted...)
			return ns
		}
		named := newNS(a, "named", a.Spec.Rules[0], a.Spec.Rules[1], a.Spec.Rules[3], a.Spec.Rules[4])
		selected := newNS(a, "selected", a.Spec.Rules[2], a.Spec.Rules[4])
		other := newNS(a, "other", a.Spec.Rules[4])
		isolated := newNS(b, "named", b.Spec.Rules[0])
		sequence := 0
		claim := func(ns *corev1.Namespace, requests []resources.DeviceRequest) client.Object {
			sequence++
			obj := draClaimObject(version, kind, fmt.Sprintf("claim-%d", sequence), ns.Name, requests)
			obj.SetLabels(map[string]string{"env": "e2e"})
			return obj
		}
		create := func(actor client.Client, obj client.Object) {
			Eventually(func() error { return actor.Create(ctx, obj) }, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
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
				g.Expect(requests).NotTo(BeEmpty())
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		deny := func(actor client.Client, obj client.Object, reason string) {
			err := actor.Create(ctx, obj)
			Expect(err).To(HaveOccurred(), "an unauthorized create must never succeed")
			Expect(err.Error()).To(ContainSubstring(reason))
			Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), obj))).To(BeTrue())
		}
		By("applying different profiles within one tenant and isolating another tenant")
		create(ownerA, claim(named, draExactRequests(classes[0])))
		deny(ownerA, claim(named, draAlternativeRequests(classes[0], classes[2])), "firstAvailable[1].deviceClassName")
		create(ownerA, claim(selected, draAlternativeRequests(classes[0], classes[1])))
		deny(ownerA, claim(selected, draExactRequests(classes[2])), "not allowed")
		create(ownerA, claim(other, draExactRequests(classes[2])))
		create(ownerB, claim(isolated, draExactRequests(classes[2])))
		deny(ownerB, claim(isolated, draExactRequests(classes[0])), "not allowed")
		By("continuing metadata enforcement after a class is allowed")
		metadataDenied := claim(named, draExactRequests(classes[0]))
		metadataDenied.SetLabels(map[string]string{"env": "e2e", "dra-denied": "true"})
		deny(ownerA, metadataDenied, "metadata label")
		By("using the destination namespace profile even with explicitly delegated access")
		cross := claim(isolated, draExactRequests(classes[2]))
		Expect(apierrors.IsForbidden(ownerA.Create(ctx, cross))).To(BeTrue())
		role := &rbacv1.Role{Name: "claim-writer", Namespace: isolated.Name, Rules: []rbacv1.PolicyRule{{APIGroups: []string{resources.GroupName}, Resources: []string{"resourceclaims", "resourceclaimtemplates"}, Verbs: []string{"create"}}}}
		Expect(k8sClient.Create(ctx, role)).To(Succeed())
		binding := &rbacv1.RoleBinding{Name: role.Name, Namespace: role.Namespace, RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: role.Name}, Subjects: []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: "User", Name: a.Name}}}
		Expect(k8sClient.Create(ctx, binding)).To(Succeed())
		create(ownerA, claim(isolated, draExactRequests(classes[2])))
		deny(ownerA, claim(isolated, draExactRequests(classes[0])), "not allowed")

		var controllerTemplate client.Object
		claimPod := func(name string) *corev1.Pod {
			pod := MakePod(selected.Name, name, map[string]string{"env": "e2e"}, nil, "registry.k8s.io/pause:3.10", "", "")
			pod.Spec.ResourceClaims = []corev1.PodResourceClaim{{Name: "device", ResourceClaimTemplateName: new(controllerTemplate.GetName())}}
			pod.Spec.Containers[0].Resources.Claims = []corev1.ResourceClaim{{Name: "device"}}
			return pod
		}
		if kind == "ResourceClaimTemplate" {
			By("checking claims generated by kube-controller-manager against the namespace profile")
			controllerTemplate = claim(selected, draExactRequests(classes[0]))
			create(ownerA, controllerTemplate)
			pod := claimPod("allowed-controller-claim")
			Expect(ownerA.Create(ctx, pod)).To(Succeed())
			Eventually(func(g Gomega) {
				current := &corev1.Pod{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(pod), current)).To(Succeed())
				g.Expect(current.Status.ResourceClaimStatuses).To(HaveLen(1))
				g.Expect(current.Status.ResourceClaimStatuses[0].ResourceClaimName).NotTo(BeNil())
				generated := draObjectForVersion(&resources.ResourceClaim{}, version)
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: selected.Name, Name: *current.Status.ResourceClaimStatuses[0].ResourceClaimName}, generated)).To(Succeed())
				g.Expect(generated.GetOwnerReferences()).To(ContainElement(HaveField("UID", pod.UID)))
				requests, _, err := unstructured.NestedSlice(generated.Object, "spec", "devices", "requests")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(requests).To(HaveLen(1))
				g.Expect(requests[0].(map[string]any)["exactly"].(map[string]any)["deviceClassName"]).To(Equal(classes[0]))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}

		By("converging after namespace selection changes")
		namespaceRole := &rbacv1.ClusterRole{Name: prefix + "-namespace-update", Labels: map[string]string{"env": "e2e"}, Rules: []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"namespaces"}, ResourceNames: []string{named.Name}, Verbs: []string{"get", "update"}}}}
		Expect(k8sClient.Create(ctx, namespaceRole)).To(Succeed())
		DeferCleanup(DeleteInBackground, namespaceRole)
		namespaceBinding := &rbacv1.ClusterRoleBinding{Name: namespaceRole.Name, Labels: map[string]string{"env": "e2e"}, RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: namespaceRole.Name}, Subjects: []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: "User", Name: a.Name}}}
		Expect(k8sClient.Create(ctx, namespaceBinding)).To(Succeed())
		DeferCleanup(DeleteInBackground, namespaceBinding)
		Eventually(func() error {
			current := &corev1.Namespace{}
			if err := ownerA.Get(ctx, client.ObjectKeyFromObject(named), current); err != nil {
				return err
			}
			current.Labels["dra-profile"] = "other"
			return ownerA.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		waitProfile(named, a.Spec.Rules[4])
		create(ownerA, claim(named, draExactRequests(classes[2])))
		By("using current DeviceClass labels without reusing another namespace's decision")
		Eventually(func() error {
			current := draObjectForVersion(&resources.DeviceClass{}, version)
			if err := k8sClient.Get(ctx, client.ObjectKey{Name: classes[1]}, current); err != nil {
				return err
			}
			labels := current.GetLabels()
			labels["dra-pool"] = prefix + "-b"
			current.SetLabels(labels)
			return k8sClient.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		deny(ownerA, claim(selected, draExactRequests(classes[1])), "not allowed")
		create(ownerB, claim(isolated, draExactRequests(classes[2])))
		By("converging after a rule changes")
		Eventually(func() error {
			current := &capsulev1beta2.Tenant{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(a), current); err != nil {
				return err
			}
			current.Spec.Rules[2].Enforce.Devices.Classes = []rules.DeviceClassMatch{{Exact: classes[1:2]}}
			return k8sClient.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		changed := policy("selected", rules.ActionTypeAllow, rules.DeviceClassMatch{Exact: classes[1:2]})
		waitProfile(selected, changed, a.Spec.Rules[4])
		create(ownerA, claim(selected, draExactRequests(classes[1])))
		deny(ownerA, claim(selected, draExactRequests(classes[0])), "not allowed")

		if controllerTemplate != nil {
			By("rejecting generated claims when an older template no longer matches the profile")
			pod := claimPod("denied-controller-claim")
			Expect(ownerA.Create(ctx, pod)).To(Succeed())
			Eventually(func(g Gomega) {
				list := &corev1.EventList{}
				g.Expect(k8sClient.List(ctx, list, client.InNamespace(selected.Name))).To(Succeed())
				found := false
				for _, event := range list.Items {
					if event.InvolvedObject.UID == pod.UID && strings.Contains(event.Message, "device class") && strings.Contains(event.Message, "not allowed") {
						found = true
					}
				}
				g.Expect(found).To(BeTrue(), "the claim controller must report the Capsule class denial")
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			list := &unstructured.UnstructuredList{}
			list.SetAPIVersion(resources.GroupName + "/" + version)
			list.SetKind("ResourceClaimList")
			Expect(k8sClient.List(ctx, list, client.InNamespace(selected.Name))).To(Succeed())
			for _, generated := range list.Items {
				for _, owner := range generated.GetOwnerReferences() {
					Expect(owner.UID).NotTo(Equal(pod.UID), "a forbidden generated claim must not persist")
				}
			}
		}

		By("retaining the legacy Tenant restriction as an additional boundary")
		Eventually(func() error {
			current := &capsulev1beta2.Tenant{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(a), current); err != nil {
				return err
			}
			current.Spec.DeviceClasses = &api.SelectorAllowedListSpec{Exact: classes[:1]}
			return k8sClient.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		deny(ownerA, claim(selected, draExactRequests(classes[1])), "Device Class "+classes[1]+" is forbidden")
		By("rejecting invalid policy while valid tenants are present")
		invalid := &capsulev1beta2.Tenant{Name: prefix + "-invalid", Spec: capsulev1beta2.TenantSpec{Owners: a.Spec.Owners, Rules: []*rules.NamespaceRuleBodyTenant{policy("", rules.ActionTypeAllow, rules.DeviceClassMatch{Expression: "["})}}}
		Expect(k8sClient.Create(ctx, invalid)).To(MatchError(ContainSubstring("enforce.devices.classes[0].exp")))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(invalid), invalid))).To(BeTrue())
	}, Entry("ResourceClaim", "ResourceClaim"), Entry("ResourceClaimTemplate", "ResourceClaimTemplate"))
})
