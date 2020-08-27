//+build e2e

/*
Copyright 2020 Clastix Labs.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

import (
	"context"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/envtest/printer"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	capsulev1alpha "github.com/clastix/capsule/api/v1alpha1"
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	cfg                    *rest.Config
	k8sClient              client.Client
	testEnv                *envtest.Environment
	defaulManagerPodArgs   []string
	tenantRoleBindingNames = []string{"namespace:admin", "namespace:deleter"}
)

const (
	capsuleDeploymentName       = "capsule-controller-manager"
	capsuleNamespace            = "capsule-system"
	capsuleManagerContainerName = "manager"
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecsWithDefaultAndCustomReporters(t,
		"Controller Suite",
		[]Reporter{printer.NewlineReporter{}})
}

var _ = BeforeSuite(func(done Done) {
	logf.SetLogger(zap.LoggerTo(GinkgoWriter, true))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join("..", "config", "crd", "bases")},
		UseExistingCluster: func(v bool) *bool {
			return &v
		}(true),
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(cfg).ToNot(BeNil())

	err = capsulev1alpha.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).ToNot(HaveOccurred())
	Expect(k8sClient).ToNot(BeNil())

	capsuleDeployment := &appsv1.Deployment{}
	k8sClient.Get(context.TODO(), types.NamespacedName{Name: capsuleDeploymentName, Namespace: capsuleNamespace}, capsuleDeployment)
	for _, container := range capsuleDeployment.Spec.Template.Spec.Containers {
		if container.Name == capsuleManagerContainerName {
			defaulManagerPodArgs = container.Args
		}
	}
	Expect(defaulManagerPodArgs).ToNot(BeEmpty())
	close(done)
}, 60)

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	Expect(testEnv.Stop()).ToNot(HaveOccurred())
})

func ownerClient(tenant *capsulev1alpha.Tenant) (cs kubernetes.Interface) {
	c, err := config.GetConfig()
	Expect(err).ToNot(HaveOccurred())
	c.Impersonate.Groups = []string{capsulev1alpha.GroupVersion.Group, tenant.Spec.Owner.Name}
	c.Impersonate.UserName = tenant.Spec.Owner.Name
	cs, err = kubernetes.NewForConfig(c)
	Expect(err).ToNot(HaveOccurred())
	return
}
