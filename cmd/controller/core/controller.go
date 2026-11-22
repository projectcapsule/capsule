// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "go.uber.org/automaxprocs"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	utilVersion "k8s.io/apimachinery/pkg/util/version"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	ctrlwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"

	capsulev1beta1 "github.com/projectcapsule/capsule/api/v1beta1"
	"github.com/projectcapsule/capsule/cmd/controller/common"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/internal/controllers/admission"
	cacheinvalidator "github.com/projectcapsule/capsule/internal/controllers/cfg/invalidator"
	configcontroller "github.com/projectcapsule/capsule/internal/controllers/cfg/status"
	customquotacontroller "github.com/projectcapsule/capsule/internal/controllers/customquotas"
	globalresourcequotacontroller "github.com/projectcapsule/capsule/internal/controllers/globalresourcequotas"
	podlabelscontroller "github.com/projectcapsule/capsule/internal/controllers/pod"
	"github.com/projectcapsule/capsule/internal/controllers/pv"
	rbaccontroller "github.com/projectcapsule/capsule/internal/controllers/rbac"
	"github.com/projectcapsule/capsule/internal/controllers/resourcepools"
	rulestatuscontroller "github.com/projectcapsule/capsule/internal/controllers/rulestatus"
	servicelabelscontroller "github.com/projectcapsule/capsule/internal/controllers/servicelabels"
	tenantcontroller "github.com/projectcapsule/capsule/internal/controllers/tenant"
	tenantownercontroller "github.com/projectcapsule/capsule/internal/controllers/tenantowner"
	tlscontroller "github.com/projectcapsule/capsule/internal/controllers/tls"
	"github.com/projectcapsule/capsule/internal/metrics"
	"github.com/projectcapsule/capsule/internal/webhook"
	cfgvalidation "github.com/projectcapsule/capsule/internal/webhook/cfg"
	customquotavalidation "github.com/projectcapsule/capsule/internal/webhook/customquota"
	"github.com/projectcapsule/capsule/internal/webhook/defaults"
	"github.com/projectcapsule/capsule/internal/webhook/dra"
	"github.com/projectcapsule/capsule/internal/webhook/gateway"
	"github.com/projectcapsule/capsule/internal/webhook/generic"
	globalresourcequotavalidation "github.com/projectcapsule/capsule/internal/webhook/globalresourcequota"
	"github.com/projectcapsule/capsule/internal/webhook/ingress"
	namespacemutation "github.com/projectcapsule/capsule/internal/webhook/namespace/mutation"
	namespacevalidation "github.com/projectcapsule/capsule/internal/webhook/namespace/validation"
	"github.com/projectcapsule/capsule/internal/webhook/node"
	"github.com/projectcapsule/capsule/internal/webhook/owners"
	"github.com/projectcapsule/capsule/internal/webhook/pod"
	"github.com/projectcapsule/capsule/internal/webhook/pvc"
	"github.com/projectcapsule/capsule/internal/webhook/resourcepool"
	"github.com/projectcapsule/capsule/internal/webhook/route"
	rulesgenericmutation "github.com/projectcapsule/capsule/internal/webhook/rules/generic/mutation"
	rulesgenericvalidation "github.com/projectcapsule/capsule/internal/webhook/rules/generic/validation"
	networkpolicyrules "github.com/projectcapsule/capsule/internal/webhook/rules/networkpolicies/validation"
	podrules "github.com/projectcapsule/capsule/internal/webhook/rules/pods/validation"
	pvcrules "github.com/projectcapsule/capsule/internal/webhook/rules/pvc/validation"
	servicerules "github.com/projectcapsule/capsule/internal/webhook/rules/services/validation"
	volumesnapshotrules "github.com/projectcapsule/capsule/internal/webhook/rules/volumesnapshots/validation"
	"github.com/projectcapsule/capsule/internal/webhook/service"
	"github.com/projectcapsule/capsule/internal/webhook/serviceaccounts"
	tenantmutation "github.com/projectcapsule/capsule/internal/webhook/tenant/mutation"
	tenantvalidation "github.com/projectcapsule/capsule/internal/webhook/tenant/validation"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	evt "github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/runtime/indexers"
	"github.com/projectcapsule/capsule/pkg/utils"
)

