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
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	cfg       *rest.Config
	k8sClient client.Client

	testEnv *envtest.Environment
)

var log = ctrl.Log.WithName("e2e-tests")

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

	ctrlClient, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).ToNot(HaveOccurred())
	Expect(ctrlClient).ToNot(BeNil())

	k8sClient = &e2eClient{Client: ctrlClient}

	ModifyCapsuleConfigurationOpts(func(cfg *capsulev1beta2.CapsuleConfiguration) {
		cfg.Spec = configuration.DefaultCapsuleConfiguration()
	})

})

var _ = AfterSuite(func() {
	Eventually(func() error {
		var nsList corev1.NamespaceList

		// List all namespaces with env=e2e
		if err := k8sClient.List(
			context.TODO(),
			&nsList,
			client.MatchingLabels{"env": "e2e"},
		); err != nil {
			return err
		}

		// If none left, we’re done
		if len(nsList.Items) == 0 {
			return nil
		}

		// Try deleting all; if any delete fails with something other than NotFound,
		// return the error so Eventually keeps retrying.
		for i := range nsList.Items {
			ns := &nsList.Items[i]
			if err := k8sClient.Delete(context.TODO(), ns); err != nil && !apierrors.IsNotFound(err) {
				return err
			}
		}

		// Return a non-nil error to tell Eventually "not done yet"
		return fmt.Errorf("still have %d namespaces with env=e2e", len(nsList.Items))
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

	By("tearing down the test environment")

	Expect(testEnv.Stop()).ToNot(HaveOccurred())
})

func ownerClient(owner api.UserSpec) (cs kubernetes.Interface) {
	c, err := config.GetConfig()
	Expect(err).ToNot(HaveOccurred())
	c.Impersonate.Groups = []string{"projectcapsule.dev", owner.Name}
	c.Impersonate.UserName = owner.Name
	cs, err = kubernetes.NewForConfig(c)
	Expect(err).ToNot(HaveOccurred())

	return cs
}

func impersonationClient(user string, groups []string) client.Client {
	impersonatedCfg := rest.CopyConfig(cfg)
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
