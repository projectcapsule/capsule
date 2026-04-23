// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"crypto/tls"
	goflag "flag"
	"fmt"
	"os"
	"path/filepath"
	goRuntime "runtime"

	flag "github.com/spf13/pflag"
	_ "go.uber.org/automaxprocs"
	"go.uber.org/zap/zapcore"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	utilVersion "k8s.io/apimachinery/pkg/util/version"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	ctrlwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	capsulev1beta1 "github.com/projectcapsule/capsule/api/v1beta1"
	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/internal/controllers/admission"
	cachecontroller "github.com/projectcapsule/capsule/internal/controllers/cfg/caches"
	configcontroller "github.com/projectcapsule/capsule/internal/controllers/cfg/status"
	customquotacontroller "github.com/projectcapsule/capsule/internal/controllers/customquotas"
	podlabelscontroller "github.com/projectcapsule/capsule/internal/controllers/pod"
	"github.com/projectcapsule/capsule/internal/controllers/pv"
	rbaccontroller "github.com/projectcapsule/capsule/internal/controllers/rbac"
	"github.com/projectcapsule/capsule/internal/controllers/resourcepools"
	"github.com/projectcapsule/capsule/internal/controllers/resources"
	servicelabelscontroller "github.com/projectcapsule/capsule/internal/controllers/servicelabels"
	tenantcontroller "github.com/projectcapsule/capsule/internal/controllers/tenant"
	tlscontroller "github.com/projectcapsule/capsule/internal/controllers/tls"
	utilscontroller "github.com/projectcapsule/capsule/internal/controllers/utils"
	"github.com/projectcapsule/capsule/internal/metrics"
	"github.com/projectcapsule/capsule/internal/webhook"
	cfgvalidation "github.com/projectcapsule/capsule/internal/webhook/cfg"
	customquotavalidation "github.com/projectcapsule/capsule/internal/webhook/customquota"
	"github.com/projectcapsule/capsule/internal/webhook/defaults"
	"github.com/projectcapsule/capsule/internal/webhook/dra"
	"github.com/projectcapsule/capsule/internal/webhook/gateway"
	"github.com/projectcapsule/capsule/internal/webhook/generic"
	"github.com/projectcapsule/capsule/internal/webhook/ingress"
	namespacemutation "github.com/projectcapsule/capsule/internal/webhook/namespace/mutation"
	namespacevalidation "github.com/projectcapsule/capsule/internal/webhook/namespace/validation"
	"github.com/projectcapsule/capsule/internal/webhook/node"
	"github.com/projectcapsule/capsule/internal/webhook/pod"
	"github.com/projectcapsule/capsule/internal/webhook/pvc"
	"github.com/projectcapsule/capsule/internal/webhook/resourcepool"
	"github.com/projectcapsule/capsule/internal/webhook/route"
	"github.com/projectcapsule/capsule/internal/webhook/service"
	"github.com/projectcapsule/capsule/internal/webhook/serviceaccounts"
	tenantmutation "github.com/projectcapsule/capsule/internal/webhook/tenant/mutation"
	tenantvalidation "github.com/projectcapsule/capsule/internal/webhook/tenant/validation"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/runtime/indexers"
	"github.com/projectcapsule/capsule/pkg/utils"
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
	utilruntime.Must(admissionv1.AddToScheme(scheme))

}

func printVersion() {
	setupLog.Info(fmt.Sprintf("Capsule Version %s %s%s", GitTag, GitCommit, GitDirty))
	setupLog.Info(fmt.Sprintf("Build from: %s", GitRepo))
	setupLog.Info(fmt.Sprintf("Build date: %s", BuildTime))
	setupLog.Info(fmt.Sprintf("Go Version: %s", goRuntime.Version()))
	setupLog.Info(fmt.Sprintf("Go OS/Arch: %s/%s", goRuntime.GOOS, goRuntime.GOARCH))
}

