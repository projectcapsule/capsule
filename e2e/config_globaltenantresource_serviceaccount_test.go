// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

// CapsuleConfiguration is shared by every replication. Ordered alone does not
// isolate its changes from other containers running on parallel workers.
var _ = Describe("GlobalTenantResource service account resolution", Serial,
	Label("config", "replications", "global", "globaltenantresource"), func() {
		It("tracks configuration changes in the resolved service account status", func() {
			ctx := context.Background()
			original := &capsulev1beta2.CapsuleConfiguration{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: defaultConfigurationName}, original)).To(Succeed())
			DeferCleanup(func() {
				ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
					configuration.Spec.Impersonation.GlobalDefaultServiceAccount = original.Spec.Impersonation.GlobalDefaultServiceAccount
					configuration.Spec.Impersonation.GlobalDefaultServiceAccountNamespace = original.Spec.Impersonation.GlobalDefaultServiceAccountNamespace
				})
			})

			setDefault := func(name meta.RFC1123Name, namespace meta.RFC1123SubdomainName) {
				ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
					configuration.Spec.Impersonation.GlobalDefaultServiceAccount = name
					configuration.Spec.Impersonation.GlobalDefaultServiceAccountNamespace = namespace
				})
			}
			setDefault("", "")
			ensureServiceAccount(ControllerNamespace, "default")
			controllerName := meta.RFC1123Name(ControllerServiceAccount)
			controllerNamespace := meta.RFC1123SubdomainName(ControllerNamespace)

			gtr := newRawConfigMapGlobalTenantResource("gtr-sa-resolution", map[string]string{"mode": "default"})
			gtr.Spec.ServiceAccount = nil
			// Select no existing Tenant: this test concerns identity resolution,
			// and changing the global default must not create unrelated objects.
			gtr.Spec.TenantSelector = metav1.LabelSelector{MatchLabels: map[string]string{"e2e.projectcapsule.dev/service-account-resolution": gtr.Name}}
			// Configuration events must trigger these reconciles; periodic resync
			// must not hide a missing watch within the assertion timeout.
			gtr.Spec.ResyncPeriod = metav1.Duration{Duration: time.Hour}
			EventuallyCreation(func() error { return k8sClient.Create(ctx, gtr) }).Should(Succeed())
			DeferCleanup(func() { EventuallyDeletion(gtr) })

			expectIdentity := func(name meta.RFC1123Name, namespace meta.RFC1123SubdomainName, fallback bool) {
				Eventually(func(g Gomega) {
					configuration := &capsulev1beta2.CapsuleConfiguration{}
					g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: defaultConfigurationName}, configuration)).To(Succeed())
					if fallback {
						g.Expect(configuration.Spec.Impersonation.GlobalDefaultServiceAccount).To(BeEmpty(), "global configuration changed while waiting for controller identity")
						g.Expect(configuration.Spec.Impersonation.GlobalDefaultServiceAccountNamespace).To(BeEmpty())
					} else {
						g.Expect(configuration.Spec.Impersonation.GlobalDefaultServiceAccount).To(Equal(name), "global configuration changed while waiting for status")
						g.Expect(configuration.Spec.Impersonation.GlobalDefaultServiceAccountNamespace).To(Equal(namespace))
					}

					current := &capsulev1beta2.GlobalTenantResource{}
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(gtr), current)).To(Succeed())
					g.Expect(current.Spec.ServiceAccount).To(BeNil())
					g.Expect(current.Generation).To(Equal(gtr.Generation), "identity must update without changing the resource spec")
					g.Expect(current.Status.ServiceAccount).NotTo(BeNil())
					g.Expect(current.Status.ServiceAccount.Name).To(Equal(name), "configuration resourceVersion=%s, resource status=%+v", configuration.ResourceVersion, current.Status)
					g.Expect(current.Status.ServiceAccount.Namespace).To(Equal(namespace))
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}

			By("resolving the controller identity when no global default is configured")
			expectIdentity(controllerName, controllerNamespace, true)

			By("reconciling the new global default without changing the GlobalTenantResource")
			setDefault("default", controllerNamespace)
			expectIdentity("default", controllerNamespace, false)

			By("returning to the controller identity when the global default is removed")
			setDefault("", "")
			expectIdentity(controllerName, controllerNamespace, true)
		})
	})
