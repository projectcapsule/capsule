// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

const (
	// cacheLagTolerance bounds how long an admission decision may still follow
	// the state before a change. The typed tenant handlers and the metadata
	// webhook read the Namespace, its Tenant and the managed RuleStatus from
	// the manager informer cache first (pkg/tenant/cache_first.go), so a
	// change that passes every consistency check of that cache reaches
	// admission after the watch delay of those informers: milliseconds in a
	// healthy cluster. The bound leaves room for a loaded CI node and for the
	// second controller replica, whose cache lags independently. It is far
	// below defaultTimeoutInterval, so a decision that stays stale until a
	// resync or a later reconciliation fails the spec.
	cacheLagTolerance = 15 * time.Second
	// cacheLagSettle is how long a converged decision must then hold. The
	// webhook Service balances the probes between both controller replicas,
	// so a replica that kept a stale answer shows up within a few polls.
	cacheLagSettle = 3 * time.Second
)

var _ = Describe("admission decisions from the manager cache", Label("tenant", "rules", "enforce", "services", "admission-cache"), func() {
	// Names are unique per spec, so the specs of this container can run
	// concurrently on different Ginkgo processes.
	var (
		ctx    context.Context
		prefix string

		ownerA  rbac.UserSpec
		ownerB  rbac.UserSpec
		tenantA *capsulev1beta2.Tenant
		tenantB *capsulev1beta2.Tenant
	)

	// strictProfile selects the namespaces of Tenant A that also deny
	// ExternalName Services.
	strictProfile := map[string]string{"admission-cache-profile": "strict"}

	denyServiceTypes := func(selector map[string]string, serviceTypes ...rules.ServiceType) *rules.NamespaceRuleBodyTenant {
		rule := &rules.NamespaceRuleBodyTenant{
			NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
				Enforce: &rules.NamespaceRuleEnforceBody{
					Action: rules.ActionTypeDeny,
					Services: rules.NamespaceRuleEnforceServicesBody{
						Types: serviceTypes,
					},
				},
			},
		}

		if selector != nil {
			rule.NamespaceSelector = &metav1.LabelSelector{MatchLabels: selector}
		}

		return rule
	}

	newTenant := func(name string, owner rbac.UserSpec, tenantRules ...*rules.NamespaceRuleBodyTenant) *capsulev1beta2.Tenant {
		return &capsulev1beta2.Tenant{
			ObjectMeta: metav1.ObjectMeta{
				Name:   name,
				Labels: map[string]string{"env": "e2e"},
			},
			Spec: capsulev1beta2.TenantSpec{
				Owners: rbac.OwnerListSpec{{UserSpec: owner}},
				Rules:  tenantRules,
			},
		}
	}

	BeforeEach(func() {
		ctx = context.Background()
		prefix = "e2e-admission-cache-" + rand.String(6)

		ownerA = rbac.UserSpec{Name: prefix + "-owner-a", Kind: rbac.UserOwner}
		ownerB = rbac.UserSpec{Name: prefix + "-owner-b", Kind: rbac.UserOwner}

		// Two namespace profiles in Tenant A, a third one in Tenant B: a decision
		// taken from the wrong Tenant, namespace or rule set is visible.
		tenantA = newTenant(prefix+"-a", ownerA,
			denyServiceTypes(nil, rules.ServiceTypeNodePort),
			denyServiceTypes(strictProfile, rules.ServiceTypeExternalName),
		)
		tenantB = newTenant(prefix+"-b", ownerB,
			denyServiceTypes(nil, rules.ServiceTypeExternalName),
		)

		for _, tnt := range []*capsulev1beta2.Tenant{tenantA, tenantB} {
			EventuallyCreation(func() error {
				tnt.ResourceVersion = ""

				return k8sClient.Create(ctx, tnt)
			}).Should(Succeed())
			// The Tenant name is unique per spec: no need to wait for it to be gone.
			DeferCleanup(DeleteInBackground, tnt)
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
		}
	})

	createNamespace := func(tnt *capsulev1beta2.Tenant, owner rbac.UserSpec, name string, labels map[string]string) string {
		ns := NewNamespace(name, mergeMaps(labels, map[string]string{meta.TenantLabel: tnt.GetName()}))
		NamespaceCreation(ns, owner, defaultTimeoutInterval).Should(Succeed())
		DeferCleanup(DeleteNamespaceInBackground, ctx, ns.GetName())

		return ns.GetName()
	}

	type serviceTypeDenial struct {
		action rules.ActionType
		types  []rules.ServiceType
	}

	denial := func(serviceTypes ...rules.ServiceType) serviceTypeDenial {
		return serviceTypeDenial{action: rules.ActionTypeDeny, types: serviceTypes}
	}

	// expectRuleStatus waits until the managed RuleStatus of a namespace has
	// observed its current generation and holds the wanted Service type rules.
	expectRuleStatus := func(namespace string, want ...serviceTypeDenial) {
		Eventually(func(g Gomega) {
			rs := &capsulev1beta2.RuleStatus{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKey{
				Namespace: namespace,
				Name:      meta.NameForManagedRuleStatus(),
			}, rs)).To(Succeed())
			g.Expect(rs.Status.ObservedGeneration).To(Equal(rs.GetGeneration()))

			ready := rs.Status.Conditions.GetConditionByType(meta.ReadyCondition)
			g.Expect(ready).NotTo(BeNil())
			g.Expect(ready.Status).To(Equal(metav1.ConditionTrue))

			var got []serviceTypeDenial

			for _, body := range rs.Status.Rules {
				if body != nil && body.Enforce != nil {
					got = append(got, serviceTypeDenial{action: body.Enforce.Action, types: body.Enforce.Services.Types})
				}
			}

			g.Expect(got).To(Equal(want))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	}

	canCreateServices := func(owner rbac.UserSpec, namespace string) error {
		review, err := ownerClient(owner).AuthorizationV1().SelfSubjectAccessReviews().Create(ctx,
			&authorizationv1.SelfSubjectAccessReview{
				Spec: authorizationv1.SelfSubjectAccessReviewSpec{
					ResourceAttributes: &authorizationv1.ResourceAttributes{
						Namespace: namespace,
						Verb:      "create",
						Resource:  "services",
					},
				},
			}, metav1.CreateOptions{})
		if err != nil {
			return err
		}

		if !review.Status.Allowed {
			return fmt.Errorf("%s cannot create Services in %s yet: %s", owner.Name, namespace, review.Status.Reason)
		}

		return nil
	}

	// expectOwnerRBAC waits for the owner RoleBinding of a namespace, so that a
	// later denial comes from Capsule and not from RBAC.
	expectOwnerRBAC := func(owner rbac.UserSpec, namespace string) {
		Eventually(func() error {
			return canCreateServices(owner, namespace)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	}

	// grantServiceCreation binds the owners to Service creation in every
	// namespace. The owner RoleBinding of a new namespace comes from the
	// tenant controller after the namespace exists; with this binding the
	// owners write their first object while the controller caches may not
	// have observed the namespace, its RuleStatus or its RoleBindings yet.
	// Capsule's decision does not depend on RBAC: the rules have no audience.
	grantServiceCreation := func(owners ...rbac.UserSpec) {
		labels := map[string]string{"env": "e2e", e2eRunLabel: prefix}

		role := &rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{Name: prefix + "-services", Labels: labels},
			Rules: []rbacv1.PolicyRule{{
				APIGroups: []string{""},
				Resources: []string{"services"},
				Verbs:     []string{"create"},
			}},
		}
		Expect(k8sClient.Create(ctx, role)).To(Succeed())
		DeferCleanup(DeleteInBackground, role)

		binding := &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: prefix + "-services", Labels: labels},
			RoleRef: rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName,
				Kind:     "ClusterRole",
				Name:     role.GetName(),
			},
		}

		for _, owner := range owners {
			binding.Subjects = append(binding.Subjects, rbacv1.Subject{
				APIGroup: rbacv1.GroupName,
				Kind:     rbacv1.UserKind,
				Name:     owner.Name,
			})
		}

		Expect(k8sClient.Create(ctx, binding)).To(Succeed())
		DeferCleanup(DeleteInBackground, binding)

		for _, owner := range owners {
			expectOwnerRBAC(owner, prefix+"-not-created-yet")
		}
	}

	newService := func(name string, serviceType corev1.ServiceType) *corev1.Service {
		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:   name,
				Labels: map[string]string{"env": "e2e"},
			},
			Spec: corev1.ServiceSpec{Type: serviceType},
		}

		if serviceType == corev1.ServiceTypeExternalName {
			svc.Spec.ExternalName = "backend.example.com"

			return svc
		}

		svc.Spec.Ports = []corev1.ServicePort{{
			Name:       "http",
			Protocol:   corev1.ProtocolTCP,
			Port:       8080,
			TargetPort: intstr.FromInt32(8080),
		}}

		return svc
	}

	ruleDenial := func(serviceType corev1.ServiceType) string {
		return fmt.Sprintf("service type %q at spec.type is denied by namespace rule", serviceType)
	}

	isRuleDenial := func(err error, serviceType corev1.ServiceType) bool {
		return apierrors.IsForbidden(err) && strings.Contains(err.Error(), ruleDenial(serviceType))
	}

	hasTenantLabels := func(labels map[string]string, tnt *capsulev1beta2.Tenant) bool {
		return labels[meta.NewTenantLabel] == tnt.GetName() && labels[meta.ManagedByCapsuleLabel] == tnt.GetName()
	}

	// expectAdmitted creates a Service once: the request must be admitted, and
	// the stored Service must carry the metadata labels of the Tenant.
	expectAdmitted := func(cs kubernetes.Interface, namespace string, tnt *capsulev1beta2.Tenant, svc *corev1.Service) {
		created, err := cs.CoreV1().Services(namespace).Create(ctx, svc, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), "%s Service %s/%s must be admitted", svc.Spec.Type, namespace, svc.GetName())

		Eventually(func(g Gomega) {
			stored := &corev1.Service{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: svc.GetName()}, stored)).To(Succeed())
			g.Expect(stored.GetUID()).To(Equal(created.GetUID()))
			g.Expect(stored.GetLabels()).To(HaveKeyWithValue(meta.NewTenantLabel, tnt.GetName()))
			g.Expect(stored.GetLabels()).To(HaveKeyWithValue(meta.ManagedByCapsuleLabel, tnt.GetName()))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	}

	// expectDenied creates a Service once: the request must be denied by the
	// namespace rule, and nothing may be stored.
	expectDenied := func(cs kubernetes.Interface, namespace string, svc *corev1.Service) {
		_, err := cs.CoreV1().Services(namespace).Create(ctx, svc, metav1.CreateOptions{})
		if err == nil {
			Fail(fmt.Sprintf("%s Service %s/%s was admitted, want a denial by the namespace rule",
				svc.Spec.Type, namespace, svc.GetName()))
		}

		Expect(apierrors.IsForbidden(err)).To(BeTrue(), "unexpected error: %v", err)
		Expect(err.Error()).To(ContainSubstring(ruleDenial(svc.Spec.Type)))

		err = k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: svc.GetName()}, &corev1.Service{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "denied Service %s/%s must not exist: %v", namespace, svc.GetName(), err)
	}

	type decision struct {
		cs          kubernetes.Interface
		namespace   string
		tenant      *capsulev1beta2.Tenant
		serviceType corev1.ServiceType
		admitted    bool
	}

	// check sends a dry-run create, so a probe stores nothing even when the
	// answer is not the wanted one yet.
	check := func(d decision) error {
		svc := newService(prefix+"-probe-"+strings.ToLower(string(d.serviceType)), d.serviceType)

		got, err := d.cs.CoreV1().Services(d.namespace).Create(ctx, svc, metav1.CreateOptions{
			DryRun: []string{metav1.DryRunAll},
		})

		switch {
		case d.admitted && err != nil:
			return fmt.Errorf("%s Service denied in %s, want it admitted: %w", d.serviceType, d.namespace, err)
		case d.admitted && !hasTenantLabels(got.GetLabels(), d.tenant):
			return fmt.Errorf("%s Service in %s admitted with labels %v, want the labels of Tenant %s",
				d.serviceType, d.namespace, got.GetLabels(), d.tenant.GetName())
		case d.admitted:
			return nil
		case err == nil:
			return fmt.Errorf("%s Service admitted in %s, want a denial by the namespace rule", d.serviceType, d.namespace)
		case !isRuleDenial(err, d.serviceType):
			return fmt.Errorf("%s Service in %s not denied by the namespace rule: %w", d.serviceType, d.namespace, err)
		}

		return nil
	}

	checkAll := func(decisions []decision) error {
		for _, d := range decisions {
			if err := check(d); err != nil {
				return err
			}
		}

		return nil
	}

	// expectDecisionsAfterLag waits, within cacheLagTolerance, for every
	// decision to follow the new state, then requires them to hold.
	expectDecisionsAfterLag := func(decisions ...decision) {
		Eventually(func() error {
			return checkAll(decisions)
		}, cacheLagTolerance, defaultPollInterval).Should(Succeed())

		Consistently(func() error {
			return checkAll(decisions)
		}, cacheLagSettle, defaultPollInterval).Should(Succeed())
	}

	It("enforces the Tenant rules in namespaces that exist for a while", func() {
		nsA := createNamespace(tenantA, ownerA, "", nil)
		nsStrict := createNamespace(tenantA, ownerA, "", strictProfile)
		nsB := createNamespace(tenantB, ownerB, "", nil)

		// The RuleStatus and the RoleBindings come from the controllers, which
		// read the same informers as the webhook: once they exist, the webhook
		// caches have observed the namespaces too.
		expectRuleStatus(nsA, denial(rules.ServiceTypeNodePort))
		expectRuleStatus(nsStrict, denial(rules.ServiceTypeNodePort), denial(rules.ServiceTypeExternalName))
		expectRuleStatus(nsB, denial(rules.ServiceTypeExternalName))
		expectOwnerRBAC(ownerA, nsA)
		expectOwnerRBAC(ownerA, nsStrict)
		expectOwnerRBAC(ownerB, nsB)

		csA := ownerClient(ownerA)
		csB := ownerClient(ownerB)

		By("admitting what the rules of each namespace allow, with the metadata of its Tenant")
		expectAdmitted(csA, nsA, tenantA, newService("cluster-ip", corev1.ServiceTypeClusterIP))
		expectAdmitted(csA, nsA, tenantA, newService("external-name", corev1.ServiceTypeExternalName))
		expectAdmitted(csA, nsStrict, tenantA, newService("cluster-ip", corev1.ServiceTypeClusterIP))
		expectAdmitted(csB, nsB, tenantB, newService("node-port", corev1.ServiceTypeNodePort))

		By("denying what the rules of each namespace deny")
		expectDenied(csA, nsA, newService("node-port", corev1.ServiceTypeNodePort))
		expectDenied(csA, nsStrict, newService("external-name", corev1.ServiceTypeExternalName))
		expectDenied(csB, nsB, newService("external-name", corev1.ServiceTypeExternalName))

		By("keeping the namespaces of Tenant B out of reach of the owner of Tenant A")
		_, err := csA.CoreV1().Services(nsB).Create(ctx, newService("cross-tenant", corev1.ServiceTypeClusterIP), metav1.CreateOptions{})
		Expect(apierrors.IsForbidden(err)).To(BeTrue(), "unexpected error: %v", err)
		Expect(err.Error()).To(ContainSubstring(`cannot create resource "services"`))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: nsB, Name: "cross-tenant"}, &corev1.Service{}))).To(BeTrue())
	})

	Context("in a namespace created a moment ago", func() {
		BeforeEach(func() {
			grantServiceCreation(ownerA, ownerB)
		})

		// firstWriters is the number of concurrent first writers per namespace.
		const firstWriters = 4

		// raceFirstServices starts writers that create Services of one type in
		// a namespace that does not exist yet, then creates the namespace. A
		// writer retries only while the API server answers that the namespace
		// does not exist, and returns its first other answer, by Service name.
		// Those answers come while the webhook informers are the least likely
		// to hold the namespace, and before its RuleStatus exists: the handlers
		// must resolve the Tenant from the API reader and render the rules from
		// the namespace snapshot.
		raceFirstServices := func(
			cs kubernetes.Interface,
			tnt *capsulev1beta2.Tenant,
			owner rbac.UserSpec,
			namespace string,
			labels map[string]string,
			serviceType corev1.ServiceType,
		) map[string]error {
			type answer struct {
				service string
				err     error
			}

			answers := make(chan answer, firstWriters)
			deadline := time.Now().Add(defaultTimeoutInterval)
			// The answer of the NamespaceLifecycle admission plugin, not of a webhook.
			missing := fmt.Sprintf("namespaces %q not found", namespace)

			for writer := range firstWriters {
				go func() {
					defer GinkgoRecover()

					for attempt := 0; ; attempt++ {
						svc := newService(fmt.Sprintf("first-%d-%d", writer, attempt), serviceType)

						_, err := cs.CoreV1().Services(namespace).Create(ctx, svc, metav1.CreateOptions{})
						if apierrors.IsNotFound(err) && strings.Contains(err.Error(), missing) &&
							!strings.Contains(err.Error(), "admission webhook") && time.Now().Before(deadline) {
							continue
						}

						answers <- answer{service: svc.GetName(), err: err}

						return
					}
				}()
			}

			createNamespace(tnt, owner, namespace, labels)

			got := make(map[string]error, firstWriters)
			for range firstWriters {
				a := <-answers
				got[a.service] = a.err
			}

			return got
		}

		// No retry past the namespace lookup: every first answer must be right.
		expectFirstDenied := func(namespace string, serviceType corev1.ServiceType, answers map[string]error) {
			for name, err := range answers {
				if err == nil {
					Fail(fmt.Sprintf("first %s Service %s/%s was admitted, want a denial by the namespace rule",
						serviceType, namespace, name))
				}

				Expect(isRuleDenial(err, serviceType)).To(BeTrue(), "first %s Service %s/%s: %v", serviceType, namespace, name, err)
				Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &corev1.Service{}))).To(BeTrue())
			}
		}

		expectFirstAdmitted := func(namespace string, tnt *capsulev1beta2.Tenant, serviceType corev1.ServiceType, answers map[string]error) {
			for name, err := range answers {
				Expect(err).NotTo(HaveOccurred(), "first %s Service %s/%s must be admitted", serviceType, namespace, name)

				stored := &corev1.Service{}
				Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, stored)).To(Succeed())
				Expect(stored.GetLabels()).To(HaveKeyWithValue(meta.NewTenantLabel, tnt.GetName()))
				Expect(stored.GetLabels()).To(HaveKeyWithValue(meta.ManagedByCapsuleLabel, tnt.GetName()))
			}
		}

		It("judges the first objects by the rules of the Tenant and labels them for the Tenant", func() {
			csA := ownerClient(ownerA)
			csB := ownerClient(ownerB)

			By("denying disallowed first objects")
			nsDenied := prefix + "-denied"
			expectFirstDenied(nsDenied, corev1.ServiceTypeNodePort,
				raceFirstServices(csA, tenantA, ownerA, nsDenied, nil, corev1.ServiceTypeNodePort))
			expectAdmitted(csA, nsDenied, tenantA, newService("cluster-ip", corev1.ServiceTypeClusterIP))

			By("admitting and labelling allowed first objects")
			nsAdmitted := prefix + "-admitted"
			expectFirstAdmitted(nsAdmitted, tenantA, corev1.ServiceTypeClusterIP,
				raceFirstServices(csA, tenantA, ownerA, nsAdmitted, nil, corev1.ServiceTypeClusterIP))
			expectDenied(csA, nsAdmitted, newService("node-port", corev1.ServiceTypeNodePort))

			By("selecting the rules by the labels of the new namespace")
			nsStrict := prefix + "-strict"
			expectFirstDenied(nsStrict, corev1.ServiceTypeExternalName,
				raceFirstServices(csA, tenantA, ownerA, nsStrict, strictProfile, corev1.ServiceTypeExternalName))

			By("judging the first objects of a new namespace of Tenant B by Tenant B only")
			nsB := prefix + "-b-first"
			expectFirstAdmitted(nsB, tenantB, corev1.ServiceTypeNodePort,
				raceFirstServices(csB, tenantB, ownerB, nsB, nil, corev1.ServiceTypeNodePort))
			expectDenied(csB, nsB, newService("external-name", corev1.ServiceTypeExternalName))

			By("publishing the same rules in the RuleStatus afterwards")
			expectRuleStatus(nsDenied, denial(rules.ServiceTypeNodePort))
			expectRuleStatus(nsStrict, denial(rules.ServiceTypeNodePort), denial(rules.ServiceTypeExternalName))
			expectRuleStatus(nsB, denial(rules.ServiceTypeExternalName))
		})
	})

	It("judges objects by the new Tenant rules once the RuleStatus observes them", func() {
		nsA := createNamespace(tenantA, ownerA, "", nil)
		nsB := createNamespace(tenantB, ownerB, "", nil)

		expectRuleStatus(nsA, denial(rules.ServiceTypeNodePort))
		expectRuleStatus(nsB, denial(rules.ServiceTypeExternalName))
		expectOwnerRBAC(ownerA, nsA)
		expectOwnerRBAC(ownerB, nsB)

		csA := ownerClient(ownerA)
		csB := ownerClient(ownerB)

		By("enforcing the rules before the change")
		expectDenied(csA, nsA, newService("before-node-port", corev1.ServiceTypeNodePort))
		Expect(check(decision{csA, nsA, tenantA, corev1.ServiceTypeLoadBalancer, true})).To(Succeed())
		Expect(check(decision{csB, nsB, tenantB, corev1.ServiceTypeLoadBalancer, true})).To(Succeed())

		By("replacing the NodePort rule of Tenant A with a LoadBalancer rule")
		UpdateTenantEventually(tenantA, func(current *capsulev1beta2.Tenant) {
			current.Spec.Rules = []*rules.NamespaceRuleBodyTenant{
				denyServiceTypes(nil, rules.ServiceTypeLoadBalancer),
				denyServiceTypes(strictProfile, rules.ServiceTypeExternalName),
			}
		})
		expectRuleStatus(nsA, denial(rules.ServiceTypeLoadBalancer))

		// The test reads the RuleStatus from the API server; the webhook caches
		// observe it after their watch delay (cacheLagTolerance).
		By("judging by the new rules within the cache lag tolerance, in Tenant A only")
		expectDecisionsAfterLag(
			decision{csA, nsA, tenantA, corev1.ServiceTypeNodePort, true},
			decision{csA, nsA, tenantA, corev1.ServiceTypeLoadBalancer, false},
			decision{csA, nsA, tenantA, corev1.ServiceTypeExternalName, true},
			decision{csB, nsB, tenantB, corev1.ServiceTypeNodePort, true},
			decision{csB, nsB, tenantB, corev1.ServiceTypeLoadBalancer, true},
			decision{csB, nsB, tenantB, corev1.ServiceTypeExternalName, false},
		)

		By("storing only what the new rules admit")
		expectAdmitted(csA, nsA, tenantA, newService("after-node-port", corev1.ServiceTypeNodePort))
		expectDenied(csA, nsA, newService("after-load-balancer", corev1.ServiceTypeLoadBalancer))
	})

	It("judges objects in a namespace recreated under the same name by its new Tenant", func() {
		// Owner B writes to the recreated namespace before the tenant controller
		// binds it there, so the probes start with the recreation.
		grantServiceCreation(ownerB)

		name := createNamespace(tenantA, ownerA, "", nil)
		expectRuleStatus(name, denial(rules.ServiceTypeNodePort))
		expectOwnerRBAC(ownerA, name)

		csA := ownerClient(ownerA)
		csB := ownerClient(ownerB)

		By("judging the namespace by Tenant A, which fills the webhook caches for its name")
		expectDenied(csA, name, newService("old-node-port", corev1.ServiceTypeNodePort))
		expectAdmitted(csA, name, tenantA, newService("old-external-name", corev1.ServiceTypeExternalName))

		By("deleting the namespace and recreating it under the same name in Tenant B")
		// The name is reused: wait for the namespace to be gone.
		ForceDeleteNamespace(ctx, name)
		createNamespace(tenantB, ownerB, name, nil)

		// A decision taken from Tenant A must not outlast the watch delay of
		// the webhook caches (cacheLagTolerance) after the recreation.
		By("judging by Tenant B within the cache lag tolerance")
		expectDecisionsAfterLag(
			decision{csB, name, tenantB, corev1.ServiceTypeNodePort, true},
			decision{csB, name, tenantB, corev1.ServiceTypeExternalName, false},
		)

		By("storing objects with the metadata of Tenant B")
		expectAdmitted(csB, name, tenantB, newService("new-node-port", corev1.ServiceTypeNodePort))
		expectDenied(csB, name, newService("new-external-name", corev1.ServiceTypeExternalName))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: name, Name: "old-external-name"}, &corev1.Service{}))).To(BeTrue())

		NamespaceIsPartOfTenant(tenantB, NewNamespace(name)).Should(Succeed())
		NamespaceIsNotPartOfTenant(tenantA, NewNamespace(name)).Should(Succeed())
		expectRuleStatus(name, denial(rules.ServiceTypeExternalName))
	})
})
