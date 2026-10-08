// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

var _ = Describe("NetworkPolicy CIDR namespace profiles", Label("tenant", "rules", "enforce", "networkpolicies"), func() {
	for _, direction := range []networkingv1.PolicyType{networkingv1.PolicyTypeEgress, networkingv1.PolicyTypeIngress} {
		name := strings.ToLower(string(direction))
		peerPath := "spec.egress[0].to"
		if direction == networkingv1.PolicyTypeIngress {
			peerPath = "spec.ingress[0].from"
		}
		It("validates "+name+" grants, exceptions, rule composition and changes without crossing tenant boundaries", Label(name+"-cidrs"), func() {
			ctx := context.Background()
			prefix := "e2e-cidrs-" + rand.String(8)
			cidrRules := func(p *rules.NamespaceRuleEnforceNetworkPoliciesBody) *rules.NetworkPolicyCIDRRule {
				if direction == networkingv1.PolicyTypeIngress {
					return p.Ingress
				}
				return p.Egress
			}
			peers := func(p *networkingv1.NetworkPolicy) *[]networkingv1.NetworkPolicyPeer {
				if direction == networkingv1.PolicyTypeIngress {
					return &p.Spec.Ingress[0].From
				}
				return &p.Spec.Egress[0].To
			}
			ipBlock := func(p *networkingv1.NetworkPolicy) *networkingv1.IPBlock { return (*peers(p))[0].IPBlock }
			profileRule := func(profile string, action rules.ActionType, cidrs ...string) *rules.NamespaceRuleBodyTenant {
				body := &rules.NamespaceRuleBodyTenant{
					NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"network-profile": profile}},
					NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{
						Action: action, Network: rules.NamespaceRuleEnforceNetworkBody{Policies: rules.NamespaceRuleEnforceNetworkPoliciesBody{Egress: &rules.NetworkPolicyCIDRRule{CIDRs: cidrs}}},
					}},
				}
				if direction == networkingv1.PolicyTypeIngress {
					body.Enforce.Network.Policies.Ingress, body.Enforce.Network.Policies.Egress = body.Enforce.Network.Policies.Egress, nil
				}
				return body
			}
			a := &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: prefix + "-a", Labels: map[string]string{"env": "e2e"}}, Spec: capsulev1beta2.TenantSpec{
				Owners: rbac.OwnerListSpec{{Kind: "User", Name: prefix + "-a"}},
				Rules: []*rules.NamespaceRuleBodyTenant{
					profileRule("restricted", rules.ActionTypeDeny, "10.20.0.0/16", "fd00:1234::/48"),
					profileRule("allowlist", rules.ActionTypeAllow, "192.0.2.0/25", "192.0.2.128/25"),
					profileRule("audit", rules.ActionTypeAudit, "10.0.0.0/8", "10.20.0.0/16", "10.20.0.0/17"),
					profileRule("conditional", rules.ActionTypeDeny, "10.20.0.0/16"),
					profileRule("restricted", rules.ActionTypeDeny, "192.0.2.0/24"),
				},
			}}
			a.Spec.Rules[3].Enforce.Conditions = []rules.AdmissionCondition{{Expression: `has(object.metadata.labels) && 'blocked' in object.metadata.labels`}}
			a.Spec.Rules[4].Audience = []rules.Audience{{Kind: rules.AudienceKindUser, Name: "different-owner"}}
			mixed := profileRule("allowlist", rules.ActionTypeDeny, "203.0.113.0/24")
			mixed.Enforce.Network.Policies.Ingress, mixed.Enforce.Network.Policies.Egress = mixed.Enforce.Network.Policies.Egress, mixed.Enforce.Network.Policies.Ingress
			a.Spec.Rules = append(a.Spec.Rules, mixed)

			b := &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: prefix + "-b", Labels: map[string]string{"env": "e2e"}}, Spec: capsulev1beta2.TenantSpec{
				Owners: rbac.OwnerListSpec{{Kind: "User", Name: prefix + "-b"}}, Rules: []*rules.NamespaceRuleBodyTenant{profileRule("restricted", rules.ActionTypeDeny, "192.0.2.0/24")},
			}}
			for _, tnt := range []*capsulev1beta2.Tenant{a, b} {
				Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
				DeferCleanup(EventuallyDeletion, tnt)
				TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
			}
			By("rejecting invalid CIDR rules without changing the tenant profile")
			Eventually(func(g Gomega) {
				current := &capsulev1beta2.Tenant{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(a), current)).To(Succeed())
				cidrRules(&current.Spec.Rules[0].Enforce.Network.Policies).CIDRs = []string{"::ffff:192.0.2.1/128"}
				err := k8sClient.Update(ctx, current)
				if err == nil {
					Fail("invalid CIDR rule update succeeded")
				}
				g.Expect(err).To(MatchError(And(ContainSubstring("network.policies."+name+".cidrs[0]"), ContainSubstring("IPv4-mapped IPv6 is unsupported"))))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			currentTenant := &capsulev1beta2.Tenant{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(a), currentTenant)).To(Succeed())
			Expect(cidrRules(&currentTenant.Spec.Rules[0].Enforce.Network.Policies).CIDRs).To(Equal([]string{"10.20.0.0/16", "fd00:1234::/48"}))

			ownerA, ownerB := impersonationClient(a.Name, withDefaultGroups(nil)), impersonationClient(b.Name, withDefaultGroups(nil))
			waitRules := func(ns *corev1.Namespace, want ...string) {
				Eventually(func(g Gomega) {
					rs := &capsulev1beta2.RuleStatus{}
					g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: meta.NameForManagedRuleStatus(), Namespace: ns.Name}, rs)).To(Succeed())
					g.Expect(rs.Status.ObservedGeneration).To(Equal(rs.Generation))
					ready := rs.Status.Conditions.GetConditionByType(meta.ReadyCondition)
					g.Expect(ready).NotTo(BeNil())
					g.Expect(ready.Status).To(Equal(metav1.ConditionTrue))
					var got []string
					for _, body := range rs.Status.Rules {
						if body.Enforce != nil && cidrRules(&body.Enforce.Network.Policies) != nil {
							got = append(got, cidrRules(&body.Enforce.Network.Policies).CIDRs...)
						}
					}
					g.Expect(got).To(Equal(want))
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}
			newNS := func(tnt *capsulev1beta2.Tenant, profile string, count uint, want ...string) *corev1.Namespace {
				ns := NewNamespace("", map[string]string{meta.TenantLabel: tnt.Name, "network-profile": profile})
				NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
				TenantNamespaceReady(tnt, ns, count)
				waitRules(ns, want...)
				return ns
			}
			restricted := newNS(a, "restricted", 1, "10.20.0.0/16", "fd00:1234::/48", "192.0.2.0/24")
			other := newNS(a, "other", 2)
			allowlist := newNS(a, "allowlist", 3, "192.0.2.0/25", "192.0.2.128/25")
			audited := newNS(a, "audit", 4, "10.0.0.0/8", "10.20.0.0/16", "10.20.0.0/17")
			conditional := newNS(a, "conditional", 5, "10.20.0.0/16")
			isolated := newNS(b, "restricted", 1, "192.0.2.0/24")
			policy := func(ns *corev1.Namespace, name, cidr string, except ...string) *networkingv1.NetworkPolicy {
				obj := &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns.Name, Labels: map[string]string{"env": "e2e"}}, Spec: networkingv1.NetworkPolicySpec{
					PodSelector: metav1.LabelSelector{}, PolicyTypes: []networkingv1.PolicyType{direction},
					Egress: []networkingv1.NetworkPolicyEgressRule{{To: []networkingv1.NetworkPolicyPeer{{IPBlock: &networkingv1.IPBlock{CIDR: cidr, Except: except}}}}},
				}}
				if direction == networkingv1.PolicyTypeIngress {
					obj.Spec.Ingress = []networkingv1.NetworkPolicyIngressRule{{From: obj.Spec.Egress[0].To}}
					obj.Spec.Egress = nil
				}
				return obj
			}
			create := func(actor client.Client, obj *networkingv1.NetworkPolicy) {
				Expect(actor.Create(ctx, obj)).To(Succeed())
				Eventually(func(g Gomega) {
					stored := &networkingv1.NetworkPolicy{}
					g.Expect(actor.Get(ctx, client.ObjectKeyFromObject(obj), stored)).To(Succeed())
					g.Expect(stored.Spec).To(Equal(obj.Spec))
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}
			deny := func(actor client.Client, obj *networkingv1.NetworkPolicy) {
				Expect(actor.Create(ctx, obj)).To(MatchError(And(ContainSubstring("networkPolicy "+name+" CIDR"), ContainSubstring(peerPath))))
				Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), &networkingv1.NetworkPolicy{}))).To(BeTrue())
			}
			By("rejecting overlapping grants, including supernets, both address families and unrestricted peers")
			deny(ownerA, policy(restricted, "subnet", "10.20.4.0/24"))
			deny(ownerA, policy(restricted, "supernet", "10.0.0.0/8"))
			deny(ownerA, policy(restricted, "partial-except", "10.0.0.0/8", "10.20.0.0/17"))
			deny(ownerA, policy(restricted, "ipv6", "::/0"))
			unrestricted := policy(restricted, "unrestricted", "0.0.0.0/0")
			*peers(unrestricted) = nil
			deny(ownerA, unrestricted)
			create(ownerA, policy(restricted, "excluded", "10.0.0.0/8", "10.20.0.0/17", "10.20.128.0/17"))
			create(ownerA, policy(restricted, "ipv6-excluded", "::/0", "fd00:1234::/48"))
			allowed := policy(restricted, "allowed", "192.0.2.0/24")
			create(ownerA, allowed)
			noGrants := policy(restricted, "no-grants", "0.0.0.0/0")
			noGrants.Spec.Egress, noGrants.Spec.Ingress = nil, nil
			create(ownerA, noGrants)
			selectedPeers := policy(restricted, "selectors", "0.0.0.0/0")
			*peers(selectedPeers) = []networkingv1.NetworkPolicyPeer{{PodSelector: &metav1.LabelSelector{}}}
			create(ownerA, selectedPeers)

			By("keeping namespace profiles, other tenants and existing authorization separate")
			existing := policy(other, "existing", "10.20.0.0/16")
			create(ownerA, existing)
			create(ownerB, policy(isolated, "tenant-b", "10.20.0.0/16"))
			deny(ownerB, policy(isolated, "denied-b", "192.0.2.0/24"))
			crossTenant := policy(isolated, "cross-tenant", "10.20.0.0/16")
			Expect(ownerA.Create(ctx, crossTenant)).To(MatchError(ContainSubstring("forbidden")))
			Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(crossTenant), &networkingv1.NetworkPolicy{}))).To(BeTrue())
			create(ownerA, policy(allowlist, "allowed-union", "192.0.2.0/24"))
			deny(ownerA, policy(allowlist, "allow-miss", "0.0.0.0/0"))

			By("keeping ingress and egress decisions independent on the same policy")
			both := policy(allowlist, "both-directions", "192.0.2.0/24")
			both.Spec.PolicyTypes = []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress}
			oppositePeer := []networkingv1.NetworkPolicyPeer{{IPBlock: &networkingv1.IPBlock{CIDR: "203.0.113.0/24"}}}
			opposite := "ingress"
			if direction == networkingv1.PolicyTypeIngress {
				opposite = "egress"
				both.Spec.Egress = []networkingv1.NetworkPolicyEgressRule{{To: oppositePeer}}
			} else {
				both.Spec.Ingress = []networkingv1.NetworkPolicyIngressRule{{From: oppositePeer}}
			}
			Expect(ownerA.Create(ctx, both)).To(MatchError(ContainSubstring("networkPolicy " + opposite + " CIDR")))
			Expect(apierrors.IsNotFound(ownerA.Get(ctx, client.ObjectKeyFromObject(both), &networkingv1.NetworkPolicy{}))).To(BeTrue())
			oppositePeer[0].IPBlock.CIDR = "198.51.100.0/24"
			create(ownerA, both)

			By("exercising dry-run admission and recording API round-trip latency")
			for _, sample := range []struct {
				name   string
				ns     *corev1.Namespace
				cidr   string
				denied bool
			}{{"unrestricted-profile", other, "10.20.0.0/16", false}, {"cidr-allow", restricted, "192.0.2.0/24", false}, {"cidr-deny", restricted, "10.20.0.0/16", true}} {
				var durations []time.Duration
				for range 10 {
					obj := policy(sample.ns, "latency-dry-run", sample.cidr)
					start := time.Now()
					err := ownerA.Create(ctx, obj, client.DryRunAll)
					durations = append(durations, time.Since(start))
					if sample.denied {
						Expect(err).To(MatchError(ContainSubstring("networkPolicy " + name + " CIDR")))
					} else {
						Expect(err).NotTo(HaveOccurred())
					}
				}
				slices.Sort(durations)
				fmt.Fprintf(GinkgoWriter, "NetworkPolicy %s API round trip %s: n=10 median=%s max=%s (includes API server/network, not isolated webhook time)\n", name, sample.name, durations[5], durations[9])
				Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: sample.ns.Name, Name: "latency-dry-run"}, &networkingv1.NetworkPolicy{}))).To(BeTrue())
			}

			By("keeping audit and deny dry runs free of Events on creation and updates")
			for _, sample := range []struct {
				ns     *corev1.Namespace
				denied bool
			}{{audited, false}, {restricted, true}} {
				err := ownerA.Create(ctx, policy(sample.ns, "dry-create", "10.20.0.0/16"), client.DryRunAll)
				if sample.denied {
					Expect(err).To(MatchError(ContainSubstring("networkPolicy " + name + " CIDR")))
				} else {
					Expect(err).NotTo(HaveOccurred())
				}
				Expect(apierrors.IsNotFound(ownerA.Get(ctx, client.ObjectKey{Namespace: sample.ns.Name, Name: "dry-create"}, &networkingv1.NetworkPolicy{}))).To(BeTrue())
				original := policy(sample.ns, "dry-update", "192.0.2.0/24")
				create(ownerA, original)
				Eventually(func(g Gomega) {
					current := &networkingv1.NetworkPolicy{}
					g.Expect(ownerA.Get(ctx, client.ObjectKeyFromObject(original), current)).To(Succeed())
					ipBlock(current).CIDR = "10.20.0.0/16"
					err := ownerA.Update(ctx, current, client.DryRunAll)
					if sample.denied {
						if err == nil {
							Fail("disallowed dry-run update succeeded")
						}
						g.Expect(err).To(MatchError(ContainSubstring("networkPolicy " + name + " CIDR")))
					} else {
						g.Expect(err).NotTo(HaveOccurred())
					}
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
				Eventually(func(g Gomega) {
					current := &networkingv1.NetworkPolicy{}
					g.Expect(ownerA.Get(ctx, client.ObjectKeyFromObject(original), current)).To(Succeed())
					g.Expect(current.Spec).To(Equal(original.Spec))
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}
			Consistently(func(g Gomega) {
				for _, ns := range []*corev1.Namespace{audited, restricted} {
					list := &eventsv1.EventList{}
					g.Expect(k8sClient.List(ctx, list, client.InNamespace(ns.Name))).To(Succeed())
					for _, event := range list.Items {
						if event.Regarding.Kind == "NetworkPolicy" {
							g.Expect(event.Regarding.Name).NotTo(BeElementOf("dry-create", "dry-update", "latency-dry-run"))
						}
					}
				}
			}, 3*defaultPollInterval, defaultPollInterval).Should(Succeed())

			By("subtracting large exception lists while preserving protected ranges")
			large := policy(restricted, "large-exceptions", "10.0.0.0/8")
			for i := range 8192 {
				n := 2 * i
				ipBlock(large).Except = append(ipBlock(large).Except, fmt.Sprintf("10.%d.%d.%d/32", n>>16, (n>>8)&255, n&255))
			}
			start := time.Now()
			deny(ownerA, large.DeepCopy())
			fmt.Fprintf(GinkgoWriter, "NetworkPolicy with 8192 exceptions denied in %s (API round trip)\n", time.Since(start))
			ipBlock(large).Except = append(ipBlock(large).Except, "10.20.0.0/16")
			start = time.Now()
			create(ownerA, large)
			fmt.Fprintf(GinkgoWriter, "NetworkPolicy with 8193 exceptions persisted in %s (API round trip plus verification)\n", time.Since(start))

			By("rejecting updates without changing the persisted policy")
			Eventually(func(g Gomega) {
				current := &networkingv1.NetworkPolicy{}
				g.Expect(ownerA.Get(ctx, client.ObjectKeyFromObject(allowed), current)).To(Succeed())
				ipBlock(current).CIDR = "10.20.0.0/16"
				err := ownerA.Update(ctx, current)
				if err == nil {
					Fail("forbidden CIDR update succeeded")
				}
				g.Expect(err).To(MatchError(ContainSubstring("networkPolicy " + name + " CIDR")))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			stored := &networkingv1.NetworkPolicy{}
			Expect(ownerA.Get(ctx, client.ObjectKeyFromObject(allowed), stored)).To(Succeed())
			Expect(ipBlock(stored).CIDR).To(Equal("192.0.2.0/24"))

			By("auditing matching grants and evaluating conditions on metadata updates")
			auditPolicy := policy(audited, "audited", "10.20.0.0/16")
			for range 7 {
				*peers(auditPolicy) = append(*peers(auditPolicy), (*peers(auditPolicy))[0])
			}
			create(ownerA, auditPolicy)
			Eventually(func(g Gomega) {
				list := &eventsv1.EventList{}
				g.Expect(k8sClient.List(ctx, list, client.InNamespace(audited.Name))).To(Succeed())
				messages := make(map[string]int)
				for _, event := range list.Items {
					if event.Reason == events.ReasonNamespaceRuleAudit && event.Regarding.Name == "audited" && event.Regarding.Kind == "NetworkPolicy" {
						messages[event.Note]++
					}
				}
				g.Expect(messages).To(HaveLen(24), "three matching CIDRs for each of eight peers")
				for _, count := range messages {
					g.Expect(count).To(Equal(1), "audit once per peer/CIDR, not partition")
				}
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			conditionalPolicy := policy(conditional, "conditional", "10.20.0.0/16")
			create(ownerA, conditionalPolicy)
			Eventually(func(g Gomega) {
				current := &networkingv1.NetworkPolicy{}
				g.Expect(ownerA.Get(ctx, client.ObjectKeyFromObject(conditionalPolicy), current)).To(Succeed())
				current.Labels["blocked"] = "true"
				err := ownerA.Update(ctx, current)
				if err == nil {
					Fail("conditional CIDR update succeeded")
				}
				g.Expect(err).To(MatchError(ContainSubstring("networkPolicy " + name + " CIDR")))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			Expect(ownerA.Get(ctx, client.ObjectKeyFromObject(conditionalPolicy), stored)).To(Succeed())
			Expect(stored.Labels).NotTo(HaveKey("blocked"))

			By("applying namespace label changes while retaining deletion and checking recreation")
			Eventually(func() error {
				current := &corev1.Namespace{}
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(other), current); err != nil {
					return err
				}
				current.Labels["network-profile"] = "restricted"
				return k8sClient.Update(ctx, current)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			waitRules(other, "10.20.0.0/16", "fd00:1234::/48", "192.0.2.0/24")
			Expect(ownerA.Delete(ctx, existing)).To(Succeed())
			Eventually(func() bool {
				return apierrors.IsNotFound(ownerA.Get(ctx, client.ObjectKeyFromObject(existing), &networkingv1.NetworkPolicy{}))
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
			deny(ownerA, policy(other, existing.Name, "10.20.0.0/16"))

			By("composing later exceptions with existing metadata rules and updating the policy")
			Eventually(func() error {
				current := &capsulev1beta2.Tenant{}
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(a), current); err != nil {
					return err
				}
				current.Spec.Rules = append(current.Spec.Rules[:6], profileRule("restricted", rules.ActionTypeAllow, "10.20.0.0/17"))
				current.Spec.Rules[6].Enforce.Metadata = []rules.MetadataRule{{VersionKinds: apiruntime.VersionKinds{APIGroups: []string{"networking.k8s.io/v1"}, Kinds: []string{"NetworkPolicy"}}, Labels: map[string]rules.MetadataValueRule{"env": {Required: true}}}}
				return k8sClient.Update(ctx, current)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			TenantReady(a, metav1.ConditionTrue, defaultTimeoutInterval)
			waitRules(restricted, "10.20.0.0/16", "fd00:1234::/48", "192.0.2.0/24", "10.20.0.0/17")
			create(ownerA, policy(restricted, "later-allow", "10.20.0.0/17"))
			deny(ownerA, policy(restricted, "allow-not-whole", "10.20.0.0/16"))
			Eventually(func() error {
				current := &networkingv1.NetworkPolicy{}
				if err := ownerA.Get(ctx, client.ObjectKeyFromObject(allowed), current); err != nil {
					return err
				}
				ipBlock(current).CIDR = "10.20.0.0/17"
				return ownerA.Update(ctx, current)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			Eventually(func(g Gomega) {
				g.Expect(ownerA.Get(ctx, client.ObjectKeyFromObject(allowed), stored)).To(Succeed())
				g.Expect(ipBlock(stored).CIDR).To(Equal("10.20.0.0/17"))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})
	}
})
