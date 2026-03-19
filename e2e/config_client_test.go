package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
)

var _ = Describe("CapsuleConfiguration - ServiceAccountClient", Label("config", "impersonation"), func() {
	originalConfig := &capsulev1beta2.CapsuleConfiguration{}

	BeforeEach(func() {
		Expect(k8sClient.Get(context.Background(), client.ObjectKey{Name: defaultConfigurationName}, originalConfig)).To(Succeed())
	})

	AfterEach(func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec = originalConfig.Spec
		})
	})

	It("returns base config when ServiceAccountClient is nil", func() {
		capsuleCfg := configuration.NewCapsuleConfiguration(context.TODO(), k8sClient, cfg, defaultConfigurationName)
		clientCfg, err := capsuleCfg.ServiceAccountClient(context.TODO())
		Expect(err).NotTo(HaveOccurred())
		Expect(clientCfg.TLSClientConfig.Insecure).To(BeFalse())
		Expect(clientCfg.TLSClientConfig.CAData).To(Equal("dummy-ca-data"))
	})

	It("sets skip TLS verify", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.Impersonation = capsulev1beta2.ServiceAccountClient{
				SkipTLSVerify: true,
			}
		})

		capsuleCfg := configuration.NewCapsuleConfiguration(context.TODO(), k8sClient, cfg, defaultConfigurationName)
		clientCfg, err := capsuleCfg.ServiceAccountClient(context.TODO())
		Expect(err).NotTo(HaveOccurred())
		Expect(clientCfg.TLSClientConfig.Insecure).To(BeTrue())
	})

	It("loads CA from secret", func() {
		caData := []byte("dummy-ca-data")
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "custom-capsule-ca",
				Namespace: "default",
			},
			Data: map[string][]byte{
				"ca.crt": caData,
			},
		}
		Expect(k8sClient.Create(context.TODO(), secret)).To(Succeed())

		DeferCleanup(func() {
			s := &corev1.Secret{}
			err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: secret.Name, Namespace: secret.Namespace}, s)
			if err != nil {
				if apierrors.IsNotFound(err) {
					return
				}

				Expect(err).ToNot(HaveOccurred())
				return
			}

			err = k8sClient.Delete(context.TODO(), s)
			if err != nil && !apierrors.IsNotFound(err) {
				Expect(err).ToNot(HaveOccurred())
			}
		})

		// Create configuration pointing to the secret
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.Impersonation = capsulev1beta2.ServiceAccountClient{
				CASecretName:      meta.RFC1123Name(secret.Name),
				CASecretNamespace: meta.RFC1123SubdomainName(secret.Namespace),
				CASecretKey:       "ca.crt",
			}
		})

		cfg := configuration.NewCapsuleConfiguration(context.TODO(), k8sClient, cfg, defaultConfigurationName)
		clientCfg, err := cfg.ServiceAccountClient(context.TODO())
		Expect(err).NotTo(HaveOccurred())
		Expect(clientCfg.TLSClientConfig.CAData).To(Equal(caData))
	})
})
