// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
)

const healthStateAnnotation = "projectcapsule.dev/health-state"

func replicationHealthObject(parent client.Object) (client.Object, *capsulev1beta2.TenantResourceCommonStatus) {
	GinkgoHelper()
	if _, ok := parent.(*capsulev1beta2.TenantResource); ok {
		fresh := &capsulev1beta2.TenantResource{}
		Expect(k8sClient.Get(context.Background(), client.ObjectKeyFromObject(parent), fresh)).To(Succeed())
		return fresh, &fresh.Status.TenantResourceCommonStatus
	}
	fresh := &capsulev1beta2.GlobalTenantResource{}
	Expect(k8sClient.Get(context.Background(), client.ObjectKeyFromObject(parent), fresh)).To(Succeed())
	return fresh, &fresh.Status.TenantResourceCommonStatus
}

func expectReplicationHealth(parent client.Object, state metav1.ConditionStatus, reason string) *capsulev1beta2.TenantResourceCommonStatus {
	GinkgoHelper()
	var result *capsulev1beta2.TenantResourceCommonStatus
	Eventually(func(g Gomega) {
		fresh, status := replicationHealthObject(parent)
		result = status
		g.Expect(status.ObservedGeneration).To(Equal(fresh.GetGeneration()))
		condition := status.Conditions.GetConditionByType(meta.HealthyCondition)
		g.Expect(condition).NotTo(BeNil())
		g.Expect(condition.ObservedGeneration).To(Equal(fresh.GetGeneration()))
		g.Expect(condition.Status).To(Equal(state), fmt.Sprint(status.Conditions))
		if reason != "" {
			g.Expect(condition.Reason).To(Equal(reason))
		}
		g.Expect(meta.IsStatusConditionTrue(status.Conditions, meta.ReadyCondition)).To(Equal(state == metav1.ConditionTrue))
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	return result
}

func replicationHealthMetric(ctx context.Context, cs kubernetes.Interface, parent client.Object) (float64, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	lease := &coordinationv1.Lease{}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: ControllerNamespace, Name: "replications.projectcapsule.dev"}, lease); err != nil {
		return 0, false, err
	}
	if lease.Spec.HolderIdentity == nil {
		return 0, false, fmt.Errorf("replication leader is not elected")
	}
	pod, _, found := strings.Cut(*lease.Spec.HolderIdentity, "_")
	if !found {
		return 0, false, fmt.Errorf("replication leader does not identify a pod: %s", *lease.Spec.HolderIdentity)
	}
	payload, err := cs.CoreV1().Pods(ControllerNamespace).ProxyGet("http", pod, "8080", "metrics", nil).DoRaw(ctx)
	if err != nil {
		return 0, false, err
	}
	family := "capsule_global_resource_condition"
	if parent.GetNamespace() != "" {
		family = "capsule_resource_condition"
	}
	for line := range strings.SplitSeq(string(payload), "\n") {
		if !strings.HasPrefix(line, family+"{") || !strings.Contains(line, `condition="Healthy"`) || !strings.Contains(line, fmt.Sprintf("name=%q", parent.GetName())) {
			continue
		}
		if parent.GetNamespace() != "" && !strings.Contains(line, fmt.Sprintf("target_namespace=%q", parent.GetNamespace())) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0, false, fmt.Errorf("invalid condition metric: %s", line)
		}
		value, err := strconv.ParseFloat(fields[1], 64)
		return value, true, err
	}
	return 0, false, nil
}

