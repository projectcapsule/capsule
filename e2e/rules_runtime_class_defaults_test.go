// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	nodev1 "k8s.io/api/node/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func runtimeClassDefault(action rules.MutationAction, class string, conditions ...rules.AdmissionCondition) rules.NamespaceRuleMutation {
	return rules.NamespaceRuleMutation{Action: action, Conditions: conditions, Workloads: rules.WorkloadMutation{Classes: rules.WorkloadClassMutation{Runtime: class}}}
}

func runtimeClassProfile(profiles []string, body *rules.NamespaceRuleBodyNamespace) *rules.NamespaceRuleBodyTenant {
	return &rules.NamespaceRuleBodyTenant{
		NamespaceSelector:          &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "rc-profile", Operator: metav1.LabelSelectorOpIn, Values: profiles}}},
		NamespaceRuleBodyNamespace: body,
	}
}

// expectRuntimeClass verifies the class together with what Capsule merges for
// the class it assigns and the API server derives for the class a Pod names:
// the overhead of the class, and its scheduling constraints next to the ones
// the Pod already carried. replaced is the list of a rule that replaces the
// tolerations: the webhook is reinvoked once later admission work changed the
// Pod, the replace swaps the list again, and the toleration of the class is
// restored next to it — a replacement must never strip the constraints that
// confine the Pod to compatible nodes. The overhead and the node selector of
// the class persist either way.
func expectRuntimeClass(ctx context.Context, pod *corev1.Pod, class *nodev1.RuntimeClass, retained map[string]string, replaced ...corev1.Toleration) {
	Eventually(func(g Gomega) {
		stored := &corev1.Pod{}
		g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(pod), stored)).To(Succeed())
		g.Expect(stored.Spec.RuntimeClassName).To(HaveValue(Equal(class.Name)))

		var overhead corev1.ResourceList
		if class.Overhead != nil {
			overhead = class.Overhead.PodFixed
		}

		g.Expect(equality.Semantic.DeepEqual(stored.Spec.Overhead, overhead)).To(BeTrue(), "overhead %v of class %s, want %v", stored.Spec.Overhead, class.Name, overhead)

		if class.Scheduling != nil {
			for key, value := range class.Scheduling.NodeSelector {
				g.Expect(stored.Spec.NodeSelector).To(HaveKeyWithValue(key, value))
			}

			for _, toleration := range class.Scheduling.Tolerations {
				g.Expect(stored.Spec.Tolerations).To(ContainElement(toleration))
			}
		}

		for _, toleration := range replaced {
			g.Expect(stored.Spec.Tolerations).To(ContainElement(toleration))
		}

		for key, value := range retained {
			g.Expect(stored.Spec.NodeSelector).To(HaveKeyWithValue(key, value))
		}
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}

