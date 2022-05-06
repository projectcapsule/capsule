// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package main

import (
	goflag "flag"
	"fmt"
	"os"
	goRuntime "runtime"

	flag "github.com/spf13/pflag"
	"go.uber.org/zap/zapcore"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	utilVersion "k8s.io/apimachinery/pkg/util/version"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	capsulev1alpha1 "github.com/clastix/capsule/api/v1alpha1"
	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
	configcontroller "github.com/clastix/capsule/controllers/config"
	rbaccontroller "github.com/clastix/capsule/controllers/rbac"
	secretcontroller "github.com/clastix/capsule/controllers/secret"
	servicelabelscontroller "github.com/clastix/capsule/controllers/servicelabels"
	tenantcontroller "github.com/clastix/capsule/controllers/tenant"
	"github.com/clastix/capsule/pkg/configuration"
	"github.com/clastix/capsule/pkg/indexer"
	"github.com/clastix/capsule/pkg/webhook"
	"github.com/clastix/capsule/pkg/webhook/ingress"
	namespacewebhook "github.com/clastix/capsule/pkg/webhook/namespace"
	"github.com/clastix/capsule/pkg/webhook/networkpolicy"
	"github.com/clastix/capsule/pkg/webhook/node"
	"github.com/clastix/capsule/pkg/webhook/ownerreference"
	"github.com/clastix/capsule/pkg/webhook/pod"
	"github.com/clastix/capsule/pkg/webhook/pvc"
	"github.com/clastix/capsule/pkg/webhook/route"
	"github.com/clastix/capsule/pkg/webhook/service"
	"github.com/clastix/capsule/pkg/webhook/tenant"
	"github.com/clastix/capsule/pkg/webhook/utils"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(capsulev1alpha1.AddToScheme(scheme))
	utilruntime.Must(capsulev1beta1.AddToScheme(scheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))
}

func printVersion() {
	setupLog.Info(fmt.Sprintf("Capsule Version %s %s%s", GitTag, GitCommit, GitDirty))
	setupLog.Info(fmt.Sprintf("Build from: %s", GitRepo))
	setupLog.Info(fmt.Sprintf("Build date: %s", BuildTime))
	setupLog.Info(fmt.Sprintf("Go Version: %s", goRuntime.Version()))
	setupLog.Info(fmt.Sprintf("Go OS/Arch: %s/%s", goRuntime.GOOS, goRuntime.GOARCH))
}

