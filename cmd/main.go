// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	goflag "flag"
	"fmt"
	"os"
	goRuntime "runtime"

	flag "github.com/spf13/pflag"
	_ "go.uber.org/automaxprocs"
	"go.uber.org/zap/zapcore"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	utilVersion "k8s.io/apimachinery/pkg/util/version"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	ctrlwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	capsulev1beta1 "github.com/projectcapsule/capsule/api/v1beta1"
	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	configcontroller "github.com/projectcapsule/capsule/controllers/config"
	podlabelscontroller "github.com/projectcapsule/capsule/controllers/pod"
	"github.com/projectcapsule/capsule/controllers/pv"
	rbaccontroller "github.com/projectcapsule/capsule/controllers/rbac"
	"github.com/projectcapsule/capsule/controllers/resourcepools"
	"github.com/projectcapsule/capsule/controllers/resources"
	servicelabelscontroller "github.com/projectcapsule/capsule/controllers/servicelabels"
	tenantcontroller "github.com/projectcapsule/capsule/controllers/tenant"
	tlscontroller "github.com/projectcapsule/capsule/controllers/tls"
	"github.com/projectcapsule/capsule/pkg/configuration"
	"github.com/projectcapsule/capsule/pkg/indexer"
	"github.com/projectcapsule/capsule/pkg/metrics"
	"github.com/projectcapsule/capsule/pkg/webhook"
	"github.com/projectcapsule/capsule/pkg/webhook/defaults"
	"github.com/projectcapsule/capsule/pkg/webhook/gateway"
	"github.com/projectcapsule/capsule/pkg/webhook/ingress"
	namespacemutation "github.com/projectcapsule/capsule/pkg/webhook/namespace/mutation"
	namespacevalidation "github.com/projectcapsule/capsule/pkg/webhook/namespace/validation"
	"github.com/projectcapsule/capsule/pkg/webhook/networkpolicy"
	"github.com/projectcapsule/capsule/pkg/webhook/node"
	"github.com/projectcapsule/capsule/pkg/webhook/pod"
	"github.com/projectcapsule/capsule/pkg/webhook/pvc"
	"github.com/projectcapsule/capsule/pkg/webhook/resourcepool"
	"github.com/projectcapsule/capsule/pkg/webhook/route"
	"github.com/projectcapsule/capsule/pkg/webhook/service"
	"github.com/projectcapsule/capsule/pkg/webhook/serviceaccounts"
	tenantmutation "github.com/projectcapsule/capsule/pkg/webhook/tenant/mutation"
	tenantvalidation "github.com/projectcapsule/capsule/pkg/webhook/tenant/validation"
	tntresource "github.com/projectcapsule/capsule/pkg/webhook/tenantresource"
	"github.com/projectcapsule/capsule/pkg/webhook/utils"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(capsulev1beta1.AddToScheme(scheme))
	utilruntime.Must(capsulev1beta2.AddToScheme(scheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))
	utilruntime.Must(gatewayv1.Install(scheme))
}

func printVersion() {
	setupLog.Info(fmt.Sprintf("Capsule Version %s %s%s", GitTag, GitCommit, GitDirty))
	setupLog.Info(fmt.Sprintf("Build from: %s", GitRepo))
	setupLog.Info(fmt.Sprintf("Build date: %s", BuildTime))
	setupLog.Info(fmt.Sprintf("Go Version: %s", goRuntime.Version()))
	setupLog.Info(fmt.Sprintf("Go OS/Arch: %s/%s", goRuntime.GOOS, goRuntime.GOARCH))
}

