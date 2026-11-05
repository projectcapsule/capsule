// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	rbacv1 "k8s.io/api/rbac/v1"
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
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

const (
	classProfileLabel     = "ingress.projectcapsule.dev/class-owner"
	classNamespaceProfile = "ingress.projectcapsule.dev/profile"
)

type ingressProfileFixture struct {
	ctx        context.Context
	tenants    []*capsulev1beta2.Tenant
	actors     []client.Client
	namespaces []*corev1.Namespace
	classes    []*unstructured.Unstructured
}

func ingressProfileRule(profile string, body *rules.NamespaceRuleBodyNamespace) *rules.NamespaceRuleBodyTenant {
	return &rules.NamespaceRuleBodyTenant{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{classNamespaceProfile: profile}}, NamespaceRuleBodyNamespace: body}
}

func newIngressProfileFixture(build func(string) []*rules.NamespaceRuleBodyTenant) *ingressProfileFixture {
	GinkgoHelper()
	f := &ingressProfileFixture{ctx: context.Background()}
	prefix := "e2e-class-" + rand.String(7)
	role := &rbacv1.ClusterRole{Name: prefix, Labels: map[string]string{"env": "e2e"}, Rules: []rbacv1.PolicyRule{
		{APIGroups: []string{"networking.k8s.io"}, Resources: []string{"ingresses"}, Verbs: []string{"get", "list", "create", "update", "patch", "delete"}},
		{APIGroups: []string{"gateway.networking.k8s.io"}, Resources: []string{"gateways"}, Verbs: []string{"get", "list", "create", "update", "patch", "delete"}},
	}}
	Expect(k8sClient.Create(f.ctx, role)).To(Succeed())
	DeferCleanup(func() {
		for _, ns := range f.namespaces {
			DeleteNamespaceInBackground(f.ctx, ns.Name)
		}
		for _, tenant := range f.tenants {
			DeleteInBackground(tenant)
		}
		for _, class := range f.classes {
			DeleteInBackground(class)
		}
		DeleteInBackground(role)
	})
	for _, suffix := range []string{"a", "b"} {
		name := prefix + "-" + suffix
		for _, classSuffix := range []string{"edge", "public", "selected", "blocked", "forced"} {
			for _, kind := range []string{"IngressClass", "GatewayClass"} {
				group, controllerField := "networking.k8s.io/v1", "controller"
				if kind == "GatewayClass" {
					group, controllerField = "gateway.networking.k8s.io/v1", "controllerName"
				}
				labels := map[string]any{"e2e-class-run": prefix}
				if classSuffix == "selected" || classSuffix == "blocked" {
					labels[classProfileLabel] = name
				}
				class := &unstructured.Unstructured{Object: map[string]any{"apiVersion": group, "kind": kind, "metadata": map[string]any{"name": name + "-" + classSuffix, "labels": labels}, "spec": map[string]any{controllerField: "projectcapsule.dev/e2e"}}}
				Expect(k8sClient.Create(f.ctx, class)).To(Succeed())
				f.classes = append(f.classes, class)
			}
		}
		permission := &rules.NamespaceRuleBodyTenant{Permissions: rules.NamespaceRulePermissionBody{Bindings: []rbac.AdditionalRoleBindingsSpec{{ClusterRoleName: role.Name, Subjects: []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: "User", Name: name}}}}}}
		tenant := &capsulev1beta2.Tenant{Name: name, Labels: map[string]string{"env": "e2e"}, Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{Name: name, Kind: "User"}}, Rules: append([]*rules.NamespaceRuleBodyTenant{permission}, build(name)...)}}
		EventuallyCreation(func() error { return k8sClient.Create(f.ctx, tenant) }).Should(Succeed())
		f.tenants = append(f.tenants, tenant)
		TenantReady(tenant, metav1.ConditionTrue, defaultTimeoutInterval)
		f.actors = append(f.actors, impersonationClient(name, withDefaultGroups([]string{name})))
	}
	return f
}