var _ = Describe("Replication health observations", Label("tenant", "replications", "healthchecks"), func() {
	DescribeTable("prunes targets when the source spec changes during application", Label("inventory"), func(global bool) {
		ctx := context.Background()
		prefix := "health-inventory-" + rand.String(5)
		owners, namespaces := watchNamespaces(prefix)
		actor := impersonationClient(owners[0].Name, withDefaultGroups(nil))
		ns := namespaces[0].Name
		spec := capsulev1beta2.TenantResourceCommonSpec{
			Cordoned: new(true), ResyncPeriod: metav1.Duration{Duration: 24 * time.Hour},
			HealthChecks: &capsulev1beta2.TenantResourceHealthChecks{Wait: true},
			Resources: []capsulev1beta2.ResourceSpec{{
				NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": ns}},
				Policy:            &apiruntime.ResourceReplicationPolicy{Protect: new(false), Creation: apiruntime.ResourceCreationPolicyOwner, Deletion: apiruntime.ResourceDeletionPolicyRemove},
			}},
		}
		// A real watch catches the first target before the batch finishes. The
		// assertion below ensures this exercises an in-flight application.
		for i := range 96 {
			spec.Resources[0].RawItems = append(spec.Resources[0].RawItems, capsulev1beta2.RawExtension{Raw: []byte(fmt.Sprintf(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":%q,"labels":{"projectcapsule.dev/inventory-race":%q}}}`, fmt.Sprintf("%s-%d", prefix, i), prefix))})
		}
		var parent client.Object
		writer := actor
		if global {
			parent = &capsulev1beta2.GlobalTenantResource{Name: prefix, Spec: capsulev1beta2.GlobalTenantResourceSpec{TenantResourceCommonSpec: spec, Scope: api.ResourceScopeNamespace, TenantSelector: metav1.LabelSelector{MatchLabels: map[string]string{"projectcapsule.dev/watch-tenant": "0" + prefix}}, ServiceAccount: &meta.NamespacedRFC1123ObjectReferenceWithNamespace{Name: "default", Namespace: meta.RFC1123SubdomainName(ns)}}}
			writer = k8sClient
		} else {
			parent = &capsulev1beta2.TenantResource{Name: prefix, Namespace: ns, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: spec, ServiceAccount: &meta.LocalRFC1123ObjectReference{Name: "default"}}}
		}
		Expect(writer.Create(ctx, parent)).To(Succeed())
		DeferCleanup(DeleteInBackground, parent)
		Eventually(func(g Gomega) {
			fresh, status := replicationHealthObject(parent)
			g.Expect(status.ObservedGeneration).To(Equal(fresh.GetGeneration()))
			g.Expect(meta.IsStatusConditionTrue(status.Conditions, meta.CordonedCondition)).To(BeTrue())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		cs := clusterAdminClient()
		stream, err := cs.CoreV1().ConfigMaps(ns).Watch(ctx, metav1.ListOptions{LabelSelector: "projectcapsule.dev/inventory-race=" + prefix})
		Expect(err).NotTo(HaveOccurred())
		defer stream.Stop()
		update := func(remove bool) error {
			fresh := parent.DeepCopyObject().(client.Object)
			if err := writer.Get(ctx, client.ObjectKeyFromObject(parent), fresh); err != nil {
				return err
			}
			switch obj := fresh.(type) {
			case *capsulev1beta2.TenantResource:
				obj.Spec.Cordoned = new(false)
				if remove {
					obj.Spec.Resources = []capsulev1beta2.ResourceSpec{}
				}
			case *capsulev1beta2.GlobalTenantResource:
				obj.Spec.Cordoned = new(false)
				if remove {
					obj.Spec.Resources = []capsulev1beta2.ResourceSpec{}
				}
			}
			return writer.Update(ctx, fresh)
		}
		Eventually(func() error { return update(false) }, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(stream.ResultChan(), defaultTimeoutInterval).Should(Receive(HaveField("Type", Equal(watch.Added))))
		_, applying := replicationHealthObject(parent)
		Expect(applying.ProcessedItems).To(BeEmpty(), "spec must change before the first application publishes inventory")
		Eventually(func() error { return update(true) }, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		status := expectReplicationHealth(parent, metav1.ConditionTrue, "Succeeded")
		Expect(status.ProcessedItems).To(BeEmpty())
		Expect(status.Size).To(BeZero())
		Eventually(func(g Gomega) {
			for _, namespace := range namespaces {
				list := &corev1.ConfigMapList{}
				g.Expect(k8sClient.List(ctx, list, client.InNamespace(namespace.Name), client.MatchingLabels{"projectcapsule.dev/inventory-race": prefix})).To(Succeed())
				g.Expect(list.Items).To(BeEmpty())
			}
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Expect(apierrors.IsForbidden(actor.Get(ctx, client.ObjectKey{Namespace: namespaces[2].Name, Name: prefix + "-0"}, &corev1.ConfigMap{}))).To(BeTrue())
		Eventually(func(g Gomega) {
			value, present, err := replicationHealthMetric(ctx, cs, parent)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(present).To(BeTrue())
			g.Expect(value).To(Equal(float64(1)))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	}, Entry("TenantResource", false), Entry("GlobalTenantResource", true))

	DescribeTable("keeps health retries separate from application, profiles and dependency gating", func(global bool) {
		ctx := context.Background()
		prefix := "health-" + rand.String(6)
		owners, namespaces := watchNamespaces(prefix)
		actor := impersonationClient(owners[0].Name, withDefaultGroups(nil))
		sourceNamespace := namespaces[0].Name
		bindServiceAccountToTenantResourceManager(sourceNamespace, "default", namespaces[1].Name)
		if global {
			watchRole := prefix + "-watch"
			bindServiceAccountToClusterResources(sourceNamespace, "default", watchRole, watchRole, []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"configmaps"}, Verbs: []string{"list", "watch"}}})
			DeferCleanup(DeleteInBackground, &rbacv1.ClusterRole{Name: watchRole})
			DeferCleanup(DeleteInBackground, &rbacv1.ClusterRoleBinding{Name: watchRole})
		}
		source := &corev1.ConfigMap{Name: prefix, Namespace: sourceNamespace, Labels: map[string]string{watchSourceLabel: prefix, watchInputLabel: prefix}, Data: map[string]string{"value": "initial"}}
		Expect(actor.Create(ctx, source)).To(Succeed())
		spec := watchBody(prefix, sourceNamespace, prefix+"-output")
		spec.HealthChecks = &capsulev1beta2.TenantResourceHealthChecks{Wait: true, Timeout: &metav1.Duration{Duration: time.Minute}}
		spec.Resources[0].NamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"projectcapsule.dev/watch-profile": "selected"}}
		spec.Resources[0].Policy = &apiruntime.ResourceReplicationPolicy{Protect: new(false), Creation: apiruntime.ResourceCreationPolicyOwner, Deletion: apiruntime.ResourceDeletionPolicyRemove}
		spec.HealthChecks.Expressions = []capsulev1beta2.TenantResourceHealthCheckExpression{{APIVersion: "v1", Kind: "ConfigMap", Current: `has(metadata.annotations) && 'projectcapsule.dev/health-state' in metadata.annotations && metadata.annotations['projectcapsule.dev/health-state'] == 'healthy'`, Failed: `has(metadata.annotations) && 'projectcapsule.dev/health-state' in metadata.annotations && metadata.annotations['projectcapsule.dev/health-state'] == 'failed'`}}
		create := func(name string, common capsulev1beta2.TenantResourceCommonSpec) client.Object {
			if global {
				parent := &capsulev1beta2.GlobalTenantResource{Name: name, Spec: capsulev1beta2.GlobalTenantResourceSpec{TenantResourceCommonSpec: common, Scope: api.ResourceScopeNamespace, TenantSelector: metav1.LabelSelector{MatchLabels: map[string]string{"projectcapsule.dev/watch-tenant": "0" + prefix}}, ServiceAccount: &meta.NamespacedRFC1123ObjectReferenceWithNamespace{Name: "default", Namespace: meta.RFC1123SubdomainName(sourceNamespace)}}}
				if len(parent.Spec.Triggers) > 0 {
					parent.Spec.Triggers[0].NamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": sourceNamespace}}
				}
				Expect(k8sClient.Create(ctx, parent)).To(Succeed())
				DeferCleanup(DeleteInBackground, parent)
				return parent
			}
			parent := &capsulev1beta2.TenantResource{Name: name, Namespace: sourceNamespace, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: common, ServiceAccount: &meta.LocalRFC1123ObjectReference{Name: "default"}}}
			Expect(actor.Create(ctx, parent)).To(Succeed())
			return parent
		}
		parent := create(prefix, spec)
		metricClient := clusterAdminClient()
		expectMetric := func(want *float64) {
			Eventually(func(g Gomega) {
				value, found, err := replicationHealthMetric(ctx, metricClient, parent)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(found).To(Equal(want != nil))
				if want != nil {
					g.Expect(value).To(Equal(*want))
				}
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		pending := expectReplicationHealth(parent, metav1.ConditionUnknown, "HealthChecking")
		expectMetric(new(float64(0)))
		Expect(pending.Health).NotTo(BeNil())
		Expect(pending.ProcessedItems).To(HaveLen(1))
		Expect(pending.ProcessedItems[0].Health).NotTo(BeNil())
		applied := pending.ProcessedItems[0].LastApply
		deadline := pending.Health.Deadline
		dependencySpec := capsulev1beta2.TenantResourceCommonSpec{DependsOn: []meta.LocalRFC1123ObjectReference{{Name: meta.RFC1123Name(prefix)}}, ResyncPeriod: metav1.Duration{Duration: 24 * time.Hour}, Resources: []capsulev1beta2.ResourceSpec{{NamespaceSelector: spec.Resources[0].NamespaceSelector.DeepCopy(), RawItems: []capsulev1beta2.RawExtension{{Raw: []byte(fmt.Sprintf(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":%q},"data":{"value":"dependent"}}`, prefix+"-dependent"))}}}}}
		dependent := create(prefix+"-dependent", dependencySpec)
		Eventually(func(g Gomega) {
			_, status := replicationHealthObject(dependent)
			ready := status.Conditions.GetConditionByType(meta.ReadyCondition)
			g.Expect(ready).NotTo(BeNil())
			g.Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			g.Expect(ready.Message).To(ContainSubstring("not ready"))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: sourceNamespace, Name: prefix + "-dependent"}, &corev1.ConfigMap{}))).To(BeTrue())
		setState := func(namespace, state string) {
			Eventually(func() error {
				fresh := &corev1.ConfigMap{}
				if err := actor.Get(ctx, client.ObjectKey{Namespace: namespace, Name: prefix + "-output"}, fresh); err != nil {
					return err
				}
				if fresh.Annotations == nil {
					fresh.Annotations = map[string]string{}
				}
				fresh.Annotations[healthStateAnnotation] = state
				return actor.Update(ctx, fresh)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		By("reporting failed health and recovering without another apply")
		setState(sourceNamespace, "failed")
		expectReplicationHealth(parent, metav1.ConditionFalse, "HealthCheckFailed")
		expectMetric(new(float64(0)))
		setState(sourceNamespace, "healthy")
		healthy := expectReplicationHealth(parent, metav1.ConditionTrue, "Succeeded")
		expectMetric(new(float64(1)))
		Expect(healthy.ProcessedItems[0].LastApply).To(Equal(applied))
		Expect(healthy.Health.Deadline).To(Equal(deadline))
		waitWatchReady(dependent)
		By("keeping excluded namespace profiles and the second tenant untouched")
		for _, namespace := range namespaces[1:] {
			Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace.Name, Name: prefix + "-output"}, &corev1.ConfigMap{}))).To(BeTrue())
		}
		err := actor.Get(ctx, client.ObjectKey{Namespace: namespaces[2].Name, Name: prefix + "-output"}, &corev1.ConfigMap{})
		Expect(apierrors.IsForbidden(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("forbidden"))
		By("including a newly selected namespace in aggregate health")
		Eventually(func() error {
			fresh := &corev1.Namespace{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(namespaces[1]), fresh); err != nil {
				return err
			}
			fresh.Labels["projectcapsule.dev/watch-profile"] = "selected"
			return k8sClient.Update(ctx, fresh)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		expectConfigMapData(namespaces[1].Name, prefix+"-output", map[string]string{"value": "initial"})
		expectReplicationHealth(parent, metav1.ConditionUnknown, "HealthChecking")
		setState(namespaces[1].Name, "healthy")
		healthy = expectReplicationHealth(parent, metav1.ConditionTrue, "Succeeded")
		Expect(healthy.ProcessedItems).To(HaveLen(2))
		By("starting a new rollout for source-triggered content without a parent generation change")
		before, _ := replicationHealthObject(parent)
		revision := healthy.Health.Revision
		beforeToken := watchToken(parent)
		Eventually(func(g Gomega) {
			input := &corev1.ConfigMap{}
			g.Expect(actor.Get(ctx, client.ObjectKeyFromObject(source), input)).To(Succeed())
			input.Data["value"] = "updated"
			input.Data["marker"] = rand.String(8)
			g.Expect(actor.Update(ctx, input)).To(Succeed())
			fresh, status := replicationHealthObject(parent)
			g.Expect(fresh.GetGeneration()).To(Equal(before.GetGeneration()))
			g.Expect(status.Health).NotTo(BeNil())
			g.Expect(status.Health.Revision).NotTo(Equal(revision))
			g.Expect(status.Trigger).NotTo(BeNil())
			g.Expect(status.Trigger.ObservedRequest).NotTo(BeEmpty())
			g.Expect(status.Trigger.ObservedRequest).NotTo(Equal(beforeToken))
			g.Expect(status.Trigger.ObservedRequest).To(Equal(fresh.GetAnnotations()[meta.ReplicationTriggerAnnotation]))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		expectConfigMapData(sourceNamespace, prefix+"-output", map[string]string{"value": "updated"})
		expectReplicationHealth(parent, metav1.ConditionTrue, "Succeeded")
		By("removing the Healthy metric when waiting is disabled")
		writer := actor
		if global {
			writer = k8sClient
		}
		Eventually(func() error {
			fresh := parent.DeepCopyObject().(client.Object)
			if err := writer.Get(ctx, client.ObjectKeyFromObject(parent), fresh); err != nil {
				return err
			}
			switch source := fresh.(type) {
			case *capsulev1beta2.TenantResource:
				source.Spec.HealthChecks.Wait = false
			case *capsulev1beta2.GlobalTenantResource:
				source.Spec.HealthChecks.Wait = false
			}
			return writer.Update(ctx, fresh)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		waitWatchReady(parent)
		expectMetric(nil)
	}, Entry("TenantResource", false), Entry("GlobalTenantResource", true))

	It("retains the timeout across repeated applies and recovers through health-only retries", func() {
		ctx := context.Background()
		prefix := "health-timeout-" + rand.String(5)
		owners, namespaces := watchNamespaces(prefix)
		actor := impersonationClient(owners[0].Name, withDefaultGroups(nil))
		parent := &capsulev1beta2.TenantResource{Name: prefix, Namespace: namespaces[0].Name, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{HealthChecks: &capsulev1beta2.TenantResourceHealthChecks{Wait: true, Timeout: &metav1.Duration{Duration: 5 * time.Second}, Expressions: []capsulev1beta2.TenantResourceHealthCheckExpression{{APIVersion: "v1", Kind: "ConfigMap", Current: `has(metadata.annotations) && 'projectcapsule.dev/health-state' in metadata.annotations`}}}, ResyncPeriod: metav1.Duration{Duration: time.Second}, Resources: []capsulev1beta2.ResourceSpec{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": namespaces[0].Name}}, Policy: &apiruntime.ResourceReplicationPolicy{Protect: new(false)}, RawItems: []capsulev1beta2.RawExtension{{Raw: []byte(fmt.Sprintf(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":%q}}`, prefix))}}}}}}}
		Expect(actor.Create(ctx, parent)).To(Succeed())
		pending := expectReplicationHealth(parent, metav1.ConditionUnknown, "HealthChecking")
		deadline := pending.Health.Deadline
		failed := expectReplicationHealth(parent, metav1.ConditionFalse, "HealthCheckTimeout")
		Expect(failed.Health.Deadline).To(Equal(deadline))
		Eventually(func() error {
			fresh := &corev1.ConfigMap{}
			if err := actor.Get(ctx, client.ObjectKey{Namespace: parent.Namespace, Name: prefix}, fresh); err != nil {
				return err
			}
			fresh.Annotations = map[string]string{healthStateAnnotation: "healthy"}
			return actor.Update(ctx, fresh)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		expectReplicationHealth(parent, metav1.ConditionTrue, "Succeeded")
	})

	It("uses kstatus for workload rollouts while preserving apply-only readiness", func() {
		ctx := context.Background()
		prefix := "health-workload-" + rand.String(5)
		_, namespaces := watchNamespaces(prefix)
		deployment := func(replicas int) []byte {
			return []byte(fmt.Sprintf(`{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":%q},"spec":{"replicas":%d,"selector":{"matchLabels":{"app":%q}},"template":{"metadata":{"labels":{"app":%q}},"spec":{"nodeSelector":{"projectcapsule.dev/health-never-schedule":"true"},"containers":[{"name":"pause","image":"registry.k8s.io/pause:3.10.1"}]}}}}`, prefix, replicas, prefix, prefix))
		}
		parent := &capsulev1beta2.GlobalTenantResource{Name: prefix, Spec: capsulev1beta2.GlobalTenantResourceSpec{Scope: api.ResourceScopeNamespace, TenantSelector: metav1.LabelSelector{MatchLabels: map[string]string{"projectcapsule.dev/watch-tenant": "0" + prefix}}, TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{HealthChecks: &capsulev1beta2.TenantResourceHealthChecks{Wait: true, Timeout: &metav1.Duration{Duration: 5 * time.Second}}, ResyncPeriod: metav1.Duration{Duration: 24 * time.Hour}, Resources: []capsulev1beta2.ResourceSpec{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": namespaces[0].Name}}, RawItems: []capsulev1beta2.RawExtension{{Raw: deployment(0)}}}}}}}
		Expect(k8sClient.Create(ctx, parent)).To(Succeed())
		DeferCleanup(DeleteInBackground, parent)
		expectReplicationHealth(parent, metav1.ConditionTrue, "Succeeded")
		Eventually(func() error {
			fresh := &capsulev1beta2.GlobalTenantResource{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(parent), fresh); err != nil {
				return err
			}
			fresh.Spec.Resources[0].RawItems[0] = capsulev1beta2.RawExtension{Raw: deployment(1)}
			return k8sClient.Update(ctx, fresh)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		expectReplicationHealth(parent, metav1.ConditionFalse, "HealthCheckTimeout")
		By("preserving apply-only behavior when health checks are disabled")
		Eventually(func() error {
			fresh := &capsulev1beta2.GlobalTenantResource{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(parent), fresh); err != nil {
				return err
			}
			fresh.Spec.HealthChecks.Wait = false
			return k8sClient.Update(ctx, fresh)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		waitWatchReady(parent)
		_, status := replicationHealthObject(parent)
		Expect(status.Health).To(BeNil())
		Expect(status.Conditions.GetConditionByType(meta.HealthyCondition)).To(BeNil())
		Expect(status.ProcessedItems[0].Health).To(BeNil())
	})

	It("reports invalid CEL before applying and validates timeout bounds", func() {
		ctx := context.Background()
		prefix := "health-invalid-" + rand.String(5)
		owners, namespaces := watchNamespaces(prefix)
		actor := impersonationClient(owners[0].Name, withDefaultGroups(nil))
		parent := &capsulev1beta2.TenantResource{Name: prefix, Namespace: namespaces[0].Name, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{HealthChecks: &capsulev1beta2.TenantResourceHealthChecks{Wait: true, Expressions: []capsulev1beta2.TenantResourceHealthCheckExpression{{APIVersion: "v1", Kind: "ConfigMap", Current: "request.userInfo.username == 'alice'"}}}, ResyncPeriod: metav1.Duration{Duration: 24 * time.Hour}, Resources: []capsulev1beta2.ResourceSpec{{RawItems: []capsulev1beta2.RawExtension{{Raw: []byte(fmt.Sprintf(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":%q}}`, prefix))}}}}}}}
		parent.Spec.Resources[0].NamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": parent.Namespace}}
		Expect(actor.Create(ctx, parent)).To(Succeed())
		Eventually(func(g Gomega) {
			_, status := replicationHealthObject(parent)
			condition := status.Conditions.GetConditionByType(meta.ReadyCondition)
			g.Expect(condition).NotTo(BeNil())
			g.Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			g.Expect(condition.Message).To(ContainSubstring("undeclared reference"))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Expect(apierrors.IsNotFound(actor.Get(ctx, client.ObjectKey{Namespace: parent.Namespace, Name: prefix}, &corev1.ConfigMap{}))).To(BeTrue())
		Eventually(func() error {
			fresh := &capsulev1beta2.TenantResource{}
			if err := actor.Get(ctx, client.ObjectKeyFromObject(parent), fresh); err != nil {
				return err
			}
			fresh.Spec.HealthChecks.Expressions[0].Current = "true"
			return actor.Update(ctx, fresh)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		expectReplicationHealth(parent, metav1.ConditionTrue, "Succeeded")
		for _, namespace := range namespaces[1:] {
			Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace.Name, Name: prefix}, &corev1.ConfigMap{}))).To(BeTrue())
		}
		bad := parent.DeepCopy()
		bad.Name = prefix + "-timeout"
		bad.ObjectMeta = metav1.ObjectMeta{Name: prefix + "-timeout", Namespace: parent.Namespace}
		bad.Spec.HealthChecks.Timeout = &metav1.Duration{}
		err := actor.Create(ctx, bad)
		Expect(apierrors.IsInvalid(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("healthChecks.timeout must be positive"))
		Expect(apierrors.IsNotFound(actor.Get(ctx, client.ObjectKeyFromObject(bad), &capsulev1beta2.TenantResource{}))).To(BeTrue())
	})
	It("uses the replication identity for every health read", func() {
		ctx := context.Background()
		prefix := "health-rbac-" + rand.String(5)
		owners, namespaces := watchNamespaces(prefix)
		actor := impersonationClient(owners[0].Name, withDefaultGroups(nil))
		ns := namespaces[0].Name
		ensureServiceAccount(ns, "health-reader")
		restore := func() {
			bindServiceAccountToNamespacedResource(ns, "health-reader", ns, []string{"configmaps"}, []string{"get", "list", "watch", "create", "update", "patch", "delete"})
		}
		restore()
		DeferCleanup(restore)
		parent := &capsulev1beta2.TenantResource{Name: prefix, Namespace: ns, Spec: capsulev1beta2.TenantResourceSpec{ServiceAccount: &meta.LocalRFC1123ObjectReference{Name: "health-reader"}, TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{HealthChecks: &capsulev1beta2.TenantResourceHealthChecks{Wait: true, Expressions: []capsulev1beta2.TenantResourceHealthCheckExpression{{APIVersion: "v1", Kind: "ConfigMap", Current: `has(metadata.annotations) && 'projectcapsule.dev/health-state' in metadata.annotations`}}}, ResyncPeriod: metav1.Duration{Duration: 24 * time.Hour}, Resources: []capsulev1beta2.ResourceSpec{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": ns}}, Policy: &apiruntime.ResourceReplicationPolicy{Protect: new(false)}, RawItems: []capsulev1beta2.RawExtension{{Raw: []byte(fmt.Sprintf(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":%q}}`, prefix))}}}}}}}
		Expect(actor.Create(ctx, parent)).To(Succeed())
		pending := expectReplicationHealth(parent, metav1.ConditionUnknown, "HealthChecking")
		applied := pending.ProcessedItems[0].LastApply
		binding := &rbacv1.RoleBinding{Name: fmt.Sprintf("sa-health-reader-configmaps-%s-binding", ns), Namespace: ns}
		Expect(k8sClient.Delete(ctx, binding)).To(Succeed())
		Eventually(func(g Gomega) {
			_, status := replicationHealthObject(parent)
			condition := status.Conditions.GetConditionByType(meta.HealthyCondition)
			g.Expect(condition).NotTo(BeNil())
			g.Expect(condition.Status).NotTo(Equal(metav1.ConditionTrue))
			g.Expect(condition.Message).To(ContainSubstring("forbidden"))
			g.Expect(condition.Message).To(ContainSubstring("health-reader"))
			g.Expect(meta.IsStatusConditionTrue(status.Conditions, meta.ReadyCondition)).To(BeFalse())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespaces[2].Name, Name: prefix}, &corev1.ConfigMap{}))).To(BeTrue())
		Eventually(func() error {
			target := &corev1.ConfigMap{}
			if err := actor.Get(ctx, client.ObjectKey{Namespace: ns, Name: prefix}, target); err != nil {
				return err
			}
			target.Annotations = map[string]string{healthStateAnnotation: "healthy"}
			return actor.Update(ctx, target)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		restore()
		healthy := expectReplicationHealth(parent, metav1.ConditionTrue, "Succeeded")
		Expect(healthy.ProcessedItems[0].LastApply).To(Equal(applied))
	})

	It("retains the health deadline across elected-pod loss", Serial, Label("config"), func() {
		ctx := context.Background()
		prefix := "health-handoff-" + rand.String(5)
		owners, namespaces := watchNamespaces(prefix)
		actor := impersonationClient(owners[0].Name, withDefaultGroups(nil))
		ns := namespaces[0].Name
		parent := &capsulev1beta2.TenantResource{Name: prefix, Namespace: ns, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{HealthChecks: &capsulev1beta2.TenantResourceHealthChecks{Wait: true, Expressions: []capsulev1beta2.TenantResourceHealthCheckExpression{{APIVersion: "v1", Kind: "ConfigMap", Current: `has(metadata.annotations) && 'projectcapsule.dev/health-state' in metadata.annotations`}}}, ResyncPeriod: metav1.Duration{Duration: 24 * time.Hour}, Resources: []capsulev1beta2.ResourceSpec{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": ns}}, Policy: &apiruntime.ResourceReplicationPolicy{Protect: new(false)}, RawItems: []capsulev1beta2.RawExtension{{Raw: []byte(fmt.Sprintf(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":%q}}`, prefix))}}}}}}}
		Expect(actor.Create(ctx, parent)).To(Succeed())
		pending := expectReplicationHealth(parent, metav1.ConditionUnknown, "HealthChecking")
		deadline := pending.Health.Deadline
		requested := pending.Health.RequestedAt
		Eventually(func() int { return len(readyWatchControllerPods(ctx)) }, defaultTimeoutInterval, defaultPollInterval).Should(BeNumerically(">=", 2))
		leaseKey := client.ObjectKey{Name: "replications.projectcapsule.dev", Namespace: ControllerNamespace}
		lease := &coordinationv1.Lease{}
		Expect(k8sClient.Get(ctx, leaseKey, lease)).To(Succeed())
		Expect(lease.Spec.HolderIdentity).NotTo(BeNil())
		holder := *lease.Spec.HolderIdentity
		var leader *corev1.Pod
		for _, pod := range readyWatchControllerPods(ctx) {
			if strings.HasPrefix(holder, pod.Name+"_") {
				leader = pod
			}
		}
		Expect(leader).NotTo(BeNil())
		DeferCleanup(func() {
			Eventually(func() int { return len(readyWatchControllerPods(ctx)) }, defaultTimeoutInterval, defaultPollInterval).Should(BeNumerically(">=", 2))
		})
		Expect(k8sClient.Delete(ctx, leader, &client.DeleteOptions{GracePeriodSeconds: new(int64(0))})).To(Succeed())
		handoffTimeout := defaultTimeoutInterval
		if lease.Spec.LeaseDurationSeconds != nil {
			handoffTimeout += time.Duration(*lease.Spec.LeaseDurationSeconds) * time.Second
		}
		Eventually(func(g Gomega) {
			freshLease := &coordinationv1.Lease{}
			g.Expect(k8sClient.Get(ctx, leaseKey, freshLease)).To(Succeed())
			g.Expect(freshLease.Spec.HolderIdentity).NotTo(BeNil())
			g.Expect(*freshLease.Spec.HolderIdentity).NotTo(Equal(holder))
			_, status := replicationHealthObject(parent)
			g.Expect(status.Health).NotTo(BeNil())
			g.Expect(status.Health.Applying).To(BeFalse())
			g.Expect(status.Health.RequestedAt.After(requested.Time)).To(BeTrue())
			g.Expect(status.Health.Deadline).To(Equal(deadline))
			g.Expect(meta.IsStatusConditionTrue(status.Conditions, meta.ReadyCondition)).To(BeFalse())
		}, handoffTimeout, defaultPollInterval).Should(Succeed())
		Expect(apierrors.IsForbidden(actor.Get(ctx, client.ObjectKey{Namespace: namespaces[2].Name, Name: prefix}, &corev1.ConfigMap{}))).To(BeTrue())
		Eventually(func() error {
			target := &corev1.ConfigMap{}
			if err := actor.Get(ctx, client.ObjectKey{Namespace: ns, Name: prefix}, target); err != nil {
				return err
			}
			target.Annotations = map[string]string{healthStateAnnotation: "healthy"}
			return actor.Update(ctx, target)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		expectReplicationHealth(parent, metav1.ConditionTrue, "Succeeded")
	})
})
