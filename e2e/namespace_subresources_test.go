// Copyright 2020-2026 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"
	"slices"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/utils"
)

// Shared fixtures make the complete transport/policy matrix affordable in CI.
// Config changes and the real kube-system probe run serially with the config
// suite. ContinueOnFailure records every regression instead of skipping the
// remaining matrix after the first permissive admission response.
var _ = Describe("namespace subresource admission", Ordered, ContinueOnFailure, Serial, Label("config", "namespace", "namespace-subresources"), func() {
	ctx := context.Background()
	var (
		admin                  kubernetes.Interface
		actors                 map[string]kubernetes.Interface
		spaces                 map[string]string
		own, foreign, cordoned *capsulev1beta2.Tenant
	)

	BeforeAll(func() {
		original := &capsulev1beta2.CapsuleConfiguration{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: defaultConfigurationName}, original)).To(Succeed())
		administrator := rbac.UserSpec{Name: nsAdmissionPrefix + "-admin", Kind: rbac.UserOwner}
		ModifyCapsuleConfigurationOpts(func(c *capsulev1beta2.CapsuleConfiguration) {
			c.Spec.Administrators = append(c.Spec.Administrators, administrator)
			c.Spec.Users = append(c.Spec.Users, rbac.UserSpec{Name: "system:serviceaccount:" + nsAdmissionPrefix + "-actors:owner", Kind: rbac.ServiceAccountOwner})
		})
		DeferCleanup(func() {
			ModifyCapsuleConfigurationOpts(func(c *capsulev1beta2.CapsuleConfiguration) {
				c.Spec.Administrators = original.Spec.Administrators
				c.Spec.Users = original.Spec.Users
			})
		})
		adminBinding := &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: nsAdmissionPrefix + "-admin"},
			Subjects: []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: "User", Name: administrator.Name}},
			RoleRef:  rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: "cluster-admin"},
		}
		Expect(k8sClient.Create(ctx, adminBinding)).To(Succeed())
		DeferCleanup(EventuallyDeletion, adminBinding)
		admin = impersonationClientSet(administrator.Name, []string{"projectcapsule.dev", "system:authenticated"})
		nsAdmissionCheckRBAC(ctx, admin, "kube-system")

		actorNS := NewNamespace(nsAdmissionPrefix + "-actors")
		Expect(k8sClient.Create(ctx, actorNS)).To(Succeed())
		DeferCleanup(ForceDeleteNamespace, ctx, actorNS.Name)
		sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "owner", Namespace: actorNS.Name}}
		Expect(k8sClient.Create(ctx, sa)).To(Succeed())
		username, group := nsAdmissionPrefix+"-alice", nsAdmissionPrefix+"-owners"
		saUsername := "system:serviceaccount:" + actorNS.Name + ":" + sa.Name
		owners := rbac.OwnerListSpec{
			{CoreOwnerSpec: rbac.CoreOwnerSpec{UserSpec: rbac.UserSpec{Name: username, Kind: rbac.UserOwner}}},
			{CoreOwnerSpec: rbac.CoreOwnerSpec{UserSpec: rbac.UserSpec{Name: group, Kind: rbac.GroupOwner}}},
			{CoreOwnerSpec: rbac.CoreOwnerSpec{UserSpec: rbac.UserSpec{Name: saUsername, Kind: rbac.ServiceAccountOwner}}},
		}
		actors = map[string]kubernetes.Interface{
			"user":           impersonationClientSet(username, []string{"projectcapsule.dev", "system:authenticated"}),
			"group":          impersonationClientSet(nsAdmissionPrefix+"-member", []string{"projectcapsule.dev", group, "system:authenticated"}),
			"serviceaccount": impersonationClientSet(saUsername, []string{"system:serviceaccounts", "system:serviceaccounts:" + actorNS.Name, "system:authenticated"}),
			"non-owner":      impersonationClientSet(nsAdmissionPrefix+"-non-owner", []string{"projectcapsule.dev", "system:authenticated"}),
			"operator":       impersonationClientSet(nsAdmissionPrefix+"-operator", []string{"system:authenticated"}),
			"controller":     impersonationClientSet(ControllerServiceAccountFull, []string{"system:serviceaccounts", "system:serviceaccounts:" + ControllerNamespace, "system:authenticated"}),
		}
		nsAdmissionGrant(ctx, nsAdmissionPrefix, []rbacv1.Subject{
			{APIGroup: rbacv1.GroupName, Kind: "User", Name: username},
			{APIGroup: rbacv1.GroupName, Kind: "Group", Name: group},
			{Kind: "ServiceAccount", Name: sa.Name, Namespace: sa.Namespace},
			{APIGroup: rbacv1.GroupName, Kind: "User", Name: nsAdmissionPrefix + "-non-owner"},
			{APIGroup: rbacv1.GroupName, Kind: "User", Name: nsAdmissionPrefix + "-operator"},
		})
		for _, actor := range actors {
			nsAdmissionCheckRBAC(ctx, actor, "kube-system")
		}
		own = nsAdmissionTenant(nsAdmissionPrefix+"-oil", owners)
		nsAdmissionPolicies(own)
		foreign = nsAdmissionTenant(nsAdmissionPrefix+"-gas", rbac.OwnerListSpec{{CoreOwnerSpec: rbac.CoreOwnerSpec{UserSpec: rbac.UserSpec{Name: nsAdmissionPrefix + "-bob", Kind: rbac.UserOwner}}}})
		cordoned = nsAdmissionTenant(nsAdmissionPrefix+"-cordoned", owners)
		for _, tnt := range []*capsulev1beta2.Tenant{own, foreign, cordoned} {
			Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
			DeferCleanup(EventuallyDeletion, tnt)
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
		}
		spaces = map[string]string{}
		for _, target := range []struct {
			name   string
			tenant *capsulev1beta2.Tenant
			policy string
		}{
			{"own", own, "legacy"}, {"rules", own, "rules"}, {"foreign", foreign, ""}, {"unmanaged", nil, ""}, {"cordoned", cordoned, ""},
		} {
			for _, state := range []string{"active", "terminating"} {
				key := target.name + "-" + state
				ns := nsAdmissionNamespace(ctx, admin, nsAdmissionPrefix+"-"+key, target.tenant, target.policy)
				spaces[key] = ns.Name
				if state == "terminating" {
					DeferCleanup(holdNamespaceTerminating(ctx, ns.Name))
				}
			}
		}
		Expect(retry.RetryOnConflict(retry.DefaultRetry, func() error {
			current := &capsulev1beta2.Tenant{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cordoned), current); err != nil {
				return err
			}
			current.Spec.Cordoned = true
			return k8sClient.Update(ctx, current)
		})).To(Succeed())
		Eventually(func(g Gomega) {
			ns, err := admin.CoreV1().Namespaces().Get(ctx, spaces["cordoned-active"], metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(ns.Labels).To(HaveKeyWithValue(meta.CordonedLabel, "true"))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		// Verify the API server is calling the intended validator before the
		// matrix. These are admission probes, not a substitute for the RBAC checks.
		for _, name := range []string{"user", "group", "serviceaccount"} {
			Eventually(func() error {
				_, _, err := nsAdmissionAttempt(ctx, admin, actors[name], spaces["own-active"], "status", nsAdmissionTransports[0], nsAdmissionMetadataChange("labels", nsAdmissionKey+"forbidden", "replace", "changed"), true)
				if err == nil {
					return fmt.Errorf("Capsule has not classified %s as a tenant user", name)
				}
				if !apierrors.IsForbidden(err) || !strings.Contains(err.Error(), "is forbidden for the current Tenant") {
					return err
				}
				return nil
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
	})

	type policyCase struct{ name, target, field, key, operation, value, message string }
	var policies []policyCase
	for _, field := range []string{"labels", "annotations"} {
		for _, item := range []struct{ name, key, operation, value, message string }{
			{"forbidden replacement", "forbidden", "replace", "changed", "is forbidden for the current Tenant"},
			{"forbidden addition", "forbidden-new", "add", "changed", "is forbidden for the current Tenant"},
			{"forbidden regex replacement", "regex-existing", "replace", "changed", "is forbidden for the current Tenant"},
			{"forbidden regex addition", "regex-new", "add", "changed", "is forbidden for the current Tenant"},
			{"required removal", "required", "remove", "", "required"},
			{"required replacement", "required", "replace", "wrong", "does not match regex"},
			{"rule deny replacement", "rule-deny", "replace", "denied", "matched denied rule"},
			{"rule deny addition", "rule-deny-new", "add", "denied", "matched denied rule"},
			{"rule required removal", "rule-required", "remove", "", "required"},
			{"rule required replacement", "rule-required", "replace", "wrong", ""},
		} {
			policies = append(policies, policyCase{item.name + " " + field, "own", field, nsAdmissionKey + item.key, item.operation, item.value, item.message})
		}
	}
	for _, policy := range []string{"own", "rules"} {
		for _, value := range []string{"privileged", "baseline"} {
			message := "is forbidden for the current Tenant"
			if policy == "rules" {
				message = "matched denied rule"
			}
			policies = append(policies, policyCase{"PSA " + policy + " downgrade to " + value, policy, "labels", nsAdmissionPSA, "replace", value, message})
		}
	}
	for _, subresource := range []string{"", "status", "finalize"} {
		for _, transport := range nsAdmissionTransports {
			for _, state := range []string{"active", "terminating"} {
				for _, test := range policies {
					It(fmt.Sprintf("rejects %s via %s namespaces/%s while %s", test.name, transport.name, subresource, state), func() {
						nsAdmissionExpectDenied(ctx, admin, actors["user"], spaces[test.target+"-"+state], subresource, transport, nsAdmissionMetadataChange(test.field, test.key, test.operation, test.value), test.message, false)
					})
				}
				for _, operation := range []string{"remove", "replace"} {
					It(fmt.Sprintf("protects the node selector via %s namespaces/%s %s while %s", transport.name, subresource, operation, state), func() {
						change := nsAdmissionMetadataChange("annotations", utils.NodeSelectorAnnotation, operation, "foreign-pool=true")
						if subresource == "" && state == "active" {
							old, response, err := nsAdmissionAttempt(ctx, admin, actors["user"], spaces["own-"+state], subresource, transport, change, false)
							Expect(err).NotTo(HaveOccurred())
							nsAdmissionExpectMetadata(response, old)
							current, err := admin.CoreV1().Namespaces().Get(ctx, old.Name, metav1.GetOptions{})
							Expect(err).NotTo(HaveOccurred())
							nsAdmissionExpectMetadata(current, old)
						} else {
							nsAdmissionExpectDenied(ctx, admin, actors["user"], spaces["own-"+state], subresource, transport, change, "is enforced via tenant", false)
						}
					})
				}
				for _, field := range []string{"labels", "annotations"} {
					for _, target := range []string{"foreign", "unmanaged", "cordoned"} {
						for _, actor := range []string{"user", "group", "serviceaccount"} {
							It(fmt.Sprintf("rejects %s owner %s on %s namespace via %s namespaces/%s while %s", actor, field, target, transport.name, subresource, state), func() {
								message := "denied patch request for this namespace"
								if target == "unmanaged" {
									message = "namespace is not owned by any tenant"
								}
								if target == "cordoned" {
									message = "the selected tenant is cordoned"
								}
								nsAdmissionExpectDenied(ctx, admin, actors[actor], spaces[target+"-"+state], subresource, transport, nsAdmissionMetadataChange(field, nsAdmissionKey+"probe", "add", "changed"), message, false)
							})
						}
					}
					for _, actor := range []string{"user", "group", "serviceaccount"} {
						for _, operation := range []string{"add", "replace", "remove"} {
							It(fmt.Sprintf("allows %s owner %s %s via %s namespaces/%s while %s", actor, field, operation, transport.name, subresource, state), func() {
								key := "allowed"
								if operation == "add" {
									key = "allowed-new"
								}
								nsAdmissionExpectAllowed(ctx, admin, actors[actor], spaces["own-"+state], subresource, transport, nsAdmissionMetadataChange(field, nsAdmissionKey+key, operation, "changed"), false)
							})
						}
					}
				}
			}
			for _, actor := range []string{"user", "group", "serviceaccount", "non-owner"} {
				It(fmt.Sprintf("rejects %s metadata writes to kube-system via %s namespaces/%s", actor, transport.name, subresource), func() {
					key := nsAdmissionKey + "system-probe"
					current, err := admin.CoreV1().Namespaces().Get(ctx, "kube-system", metav1.GetOptions{})
					Expect(err).NotTo(HaveOccurred())
					Expect(current.Labels).NotTo(HaveKey(meta.TenantLabel))
					Expect(current.Annotations).NotTo(HaveKey(key))
					// This harmless annotation is restored even when testing a vulnerable build.
					DeferCleanup(func() {
						_, _, err := nsAdmissionAttempt(ctx, admin, admin, "kube-system", "", nsAdmissionTransports[2], nsAdmissionMetadataChange("annotations", key, "remove", ""), false)
						Expect(err).NotTo(HaveOccurred())
					})
					nsAdmissionExpectDenied(ctx, admin, actors[actor], "kube-system", subresource, transport, nsAdmissionMetadataChange("annotations", key, "add", "changed"), "namespace is not owned by any tenant", false)
				})
			}
			It(fmt.Sprintf("denies a Capsule non-owner on an owned namespace via %s namespaces/%s", transport.name, subresource), func() {
				nsAdmissionExpectDenied(ctx, admin, actors["non-owner"], spaces["own-active"], subresource, transport, nsAdmissionMetadataChange("labels", nsAdmissionKey+"probe", "add", "changed"), "denied patch request for this namespace", false)
			})
			for _, actor := range []string{"administrator", "controller", "operator"} {
				It(fmt.Sprintf("allows %s metadata on an unmanaged namespace via %s namespaces/%s", actor, transport.name, subresource), func() {
					cs := actors[actor]
					if actor == "administrator" {
						cs = admin
					}
					nsAdmissionExpectAllowed(ctx, admin, cs, spaces["unmanaged-active"], subresource, transport, nsAdmissionMetadataChange("labels", nsAdmissionKey+"operator-probe", "add", "changed"), false)
				})
			}
			It(fmt.Sprintf("enforces metadata policy during dry-run via %s namespaces/%s", transport.name, subresource), func() {
				nsAdmissionExpectDenied(ctx, admin, actors["user"], spaces["own-active"], subresource, transport, nsAdmissionMetadataChange("labels", nsAdmissionPSA, "replace", "privileged"), "is forbidden for the current Tenant", true)
				nsAdmissionExpectAllowed(ctx, admin, actors["user"], spaces["own-active"], subresource, transport, nsAdmissionMetadataChange("labels", nsAdmissionKey+"dry-run", "add", "changed"), true)
			})
			for _, change := range []string{"label", "remove label", "owner UID", "remove owner", "second owner", "migration"} {
				It(fmt.Sprintf("preserves Tenant assignment against %s via %s namespaces/%s", change, transport.name, subresource), func() {
					mutate := func(ns *corev1.Namespace) {
						switch change {
						case "label":
							ns.Labels[meta.TenantLabel] = foreign.Name
						case "remove label":
							delete(ns.Labels, meta.TenantLabel)
						case "owner UID":
							nsAdmissionTenantReference(ns).UID = types.UID("incorrect-uid")
						case "remove owner":
							ns.OwnerReferences = nil
						case "second owner":
							ns.OwnerReferences = append(ns.OwnerReferences, metav1.OwnerReference{APIVersion: capsulev1beta2.GroupVersion.String(), Kind: "Tenant", Name: foreign.Name, UID: foreign.UID})
						case "migration":
							ns.Labels[meta.TenantLabel] = foreign.Name
							ref := nsAdmissionTenantReference(ns)
							ref.Name, ref.UID = foreign.Name, foreign.UID
						}
					}
					// The mutator can repair inconsistent references on the ordinary
					// path. Subresources must reject, with no mutation to rely on.
					if subresource != "" {
						nsAdmissionExpectDenied(ctx, admin, actors["user"], spaces["own-active"], subresource, transport, mutate, "", true)
					} else {
						old, response, err := nsAdmissionAttempt(ctx, admin, actors["user"], spaces["own-active"], subresource, transport, mutate, true)
						if err == nil {
							nsAdmissionExpectMetadata(response, old)
						} else {
							Expect(err).To(MatchError(ContainSubstring("admission webhook \"namespaces.")))
						}
						current, err := admin.CoreV1().Namespaces().Get(ctx, old.Name, metav1.GetOptions{})
						Expect(err).NotTo(HaveOccurred())
						nsAdmissionExpectMetadata(current, old)
					}
				})
			}
		}
	}

	It("keeps the cordoning webhook active for namespace contents", func() {
		_, err := actors["user"].CoreV1().ConfigMaps(spaces["cordoned-active"]).Create(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "cordon-probe"}}, metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}})
		Expect(err).To(MatchError(ContainSubstring("cordoning.validating")))
	})

	It("keeps Pod Security enforcement after denied subresource writes", Label("skip-on-openshift"), func() {
		for _, target := range []string{"own-active", "rules-active"} {
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "psa-probe"}, Spec: corev1.PodSpec{HostNetwork: true, Containers: []corev1.Container{{Name: "probe", Image: "registry.k8s.io/pause:3.10", SecurityContext: &corev1.SecurityContext{Privileged: ptr.To(true)}}}}}
			for _, subresource := range []string{"", "status", "finalize"} {
				_, err := actors["user"].CoreV1().Pods(spaces[target]).Create(ctx, pod, metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}})
				Expect(err).To(MatchError(ContainSubstring("violates PodSecurity")))
				nsAdmissionExpectDenied(ctx, admin, actors["user"], spaces[target], subresource, nsAdmissionTransports[0], nsAdmissionMetadataChange("labels", nsAdmissionPSA, "replace", "privileged"), "", false)
				_, err = actors["user"].CoreV1().Pods(spaces[target]).Create(ctx, pod, metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}})
				Expect(err).To(MatchError(ContainSubstring("violates PodSecurity")))
			}
		}
	})

	It("allows status and finalizer cleanup after the Tenant disappears", func() {
		const hold = "e2e.projectcapsule.dev/hold-finalize"
		tnt := nsAdmissionTenant(nsAdmissionPrefix+"-missing", own.Spec.Owners)
		Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
		DeferCleanup(EventuallyDeletion, tnt)
		TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
		ns := nsAdmissionNamespace(ctx, admin, nsAdmissionPrefix+"-missing-tenant", tnt, "")
		_, _, err := nsAdmissionAttempt(ctx, admin, admin, ns.Name, "finalize", nsAdmissionTransports[0], func(ns *corev1.Namespace) {
			ns.Spec.Finalizers = append(ns.Spec.Finalizers, corev1.FinalizerName(hold))
		}, false)
		Expect(err).NotTo(HaveOccurred())
		// Ensure our own finalizer never survives a failed assertion.
		DeferCleanup(func() {
			_, _, err := nsAdmissionAttempt(ctx, admin, admin, ns.Name, "finalize", nsAdmissionTransports[0], func(ns *corev1.Namespace) {
				ns.Spec.Finalizers = slices.DeleteFunc(ns.Spec.Finalizers, func(value corev1.FinalizerName) bool { return value == hold })
			}, false)
			Expect(err).To(SatisfyAny(BeNil(), WithTransform(apierrors.IsNotFound, BeTrue())))
		})
		DeferCleanup(holdNamespaceTerminating(ctx, ns.Name))
		Expect(k8sClient.Delete(ctx, tnt, client.PropagationPolicy(metav1.DeletePropagationBackground))).To(Succeed())
		Expect(retry.RetryOnConflict(retry.DefaultRetry, func() error {
			current := &capsulev1beta2.Tenant{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(tnt), current); err != nil {
				if apierrors.IsNotFound(err) {
					return nil
				}
				return err
			}
			current.Finalizers = nil
			return k8sClient.Update(ctx, current)
		})).To(Succeed())
		Eventually(func() bool {
			return apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(tnt), &capsulev1beta2.Tenant{}))
		}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		current, err := admin.CoreV1().Namespaces().Get(ctx, ns.Name, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(current.DeletionTimestamp).NotTo(BeNil())
		Expect(nsAdmissionTenantReference(current).UID).To(Equal(tnt.UID))
		for _, transport := range nsAdmissionTransports {
			_, _, err = nsAdmissionAttempt(ctx, admin, admin, ns.Name, "status", transport, func(ns *corev1.Namespace) {
				ns.Status.Conditions = slices.DeleteFunc(ns.Status.Conditions, func(value corev1.NamespaceCondition) bool { return value.Type == "E2ECleanup" })
				ns.Status.Conditions = append(ns.Status.Conditions, corev1.NamespaceCondition{Type: "E2ECleanup", Status: corev1.ConditionFalse, Reason: "TestingCleanup", Message: "Tenant no longer exists: " + transport.name})
			}, false)
			Expect(err).NotTo(HaveOccurred())
		}
		_, _, err = nsAdmissionAttempt(ctx, admin, admin, ns.Name, "finalize", nsAdmissionTransports[0], func(ns *corev1.Namespace) {
			ns.Spec.Finalizers = slices.DeleteFunc(ns.Spec.Finalizers, func(value corev1.FinalizerName) bool { return value == hold })
		}, false)
		Expect(err).NotTo(HaveOccurred())
	})
})
