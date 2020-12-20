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

package main

import (
	"flag"
	"fmt"
	"os"
	"regexp"
	goRuntime "runtime"

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
	"github.com/clastix/capsule/controllers/rbac"
	"github.com/clastix/capsule/controllers/secret"
	"github.com/clastix/capsule/controllers/servicelabels"
	"github.com/clastix/capsule/pkg/indexer"
	"github.com/clastix/capsule/pkg/webhook"
	"github.com/clastix/capsule/pkg/webhook/ingress"
	"github.com/clastix/capsule/pkg/webhook/namespacequota"
	"github.com/clastix/capsule/pkg/webhook/networkpolicies"
	"github.com/clastix/capsule/pkg/webhook/ownerreference"
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
	var forceTenantPrefix bool
	var v bool
	var capsuleGroup string
	var protectedNamespaceRegexpString string
	var protectedNamespaceRegexp *regexp.Regexp
	var namespace string

	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&capsuleGroup, "capsule-user-group", capsulev1alpha1.GroupVersion.Group, "Name of the group for capsule users")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&v, "version", false, "Print the Capsule version and exit")
	flag.BoolVar(&forceTenantPrefix, "force-tenant-prefix", false, "Enforces the Tenant owner, "+
		"during Namespace creation, to name it using the selected Tenant name as prefix, separated by a dash. "+
		"This is useful to avoid Namespace name collision in a public CaaS environment.")
	flag.StringVar(&protectedNamespaceRegexpString, "protected-namespace-regex", "", "Disallow creation of namespaces, whose name matches this regexp")

	opts := zap.Options{
		EncoderConfigOptions: append([]zap.EncoderConfigOption{}, func(config *zapcore.EncoderConfig) {
			config.EncodeTime = zapcore.ISO8601TimeEncoder
		}),
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	printVersion()
	if v {
		os.Exit(0)
	}

	if namespace = os.Getenv("NAMESPACE"); len(namespace) == 0 {
		setupLog.Error(fmt.Errorf("unable to determinate the Namespace Capsule is running on"), "unable to start manager")
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
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
	if len(protectedNamespaceRegexpString) > 0 {
		protectedNamespaceRegexp, err = regexp.Compile(protectedNamespaceRegexpString)
		if err != nil {
			setupLog.Error(err, "unable to compile protected-namespace-regex", "protected-namespace-regex", protectedNamespaceRegexp)
			os.Exit(1)
		}
	}

	majorVer, minorVer, _, err := utils.GetK8sVersion()
	if err != nil {
		setupLog.Error(err, "unable to get kubernetes version")
		os.Exit(1)
	}

	_ = mgr.AddReadyzCheck("ping", healthz.Ping)
	_ = mgr.AddHealthzCheck("ping", healthz.Ping)

	setupLog.Info("starting with following options:", "metricsAddr", metricsAddr, "enableLeaderElection", enableLeaderElection, "forceTenantPrefix", forceTenantPrefix)

	if err = (&controllers.TenantReconciler{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("Tenant"),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Tenant")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	// webhooks: the order matters, don't change it and just append
	wl := append(
		make([]webhook.Webhook, 0),
		ingress.Webhook(ingress.Handler()),
		pvc.Webhook(pvc.Handler()),
		registry.Webhook(registry.Handler()),
		services.Webhook(services.Handler()),
		ownerreference.Webhook(utils.InCapsuleGroup(capsuleGroup, ownerreference.Handler(forceTenantPrefix))),
		namespacequota.Webhook(utils.InCapsuleGroup(capsuleGroup, namespacequota.Handler())),
		networkpolicies.Webhook(utils.InCapsuleGroup(capsuleGroup, networkpolicies.Handler())),
		tenantprefix.Webhook(utils.InCapsuleGroup(capsuleGroup, tenantprefix.Handler(forceTenantPrefix, protectedNamespaceRegexp))),
		tenant.Webhook(tenant.Handler()),
	)
	if err = webhook.Register(mgr, wl...); err != nil {
		setupLog.Error(err, "unable to setup webhooks")
		os.Exit(1)
	}

	rbacManager := &rbac.Manager{
		Log:          ctrl.Log.WithName("controllers").WithName("Rbac"),
		CapsuleGroup: capsuleGroup,
	}
	if err = mgr.Add(rbacManager); err != nil {
		setupLog.Error(err, "unable to create cluster roles")
		os.Exit(1)
	}
	if err = rbacManager.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Rbac")
		os.Exit(1)
	}

	if err = (&secret.CAReconciler{
		Client:    mgr.GetClient(),
		Log:       ctrl.Log.WithName("controllers").WithName("CA"),
		Scheme:    mgr.GetScheme(),
		Namespace: namespace,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Namespace")
		os.Exit(1)
	}
	if err = (&secret.TLSReconciler{
		Client:    mgr.GetClient(),
		Log:       ctrl.Log.WithName("controllers").WithName("Tls"),
		Scheme:    mgr.GetScheme(),
		Namespace: namespace,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Namespace")
		os.Exit(1)
	}

	if err = (&servicelabels.ServicesLabelsReconciler{
		Log: ctrl.Log.WithName("controllers").WithName("ServiceLabels"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ServiceLabels")
		os.Exit(1)
	}
	if err = (&servicelabels.EndpointsLabelsReconciler{
		Log: ctrl.Log.WithName("controllers").WithName("EndpointLabels"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "EndpointLabels")
		os.Exit(1)
	}
	if err = (&servicelabels.EndpointSlicesLabelsReconciler{
		Log:          ctrl.Log.WithName("controllers").WithName("EndpointSliceLabels"),
		VersionMinor: minorVer,
		VersionMajor: majorVer,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "EndpointSliceLabels")
	}

	if err = indexer.AddToManager(mgr); err != nil {
		setupLog.Error(err, "unable to setup indexers")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err = mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
