package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/configuration"
)

var _ = Describe("CapsuleConfiguration - ServiceAccountClient", Label("config", "impersonation"), func() {

	originalConfig := &capsulev1beta2.CapsuleConfiguration{}
	testingConfig := &capsulev1beta2.CapsuleConfiguration{}

	BeforeEach(func() {
		Expect(k8sClient.Get(context.Background(), client.ObjectKey{Name: defaultConfigurationName}, originalConfig)).To(Succeed())
		testingConfig = originalConfig.DeepCopy()
	})

	AfterEach(func() {
		Eventually(func() error {
			if err := k8sClient.Get(context.Background(), client.ObjectKey{Name: originalConfig.Name}, originalConfig); err != nil {
				return err
			}

			testingConfig.Spec = originalConfig.Spec
			return k8sClient.Update(context.Background(), testingConfig)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})

	It("returns base config when ServiceAccountClient is nil", func() {
		capsuleCfg := configuration.NewCapsuleConfiguration(context.TODO(), k8sClient, cfg, defaultConfigurationName)
		clientCfg, err := capsuleCfg.ServiceAccountClient(context.TODO())
		Expect(err).NotTo(HaveOccurred())
		Expect(clientCfg.Host).To(Equal(capsuleCfg.ServiceAccountClientProperties().Endpoint))
		Expect(clientCfg.TLSClientConfig.Insecure).To(BeFalse())
		Expect(clientCfg.TLSClientConfig.CAData).To(BeNil())
	})

	It("sets skip TLS verify", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.ServiceAccountClient = &api.ServiceAccountClient{
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
				Name:      "capsule-ca",
				Namespace: "default",
			},
			Data: map[string][]byte{
				"ca.crt": caData,
			},
		}
		Expect(k8sClient.Create(context.TODO(), secret)).To(Succeed())

		// Create configuration pointing to the secret
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.ServiceAccountClient = &api.ServiceAccountClient{
				CASecretName:      secret.Name,
				CASecretNamespace: secret.Namespace,
				CASecretKey:       "ca.crt",
			}
		})

		cfg := configuration.NewCapsuleConfiguration(context.TODO(), k8sClient, cfg, defaultConfigurationName)
		clientCfg, err := cfg.ServiceAccountClient(context.TODO())
		Expect(err).NotTo(HaveOccurred())
		Expect(clientCfg.TLSClientConfig.CAData).To(Equal(caData))
	})
})
