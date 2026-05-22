// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"
	"testing"

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
	ControllerNamespace string = "capsule-system"
)

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
		Eventually(func() error {
			var tnts capsulev1beta2.TenantList

			if err := k8sClient.List(
				context.TODO(),
				&tnts,
				client.MatchingLabels{"env": "e2e"},
			); err != nil {
				return err
			}

			if len(tnts.Items) == 0 {
				return nil
			}

			for i := range tnts.Items {
				ns := &tnts.Items[i]
				if err := k8sClient.Delete(context.TODO(), ns); err != nil && !apierrors.IsNotFound(err) {
					return err
				}
			}

			return fmt.Errorf("still have %d tenants with env=e2e", len(tnts.Items))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		Eventually(func() error {
			var nsList corev1.NamespaceList

			if err := k8sClient.List(
				context.TODO(),
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
				if err := k8sClient.Delete(context.TODO(), ns); err != nil && !apierrors.IsNotFound(err) {
					return err
				}
			}

			return fmt.Errorf("still have %d namespaces with env=e2e", len(nsList.Items))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		By("tearing down the test environment")

		Expect(testEnv.Stop()).ToNot(HaveOccurred())
	},
)

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