func (f *ingressProfileFixture) namespace(tenant int, profile string) *corev1.Namespace {
	GinkgoHelper()
	ns := NewNamespace("", map[string]string{meta.TenantLabel: f.tenants[tenant].Name, classNamespaceProfile: profile})
	NamespaceCreation(ns, f.tenants[tenant].Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
	f.namespaces = append(f.namespaces, ns)
	NamespaceIsPartOfTenant(f.tenants[tenant], ns).Should(Succeed())
	Eventually(func() bool {
		obj := ingressProfileObject(rules.IngressTypeGateway, ns.Name, "rbac-readiness", "")
		return apierrors.IsNotFound(f.actors[tenant].Get(f.ctx, client.ObjectKeyFromObject(obj), obj))
	}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
	return ns
}

func ingressProfileObject(kind rules.IngressType, namespace, name, class string) *unstructured.Unstructured {
	group, field := "networking.k8s.io/v1", "ingressClassName"
	spec := map[string]any{"rules": []any{map[string]any{"host": "app.example.com", "http": map[string]any{"paths": []any{map[string]any{"path": "/", "pathType": "Prefix", "backend": map[string]any{"service": map[string]any{"name": "app", "port": map[string]any{"number": int64(80)}}}}}}}}}
	if kind == rules.IngressTypeGateway {
		group, field = "gateway.networking.k8s.io/v1", "gatewayClassName"
		spec = map[string]any{"listeners": []any{map[string]any{"name": "http", "port": int64(80), "protocol": "HTTP", "hostname": "app.example.com"}}}
	}
	if class != "" {
		spec[field] = class
	}
	return &unstructured.Unstructured{Object: map[string]any{"apiVersion": group, "kind": string(kind), "metadata": map[string]any{"namespace": namespace, "name": name, "labels": map[string]any{"env": "e2e"}}, "spec": spec}}
}

func (f *ingressProfileFixture) profile(ns *corev1.Namespace, hasClasses bool, mutation string) {
	GinkgoHelper()
	Eventually(func(g Gomega) {
		status := &capsulev1beta2.RuleStatus{}
		g.Expect(k8sClient.Get(f.ctx, client.ObjectKey{Namespace: ns.Name, Name: meta.NameForManagedRuleStatus()}, status)).To(Succeed())
		g.Expect(status.Status.ObservedGeneration).To(Equal(status.Generation))
		g.Expect(meta.IsStatusConditionTrue(status.Status.Conditions, meta.ReadyCondition)).To(BeTrue())
		found := false
		var defaults []string
		for _, body := range status.Status.Rules {
			if body.Enforce != nil && len(body.Enforce.Ingress.Classes) > 0 {
				found = true
			}
			for _, m := range body.Mutate {
				if m.Ingress.Class != "" {
					defaults = append(defaults, m.Ingress.Class)
				}
			}
		}
		g.Expect(found).To(Equal(hasClasses))
		if mutation != "" {
			g.Expect(defaults).To(ContainElement(mutation))
		} else {
			g.Expect(defaults).To(BeEmpty())
		}
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}

func ingressProfileClass(obj *unstructured.Unstructured) string {
	field := "ingressClassName"
	if obj.GetKind() == "Gateway" {
		field = "gatewayClassName"
	}
	value, _, err := unstructured.NestedString(obj.Object, "spec", field)
	Expect(err).NotTo(HaveOccurred())
	return value
}

func missingProfileClassReason(kind rules.IngressType) string {
	if kind == rules.IngressTypeGateway {
		// The Gateway schema requires this field before validating admission.
		return "spec.gatewayClassName: Required value"
	}
	return "class is required"
}

func (f *ingressProfileFixture) create(actor int, kind rules.IngressType, ns *corev1.Namespace, class string) *unstructured.Unstructured {
	GinkgoHelper()
	obj := ingressProfileObject(kind, ns.Name, "class-"+rand.String(7), class)
	EventuallyCreation(func() error { return f.actors[actor].Create(f.ctx, obj) }).Should(Succeed())
	Eventually(func() error { return k8sClient.Get(f.ctx, client.ObjectKeyFromObject(obj), obj) }, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	return obj
}

func (f *ingressProfileFixture) deny(actor int, obj *unstructured.Unstructured, reason string) {
	GinkgoHelper()
	err := f.actors[actor].Create(f.ctx, obj)
	Expect(err).To(HaveOccurred())
	Expect(err.Error()).To(ContainSubstring(reason))
	Expect(apierrors.IsNotFound(k8sClient.Get(f.ctx, client.ObjectKeyFromObject(obj), obj))).To(BeTrue(), "denied resource must not be persisted")
}

var _ = Describe("Ingress and Gateway class namespace profiles", Label("tenant", "rules", "ingress", "gatewayclass", "ingress-classes"), func() {
	It("composes names, selectors and hostnames across namespace profiles and tenants", func() {
		f := newIngressProfileFixture(func(name string) []*rules.NamespaceRuleBodyTenant {
			return []*rules.NamespaceRuleBodyTenant{
				ingressProfileRule("private", &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeAllow, Ingress: rules.NamespaceRuleEnforceIngressBody{Types: []rules.IngressType{rules.IngressTypeIngress, rules.IngressTypeGateway}, Hostnames: []apiruntime.ExpressionMatch{{Exact: []string{"app.example.com"}}}, Classes: []rules.IngressClassMatch{{Exact: []string{name + "-edge"}}, {Expression: "^" + regexp.QuoteMeta(name) + "-public$"}, {MatchLabels: map[string]string{classProfileLabel: name}}}}}}),
				ingressProfileRule("private", &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeDeny, Ingress: rules.NamespaceRuleEnforceIngressBody{Types: []rules.IngressType{rules.IngressTypeIngress, rules.IngressTypeGateway}, Classes: []rules.IngressClassMatch{{Exact: []string{name + "-blocked"}}}}}}),
			}
		})
		a, other, b := f.namespace(0, "private"), f.namespace(0, "other"), f.namespace(1, "private")
		f.profile(a, true, "")
		f.profile(other, false, "")
		f.profile(b, true, "")
		for _, kind := range []rules.IngressType{rules.IngressTypeIngress, rules.IngressTypeGateway} {
			var first *unstructured.Unstructured
			for _, suffix := range []string{"edge", "public", "selected"} {
				obj := f.create(0, kind, a, f.tenants[0].Name+"-"+suffix)
				Expect(ingressProfileClass(obj)).To(Equal(f.tenants[0].Name + "-" + suffix))
				if suffix == "edge" {
					first = obj
				}
			}
			f.create(1, kind, b, f.tenants[1].Name+"-selected")
			f.create(0, kind, other, f.tenants[1].Name+"-edge")
			f.deny(0, ingressProfileObject(kind, a.Name, "denied-"+rand.String(6), f.tenants[1].Name+"-edge"), "not allowed")
			f.deny(0, ingressProfileObject(kind, a.Name, "denied-"+rand.String(6), f.tenants[0].Name+"-blocked"), "denied")
			f.deny(0, ingressProfileObject(kind, a.Name, "denied-"+rand.String(6), ""), missingProfileClassReason(kind))
			Eventually(func() error {
				current := first.DeepCopy()
				if err := f.actors[0].Get(f.ctx, client.ObjectKeyFromObject(first), current); err != nil {
					return err
				}
				field := "ingressClassName"
				if kind == rules.IngressTypeGateway {
					field = "gatewayClassName"
				}
				if err := unstructured.SetNestedField(current.Object, f.tenants[0].Name+"-blocked", "spec", field); err != nil {
					return err
				}
				err := f.actors[0].Update(f.ctx, current)
				if apierrors.IsConflict(err) {
					return err
				}
				if err == nil {
					StopTrying("disallowed class update succeeded").Now()
				}
				if !strings.Contains(err.Error(), "denied") {
					return err
				}
				return nil
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			Expect(k8sClient.Get(f.ctx, client.ObjectKeyFromObject(first), first)).To(Succeed())
			Expect(ingressProfileClass(first)).To(Equal(f.tenants[0].Name + "-edge"))
		}
		By("checking both Ingress class representations")
		conflicting := ingressProfileObject(rules.IngressTypeIngress, a.Name, "conflicting-"+rand.String(6), f.tenants[0].Name+"-edge")
		conflicting.SetAnnotations(map[string]string{meta.IngressClassAnnotation: f.tenants[1].Name + "-edge"})
		f.deny(0, conflicting, "must match `ingressClassName`")
		annotationOnly := ingressProfileObject(rules.IngressTypeIngress, a.Name, "annotation-only-"+rand.String(6), "")
		annotationOnly.SetAnnotations(map[string]string{meta.IngressClassAnnotation: f.tenants[1].Name + "-edge"})
		f.deny(0, annotationOnly, "not allowed")
		By("observing class label changes without stale match results")
		for _, classKind := range []string{"IngressClass", "GatewayClass"} {
			Eventually(func() error {
				class := &unstructured.Unstructured{}
				class.SetKind(classKind)
				class.SetAPIVersion("networking.k8s.io/v1")
				if classKind == "GatewayClass" {
					class.SetAPIVersion("gateway.networking.k8s.io/v1")
				}
				if err := k8sClient.Get(f.ctx, client.ObjectKey{Name: f.tenants[0].Name + "-selected"}, class); err != nil {
					return err
				}
				labels := class.GetLabels()
				labels[classProfileLabel] = f.tenants[1].Name
				class.SetLabels(labels)
				return k8sClient.Update(f.ctx, class)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		for _, kind := range []rules.IngressType{rules.IngressTypeIngress, rules.IngressTypeGateway} {
			f.deny(0, ingressProfileObject(kind, a.Name, "reselected-"+rand.String(6), f.tenants[0].Name+"-selected"), "not allowed")
			f.create(1, kind, b, f.tenants[0].Name+"-selected")
		}
	})

	It("defaults and replaces classes in ordered conditional mutations", func() {
		f := newIngressProfileFixture(func(name string) []*rules.NamespaceRuleBodyTenant {
			base := &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{{Conditions: []rules.AdmissionCondition{{Expression: `!has(object.metadata.labels) || !('skip-class' in object.metadata.labels)`}}, Ingress: rules.IngressMutation{Types: []rules.IngressType{rules.IngressTypeIngress, rules.IngressTypeGateway}, Class: "{{ .tenant.metadata.name }}-edge"}}}, Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeAllow, Ingress: rules.NamespaceRuleEnforceIngressBody{Types: []rules.IngressType{rules.IngressTypeIngress, rules.IngressTypeGateway}, Classes: []rules.IngressClassMatch{{Exact: []string{name + "-edge", name + "-forced", name + "-public"}}}}}}
			merge := ingressProfileRule("merge", base)
			replace := base.DeepCopy()
			replace.Mutate = append(replace.Mutate, rules.NamespaceRuleMutation{Action: rules.MutationActionReplace, Ingress: rules.IngressMutation{Types: []rules.IngressType{rules.IngressTypeIngress, rules.IngressTypeGateway}, Class: name + "-forced"}})
			return []*rules.NamespaceRuleBodyTenant{merge, ingressProfileRule("replace", replace), ingressProfileRule("merge", &rules.NamespaceRuleBodyNamespace{Audience: []rules.Audience{{Kind: rules.AudienceKindUser, Name: name + "-not-owner"}}, Mutate: []rules.NamespaceRuleMutation{{Action: rules.MutationActionReplace, Ingress: rules.IngressMutation{Types: []rules.IngressType{rules.IngressTypeIngress, rules.IngressTypeGateway}, Class: name + "-blocked"}}}})}
		})
		a, replace, other, b := f.namespace(0, "merge"), f.namespace(0, "replace"), f.namespace(0, "other"), f.namespace(1, "merge")
		f.profile(a, true, f.tenants[0].Name+"-edge")
		f.profile(replace, true, f.tenants[0].Name+"-forced")
		f.profile(other, false, "")
		f.profile(b, true, f.tenants[1].Name+"-edge")
		for _, kind := range []rules.IngressType{rules.IngressTypeIngress, rules.IngressTypeGateway} {
			Expect(ingressProfileClass(f.create(0, kind, a, ""))).To(Equal(f.tenants[0].Name + "-edge"))
			Expect(ingressProfileClass(f.create(0, kind, a, f.tenants[0].Name+"-public"))).To(Equal(f.tenants[0].Name + "-public"))
			Expect(ingressProfileClass(f.create(0, kind, replace, f.tenants[0].Name+"-public"))).To(Equal(f.tenants[0].Name + "-forced"))
			Expect(ingressProfileClass(f.create(1, kind, b, ""))).To(Equal(f.tenants[1].Name + "-edge"))
			Expect(ingressProfileClass(f.create(0, kind, other, f.tenants[1].Name+"-public"))).To(Equal(f.tenants[1].Name + "-public"))
			skipped := ingressProfileObject(kind, a.Name, "skipped-"+rand.String(6), "")
			skipped.SetLabels(map[string]string{"env": "e2e", "skip-class": "true"})
			f.deny(0, skipped, missingProfileClassReason(kind))
		}
		for _, ns := range []*corev1.Namespace{a, replace} {
			obj := ingressProfileObject(rules.IngressTypeIngress, ns.Name, "annotation-"+rand.String(6), "")
			obj.SetAnnotations(map[string]string{meta.IngressClassAnnotation: f.tenants[0].Name + "-public", "unrelated": "retained"})
			Expect(f.actors[0].Create(f.ctx, obj)).To(Succeed())
			Expect(k8sClient.Get(f.ctx, client.ObjectKeyFromObject(obj), obj)).To(Succeed())
			if ns == a {
				Expect(ingressProfileClass(obj)).To(BeEmpty())
				Expect(obj.GetAnnotations()[meta.IngressClassAnnotation]).To(Equal(f.tenants[0].Name + "-public"))
			} else {
				Expect(ingressProfileClass(obj)).To(Equal(f.tenants[0].Name + "-forced"))
				Expect(obj.GetAnnotations()[meta.IngressClassAnnotation]).To(Equal(f.tenants[0].Name + "-forced"))
			}
			Expect(obj.GetAnnotations()["unrelated"]).To(Equal("retained"))
		}
	})

	It("updates namespace profiles and preserves legacy restrictions and tenant isolation", func() {
		f := newIngressProfileFixture(func(name string) []*rules.NamespaceRuleBodyTenant {
			return []*rules.NamespaceRuleBodyTenant{ingressProfileRule("private", &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{{Ingress: rules.IngressMutation{Types: []rules.IngressType{rules.IngressTypeIngress, rules.IngressTypeGateway}, Class: name + "-edge"}}}, Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeAllow, Ingress: rules.NamespaceRuleEnforceIngressBody{Types: []rules.IngressType{rules.IngressTypeIngress, rules.IngressTypeGateway}, Classes: []rules.IngressClassMatch{{Exact: []string{name + "-edge", name + "-public", name + "-forced"}}}}}})}
		})
		Eventually(func() error {
			latest := &capsulev1beta2.Tenant{}
			if err := k8sClient.Get(f.ctx, client.ObjectKeyFromObject(f.tenants[0]), latest); err != nil {
				return err
			}
			latest.Spec.IngressOptions.AllowedClasses = &api.DefaultAllowedListSpec{Exact: []string{latest.Name + "-edge", latest.Name + "-forced"}}
			latest.Spec.GatewayOptions.AllowedClasses = &api.DefaultAllowedListSpec{Exact: []string{latest.Name + "-edge", latest.Name + "-forced"}}
			return k8sClient.Update(f.ctx, latest)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		a, other, b := f.namespace(0, "private"), f.namespace(0, "other"), f.namespace(1, "private")
		f.profile(a, true, f.tenants[0].Name+"-edge")
		f.profile(other, false, "")
		f.profile(b, true, f.tenants[1].Name+"-edge")
		var created []*unstructured.Unstructured
		for _, kind := range []rules.IngressType{rules.IngressTypeIngress, rules.IngressTypeGateway} {
			obj := f.create(0, kind, a, "")
			Expect(ingressProfileClass(obj)).To(Equal(f.tenants[0].Name + "-edge"))
			created = append(created, obj)
			f.deny(0, ingressProfileObject(kind, a.Name, "legacy-"+rand.String(6), f.tenants[0].Name+"-public"), "forbidden")
			cross := ingressProfileObject(kind, b.Name, "cross-"+rand.String(6), f.tenants[1].Name+"-edge")
			Expect(apierrors.IsForbidden(f.actors[0].Create(f.ctx, cross))).To(BeTrue())
			Expect(apierrors.IsNotFound(k8sClient.Get(f.ctx, client.ObjectKeyFromObject(cross), cross))).To(BeTrue())
			f.create(1, kind, b, f.tenants[1].Name+"-edge")
		}
		By("applying policy changes only to new admissions")
		Eventually(func() error {
			latest := &capsulev1beta2.Tenant{}
			if err := k8sClient.Get(f.ctx, client.ObjectKeyFromObject(f.tenants[0]), latest); err != nil {
				return err
			}
			latest.Spec.Rules[1].Mutate[0].Action = rules.MutationActionReplace
			latest.Spec.Rules[1].Mutate[0].Ingress.Class = latest.Name + "-forced"
			return k8sClient.Update(f.ctx, latest)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		f.profile(a, true, f.tenants[0].Name+"-forced")
		f.profile(b, true, f.tenants[1].Name+"-edge")
		for _, obj := range created {
			Expect(k8sClient.Get(f.ctx, client.ObjectKeyFromObject(obj), obj)).To(Succeed())
			Expect(ingressProfileClass(obj)).To(Equal(f.tenants[0].Name + "-edge"))
			Eventually(func() error {
				current := obj.DeepCopy()
				if err := f.actors[0].Get(f.ctx, client.ObjectKeyFromObject(obj), current); err != nil {
					return err
				}
				labels := current.GetLabels()
				labels["updated"] = "yes"
				current.SetLabels(labels)
				return f.actors[0].Update(f.ctx, current)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(f.ctx, client.ObjectKeyFromObject(obj), obj)).To(Succeed())
				g.Expect(ingressProfileClass(obj)).To(Equal(f.tenants[0].Name + "-forced"))
				g.Expect(obj.GetLabels()).To(HaveKeyWithValue("updated", "yes"))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		By("retiring tenant-wide class status")
		latest := &capsulev1beta2.Tenant{}
		Expect(k8sClient.Get(f.ctx, client.ObjectKeyFromObject(f.tenants[0]), latest)).To(Succeed())
		Expect(latest.Status.Classes.GatewayClasses).To(BeEmpty())
		encoded, err := json.Marshal(latest.Status.Classes)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(encoded)).NotTo(ContainSubstring(`"gateway"`))
		By("changing the namespace selector and reconciling the effective profile")
		Eventually(func() error {
			current := &corev1.Namespace{}
			if err := k8sClient.Get(f.ctx, client.ObjectKeyFromObject(a), current); err != nil {
				return err
			}
			current.Labels[classNamespaceProfile] = "other"
			return k8sClient.Update(f.ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		f.profile(a, false, "")
		f.create(0, rules.IngressTypeGateway, a, f.tenants[0].Name+"-edge")
		By("rejecting an invalid rule update without replacing the last valid policy")
		Eventually(func() error {
			current := &capsulev1beta2.Tenant{}
			if err := k8sClient.Get(f.ctx, client.ObjectKeyFromObject(f.tenants[0]), current); err != nil {
				return err
			}
			current.Spec.Rules[1].Enforce.Ingress.Classes = []rules.IngressClassMatch{{Expression: "("}}
			err := k8sClient.Update(f.ctx, current)
			if apierrors.IsConflict(err) {
				return err
			}
			if err == nil {
				StopTrying("invalid policy update succeeded").Now()
			}
			if !strings.Contains(err.Error(), "ingress.classes[0].exp") {
				return err
			}
			return nil
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Expect(k8sClient.Get(f.ctx, client.ObjectKeyFromObject(f.tenants[0]), latest)).To(Succeed())
		Expect(latest.Spec.Rules[1].Enforce.Ingress.Classes[0].Exact).To(ContainElement(f.tenants[0].Name + "-edge"))
	})
	It("audits class rules while suppressing dry-run effects", func() {
		f := newIngressProfileFixture(func(name string) []*rules.NamespaceRuleBodyTenant {
			return []*rules.NamespaceRuleBodyTenant{
				ingressProfileRule("audit", &rules.NamespaceRuleBodyNamespace{Audience: []rules.Audience{{Kind: rules.AudienceKindUser, Name: name + "-not-owner"}}, Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeDeny, Ingress: rules.NamespaceRuleEnforceIngressBody{Types: []rules.IngressType{rules.IngressTypeIngress, rules.IngressTypeGateway}, Classes: []rules.IngressClassMatch{{Exact: []string{name + "-edge"}}}}}}),
				ingressProfileRule("audit", &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeAudit, Ingress: rules.NamespaceRuleEnforceIngressBody{Types: []rules.IngressType{rules.IngressTypeIngress, rules.IngressTypeGateway}, Classes: []rules.IngressClassMatch{{Exact: []string{name + "-edge"}}}}}}),
				ingressProfileRule("deny", &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeDeny, Ingress: rules.NamespaceRuleEnforceIngressBody{Types: []rules.IngressType{rules.IngressTypeIngress, rules.IngressTypeGateway}, Classes: []rules.IngressClassMatch{{Exact: []string{name + "-blocked"}}}}}}),
			}
		})
		a, b, denied := f.namespace(0, "audit"), f.namespace(1, "audit"), f.namespace(0, "deny")
		f.profile(a, true, "")
		f.profile(b, true, "")
		f.profile(denied, true, "")
		for _, kind := range []rules.IngressType{rules.IngressTypeIngress, rules.IngressTypeGateway} {
			dry := ingressProfileObject(kind, a.Name, "dry-"+rand.String(6), f.tenants[0].Name+"-edge")
			Expect(f.actors[0].Create(f.ctx, dry, client.DryRunAll)).To(Succeed())
			Expect(apierrors.IsNotFound(k8sClient.Get(f.ctx, client.ObjectKeyFromObject(dry), dry))).To(BeTrue())
			live := f.create(0, kind, a, f.tenants[0].Name+"-edge")
			Eventually(func(g Gomega) {
				list := &eventsv1.EventList{}
				g.Expect(k8sClient.List(f.ctx, list, client.InNamespace(a.Name))).To(Succeed())
				found := false
				for _, event := range list.Items {
					if event.Reason == events.ReasonNamespaceRuleAudit && event.Regarding.Name == live.GetName() {
						found = true
					}
					g.Expect(event.Regarding.Name).NotTo(Equal(dry.GetName()), "dry-run class decisions must not emit Events")
				}
				g.Expect(found).To(BeTrue())
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			f.create(1, kind, b, f.tenants[1].Name+"-blocked")
			f.deny(0, ingressProfileObject(kind, denied.Name, "denied-"+rand.String(6), f.tenants[0].Name+"-blocked"), "denied")
		}
	})

})