var _ = Describe("RuntimeClass default namespace profiles", Label("tenant", "rules", "workloads", "mutation", "runtimeclass", "runtime-class-defaults"), func() {
	It("defaults or replaces the class per namespace while retaining enforcement and tenant isolation", func() {
		ctx := context.Background()
		prefix := "e2e-rc-defaults-" + rand.String(8)
		poolKey := prefix + "/pool"

		// The classes carry no environment label: the legacy RuntimeClass specs
		// sweep the classes that do, and they run in parallel.
		newClass := func(name string, overhead corev1.ResourceList, selector map[string]string) *nodev1.RuntimeClass {
			class := &nodev1.RuntimeClass{Name: name, Labels: map[string]string{"env": "e2e"}, Handler: "e2e-runtime-class"}

			if overhead != nil {
				class.Overhead = &nodev1.Overhead{PodFixed: overhead}
			}

			if selector != nil {
				// The API server merges both into every Pod that names the class.
				class.Scheduling = &nodev1.Scheduling{
					NodeSelector: selector,
					Tolerations:  []corev1.Toleration{{Key: prefix + "/runtime", Operator: corev1.TolerationOpEqual, Value: name, Effect: corev1.TaintEffectNoSchedule}},
				}
			}

			Expect(k8sClient.Create(ctx, class)).To(Succeed())
			DeferCleanup(DeleteInBackground, class)

			return class
		}

		custom := newClass(prefix+"-custom", corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")}, map[string]string{prefix + "/custom": "true"})
		conflicting := newClass(prefix+"-conflicting", nil, map[string]string{poolKey: "elsewhere"})
		// A class with scheduling constraints and no overhead: nothing but the
		// webhook's own merge would reject or constrain a Pod it is assigned to.
		scheduled := newClass(prefix+"-scheduled", nil, map[string]string{prefix + "/scheduled": "true"})
		forbidden := newClass(prefix+"-forbidden", nil, nil)
		bare := newClass(prefix+"-bare", nil, nil)
		dedicated := corev1.Toleration{Key: prefix + "/dedicated", Operator: corev1.TolerationOpExists}

		// A class whose toleration is limited in time: assigning it must not narrow
		// a broader toleration the Pod already carries.
		evictionSeconds := int64(60)
		evicting := &nodev1.RuntimeClass{Name: prefix + "-evicting", Labels: map[string]string{"env": "e2e"}, Handler: "e2e-runtime-class",
			Scheduling: &nodev1.Scheduling{
				NodeSelector: map[string]string{prefix + "/evicting": "true"},
				Tolerations:  []corev1.Toleration{{Key: prefix + "/evict", Operator: corev1.TolerationOpEqual, Value: "true", Effect: corev1.TaintEffectNoExecute, TolerationSeconds: &evictionSeconds}},
			}}
		Expect(k8sClient.Create(ctx, evicting)).To(Succeed())
		DeferCleanup(DeleteInBackground, evicting)

		var (
			tenants  []*capsulev1beta2.Tenant
			owners   []client.Client
			defaults []*nodev1.RuntimeClass
			forced   []*nodev1.RuntimeClass
		)

		for _, suffix := range []string{"a", "b"} {
			name := prefix + "-" + suffix
			defaults = append(defaults, newClass(name+"-default", corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("250m"), corev1.ResourceMemory: resource.MustParse("64Mi")}, map[string]string{prefix + "/default": name}))
			forced = append(forced, newClass(name+"-forced", corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m")}, map[string]string{poolKey: name}))

			tnt := &capsulev1beta2.Tenant{Name: name, Labels: map[string]string{"env": "e2e"}, Spec: capsulev1beta2.TenantSpec{
				Owners: rbac.OwnerListSpec{{Kind: "User", Name: name}},
				Rules: []*rules.NamespaceRuleBodyTenant{
					runtimeClassProfile([]string{"merge", "replace"}, &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{
						runtimeClassDefault(rules.MutationActionMerge, "{{ .tenant.metadata.name }}-default"),
						runtimeClassDefault(rules.MutationActionReplace, forbidden.Name, rules.AdmissionCondition{Name: "deny-class", Expression: `has(object.metadata.labels) && 'deny-class' in object.metadata.labels && object.metadata.labels['deny-class'] == 'true'`}),
					}}),
					runtimeClassProfile([]string{"replace"}, &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{runtimeClassDefault(rules.MutationActionReplace, name+"-forced")}}),
					// This class does not exist: Capsule rejects the Pod when applying it.
					runtimeClassProfile([]string{"merge", "replace"}, &rules.NamespaceRuleBodyNamespace{
						Audience: []rules.Audience{{Kind: rules.AudienceKindUser, Name: "not-the-runtime-class-owner"}},
						Mutate:   []rules.NamespaceRuleMutation{runtimeClassDefault(rules.MutationActionReplace, prefix+"-wrong-audience")},
					}),
					runtimeClassProfile([]string{"merge", "replace"}, &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{
						Action:    rules.ActionTypeDeny,
						Workloads: rules.NamespaceRuleEnforceWorkloadsBody{Classes: rules.WorkloadClassEnforcement{Runtime: []rules.WorkloadClassMatch{{Exact: []string{forbidden.Name}}}}},
					}}),
					runtimeClassProfile([]string{"bare"}, &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{runtimeClassDefault(rules.MutationActionReplace, bare.Name)}}),
					runtimeClassProfile([]string{"scheduled"}, &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{runtimeClassDefault(rules.MutationActionReplace, scheduled.Name)}}),
					runtimeClassProfile([]string{"evicting"}, &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{runtimeClassDefault(rules.MutationActionReplace, evicting.Name)}}),
					// A placement replacement next to a class the Pod names on its own:
					// the replaced selector must not strip the scheduling of the class.
					runtimeClassProfile([]string{"placement"}, &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{
						{Action: rules.MutationActionReplace, Workloads: rules.WorkloadMutation{Placement: rules.WorkloadPlacementMutation{NodeSelector: map[string]string{poolKey: "placement"}}}},
					}}),
					runtimeClassProfile([]string{"conflicted"}, &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{
						{Action: rules.MutationActionReplace, Workloads: rules.WorkloadMutation{Placement: rules.WorkloadPlacementMutation{NodeSelector: map[string]string{prefix + "/scheduled": "elsewhere"}}}},
					}}),
					// Entries that replace the class in turn, next to one that changes the Pod
					// again when the API server calls the webhook a second time.
					runtimeClassProfile([]string{"chain"}, &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{
						runtimeClassDefault(rules.MutationActionReplace, name+"-default"),
						runtimeClassDefault(rules.MutationActionReplace, name+"-forced"),
						{Action: rules.MutationActionReplace, Workloads: rules.WorkloadMutation{Placement: rules.WorkloadPlacementMutation{Tolerations: []corev1.Toleration{dedicated}}}},
					}}),
					runtimeClassProfile([]string{"missing"}, &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{runtimeClassDefault(rules.MutationActionMerge, prefix+"-absent")}}),
					// The pairing that confines a namespace to a set of classes: the allow list
					// alone admits a Pod without a class, and the default leaves none.
					runtimeClassProfile([]string{"confined"}, &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{runtimeClassDefault(rules.MutationActionMerge, name+"-default")}}),
					runtimeClassProfile([]string{"confined"}, &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{
						Action:    rules.ActionTypeAllow,
						Workloads: rules.NamespaceRuleEnforceWorkloadsBody{Classes: rules.WorkloadClassEnforcement{Runtime: []rules.WorkloadClassMatch{{Exact: []string{name + "-default", custom.Name}}}}},
					}}),
				},
			}}
			Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
			DeferCleanup(DeleteInBackground, tnt)
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)

			tenants = append(tenants, tnt)
			owners = append(owners, impersonationClient(name, withDefaultGroups(nil)))
		}

		expectProfile := func(ns *corev1.Namespace, count int, class string) {
			waitWorkloadClassProfile(ctx, ns, func(g Gomega, bodies []*rules.NamespaceRuleBodyNamespace) {
				g.Expect(bodies).To(HaveLen(count))

				if count > 0 {
					g.Expect(bodies[0].Mutate[0].Workloads.Classes.Runtime).To(Equal(class))
				}
			})
		}
		newNamespace := func(tenant int, profile string) *corev1.Namespace {
			ns := NewNamespace("", map[string]string{meta.TenantLabel: tenants[tenant].Name, "rc-profile": profile})
			NamespaceCreation(ns, tenants[tenant].Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tenants[tenant], ns).Should(Succeed())
			DeferCleanup(DeleteNamespaceInBackground, ctx, ns.Name)

			switch profile {
			case "merge":
				expectProfile(ns, 3, defaults[tenant].Name)
			case "replace":
				expectProfile(ns, 4, defaults[tenant].Name)
			case "bare":
				expectProfile(ns, 1, bare.Name)
			case "scheduled":
				expectProfile(ns, 1, scheduled.Name)
			case "evicting":
				expectProfile(ns, 1, evicting.Name)
			case "placement", "conflicted":
				expectProfile(ns, 1, "")
			case "chain":
				expectProfile(ns, 1, defaults[tenant].Name)
			case "missing":
				expectProfile(ns, 1, prefix+"-absent")
			case "confined":
				expectProfile(ns, 2, defaults[tenant].Name)
			default:
				expectProfile(ns, 0, "")
			}

			return ns
		}

		mergeNS, replaceNS, bareNS, chainNS := newNamespace(0, "merge"), newNamespace(0, "replace"), newNamespace(0, "bare"), newNamespace(0, "chain")
		otherNS, missingNS, confinedNS, scheduledNS := newNamespace(0, "other"), newNamespace(0, "missing"), newNamespace(0, "confined"), newNamespace(0, "scheduled")
		evictingNS := newNamespace(0, "evicting")
		placementNS, conflictedNS := newNamespace(0, "placement"), newNamespace(0, "conflicted")
		tenantBNS := newNamespace(1, "merge")

		sequence := 0
		newPod := func(ns *corev1.Namespace, class string) *corev1.Pod {
			sequence++

			return runtimeClassPod(ns.Name, fmt.Sprintf("pod-%d", sequence), class)
		}
		createPod := func(owner client.Client, ns *corev1.Namespace, class string, want *nodev1.RuntimeClass, retained map[string]string, replaced ...corev1.Toleration) *corev1.Pod {
			pod := newPod(ns, class)
			Eventually(func() error { return owner.Create(ctx, pod) }, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			expectRuntimeClass(ctx, pod, want, retained, replaced...)

			return pod
		}
		rejected := func(owner client.Client, pod *corev1.Pod, reasons ...string) {
			err := owner.Create(ctx, pod)
			Expect(err).To(HaveOccurred(), "the Pod must not be admitted")

			for _, reason := range reasons {
				Expect(err.Error()).To(ContainSubstring(reason))
			}

			Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(pod), &corev1.Pod{}))).To(BeTrue())
		}

		By("filling an omitted class and preserving an explicit one")
		original := createPod(owners[0], mergeNS, "", defaults[0], nil)
		createPod(owners[0], mergeNS, custom.Name, custom, nil)
		createPod(owners[0], mergeNS, defaults[0].Name, defaults[0], nil)

		By("replacing every class and carrying the overhead of the replacement")
		createPod(owners[0], replaceNS, "", forced[0], nil)
		// The API server set the overhead and the node selector of the class the Pod
		// named before the mutation. Capsule replaces the overhead with the one of
		// the class it assigns and merges that class's scheduling constraints.
		createPod(owners[0], replaceNS, custom.Name, forced[0], custom.Scheduling.NodeSelector)
		createPod(owners[0], bareNS, custom.Name, bare, custom.Scheduling.NodeSelector)

		By("carrying the constraints of a scheduling-only replacement class")
		// The class defines no overhead, so the API server's validation accepts the
		// Pod with or without the class's node selector and tolerations: only the
		// webhook's own merge constrains the Pod to the nodes of its runtime.
		createPod(owners[0], scheduledNS, "", scheduled, nil)
		createPod(owners[0], scheduledNS, custom.Name, scheduled, custom.Scheduling.NodeSelector)

		By("keeping a broader toleration the Pod carries over the one the class limits in time")
		// The class tolerates its taint for a minute; the Pod tolerates it without
		// a limit. The merge keeps the superset, as the API server's own plugin
		// does, so the assignment cannot let the Pod be evicted earlier.
		broader := corev1.Toleration{Key: prefix + "/evict", Operator: corev1.TolerationOpEqual, Value: "true", Effect: corev1.TaintEffectNoExecute}
		tolerant := newPod(evictingNS, "")
		tolerant.Spec.Tolerations = []corev1.Toleration{broader}
		Eventually(func() error { return owners[0].Create(ctx, tolerant) }, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func(g Gomega) {
			stored := &corev1.Pod{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tolerant), stored)).To(Succeed())
			g.Expect(stored.Spec.RuntimeClassName).To(HaveValue(Equal(evicting.Name)))
			g.Expect(stored.Spec.NodeSelector).To(HaveKeyWithValue(prefix+"/evicting", "true"))
			g.Expect(stored.Spec.Tolerations).To(ContainElement(broader))
			g.Expect(stored.Spec.Tolerations).NotTo(ContainElement(evicting.Scheduling.Tolerations[0]))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		// A Pod without a toleration of its own receives the class's limited one.
		createPod(owners[0], evictingNS, "", evicting, nil)

		By("keeping the overhead of the final class when the API server calls the webhook again")
		// Capsule derives the overhead, the node selector, and the toleration of the
		// class the entries end at. Later admission work changes the Pod and the
		// webhook is called again: the entries replace the class in turn and the
		// tolerations once more, and since the Pod ends with the same class, the
		// toleration of the class is restored next to the replaced list while the
		// overhead and node selector persist.
		createPod(owners[0], chainNS, "", forced[0], nil, dedicated)
		createPod(owners[0], chainNS, custom.Name, forced[0], custom.Scheduling.NodeSelector, dedicated)

		By("rejecting a replacement whose node selector conflicts with the replaced class")
		rejected(owners[0], newPod(replaceNS, conflicting.Name), "runtimeClass.scheduling.nodeSelector", poolKey)

		By("restoring the scheduling of a named class after a placement replacement")
		// The profile replaces the node selector of every Pod. For a Pod that
		// names a scheduling-only class, the replacement would strip the selector
		// that confines the Pod to the nodes of its runtime, and nothing after
		// the webhook would put it back: Capsule restores the scheduling of the
		// class next to the replaced selector.
		createPod(owners[0], placementNS, scheduled.Name, scheduled, map[string]string{poolKey: "placement"})

		// A replacement that contradicts the selector of the class cannot be
		// restored and rejects the Pod; one without a class keeps the replacement.
		rejected(owners[0], newPod(conflictedNS, scheduled.Name), "runtimeClass.scheduling.nodeSelector", prefix+"/scheduled")
		classless := newPod(conflictedNS, "")
		Eventually(func() error { return owners[0].Create(ctx, classless) }, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func(g Gomega) {
			stored := &corev1.Pod{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(classless), stored)).To(Succeed())
			g.Expect(stored.Spec.RuntimeClassName).To(BeNil())
			g.Expect(stored.Spec.NodeSelector).To(HaveKeyWithValue(prefix+"/scheduled", "elsewhere"))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		By("leaving non-selected namespaces and the other tenant's defaults alone")
		unselected := newPod(otherNS, "")
		Eventually(func() error { return owners[0].Create(ctx, unselected) }, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func(g Gomega) {
			stored := &corev1.Pod{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(unselected), stored)).To(Succeed())
			g.Expect(stored.Spec.RuntimeClassName).To(BeNil())
			g.Expect(stored.Spec.Overhead).To(BeEmpty())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		createPod(owners[0], otherNS, custom.Name, custom, nil)
		createPod(owners[1], tenantBNS, "", defaults[1], nil)

		By("enforcing the final class for both supplied and mutated names")
		for _, mutated := range []bool{false, true} {
			pod := newPod(mergeNS, forbidden.Name)
			if mutated {
				pod.Spec.RuntimeClassName = nil
				pod.Labels["deny-class"] = "true"
			}

			rejected(owners[0], pod, forbidden.Name, "spec.runtimeClassName", "is denied by namespace rule")
		}

		By("confining a namespace with a default next to an allow list, also for the Pods of its controllers")
		createPod(owners[0], confinedNS, "", defaults[0], nil)
		createPod(owners[0], confinedNS, custom.Name, custom, nil)
		rejected(owners[0], newPod(confinedNS, bare.Name), bare.Name, "spec.runtimeClassName", "is not allowed by namespace rule")

		// The ReplicaSet controller creates these Pods, not the tenant owner. The rules
		// target Pods only, so the templates are admitted as they are.
		controlled := func(name, class string) string {
			workload := MakeDeployment(confinedNS.Name, name, 1, map[string]string{"env": "e2e"}, "")
			workload.Labels = map[string]string{"env": "e2e"}
			// The Pods stay Pending, so no node needs the handler of their class.
			workload.Spec.Template.Spec.SchedulingGates = []corev1.PodSchedulingGate{{Name: "example.com/e2e-runtime-class"}}

			if class != "" {
				workload.Spec.Template.Spec.RuntimeClassName = &class
			}

			Eventually(func() error { return owners[0].Create(ctx, workload) }, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			return workload.Name
		}

		defaulted := controlled("defaulted", "")
		ExpectPodsForDeployment(ctx, confinedNS.Name, defaulted, 1)

		controlledPods := &corev1.PodList{}
		Expect(k8sClient.List(ctx, controlledPods, client.InNamespace(confinedNS.Name), client.MatchingLabels{"app": defaulted})).To(Succeed())
		Expect(controlledPods.Items).To(HaveLen(1))
		expectRuntimeClass(ctx, &controlledPods.Items[0], defaults[0], nil)

		outside := controlled("outside", bare.Name)
		Eventually(func(g Gomega) {
			failure, err := replicaSetFailureForDeployment(ctx, confinedNS.Name, outside)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(failure).NotTo(BeNil())
			g.Expect(failure.Status).To(Equal(corev1.ConditionTrue))
			g.Expect(failure.Message).To(ContainSubstring(fmt.Sprintf("runtime class %q at spec.runtimeClassName is not allowed by namespace rule", bare.Name)))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		ExpectPodsForDeployment(ctx, confinedNS.Name, outside, 0)

		By("rejecting a Pod whose default class does not exist")
		rejected(owners[0], newPod(missingNS, ""), fmt.Sprintf("RuntimeClass %q not found", prefix+"-absent"))
		createPod(owners[0], missingNS, custom.Name, custom, nil)

		By("rejecting cross-tenant Pod creation through RBAC")
		crossTenant := newPod(tenantBNS, "")
		err := owners[0].Create(ctx, crossTenant)
		Expect(apierrors.IsForbidden(err)).To(BeTrue())
		Expect(err).To(MatchError(ContainSubstring(`cannot create resource "pods"`)))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(crossTenant), &corev1.Pod{}))).To(BeTrue())

		By("rejecting an invalid class name without changing the active policy")
		Eventually(func(g Gomega) {
			current := &capsulev1beta2.Tenant{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tenants[0]), current)).To(Succeed())
			current.Spec.Rules[0].Mutate[0].Workloads.Classes.Runtime = "Invalid/Class"

			err := k8sClient.Update(ctx, current)
			if err == nil {
				Fail("an invalid runtime class name was accepted")
			}

			g.Expect(err).To(MatchError(And(ContainSubstring("workloads.classes.runtime"), ContainSubstring("invalid runtime class name"))))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		storedTenant := &capsulev1beta2.Tenant{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tenants[0]), storedTenant)).To(Succeed())
		Expect(storedTenant.Spec.Rules[0].Mutate[0].Workloads.Classes.Runtime).To(Equal("{{ .tenant.metadata.name }}-default"))

		By("applying a changed default only to newly created Pods")
		Eventually(func() error {
			current := &capsulev1beta2.Tenant{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(tenants[0]), current); err != nil {
				return err
			}

			current.Spec.Rules[0].Mutate[0].Workloads.Classes.Runtime = forced[0].Name

			return k8sClient.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		TenantReady(tenants[0], metav1.ConditionTrue, defaultTimeoutInterval)
		expectProfile(mergeNS, 3, forced[0].Name)
		createPod(owners[0], mergeNS, "", forced[0], nil)
		Eventually(func() error {
			current := &corev1.Pod{}
			if err := owners[0].Get(ctx, client.ObjectKeyFromObject(original), current); err != nil {
				return err
			}

			current.Labels["updated"] = "true"

			return owners[0].Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func(g Gomega) {
			stored := &corev1.Pod{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(original), stored)).To(Succeed())
			g.Expect(stored.Labels).To(HaveKeyWithValue("updated", "true"))
			g.Expect(stored.Spec.RuntimeClassName).To(HaveValue(Equal(defaults[0].Name)))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		createPod(owners[1], tenantBNS, "", defaults[1], nil)

		By("selecting a different namespace profile after a label change")
		Eventually(func() error {
			current := &corev1.Namespace{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(otherNS), current); err != nil {
				return err
			}

			current.Labels["rc-profile"] = "merge"

			return k8sClient.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		expectProfile(otherNS, 3, forced[0].Name)
		createPod(owners[0], otherNS, "", forced[0], nil)

		By("carrying the constraints the class holds at admission, not cached ones")
		// The scheduling of an assigned class is read from the API server when
		// the Pod is admitted: a Pod created right after the class changed must
		// carry the new constraint, which nothing downstream would reject.
		Eventually(func() error {
			current := &nodev1.RuntimeClass{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(scheduled), current); err != nil {
				return err
			}

			current.Scheduling.NodeSelector[prefix+"/phase"] = "two"

			return k8sClient.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		scheduled.Scheduling.NodeSelector[prefix+"/phase"] = "two"
		createPod(owners[0], scheduledNS, "", scheduled, nil)
	})
})
