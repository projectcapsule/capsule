// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	cfg       *rest.Config
	k8sClient client.Client

	testEnv *envtest.Environment
)

var log = ctrl.Log.WithName("e2e-tests")

const (
	ControllerNamespace      string = "capsule-system"
	ControllerServiceAccount string = "capsule"
	e2eCleanupTimeout               = 5 * time.Minute
)

var ControllerServiceAccountFull = "system:serviceaccount:" + ControllerNamespace + ":" + ControllerServiceAccount

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		UseExistingCluster: ptr.To(true),
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(cfg).ToNot(BeNil())

	Expect(capsulev1beta2.AddToScheme(scheme.Scheme)).NotTo(HaveOccurred())

	tuneE2ERestConfig(cfg)

	ctrlClient, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).ToNot(HaveOccurred())
	Expect(ctrlClient).ToNot(BeNil())

	k8sClient = &e2eClient{Client: ctrlClient}
})

var _ = SynchronizedAfterSuite(
	func() {
		// Runs on every parallel process.
		// Keep this empty, or put per-worker cleanup here.
	},
	func() {
		Expect(triggerE2ETenantDeletion(context.TODO())).To(Succeed())

		Eventually(func() error {
			return cleanupE2ENamespaces(context.TODO())
		}, e2eCleanupTimeout, defaultPollInterval).Should(Succeed())

		Eventually(func() error {
			return cleanupE2ETenants(context.TODO())
		}, e2eCleanupTimeout, defaultPollInterval).Should(Succeed())

		By("tearing down the test environment")

		Expect(testEnv.Stop()).ToNot(HaveOccurred())
	},
)

func triggerE2ETenantDeletion(ctx context.Context) error {
	var tnts capsulev1beta2.TenantList

	if err := k8sClient.List(
		ctx,
		&tnts,
		client.MatchingLabels{"env": "e2e"},
	); err != nil {
		return err
	}

	for i := range tnts.Items {
		tnt := &tnts.Items[i]
		if err := k8sClient.Delete(ctx, tnt); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}

	return nil
}

func cleanupE2ETenants(ctx context.Context) error {
	var tnts capsulev1beta2.TenantList

	if err := k8sClient.List(
		ctx,
		&tnts,
		client.MatchingLabels{"env": "e2e"},
	); err != nil {
		return err
	}

	if len(tnts.Items) == 0 {
		return nil
	}

	for i := range tnts.Items {
		tnt := &tnts.Items[i]
		if err := k8sClient.Delete(ctx, tnt); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}

	return fmt.Errorf("still have %d tenants with env=e2e: %s", len(tnts.Items), describeE2ETenants(tnts.Items))
}

func cleanupE2ENamespaces(ctx context.Context) error {
	var nsList corev1.NamespaceList

	if err := k8sClient.List(
		ctx,
		&nsList,
		client.MatchingLabels{"env": "e2e"},
	); err != nil {
		return err
	}

	if len(nsList.Items) == 0 {
		return nil
	}

	for i := range nsList.Items {
		ns := &nsList.Items[i]
		if err := k8sClient.Delete(ctx, ns); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}

	return fmt.Errorf("still have %d namespaces with env=e2e: %s", len(nsList.Items), describeE2ENamespaces(nsList.Items))
}

func describeE2ETenants(items []capsulev1beta2.Tenant) string {
	names := make([]string, 0, len(items))

	for i := range items {
		tnt := items[i]
		state := tnt.GetName()
		if tnt.DeletionTimestamp != nil {
			state += fmt.Sprintf("(deleting for %s)", time.Since(tnt.DeletionTimestamp.Time).Round(time.Second))
		}
		if len(tnt.Finalizers) > 0 {
			state += fmt.Sprintf("(finalizers=%s)", strings.Join(tnt.Finalizers, ","))
		}
		if tnt.Status.Size > 0 {
			state += fmt.Sprintf("(namespaces=%d)", tnt.Status.Size)
		}
		names = append(names, state)
	}

	return strings.Join(names, ", ")
}

func describeE2ENamespaces(items []corev1.Namespace) string {
	names := make([]string, 0, len(items))

	for i := range items {
		ns := items[i]
		state := fmt.Sprintf("%s(phase=%s)", ns.GetName(), ns.Status.Phase)
		if ns.DeletionTimestamp != nil {
			state += fmt.Sprintf("(deleting for %s)", time.Since(ns.DeletionTimestamp.Time).Round(time.Second))
		}
		if len(ns.Spec.Finalizers) > 0 {
			finalizers := make([]string, 0, len(ns.Spec.Finalizers))
			for _, finalizer := range ns.Spec.Finalizers {
				finalizers = append(finalizers, string(finalizer))
			}
			state += fmt.Sprintf("(spec.finalizers=%s)", strings.Join(finalizers, ","))
		}
		if len(ns.Finalizers) > 0 {
			state += fmt.Sprintf("(metadata.finalizers=%s)", strings.Join(ns.Finalizers, ","))
		}
		names = append(names, state)
	}

	return strings.Join(names, ", ")
}

func clusterAdminClient() (cs kubernetes.Interface) {
	c, err := config.GetConfig()
	Expect(err).ToNot(HaveOccurred())
	tuneE2ERestConfig(c)

	cs, err = kubernetes.NewForConfig(c)
	Expect(err).ToNot(HaveOccurred())

	return cs
}

func ownerClient(owner rbac.UserSpec) (cs kubernetes.Interface) {
	c, err := config.GetConfig()
	Expect(err).ToNot(HaveOccurred())
	tuneE2ERestConfig(c)

	c.Impersonate.Groups = []string{"projectcapsule.dev", owner.Name}
	c.Impersonate.UserName = owner.Name
	cs, err = kubernetes.NewForConfig(c)
	Expect(err).ToNot(HaveOccurred())

	return cs
}

func impersonationClientSet(user string, groups []string) (cs kubernetes.Interface) {
	c, err := config.GetConfig()
	Expect(err).ToNot(HaveOccurred())
	tuneE2ERestConfig(c)

	c.Impersonate.Groups = groups
	c.Impersonate.UserName = user
	cs, err = kubernetes.NewForConfig(c)
	Expect(err).ToNot(HaveOccurred())

	return cs
}

func impersonationClient(user string, groups []string) client.Client {
	impersonatedCfg := rest.CopyConfig(cfg)
	tuneE2ERestConfig(impersonatedCfg)

	impersonatedCfg.Impersonate = rest.ImpersonationConfig{
		UserName: user,
		Groups:   groups,
	}

	c, err := client.New(impersonatedCfg, client.Options{Scheme: k8sClient.Scheme()})
	Expect(err).ToNot(HaveOccurred())

	return c
}

func withDefaultGroups(groups []string) []string {
	return append([]string{"projectcapsule.dev"}, groups...)
}