//nolint:maintidx,cyclop
func main() {
	controllerConfig := utilscontroller.ControllerOptions{}

	var metricsAddr, metricsCertPath, metricsCertName, metricsCertKey string
	var webhookCertPath, webhookCertName, webhookCertKey string
	var enableLeaderElection, enablePprof, version, secureMetrics, enableHTTP2 bool
	var webhookPort int

	var goFlagSet goflag.FlagSet

	var tlsOpts []func(*tls.Config)

	flag.StringVar(
		&controllerConfig.ConfigurationName,
		"configuration-name",
		"default",
		"The CapsuleConfiguration resource name to use",
	)

	flag.BoolVar(
		&enableLeaderElection,
		"enable-leader-election",
		false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.",
	)
	flag.IntVar(
		&controllerConfig.MaxConcurrentReconciles,
		"workers",
		1,
		"MaxConcurrentReconciles is the maximum number of concurrent Reconciles which can be run.",
	)
	flag.StringVar(
		&metricsAddr,
		"metrics-addr",
		":8080",
		"The address the metric endpoint binds to.",
	)
	flag.BoolVar(
		&secureMetrics,
		"metrics-secure",
		false,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.",
	)
	flag.StringVar(
		&metricsCertPath,
		"metrics-cert-path",
		"",
		"The directory that contains the metrics server certificate.",
	)
	flag.StringVar(
		&metricsCertName,
		"metrics-cert-name",
		"tls.crt",
		"The name of the metrics server certificate file.",
	)
	flag.StringVar(
		&metricsCertKey,
		"metrics-cert-key",
		"tls.key",
		"The name of the metrics server key file.",
	)
	flag.IntVar(
		&webhookPort,
		"webhook-port",
		9443,
		"The port the webhook server binds to.",
	)
	flag.StringVar(
		&webhookCertPath,
		"webhook-cert-path",
		"/tmp/k8s-webhook-server/serving-certs",
		"The directory that contains the webhook certificate.",
	)
	flag.StringVar(
		&webhookCertName,
		"webhook-cert-name",
		"tls.crt",
		"The name of the webhook certificate file.",
	)
	flag.StringVar(
		&webhookCertKey,
		"webhook-cert-key",
		"tls.key",
		"The name of the webhook key file.",
	)
	flag.BoolVar(
		&enableHTTP2,
		"enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers",
	)
	flag.BoolVar(
		&enablePprof,
		"enable-pprof",
		false,
		"Enables Pprof endpoint for profiling (not recommend in production)",
	)
	flag.BoolVar(
		&version,
		"version",
		false,
		"Print the Capsule version and exit",
	)

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

	setupLog.V(5).Info("Controller", "Options", controllerConfig)

	var ns string

	if ns = os.Getenv(configuration.EnvironmentControllerNamespace); len(ns) == 0 {
		setupLog.Error(fmt.Errorf("unable to determinate the Namespace Capsule is running on. Please export %s", configuration.EnvironmentControllerNamespace), "unable to start manager")
		os.Exit(1)
	}

	if serviceAccountName := os.Getenv(configuration.EnvironmentServiceaccountName); len(serviceAccountName) == 0 {
		setupLog.Error(fmt.Errorf("unable to determinate the ServiceAccount Capsule is running with. Please export %s", configuration.EnvironmentServiceaccountName), "unable to start manager")
		os.Exit(1)
	}

	if len(controllerConfig.ConfigurationName) == 0 {
		setupLog.Error(fmt.Errorf("missing CapsuleConfiguration resource name"), "unable to start manager")
		os.Exit(1)
	}

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	// Create watchers for metrics and webhooks certificates
	var metricsCertWatcher, webhookCertWatcher *certwatcher.CertWatcher

	// Initial webhook TLS options
	webhookTLSOpts := tlsOpts

	if len(webhookCertPath) > 0 {
		setupLog.Info(
			"Initializing webhook certificate watcher using provided certificates",
			"webhook-cert-path",
			webhookCertPath,
			"webhook-cert-name",
			webhookCertName,
			"webhook-cert-key",
			webhookCertKey,
		)

		var err error

		webhookCertWatcher, err = certwatcher.New(
			filepath.Join(webhookCertPath, webhookCertName),
			filepath.Join(webhookCertPath, webhookCertKey),
		)
		if err != nil {
			setupLog.Error(err, "Failed to initialize webhook certificate watcher")
			os.Exit(1)
		}

		webhookTLSOpts = append(webhookTLSOpts, func(config *tls.Config) {
			config.GetCertificate = webhookCertWatcher.GetCertificate
		})
	}

	// Metrics endpoint is enabled in 'config/default/kustomization.yaml'. The Metrics options configure the server.
	// More info:
	// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.4/pkg/metrics/server
	// - https://book.kubebuilder.io/reference/metrics.html
	metricsServerOptions := metricsserver.Options{
		BindAddress:   metricsAddr,
		SecureServing: secureMetrics,
		TLSOpts:       tlsOpts,
	}

	if secureMetrics {
		// FilterProvider is used to protect the metrics endpoint with authn/authz.
		// These configurations ensure that only authorized users and service accounts
		// can access the metrics endpoint. The RBAC are configured in 'config/rbac/kustomization.yaml'. More info:
		// https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.4/pkg/metrics/filters#WithAuthenticationAndAuthorization
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	// If the certificate is not specified, controller-runtime will automatically
	// generate self-signed certificates for the metrics server. While convenient for development and testing,
	// this setup is not recommended for production.
	//
	// TODO(user): If you enable certManager, uncomment the following lines:
	// - [METRICS-WITH-CERTS] at config/default/kustomization.yaml to generate and use certificates
	// managed by cert-manager for the metrics server.
	// - [PROMETHEUS-WITH-CERTS] at config/prometheus/kustomization.yaml for TLS certification.
	if len(metricsCertPath) > 0 {
		setupLog.Info(
			"Initializing metrics certificate watcher using provided certificates",
			"metrics-cert-path",
			metricsCertPath,
			"metrics-cert-name",
			metricsCertName,
			"metrics-cert-key",
			metricsCertKey,
		)

		var err error

		metricsCertWatcher, err = certwatcher.New(
			filepath.Join(metricsCertPath, metricsCertName),
			filepath.Join(metricsCertPath, metricsCertKey),
		)
		if err != nil {
			setupLog.Error(err, "to initialize metrics certificate watcher", "error", err)
			os.Exit(1)
		}

		metricsServerOptions.TLSOpts = append(
			metricsServerOptions.TLSOpts,
			func(config *tls.Config) {
				config.GetCertificate = metricsCertWatcher.GetCertificate
			},
		)
	}

	ctrlOpts := ctrl.Options{
		Scheme:  scheme,
		Metrics: metricsServerOptions,
		WebhookServer: ctrlwebhook.NewServer(ctrlwebhook.Options{
			Port:    webhookPort,
			TLSOpts: webhookTLSOpts,
		}),
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "42c733ea.clastix.capsule.io",
		HealthProbeBindAddress: ":10080",
		NewClient: func(config *rest.Config, options client.Options) (client.Client, error) {
			options.Cache.Unstructured = true

			return client.New(config, options)
		},
	}

	if enablePprof {
		ctrlOpts.PprofBindAddress = ":8082"
	}

	setupLog.Info("initializing manager")

	manager, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrlOpts)
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	_ = manager.AddReadyzCheck("ping", healthz.Ping)
	_ = manager.AddHealthzCheck("ping", healthz.Ping)

	ctx := ctrl.SetupSignalHandler()

	dc, err := discovery.NewDiscoveryClientForConfig(manager.GetConfig())
	if err != nil {
		setupLog.Error(err, "unable to create discovery client")
		os.Exit(1)
	}

	dynamicClient, err := dynamic.NewForConfig(manager.GetConfig())
	if err != nil {
		setupLog.Error(err, "unable to create dynamic client")
		os.Exit(1)
	}

	setupLog.Info("initializing capsule configuration")

	cfg := configuration.NewCapsuleConfiguration(ctx, manager.GetClient(), manager.GetConfig(), controllerConfig.ConfigurationName)

	directClient, err := client.New(ctrl.GetConfigOrDie(), client.Options{
		Scheme: manager.GetScheme(),
		Mapper: manager.GetRESTMapper(),
	})
	if err != nil {
		setupLog.Error(err, "unable to create the direct client")
		os.Exit(1)
	}

	directCfg := configuration.NewCapsuleConfiguration(ctx, directClient, manager.GetConfig(), controllerConfig.ConfigurationName)

	if directCfg.EnableTLSConfiguration() {
		tlsReconciler := &tlscontroller.Reconciler{
			Client:        directClient,
			Log:           ctrl.Log.WithName("capsule.ctrl").WithName("tls"),
			Namespace:     ns,
			Configuration: directCfg,
		}

		if err = tlsReconciler.SetupWithManager(manager); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "Namespace")
			os.Exit(1)
		}

		tlsCert := &corev1.Secret{}

		if err = directClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: directCfg.TLSSecretName()}, tlsCert); err != nil {
			setupLog.Error(err, "unable to get Capsule TLS secret")
			os.Exit(1)
		}
		// Reconcile TLS certificates before starting controllers and webhooks
		if err = tlsReconciler.ReconcileCertificates(ctx, tlsCert); err != nil {
			setupLog.Error(err, "unable to reconcile Capsule TLS secret")
			os.Exit(1)
		}
	}

	setupLog.Info("initializing caches")

	// Initialize Notifiers (Channels)
	customQuotaCh := make(chan event.TypedGenericEvent[*capsulev1beta2.CustomQuota], 1024)
	globalCustomQuotaCh := make(chan event.TypedGenericEvent[*capsulev1beta2.GlobalCustomQuota], 1024)

	// Initialize Caches
	impersonationCache := cache.NewImpersonationCache()
	registryCache := cache.NewRegistryRuleSetCache()
	customQuotaQuantityCache := cache.NewQuantityCache[string]()
	jsonPathCache := cache.NewJSONPathCache()
	targetsCache := cache.NewCompiledTargetsCache[string]()

	if err = (&tenantcontroller.Manager{
		RESTConfig:      manager.GetConfig(),
		Client:          manager.GetClient(),
		DynamicClient:   dynamicClient,
		DiscoveryClient: dc,
		Metrics:         metrics.MustMakeTenantRecorder(),
		Log:             ctrl.Log.WithName("capsule.ctrl").WithName("tenant"),
		Recorder:        manager.GetEventRecorder("tenant-controller"),
		Configuration:   cfg,
	}).SetupWithManager(manager, controllerConfig); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Tenant")
		os.Exit(1)
	}

	if err = (&capsulev1beta1.Tenant{}).SetupWebhookWithManager(manager); err != nil {
		setupLog.Error(err, "unable to create conversion webhook", "webhook", "capsulev1beta1.Tenant")
		os.Exit(1)
	}

	setupLog.Info("registering indexers")

	if err = indexers.AddToManager(ctx, setupLog, manager); err != nil {
		setupLog.Error(err, "unable to setup indexers")
		os.Exit(1)
	}

	var kubeVersion *utilVersion.Version

	if kubeVersion, err = utils.GetK8sVersionFromConfig(dc); err != nil {
		setupLog.Error(err, "unable to get kubernetes version")
		os.Exit(1)
	}

	setupLog.Info("registering webhooks")

	// webhooks: the order matters, don't change it and just append
	webhooksList := append(
		make([]handlers.Webhook, 0),
		route.GenericReplicasHandler(),
		route.GenericManagedHandler(cfg),
		route.Pod(
			pod.Handler(
				pod.ImagePullPolicy(),
				pod.ContainerRegistryLegacy(cfg),
				pod.ContainerRegistry(cfg, registryCache),
				pod.PriorityClass(),
				pod.RuntimeClass(),
			),
		),
		route.Ingress(ingress.Class(cfg, kubeVersion), ingress.Hostnames(cfg), ingress.Collision(cfg), ingress.Wildcard()),
		route.PVC(
			pvc.Handler(
				pvc.Validating(),
				pvc.PersistentVolumeReuse(),
			),
		),
		route.Service(
			service.Handler(
				service.Validating(),
			),
		),
		route.Node(handlers.InCapsuleGroups(cfg, node.UserMetadataHandler(cfg, kubeVersion))),
		route.Cordoning(handlers.InCapsuleGroups(cfg, generic.CordoningHandler(cfg))),
		route.ServiceAccounts(
			serviceaccounts.Handler(
				serviceaccounts.Promotion(cfg),
				serviceaccounts.OwnerPromotion(cfg),
			),
		),
		route.GenericCustomResources(generic.ResourceCounterHandler(manager.GetClient())),
		route.Gateway(gateway.Class(cfg)),
		route.DeviceClass(dra.DeviceClass()),
		route.Defaults(defaults.Handler(cfg, kubeVersion)),
		route.TenantMutation(
			tenantmutation.MetaHandler(),
		),
		route.TenantValidation(
			tenantvalidation.Handler(cfg,
				tenantvalidation.NameHandler(),
				tenantvalidation.RoleBindingRegexHandler(),
				tenantvalidation.IngressClassRegexHandler(),
				tenantvalidation.StorageClassRegexHandler(),
				tenantvalidation.ContainerRegistryRegexHandler(),
				tenantvalidation.RuleHandler(),
				tenantvalidation.HostnameRegexHandler(),
				tenantvalidation.FreezedEmitter(),
				tenantvalidation.ServiceAccountNameHandler(),
				tenantvalidation.ForbiddenAnnotationsRegexHandler(),
				tenantvalidation.ProtectedHandler(),
				tenantvalidation.RequiredMetadataHandler(),
				tenantvalidation.WarningHandler(cfg),
				tenantvalidation.RemainingNamespaceHandler(),
			),
		),
		route.NamespaceValidation(
			namespacevalidation.NamespaceHandler(
				cfg,
				namespacevalidation.PatchHandler(cfg),
				namespacevalidation.FreezeHandler(cfg),
				namespacevalidation.QuotaHandler(),
				namespacevalidation.PrefixHandler(cfg),
				namespacevalidation.UserMetadataHandler(),
				namespacevalidation.RequiredMetadataHandler(),
			),
		),
		route.NamespaceMutation(
			namespacemutation.NamespaceHandler(
				cfg,
				namespacemutation.OwnerReferenceHandler(cfg),
				namespacemutation.MetadataHandler(cfg),
				namespacemutation.CordoningLabelHandler(cfg),
			),
		),
		route.ResourcePoolMutation((resourcepool.PoolMutationHandler(ctrl.Log.WithName("webhooks").WithName("resourcepool")))),
		route.ResourcePoolValidation((resourcepool.PoolValidationHandler(ctrl.Log.WithName("webhooks").WithName("resourcepool")))),
		route.ResourcePoolClaimMutation((resourcepool.ClaimMutationHandler(ctrl.Log.WithName("webhooks").WithName("resourcepoolclaims")))),
		route.ResourcePoolClaimValidation((resourcepool.ClaimValidationHandler(ctrl.Log.WithName("webhooks").WithName("resourcepoolclaims")))),
		route.CustomQuotaValidation((customquotavalidation.CustomQuotaValidationHandler())),
		route.GlobalCustomQuotaValidation((customquotavalidation.GlobalCustomQuotaValidationHandler())),
		route.GenericCustomQuotas(
			customquotavalidation.ObjectCalculationHandler(
				targetsCache,
				jsonPathCache,
			),
		),
		route.GenericTenantAssignment(
			generic.TenantAssignmentHandler(),
		),
		route.ConfigValidation(
			cfgvalidation.Handler(cfg,
				cfgvalidation.WarningHandler(),
				cfgvalidation.ServiceAccountHandler(),
			),
		),
	)

	nodeWebhookSupported, _ := utils.NodeWebhookSupported(kubeVersion)
	if !nodeWebhookSupported {
		setupLog.Info("disabling node labels verification webhook as current Kubernetes version doesn't have fix for CVE-2021-25735")
	}

	if err = webhook.Register(manager, webhooksList...); err != nil {
		setupLog.Error(err, "unable to setup webhooks")
		os.Exit(1)
	}

	rbacManager := &rbaccontroller.Manager{
		Log:           ctrl.Log.WithName("capsule.ctrl").WithName("rbac"),
		Client:        manager.GetClient(),
		Configuration: cfg,
	}

	if err = manager.Add(rbacManager); err != nil {
		setupLog.Error(err, "unable to create cluster roles")
		os.Exit(1)
	}

	if err = rbacManager.SetupWithManager(ctx, manager, controllerConfig); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Rbac")
		os.Exit(1)
	}

	if err = (&servicelabelscontroller.ServicesLabelsReconciler{
		Log: ctrl.Log.WithName("capsule.ctrl").WithName("services"),
	}).SetupWithManager(ctx, manager); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ServiceLabels")
		os.Exit(1)
	}

	if err = (&servicelabelscontroller.EndpointSlicesLabelsReconciler{
		Log: ctrl.Log.WithName("capsule.ctrl").WithName("endpointslices"),
	}).SetupWithManager(ctx, manager); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "EndpointSliceLabels")
	}

	if err = (&podlabelscontroller.MetadataReconciler{
		Client: manager.GetClient(),
		Log:    ctrl.Log.WithName("capsule.ctrl").WithName("pods"),
	}).SetupWithManager(ctx, manager); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "PodLabels")
		os.Exit(1)
	}

	if err = (&pv.Controller{}).SetupWithManager(manager, controllerConfig); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "PersistentVolume")
		os.Exit(1)
	}

	if err = (&configcontroller.Manager{
		Rest:   manager.GetConfig(),
		Client: manager.GetClient(),
		Log:    ctrl.Log.WithName("capsule.ctrl").WithName("configuration"),
	}).SetupWithManager(manager, controllerConfig); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "CapsuleConfiguration")
		os.Exit(1)
	}

	setupLog.Info("initializing", "controller", "serviceaccounts")

	if err = (&cachecontroller.Manager{
		Log:                ctrl.Log.WithName("chache").WithName("clients"),
		Client:             manager.GetClient(),
		Configuration:      cfg,
		ImpersonationCache: impersonationCache,
		RegistryCache:      registryCache,
	}).SetupWithManager(manager, controllerConfig); err != nil {
		setupLog.Error(err, "unable to create controller", "cache", "ServiceAccounts")
		os.Exit(1)
	}

	if err := resources.Add(
		ctrl.Log.WithName("controllers").WithName("TenantResources"),
		manager,
		cfg,
		controllerConfig,
		impersonationCache,
	); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "tenantresources")
		os.Exit(1)
	}

	if err := admission.Add(
		ctrl.Log.WithName("capsule.ctrl").WithName("admission"),
		manager,
		manager.GetEventRecorder("admission-ctrl"),
		controllerConfig,
		directCfg,
	); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "admission")
		os.Exit(1)
	}

	if err := resourcepools.Add(
		ctrl.Log.WithName("capsule.ctrl").WithName("resourcepools"),
		manager,
		manager.GetEventRecorder("pools-ctrl"),
		controllerConfig,
	); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "resourcepools")
		os.Exit(1)
	}

	if err = customquotacontroller.Add(ctrl.Log.WithName("controllers").WithName("CustomQuotas"),
		manager,
		manager.GetEventRecorder("customquotas-ctrl"),
		controllerConfig,
		customQuotaQuantityCache,
		jsonPathCache,
		targetsCache,
		customQuotaCh,
		globalCustomQuotaCh,
	); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "customquotas")
		os.Exit(1)
	}

	setupLog.Info("starting manager")

	if err = manager.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