// nolint:maintidx
func main() {
	var enableLeaderElection, enableSecretController, version bool

	var metricsAddr, namespace, configurationName string

	var goFlagSet goflag.FlagSet

	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.BoolVar(&enableSecretController, "enable-secret-controller", true,
		"Enable secret controller which reconciles TLS and CA secrets for capsule webhooks.")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&version, "version", false, "Print the Capsule version and exit")
	flag.StringVar(&configurationName, "configuration-name", "default", "The CapsuleConfiguration resource name to use")

	opts := zap.Options{
		EncoderConfigOptions: append([]zap.EncoderConfigOption{}, func(config *zapcore.EncoderConfig) {
			config.EncodeTime = zapcore.ISO8601TimeEncoder
		}),
	}

	opts.BindFlags(&goFlagSet)
	flag.CommandLine.AddGoFlagSet(&goFlagSet)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	printVersion()

	if version {
		os.Exit(0)
	}

	if namespace = os.Getenv("NAMESPACE"); len(namespace) == 0 {
		setupLog.Error(fmt.Errorf("unable to determinate the Namespace Capsule is running on"), "unable to start manager")
		os.Exit(1)
	}

	if len(configurationName) == 0 {
		setupLog.Error(fmt.Errorf("missing CapsuleConfiguration resource name"), "unable to start manager")
		os.Exit(1)
	}

	manager, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "42c733ea.clastix.capsule.io",
		HealthProbeBindAddress: ":10080",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	_ = manager.AddReadyzCheck("ping", healthz.Ping)
	_ = manager.AddHealthzCheck("ping", healthz.Ping)

	ctx := ctrl.SetupSignalHandler()

	cfg := configuration.NewCapsuleConfiguration(ctx, manager.GetClient(), configurationName)

	if enableSecretController {
		if err = (&secretcontroller.CAReconciler{
			Client:        manager.GetClient(),
			Log:           ctrl.Log.WithName("controllers").WithName("CA"),
			Namespace:     namespace,
			Configuration: cfg,
		}).SetupWithManager(manager); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "Namespace")
			os.Exit(1)
		}

		if err = (&secretcontroller.TLSReconciler{
			Client:        manager.GetClient(),
			Log:           ctrl.Log.WithName("controllers").WithName("Tls"),
			Namespace:     namespace,
			Configuration: cfg,
		}).SetupWithManager(manager); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "Namespace")
			os.Exit(1)
		}
	}

	clientset, err := kubernetes.NewForConfig(ctrl.GetConfigOrDie())
	if err != nil {
		setupLog.Error(err, "unable to create kubernetes clientset")
		os.Exit(1)
	}

	directClient, err := client.New(ctrl.GetConfigOrDie(), client.Options{
		Scheme: manager.GetScheme(),
		Mapper: manager.GetRESTMapper(),
	})
	if err != nil {
		setupLog.Error(err, "unable to create the direct client")
		os.Exit(1)
	}

	directCfg := configuration.NewCapsuleConfiguration(ctx, directClient, configurationName)

	ca, err := clientset.CoreV1().Secrets(namespace).Get(ctx, directCfg.CASecretName(), metav1.GetOptions{})
	if err != nil {
		setupLog.Error(err, "unable to get Capsule CA secret")
		os.Exit(1)
	}

	tls, err := clientset.CoreV1().Secrets(namespace).Get(ctx, directCfg.TLSSecretName(), metav1.GetOptions{})
	if err != nil {
		setupLog.Error(err, "unable to get Capsule TLS secret")
		os.Exit(1)
	}
	// nolint:nestif
	if len(ca.Data) > 0 && len(tls.Data) > 0 {
		if err = (&tenantcontroller.Manager{
			RESTConfig: manager.GetConfig(),
			Client:     manager.GetClient(),
			Log:        ctrl.Log.WithName("controllers").WithName("Tenant"),
			Recorder:   manager.GetEventRecorderFor("tenant-controller"),
		}).SetupWithManager(manager); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "Tenant")
			os.Exit(1)
		}

		if err = (&capsulev1alpha1.Tenant{}).SetupWebhookWithManager(manager); err != nil {
			setupLog.Error(err, "unable to create conversion webhook", "webhook", "Tenant")
			os.Exit(1)
		}

		if err = indexer.AddToManager(ctx, setupLog, manager); err != nil {
			setupLog.Error(err, "unable to setup indexers")
			os.Exit(1)
		}

		var kubeVersion *utilVersion.Version

		if kubeVersion, err = utils.GetK8sVersion(); err != nil {
			setupLog.Error(err, "unable to get kubernetes version")
			os.Exit(1)
		}

		// webhooks: the order matters, don't change it and just append
		webhooksList := append(
			make([]webhook.Webhook, 0),
			route.Pod(pod.ImagePullPolicy(), pod.ContainerRegistry(), pod.PriorityClass()),
			route.Namespace(utils.InCapsuleGroups(cfg, namespacewebhook.QuotaHandler(), namespacewebhook.FreezeHandler(cfg), namespacewebhook.PrefixHandler(cfg), namespacewebhook.UserMetadataHandler())),
			route.Ingress(ingress.Class(cfg), ingress.Hostnames(cfg), ingress.Collision(cfg), ingress.Wildcard()),
			route.PVC(pvc.Handler()),
			route.Service(service.Handler()),
			route.NetworkPolicy(utils.InCapsuleGroups(cfg, networkpolicy.Handler())),
			route.Tenant(tenant.NameHandler(), tenant.RoleBindingRegexHandler(), tenant.IngressClassRegexHandler(), tenant.StorageClassRegexHandler(), tenant.ContainerRegistryRegexHandler(), tenant.HostnameRegexHandler(), tenant.FreezedEmitter(), tenant.ServiceAccountNameHandler(), tenant.ForbiddenAnnotationsRegexHandler(), tenant.ProtectedHandler()),
			route.OwnerReference(utils.InCapsuleGroups(cfg, ownerreference.Handler(cfg))),
			route.Cordoning(tenant.CordoningHandler(cfg), tenant.ResourceCounterHandler()),
			route.Node(utils.InCapsuleGroups(cfg, node.UserMetadataHandler(cfg, kubeVersion))),
		)

		nodeWebhookSupported, _ := utils.NodeWebhookSupported(kubeVersion)
		if !nodeWebhookSupported {
			setupLog.Info("Disabling node labels verification webhook as current Kubernetes version doesn't have fix for CVE-2021-25735")
		}

		if err = webhook.Register(manager, webhooksList...); err != nil {
			setupLog.Error(err, "unable to setup webhooks")
			os.Exit(1)
		}

		rbacManager := &rbaccontroller.Manager{
			Log:           ctrl.Log.WithName("controllers").WithName("Rbac"),
			Configuration: cfg,
		}

		if err = manager.Add(rbacManager); err != nil {
			setupLog.Error(err, "unable to create cluster roles")
			os.Exit(1)
		}

		if err = rbacManager.SetupWithManager(ctx, manager, configurationName); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "Rbac")
			os.Exit(1)
		}

		if err = (&servicelabelscontroller.ServicesLabelsReconciler{
			Log: ctrl.Log.WithName("controllers").WithName("ServiceLabels"),
		}).SetupWithManager(ctx, manager); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "ServiceLabels")
			os.Exit(1)
		}

		if err = (&servicelabelscontroller.EndpointsLabelsReconciler{
			Log: ctrl.Log.WithName("controllers").WithName("EndpointLabels"),
		}).SetupWithManager(ctx, manager); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "EndpointLabels")
			os.Exit(1)
		}

		if err = (&servicelabelscontroller.EndpointSlicesLabelsReconciler{
			Log:          ctrl.Log.WithName("controllers").WithName("EndpointSliceLabels"),
			VersionMinor: kubeVersion.Minor(),
			VersionMajor: kubeVersion.Major(),
		}).SetupWithManager(ctx, manager); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "EndpointSliceLabels")
		}

		if err = (&configcontroller.Manager{
			Log: ctrl.Log.WithName("controllers").WithName("CapsuleConfiguration"),
		}).SetupWithManager(manager, configurationName); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "CapsuleConfiguration")
			os.Exit(1)
		}
	} else {
		setupLog.Info("skip registering a tenant controller, missing CA secret")
	}

	setupLog.Info("starting manager")

	if err = manager.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