//nolint:maintidx
func main() {
	var enableLeaderElection, version bool

	var metricsAddr, namespace, configurationName string

	var webhookPort int

	var goFlagSet goflag.FlagSet

	flag.IntVar(&webhookPort, "webhook-port", 9443, "The port the webhook server binds to.")
	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
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
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		WebhookServer: ctrlwebhook.NewServer(ctrlwebhook.Options{
			Port: webhookPort,
		}),
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "42c733ea.clastix.capsule.io",
		HealthProbeBindAddress: ":10080",
		NewClient: func(config *rest.Config, options client.Options) (client.Client, error) {
			options.Cache.Unstructured = true

			return client.New(config, options)
		},
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	_ = manager.AddReadyzCheck("ping", healthz.Ping)
	_ = manager.AddHealthzCheck("ping", healthz.Ping)

	ctx := ctrl.SetupSignalHandler()

	cfg := configuration.NewCapsuleConfiguration(ctx, manager.GetClient(), manager.GetConfig(), configurationName)

	directClient, err := client.New(ctrl.GetConfigOrDie(), client.Options{
		Scheme: manager.GetScheme(),
		Mapper: manager.GetRESTMapper(),
	})
	if err != nil {
		setupLog.Error(err, "unable to create the direct client")
		os.Exit(1)
	}

	directCfg := configuration.NewCapsuleConfiguration(ctx, directClient, manager.GetConfig(), configurationName)

	if directCfg.EnableTLSConfiguration() {
		tlsReconciler := &tlscontroller.Reconciler{
			Client:        directClient,
			Log:           ctrl.Log.WithName("controllers").WithName("TLS"),
			Namespace:     namespace,
			Configuration: directCfg,
		}

		if err = tlsReconciler.SetupWithManager(manager); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "Namespace")
			os.Exit(1)
		}

		tlsCert := &corev1.Secret{}

		if err = directClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: directCfg.TLSSecretName()}, tlsCert); err != nil {
			setupLog.Error(err, "unable to get Capsule TLS secret")
			os.Exit(1)
		}
		// Reconcile TLS certificates before starting controllers and webhooks
		if err = tlsReconciler.ReconcileCertificates(ctx, tlsCert); err != nil {
			setupLog.Error(err, "unable to reconcile Capsule TLS secret")
			os.Exit(1)
		}
	}

	if err = (&tenantcontroller.Manager{
		RESTConfig: manager.GetConfig(),
		Client:     manager.GetClient(),
		Metrics:    metrics.MustMakeTenantRecorder(),
		Log:        ctrl.Log.WithName("controllers").WithName("Tenant"),
		Recorder:   manager.GetEventRecorderFor("tenant-controller"),
	}).SetupWithManager(manager); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Tenant")
		os.Exit(1)
	}

	if err = (&capsulev1beta1.Tenant{}).SetupWebhookWithManager(manager); err != nil {
		setupLog.Error(err, "unable to create conversion webhook", "webhook", "capsulev1beta1.Tenant")
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
		route.Pod(pod.ImagePullPolicy(), pod.ContainerRegistry(cfg), pod.PriorityClass(), pod.RuntimeClass()),
		route.Namespace(utils.InCapsuleGroups(cfg, namespacevalidation.PatchHandler(cfg), namespacevalidation.QuotaHandler(), namespacevalidation.FreezeHandler(cfg), namespacevalidation.PrefixHandler(cfg), namespacevalidation.UserMetadataHandler())),
		route.Ingress(ingress.Class(cfg, kubeVersion), ingress.Hostnames(cfg), ingress.Collision(cfg), ingress.Wildcard()),
		route.PVC(pvc.Validating(), pvc.PersistentVolumeReuse()),
		route.Service(service.Handler()),
		route.TenantResourceNamespacedMutation(tntresource.NamespacedMutatingHandler(cfg)),
		route.TenantResourceGlobalMutation(tntresource.GlobalMutatingHandler(cfg)),
		route.TenantResourceObjectsValidation(utils.InCapsuleGroups(cfg, tntresource.ObjectsValidatingHandler())),
		route.NetworkPolicy(utils.InCapsuleGroups(cfg, networkpolicy.Handler())),
		route.TenantMutating(tenantmutation.MetaHandler()),
		route.TenantValidating(tenantvalidation.NameHandler(), tenantvalidation.RoleBindingRegexHandler(), tenantvalidation.IngressClassRegexHandler(), tenantvalidation.StorageClassRegexHandler(), tenantvalidation.ContainerRegistryRegexHandler(), tenantvalidation.HostnameRegexHandler(), tenantvalidation.FreezedEmitter(), tenantvalidation.ServiceAccountNameHandler(), tenantvalidation.ForbiddenAnnotationsRegexHandler(), tenantvalidation.ProtectedHandler()),
		route.Cordoning(tenantvalidation.CordoningHandler(cfg)),
		route.Node(utils.InCapsuleGroups(cfg, node.UserMetadataHandler(cfg, kubeVersion))),
		route.ServiceAccounts(serviceaccounts.Handler(cfg)),
		route.NamespacePatch(utils.InCapsuleGroups(cfg, namespacemutation.CordoningLabelHandler(cfg), namespacemutation.OwnerReferenceHandler(cfg), namespacemutation.MetadataHandler(cfg))),
		route.CustomResources(tenantvalidation.ResourceCounterHandler(manager.GetClient())),
		route.Gateway(gateway.Class(cfg)),
		route.Defaults(defaults.Handler(cfg, kubeVersion)),
		route.ResourcePoolMutation((resourcepool.PoolMutationHandler(ctrl.Log.WithName("webhooks").WithName("resourcepool")))),
		route.ResourcePoolValidation((resourcepool.PoolValidationHandler(ctrl.Log.WithName("webhooks").WithName("resourcepool")))),
		route.ResourcePoolClaimMutation((resourcepool.ClaimMutationHandler(ctrl.Log.WithName("webhooks").WithName("resourcepoolclaims")))),
		route.ResourcePoolClaimValidation((resourcepool.ClaimValidationHandler(ctrl.Log.WithName("webhooks").WithName("resourcepoolclaims")))),
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
		Client:        manager.GetClient(),
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

	if err = (&servicelabelscontroller.EndpointSlicesLabelsReconciler{
		Log: ctrl.Log.WithName("controllers").WithName("EndpointSliceLabels"),
	}).SetupWithManager(ctx, manager); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "EndpointSliceLabels")
	}

	if err = (&podlabelscontroller.MetadataReconciler{Client: manager.GetClient()}).SetupWithManager(ctx, manager); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "PodLabels")
		os.Exit(1)
	}

	if err = (&pv.Controller{}).SetupWithManager(manager); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "PersistentVolume")
		os.Exit(1)
	}

	if err = (&configcontroller.Manager{
		Log: ctrl.Log.WithName("controllers").WithName("CapsuleConfiguration"),
	}).SetupWithManager(manager, configurationName); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "CapsuleConfiguration")
		os.Exit(1)
	}

	if err := resources.Add(
		ctrl.Log.WithName("controllers").WithName("TenantResources"),
		manager,
		cfg,
	); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "tenantresources")
		os.Exit(1)
	}

	if err := resourcepools.Add(
		ctrl.Log.WithName("controllers").WithName("ResourcePools"),
		manager,
		manager.GetEventRecorderFor("pools-ctrl"),
	); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "resourcepools")
		os.Exit(1)
	}

	setupLog.Info("starting manager")

	if err = manager.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
