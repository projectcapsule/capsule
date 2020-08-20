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
	goRuntime "runtime"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	capsulev1alpha1 "github.com/clastix/capsule/api/v1alpha1"
	"github.com/clastix/capsule/controllers"
	"github.com/clastix/capsule/controllers/secret"
	"github.com/clastix/capsule/pkg/indexer"
	"github.com/clastix/capsule/pkg/webhook"
	"github.com/clastix/capsule/pkg/webhook/ingress"
	"github.com/clastix/capsule/pkg/webhook/namespace_quota"
	"github.com/clastix/capsule/pkg/webhook/network_policies"
	"github.com/clastix/capsule/pkg/webhook/owner_reference"
	"github.com/clastix/capsule/pkg/webhook/pvc"
	"github.com/clastix/capsule/pkg/webhook/tenant_prefix"
	"github.com/clastix/capsule/version"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(capsulev1alpha1.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func printVersion() {
	setupLog.Info(fmt.Sprintf("Operator Version: %s", version.Version))
	setupLog.Info(fmt.Sprintf("Go Version: %s", goRuntime.Version()))
	setupLog.Info(fmt.Sprintf("Go OS/Arch: %s/%s", goRuntime.GOOS, goRuntime.GOARCH))
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var forceTenantPrefix bool
	var v bool

	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&v, "version", false, "Print the Capsule version and exit")
	flag.BoolVar(&forceTenantPrefix, "force-tenant-prefix", false, "Enforces the Tenant owner, "+
		"during Namespace creation, to name it using the selected Tenant name as prefix, separated by a dash. "+
		"This is useful to avoid Namespace name collision in a public CaaS environment.")
	opts := zap.Options{}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	printVersion()
	if v {
		os.Exit(0)
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

	_ = mgr.AddReadyzCheck("ping", healthz.Ping)
	_ = mgr.AddHealthzCheck("ping", healthz.Ping)

	if err = (&controllers.TenantReconciler{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("Tenant"),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Tenant")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	//webhooks
	wl := make([]webhook.Webhook, 0)
	wl = append(wl, &ingress.ExtensionIngress{}, &ingress.NetworkIngress{}, pvc.Webhook{}, &owner_reference.Webhook{}, &namespace_quota.Webhook{}, network_policies.Webhook{}, tenant_prefix.Webhook{ForceTenantPrefix: forceTenantPrefix})
	err = webhook.Register(mgr, wl...)
	if err != nil {
		setupLog.Error(err, "unable to setup webhooks")
		os.Exit(1)
	}

	if err = (&controllers.NamespaceReconciler{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("Namespace"),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Namespace")
		os.Exit(1)
	}

	if err = (&secret.CaReconciler{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("CA"),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Namespace")
		os.Exit(1)
	}
	if err = (&secret.TlsReconciler{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("Tls"),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Namespace")
		os.Exit(1)
	}

	if err := indexer.AddToManager(mgr); err != nil {
		setupLog.Error(err, "unable to setup indexers")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
