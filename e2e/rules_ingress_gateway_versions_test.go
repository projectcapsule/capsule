// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule/pkg/api/rules"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
)

func gatewayVersionObject(version, namespace, name, class string) *unstructured.Unstructured {
	obj := ingressProfileObject(rules.IngressTypeGateway, namespace, name, class)
	obj.SetAPIVersion("gateway.networking.k8s.io/" + version)
	return obj
}

func (f *ingressProfileFixture) createGatewayVersion(actor int, version, namespace, class string) *unstructured.Unstructured {
	GinkgoHelper()
	obj := gatewayVersionObject(version, namespace, "gateway-"+rand.String(7), class)
	EventuallyCreation(func() error { return f.actors[actor].Create(f.ctx, obj) }).Should(Succeed())
	// Gateway API v1 is canonical. Verify that a compatibility-version write
	// persisted the same object, class and UID when read through v1.
	canonical := gatewayVersionObject("v1", namespace, obj.GetName(), "")
	Eventually(func() error {
		return k8sClient.Get(f.ctx, client.ObjectKeyFromObject(canonical), canonical)
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	Expect(canonical.GetAPIVersion()).To(Equal("gateway.networking.k8s.io/v1"))
	Expect(canonical.GetUID()).To(Equal(obj.GetUID()))
	return canonical
}

var _ = Describe("Gateway API v1 profiles with served-version compatibility", Label("tenant", "rules", "ingress", "gatewayclass", "ingress-classes", "gateway-version-compatibility"), func() {
	It("enforces v1 class and hostname rules on served Gateway versions", func() {
		f := newIngressProfileFixture(func(name string) []*rules.NamespaceRuleBodyTenant {
			return []*rules.NamespaceRuleBodyTenant{ingressProfileRule("private", &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{
				Action: rules.ActionTypeAllow,
				Ingress: rules.NamespaceRuleEnforceIngressBody{
					Types: []rules.IngressType{rules.IngressTypeGateway}, Hostnames: []apiruntime.ExpressionMatch{{Exact: []string{"app.example.com"}}},
					Classes: []rules.IngressClassMatch{{Exact: []string{name + "-edge"}}, {MatchLabels: map[string]string{classProfileLabel: name}}},
				},
			}})}
		})
		a, other, b := f.namespace(0, "private"), f.namespace(0, "other"), f.namespace(1, "private")
		f.profile(a, true, "")
		f.profile(other, false, "")
		f.profile(b, true, "")
		for _, version := range []string{"v1", "v1beta1"} {
			obj := f.createGatewayVersion(0, version, a.Name, f.tenants[0].Name+"-selected")
			Expect(ingressProfileClass(obj)).To(Equal(f.tenants[0].Name + "-selected"))
			f.createGatewayVersion(1, version, b.Name, f.tenants[1].Name+"-selected")
			f.createGatewayVersion(0, version, other.Name, f.tenants[1].Name+"-edge")
			f.deny(0, gatewayVersionObject(version, a.Name, "forbidden-"+rand.String(7), f.tenants[1].Name+"-selected"), "not allowed")
			f.deny(1, gatewayVersionObject(version, b.Name, "forbidden-"+rand.String(7), f.tenants[0].Name+"-selected"), "not allowed")
			f.deny(0, gatewayVersionObject(version, b.Name, "cross-tenant-"+rand.String(7), f.tenants[1].Name+"-edge"), "cannot create resource")
			badHost := gatewayVersionObject(version, a.Name, "hostname-"+rand.String(7), f.tenants[0].Name+"-edge")
			Expect(unstructured.SetNestedSlice(badHost.Object, []any{map[string]any{"name": "http", "protocol": "HTTP", "port": int64(80), "hostname": "outside.example.net"}}, "spec", "listeners")).To(Succeed())
			f.deny(0, badHost, "not allowed")
			Eventually(func() error {
				current := gatewayVersionObject(version, a.Name, obj.GetName(), "")
				if err := f.actors[0].Get(f.ctx, client.ObjectKeyFromObject(current), current); err != nil {
					return err
				}
				if err := unstructured.SetNestedField(current.Object, f.tenants[1].Name+"-edge", "spec", "gatewayClassName"); err != nil {
					return err
				}
				err := f.actors[0].Update(f.ctx, current)
				if apierrors.IsConflict(err) {
					return err
				}
				if err == nil {
					StopTrying("disallowed class update succeeded through " + version).Now()
				}
				if !strings.Contains(err.Error(), "not allowed") {
					return err
				}
				return nil
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			Expect(k8sClient.Get(f.ctx, client.ObjectKeyFromObject(obj), obj)).To(Succeed())
			Expect(ingressProfileClass(obj)).To(Equal(f.tenants[0].Name + "-selected"))
		}
	})

	It("applies v1 class defaults and replacements to served Gateway versions", func() {
		f := newIngressProfileFixture(func(name string) []*rules.NamespaceRuleBodyTenant {
			merge := &rules.NamespaceRuleBodyNamespace{
				Mutate: []rules.NamespaceRuleMutation{{Ingress: rules.IngressMutation{Types: []rules.IngressType{rules.IngressTypeGateway}, Class: "{{ .tenant.metadata.name }}-edge"}}},
				Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeAllow, Ingress: rules.NamespaceRuleEnforceIngressBody{
					Types: []rules.IngressType{rules.IngressTypeGateway}, Classes: []rules.IngressClassMatch{{Exact: []string{name + "-edge", name + "-forced", name + "-public"}}},
				}},
			}
			replace := merge.DeepCopy()
			replace.Mutate[0].Action = rules.MutationActionReplace
			replace.Mutate[0].Ingress.Class = name + "-forced"
			return []*rules.NamespaceRuleBodyTenant{ingressProfileRule("merge", merge), ingressProfileRule("replace", replace)}
		})
		a, replace, other, b := f.namespace(0, "merge"), f.namespace(0, "replace"), f.namespace(0, "other"), f.namespace(1, "merge")
		f.profile(a, true, f.tenants[0].Name+"-edge")
		f.profile(replace, true, f.tenants[0].Name+"-forced")
		f.profile(other, false, "")
		f.profile(b, true, f.tenants[1].Name+"-edge")
		for _, version := range []string{"v1", "v1beta1"} {
			Expect(ingressProfileClass(f.createGatewayVersion(0, version, a.Name, ""))).To(Equal(f.tenants[0].Name + "-edge"))
			Expect(ingressProfileClass(f.createGatewayVersion(0, version, a.Name, f.tenants[0].Name+"-public"))).To(Equal(f.tenants[0].Name + "-public"))
			Expect(ingressProfileClass(f.createGatewayVersion(1, version, b.Name, ""))).To(Equal(f.tenants[1].Name + "-edge"))
			Expect(ingressProfileClass(f.createGatewayVersion(0, version, other.Name, f.tenants[1].Name+"-public"))).To(Equal(f.tenants[1].Name + "-public"))
			f.deny(0, gatewayVersionObject(version, a.Name, "forbidden-"+rand.String(7), f.tenants[1].Name+"-public"), "not allowed")
			for _, profile := range []struct{ namespace, want string }{{a.Name, f.tenants[0].Name + "-public"}, {replace.Name, f.tenants[0].Name + "-forced"}} {
				obj := f.createGatewayVersion(0, version, profile.namespace, f.tenants[0].Name+"-public")
				Expect(ingressProfileClass(obj)).To(Equal(profile.want))
				Eventually(func() error {
					current := gatewayVersionObject(version, profile.namespace, obj.GetName(), "")
					if err := f.actors[0].Get(f.ctx, client.ObjectKeyFromObject(current), current); err != nil {
						return err
					}
					if err := unstructured.SetNestedField(current.Object, f.tenants[0].Name+"-public", "spec", "gatewayClassName"); err != nil {
						return err
					}
					labels := current.GetLabels()
					labels["updated-through"] = version
					current.SetLabels(labels)
					return f.actors[0].Update(f.ctx, current)
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(f.ctx, client.ObjectKeyFromObject(obj), obj)).To(Succeed())
					g.Expect(ingressProfileClass(obj)).To(Equal(profile.want))
					g.Expect(obj.GetLabels()).To(HaveKeyWithValue("updated-through", version))
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}
		}
	})
})
