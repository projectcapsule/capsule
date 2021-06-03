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
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	capsulev1alpha1 "github.com/clastix/capsule/api/v1alpha1"
	"github.com/clastix/capsule/controllers"
	config "github.com/clastix/capsule/controllers/config"
	"github.com/clastix/capsule/controllers/rbac"
	"github.com/clastix/capsule/controllers/secret"
	"github.com/clastix/capsule/controllers/servicelabels"
	"github.com/clastix/capsule/pkg/configuration"
	"github.com/clastix/capsule/pkg/indexer"
	"github.com/clastix/capsule/pkg/webhook"
	"github.com/clastix/capsule/pkg/webhook/ingress"
	"github.com/clastix/capsule/pkg/webhook/namespacequota"
	"github.com/clastix/capsule/pkg/webhook/networkpolicies"
	"github.com/clastix/capsule/pkg/webhook/ownerreference"
	"github.com/clastix/capsule/pkg/webhook/podpriority"
	"github.com/clastix/capsule/pkg/webhook/pvc"
	"github.com/clastix/capsule/pkg/webhook/registry"
	"github.com/clastix/capsule/pkg/webhook/services"
	"github.com/clastix/capsule/pkg/webhook/tenant"
	"github.com/clastix/capsule/pkg/webhook/tenantprefix"
	"github.com/clastix/capsule/pkg/webhook/utils"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(capsulev1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func printVersion() {
	setupLog.Info(fmt.Sprintf("Capsule Version %s %s%s", GitTag, GitCommit, GitDirty))
	setupLog.Info(fmt.Sprintf("Build from: %s", GitRepo))
	setupLog.Info(fmt.Sprintf("Build date: %s", BuildTime))
	setupLog.Info(fmt.Sprintf("Go Version: %s", goRuntime.Version()))
	setupLog.Info(fmt.Sprintf("Go OS/Arch: %s/%s", goRuntime.GOOS, goRuntime.GOARCH))
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var version bool
	var namespace, configurationName string
	var goFlagSet goflag.FlagSet

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

	majorVer, minorVer, _, err := utils.GetK8sVersion()
	if err != nil {
		setupLog.Error(err, "unable to get kubernetes version")
		os.Exit(1)
	}

	_ = manager.AddReadyzCheck("ping", healthz.Ping)
	_ = manager.AddHealthzCheck("ping", healthz.Ping)

	if err = (&controllers.TenantReconciler{
		Client: manager.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("Tenant"),
		Scheme: manager.GetScheme(),
	}).SetupWithManager(manager); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Tenant")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	cfg := configuration.NewCapsuleConfiguration(manager.GetClient(), configurationName)

	// webhooks: the order matters, don't change it and just append
	webhooksList := append(
		make([]webhook.Webhook, 0),
		ingress.Webhook(ingress.Handler(cfg)),
		pvc.Webhook(pvc.Handler()),
		registry.Webhook(registry.Handler()),
		podpriority.Webhook(podpriority.Handler()),
		services.Webhook(services.Handler()),
		ownerreference.Webhook(utils.InCapsuleGroups(cfg, ownerreference.Handler(cfg))),
		namespacequota.Webhook(utils.InCapsuleGroups(cfg, namespacequota.Handler())),
		networkpolicies.Webhook(utils.InCapsuleGroups(cfg, networkpolicies.Handler())),
		tenantprefix.Webhook(utils.InCapsuleGroups(cfg, tenantprefix.Handler(cfg))),
		tenant.Webhook(tenant.Handler(cfg)),
	)
	if err = webhook.Register(manager, webhooksList...); err != nil {
		setupLog.Error(err, "unable to setup webhooks")
		os.Exit(1)
	}

	rbacManager := &rbac.Manager{
		Log:           ctrl.Log.WithName("controllers").WithName("Rbac"),
		Configuration: cfg,
	}
	if err = manager.Add(rbacManager); err != nil {
		setupLog.Error(err, "unable to create cluster roles")
		os.Exit(1)
	}
	if err = rbacManager.SetupWithManager(manager, configurationName); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Rbac")
		os.Exit(1)
	}

	if err = (&secret.CAReconciler{
		Client:    manager.GetClient(),
		Log:       ctrl.Log.WithName("controllers").WithName("CA"),
		Scheme:    manager.GetScheme(),
		Namespace: namespace,
	}).SetupWithManager(manager); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Namespace")
		os.Exit(1)
	}
	if err = (&secret.TLSReconciler{
		Client:    manager.GetClient(),
		Log:       ctrl.Log.WithName("controllers").WithName("Tls"),
		Scheme:    manager.GetScheme(),
		Namespace: namespace,
	}).SetupWithManager(manager); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Namespace")
		os.Exit(1)
	}

	if err = (&servicelabels.ServicesLabelsReconciler{
		Log: ctrl.Log.WithName("controllers").WithName("ServiceLabels"),
	}).SetupWithManager(manager); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ServiceLabels")
		os.Exit(1)
	}
	if err = (&servicelabels.EndpointsLabelsReconciler{
		Log: ctrl.Log.WithName("controllers").WithName("EndpointLabels"),
	}).SetupWithManager(manager); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "EndpointLabels")
		os.Exit(1)
	}
	if err = (&servicelabels.EndpointSlicesLabelsReconciler{
		Log:          ctrl.Log.WithName("controllers").WithName("EndpointSliceLabels"),
		VersionMinor: minorVer,
		VersionMajor: majorVer,
	}).SetupWithManager(manager); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "EndpointSliceLabels")
	}

	if err = (&config.Manager{
		Log: ctrl.Log.WithName("controllers").WithName("CapsuleConfiguration"),
	}).SetupWithManager(manager, configurationName); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "CapsuleConfiguration")
		os.Exit(1)
	}

	if err = indexer.AddToManager(manager); err != nil {
		setupLog.Error(err, "unable to setup indexers")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err = manager.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