//nolint:maintidx,gocyclo,cyclop,gocognit
func Run(ctx context.Context, options common.Options) error {
	setupLog := ctrl.Log.WithName("setup").WithName("core")

	b, err := common.NewBootstrap(ctx, options)
	if err != nil {
		return err
	}

	ns, directClient, directCfg := b.Namespace, b.DirectClient, b.Configuration
	controllerConfig := options.Controller
	tlsReconciler := &tlscontroller.Reconciler{}

	if directCfg.EnableTLSConfiguration() {
		tlsReconciler = &tlscontroller.Reconciler{
			Client:        directClient,
			Log:           ctrl.Log.WithName("capsule.ctrl").WithName("tls"),
			Namespace:     ns,
			Configuration: directCfg,
		}

		tlsCert := &corev1.Secret{}

		if err = directClient.Get(ctx, types.NamespacedName{
			Namespace: ns,
			Name:      directCfg.TLSSecretName(),
		}, tlsCert); err != nil {
			if !apierrors.IsNotFound(err) {
				return fmt.Errorf("unable to get Capsule TLS secret: %w", err)
			}

			tlsCert = &corev1.Secret{}
			tlsCert.Name = directCfg.TLSSecretName()
			tlsCert.Namespace = ns
			tlsCert.Data = map[string][]byte{}
		}

		// Reconcile TLS certificates before starting controllers and webhooks
		if err = tlsReconciler.ReconcileCertificates(ctx, ctrl.Log.WithName("capsule.setup").WithName("tls"), tlsCert); err != nil {
			return fmt.Errorf("unable to reconcile Capsule TLS secret: %w", err)
		}
	}

	ctrlOpts, metricsWatcher, err := options.ManagerOptions(b, "42c733ea.clastix.capsule.io")
	if err != nil {
		return err
	}
	// Initial webhook TLS options
	webhookTLSOpts := options.TLSOptions()

	var webhookCertWatcher *certwatcher.CertWatcher

	if len(options.WebhookCertPath) > 0 {
		setupLog.Info(
			"Initializing webhook certificate watcher using provided certificates",
			"webhook-cert-path",
			options.WebhookCertPath,
			"webhook-cert-name",
			options.WebhookCertName,
			"webhook-cert-key",
			options.WebhookCertKey,
		)

		var err error

		webhookCertWatcher, err = certwatcher.New(
			filepath.Join(options.WebhookCertPath, options.WebhookCertName),
			filepath.Join(options.WebhookCertPath, options.WebhookCertKey),
		)
		if err != nil {
			return fmt.Errorf("failed to initialize webhook certificate watcher: %w", err)
		}

		webhookTLSOpts = append(webhookTLSOpts, func(config *tls.Config) {
			config.GetCertificate = webhookCertWatcher.GetCertificate
		})
	}

	ctrlOpts.WebhookServer = ctrlwebhook.NewServer(ctrlwebhook.Options{Port: options.WebhookPort, TLSOpts: webhookTLSOpts})

	manager, err := ctrl.NewManager(b.Config, ctrlOpts)
	if err != nil {
		return fmt.Errorf("create core manager: %w", err)
	}

	if err := common.AddHealthChecks(manager, metricsWatcher); err != nil {
		return err
	}

	if webhookCertWatcher != nil {
		if err := manager.Add(webhookCertWatcher); err != nil {
			return fmt.Errorf("unable to add webhook certificate watcher: %w", err)
		}
	}

	if directCfg.EnableTLSConfiguration() && webhookCertWatcher != nil {
		if err := manager.AddReadyzCheck(
			"webhook-certificate",
			tlscontroller.WebhookCertificateReadinessCheck(
				directClient,
				directCfg,
				ns,
				webhookCertWatcher.GetCertificate,
			),
		); err != nil {
			return fmt.Errorf("unable to add webhook certificate readiness check: %w", err)
		}
	}

	dc, err := discovery.NewDiscoveryClientForConfig(manager.GetConfig())
	if err != nil {
		return fmt.Errorf("unable to create discovery client: %w", err)
	}

	dynamicClient, err := dynamic.NewForConfig(manager.GetConfig())
	if err != nil {
		return fmt.Errorf("unable to create dynamic client: %w", err)
	}

	setupLog.Info("initializing capsule configuration")

	cfg := configuration.NewCapsuleConfiguration(ctx, manager.GetClient(), manager.GetAPIReader(), manager.GetConfig(), controllerConfig.ConfigurationName)

	setupLog.Info("initializing caches")

	// Initialize Caches
	regexCache := cache.NewRegexCache()
	labelSelectorCache := cache.NewLabelSelectorCache()

	registryCache := cache.NewRegistryRuleSetCache(regexCache)
	jsonPathCache := cache.NewJSONPathCache()

	celCache, err := cache.NewCELCache()
	if err != nil {
		return fmt.Errorf("unable to initialize Kubernetes CEL cache: %w", err)
	}

	targetsCache := cache.NewCompiledTargetsCache[string]()

	if directCfg.EnableTLSConfiguration() {
		if err = tlsReconciler.SetupWithManager(manager, controllerConfig); err != nil {
			return fmt.Errorf("unable to create controller: controller Namespace: %w", err)
		}
	}

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
		return fmt.Errorf("unable to create controller: controller Tenant: %w", err)
	}

	if err = (&capsulev1beta1.Tenant{}).SetupWebhookWithManager(manager); err != nil {
		return fmt.Errorf("unable to create conversion webhook: webhook capsulev1beta1.Tenant: %w", err)
	}

	setupLog.Info("registering indexers")

	if err = indexers.AddToManager(ctx, setupLog, manager); err != nil {
		return fmt.Errorf("unable to setup indexers: %w", err)
	}

	var kubeVersion *utilVersion.Version

	if kubeVersion, err = utils.GetK8sVersionFromConfig(dc); err != nil {
		return fmt.Errorf("unable to get Kubernetes version: %w", err)
	}

	setupLog.Info("registering webhooks")

	// webhooks: the order matters, don't change it and just append
	webhooksList := append(
		make([]handlers.Webhook, 0),
		rulesgenericmutation.Register(cfg, celCache),
		rulesgenericvalidation.Register(
			regexCache,
			labelSelectorCache,
			cfg,
			celCache,
			podrules.TemplateRules(regexCache, registryCache, labelSelectorCache, celCache),
			rulesgenericvalidation.ForKind(
				corev1.SchemeGroupVersion.WithKind("Pod").GroupKind(),
				pod.Handler(cfg,
					podrules.PodRules(regexCache, registryCache, labelSelectorCache, celCache),
				),
				"ephemeralcontainers",
			),
			rulesgenericvalidation.ForKind(
				corev1.SchemeGroupVersion.WithKind("Service").GroupKind(),
				service.Handler(cfg,
					servicerules.ServiceRules(regexCache, celCache),
				),
			),
			networkpolicyrules.Handler(cfg, celCache),
			volumesnapshotrules.Handler(cfg, regexCache, labelSelectorCache, celCache),
			pvcrules.AttributeClassHandler(cfg, regexCache, labelSelectorCache, celCache),
		),
		route.GenericReplicasHandler(),
		route.GenericManagedHandler(cfg),
		route.Pod(
			pod.Handler(cfg,
				pod.ImagePullPolicy(),
				pod.ContainerRegistryLegacy(cfg),
				pod.PriorityClass(),
				pod.RuntimeClass(),
			),
		),
		route.Ingress(ingress.Class(cfg, kubeVersion), ingress.Hostnames(cfg), ingress.Collision(cfg), ingress.Wildcard()),
		route.PVCValidating(
			pvc.Handler(
				pvc.PersistentVolumeValidatingVolume(pvcrules.VolumeRules(cfg, labelSelectorCache, celCache)),
				pvc.PersistentVolumeValidatingClass(),
			),
		),
		route.PVCMutating(
			pvc.MutatingHandler(
				pvc.PersistentVolumeMutatingVolume(),
			),
		),
		route.Service(
			service.Handler(cfg,
				service.Validating(),
			),
		),
		route.Node(handlers.InCapsuleGroups(cfg, node.UserMetadataHandler(cfg, kubeVersion))),
		route.Cordoning(handlers.InCapsuleGroups(cfg, generic.CordoningHandler(cfg))),
		route.ServiceAccountReferences(serviceaccounts.ReferenceProtection()),
		route.ServiceAccounts(
			serviceaccounts.Handler(
				cfg,
				serviceaccounts.Promotion(cfg),
				serviceaccounts.OwnerPromotion(cfg),
			),
		),
		route.GenericCustomResources(generic.ResourceCounterHandler(manager.GetClient())),
		route.Gateway(gateway.Class(cfg)),
		route.DeviceClass(dra.DeviceClass(cfg, regexCache, labelSelectorCache, celCache)),
		route.Defaults(defaults.Handler(cfg, kubeVersion, celCache)),
		route.TenantMutation(
			tenantmutation.MetaHandler(),
		),
		route.TenantValidation(
			tenantvalidation.Handler(cfg,
				tenantvalidation.NameHandler(),
				tenantvalidation.NamespaceMetadataHandler(),
				tenantvalidation.RoleBindingRegexHandler(),
				tenantvalidation.IngressClassRegexHandler(),
				tenantvalidation.StorageClassRegexHandler(),
				tenantvalidation.ContainerRegistryRegexHandler(),
				tenantvalidation.PriorityClassRegexHandler(),
				tenantvalidation.RuntimeClassRegexHandler(),
				tenantvalidation.GatewayClassRegexHandler(),
				tenantvalidation.DeviceClassRegexHandler(),
				tenantvalidation.RuleHandler(manager.GetRESTMapper(), celCache),
				tenantvalidation.HostnameRegexHandler(),
				tenantvalidation.FreezedEmitter(),
				tenantvalidation.OwnersHandler(),
				tenantvalidation.ForbiddenAnnotationsRegexHandler(),
				tenantvalidation.ProtectedHandler(),
				tenantvalidation.RequiredMetadataHandler(),
				// Must run last, because always returns response
				tenantvalidation.WarningHandler(cfg),
			),
		),
		route.TenantOwnersValidation(
			owners.UserMetadataHandler(),
		),
		route.NamespaceValidation(
			namespacevalidation.NamespaceHandler(
				cfg,
				namespacevalidation.CordoningHandler(cfg),
				namespacevalidation.PrefixHandler(cfg),
				namespacevalidation.RulesMetadataHandler(regexCache, cfg, celCache),
				namespacevalidation.UserMetadataHandler(),
				namespacevalidation.RequiredMetadataHandler(),
				// Must run last: it reserves a slot of the namespace quota,
				// which a Namespace denied afterwards would hold until it expires.
				namespacevalidation.QuotaHandler(),
			),
		),
		route.NamespaceMutation(
			namespacemutation.NamespaceHandler(
				cfg,
				namespacemutation.OwnerReferenceHandler(cfg),
				namespacemutation.MetadataHandler(cfg),
				// Tenant metadata must be resolved before applying namespace rules.
				namespacemutation.RulesMetadataHandler(cfg, celCache),
			),
		),
		route.ResourcePoolMutation(resourcepool.PoolMutationHandler(ctrl.Log.WithName("webhooks").WithName("resourcepool"))),
		route.ResourcePoolValidation(resourcepool.PoolValidationHandler(ctrl.Log.WithName("webhooks").WithName("resourcepool"))),
		route.ResourcePoolClaimMutation(resourcepool.ClaimMutationHandler(ctrl.Log.WithName("webhooks").WithName("resourcepoolclaims"))),
		route.ResourcePoolClaimValidation(resourcepool.ClaimValidationHandler(ctrl.Log.WithName("webhooks").WithName("resourcepoolclaims"))),
		route.CustomQuotaValidation(customquotavalidation.CustomQuotaValidationHandler(
			targetsCache,
			jsonPathCache,
			celCache,
		)),
		route.GlobalCustomQuotaValidation(customquotavalidation.GlobalCustomQuotaValidationHandler(
			targetsCache,
			jsonPathCache,
			celCache,
		)),
		route.GlobalResourceQuotaCalculation(globalresourcequotavalidation.Handler()),
		route.CalculationCustomQuotas(
			customquotavalidation.ObjectCalculationHandler(
				targetsCache,
				jsonPathCache,
				celCache,
			),
		),
		route.GenericTenantAssignment(
			generic.TenantAssignmentHandler(),
		),
		route.ConfigValidation(
			cfgvalidation.Handler(cfg,
				cfgvalidation.ValidationHandler(regexCache),
				cfgvalidation.ServiceAccountHandler(),
				cfgvalidation.OwnerHandler(),
				// Must run last, because always returns response
				cfgvalidation.WarningHandler(),
			),
		),
		route.RulesValidating(manager.GetRESTMapper(), cfg, celCache),
	)

	nodeWebhookSupported, _ := utils.NodeWebhookSupported(kubeVersion)
	if !nodeWebhookSupported {
		setupLog.Info("disabling node labels verification webhook as current Kubernetes version doesn't have fix for CVE-2021-25735")
	}

	if err = webhook.Register(
		manager,
		evt.NewEventRecorder( //nolint:contextcheck // Existing bounded recorder owns process-lifetime asynchronous event delivery.
			manager.GetClient(),
			ctrl.Log.WithName("capsule.ctrl").WithName("events"),
			manager.GetEventRecorder("tenant-controller"),
			cfg,
		),
		webhook.RegistrationOptions{
			EnableTracing: options.EnableTracing,
		},
		webhooksList...); err != nil {
		return fmt.Errorf("unable to setup webhooks: %w", err)
	}

	rbacManager := &rbaccontroller.Manager{
		Log:           ctrl.Log.WithName("capsule.ctrl").WithName("rbac"),
		Client:        manager.GetClient(),
		Configuration: cfg,
	}

	if err = manager.Add(rbacManager); err != nil {
		return fmt.Errorf("unable to create cluster roles: %w", err)
	}

	if err = rbacManager.SetupWithManager(ctx, manager, controllerConfig); err != nil {
		return fmt.Errorf("unable to create controller: controller Rbac: %w", err)
	}

	if err = (&tenantownercontroller.TenantOwnerManager{
		Log:    ctrl.Log.WithName("capsule.ctrl").WithName("tenantowners"),
		Client: manager.GetClient(),
	}).SetupWithManager(manager, controllerConfig, metrics.MustMakeTenantOwnerRecorder()); err != nil {
		return fmt.Errorf("unable to create controller: controller TenantOwners: %w", err)
	}

	if err = (&servicelabelscontroller.ServicesLabelsReconciler{
		Log: ctrl.Log.WithName("capsule.ctrl").WithName("services"),
	}).SetupWithManager(ctx, manager, controllerConfig); err != nil {
		return fmt.Errorf("unable to create controller: controller ServiceLabels: %w", err)
	}

	if err = (&servicelabelscontroller.EndpointSlicesLabelsReconciler{
		Log: ctrl.Log.WithName("capsule.ctrl").WithName("endpointslices"),
	}).SetupWithManager(ctx, manager, controllerConfig); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "EndpointSliceLabels")
	}

	if err = (&podlabelscontroller.MetadataReconciler{
		Client: manager.GetClient(),
		Log:    ctrl.Log.WithName("capsule.ctrl").WithName("pods"),
	}).SetupWithManager(ctx, manager, controllerConfig); err != nil {
		return fmt.Errorf("unable to create controller: controller PodLabels: %w", err)
	}

	if err = (&pv.Controller{}).SetupWithManager(manager, controllerConfig); err != nil {
		return fmt.Errorf("unable to create controller: controller PersistentVolume: %w", err)
	}

	configrecorder := metrics.MustMakeConfigRecorder()

	if err = (&configcontroller.Manager{
		Rest:   manager.GetConfig(),
		Client: manager.GetClient(),
		Log:    ctrl.Log.WithName("capsule.ctrl").WithName("configuration"),
	}).SetupWithManager(manager, controllerConfig, configrecorder); err != nil {
		return fmt.Errorf("unable to create controller: controller CapsuleConfiguration: %w", err)
	}

	if err = (&rulestatuscontroller.Manager{
		Client:     manager.GetClient(),
		RESTConfig: manager.GetConfig(),
		Log:        ctrl.Log.WithName("capsule.ctrl").WithName("ruleset"),
		Metrics:    metrics.MustMakeRuleStatusRecorder(),
	}).SetupWithManager(manager, controllerConfig); err != nil {
		return fmt.Errorf("unable to create controller: controller RuleSet: %w", err)
	}

	setupLog.Info("initializing controllers")

	localInvalidator := &cacheinvalidator.CacheInvalidator{
		Log:           ctrl.Log.WithName("capsule.ctrl").WithName("invalidator"),
		Client:        manager.GetClient(),
		Configuration: directCfg,
		RegistryCache: registryCache,
		JSONPathCache: jsonPathCache,
		CELCache:      celCache,
		TargetsCache:  targetsCache,
		RegexCache:    regexCache,
	}

	if err := localInvalidator.SetupWithManager(manager, controllerConfig, configrecorder); err != nil {
		return fmt.Errorf("unable to create controller: controller invalidator: %w", err)
	}

	if err := admission.Add(
		ctrl.Log.WithName("capsule.ctrl").WithName("admission"),
		manager,
		manager.GetEventRecorder("admission-ctrl"),
		controllerConfig,
		directCfg,
	); err != nil {
		return fmt.Errorf("unable to create controller: controller admission: %w", err)
	}

	if err := resourcepools.Add(
		ctrl.Log.WithName("capsule.ctrl").WithName("resourcepools"),
		manager,
		manager.GetEventRecorder("pools-ctrl"),
		controllerConfig,
	); err != nil {
		return fmt.Errorf("unable to create controller: controller resourcepools: %w", err)
	}

	if err := globalresourcequotacontroller.Add(
		ctrl.Log.WithName("capsule.ctrl").WithName("globalresourcequotas"),
		manager,
		manager.GetEventRecorder("globalresourcequotas-ctrl"),
		controllerConfig,
	); err != nil {
		return fmt.Errorf("unable to create controller: controller globalresourcequotas: %w", err)
	}

	if err = customquotacontroller.Add(ctrl.Log.WithName("controllers").WithName("CustomQuotas"),
		manager,
		manager.GetEventRecorder("customquotas-ctrl"),
		controllerConfig,
		jsonPathCache,
		celCache,
		targetsCache,
	); err != nil {
		return fmt.Errorf("unable to create controller: controller customquotas: %w", err)
	}

	setupLog.Info("starting manager")

	if options.TracingBasicAuthUsername == "" {
		options.TracingBasicAuthUsername = os.Getenv("CAPSULE_TRACING_OTLP_BASIC_AUTH_USERNAME")
	}

	if options.TracingBasicAuthPassword == "" {
		options.TracingBasicAuthPassword = os.Getenv("CAPSULE_TRACING_OTLP_BASIC_AUTH_PASSWORD")
	}

	tracingShutdown, err := setupTracing(ctx, tracingOptions{
		enabled:               options.EnableTracing,
		endpoint:              options.TracingEndpoint,
		insecure:              options.TracingInsecure,
		sampleRatio:           options.TracingSampleRatio,
		headers:               options.TracingHeaders,
		basicAuthUsername:     options.TracingBasicAuthUsername,
		basicAuthPassword:     options.TracingBasicAuthPassword,
		timeout:               options.TracingTimeout,
		compression:           options.TracingCompression,
		tlsServerName:         options.TracingTLSServerName,
		tlsInsecureSkipVerify: options.TracingTLSInsecureSkipVerify,
	})
	if err != nil {
		return fmt.Errorf("unable to initialize tracing: %w", err)
	}

	if err = manager.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")

		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		if shutdownErr := tracingShutdown(shutdownCtx); shutdownErr != nil {
			setupLog.Error(shutdownErr, "unable to shutdown tracing")
		}

		cancel()

		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	if err := tracingShutdown(shutdownCtx); err != nil {
		setupLog.Error(err, "unable to shutdown tracing")
	}

	cancel()

	return nil
}
