// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
)

type Bootstrap struct {
	Namespace     string
	Config        *rest.Config
	Scheme        *runtime.Scheme
	DirectClient  client.Client
	Configuration configuration.Configuration
}

func NewBootstrap(ctx context.Context, options Options) (*Bootstrap, error) {
	namespace := os.Getenv(configuration.EnvironmentControllerNamespace)
	if namespace == "" {
		return nil, fmt.Errorf("missing %s environment variable", configuration.EnvironmentControllerNamespace)
	}

	if os.Getenv(configuration.EnvironmentServiceaccountName) == "" {
		return nil, fmt.Errorf("missing %s environment variable", configuration.EnvironmentServiceaccountName)
	}

	if options.Controller.ConfigurationName == "" {
		return nil, fmt.Errorf("missing CapsuleConfiguration resource name")
	}

	config, err := ctrl.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("load Kubernetes configuration: %w", err)
	}

	config.WarningHandler = rest.NoWarnings{}
	config.QPS = options.ClientConnectionQPS
	config.Burst = int(options.ClientConnectionBurst)
	scheme := NewScheme()

	direct, err := client.New(ctrl.GetConfigOrDie(), client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("create direct client: %w", err)
	}

	return &Bootstrap{Namespace: namespace, Config: config, Scheme: scheme, DirectClient: direct, Configuration: configuration.NewCapsuleConfiguration(ctx, direct, direct, config, options.Controller.ConfigurationName)}, nil
}

func (o Options) TLSOptions() []func(*tls.Config) {
	if o.EnableHTTP2 {
		return nil
	}

	return []func(*tls.Config){func(config *tls.Config) { config.NextProtos = []string{"http/1.1"} }}
}

func (o Options) ManagerOptions(b *Bootstrap, leaseID string) (ctrl.Options, *certwatcher.CertWatcher, error) {
	healthProbeAddr := o.HealthProbeAddr
	if healthProbeAddr == "" {
		healthProbeAddr = ":10080"
	}

	metricsOptions := metricsserver.Options{BindAddress: o.MetricsAddr, SecureServing: o.SecureMetrics, TLSOpts: o.TLSOptions()}
	if o.SecureMetrics {
		metricsOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	var watcher *certwatcher.CertWatcher

	if o.MetricsCertPath != "" {
		var err error

		watcher, err = certwatcher.New(filepath.Join(o.MetricsCertPath, o.MetricsCertName), filepath.Join(o.MetricsCertPath, o.MetricsCertKey))
		if err != nil {
			return ctrl.Options{}, nil, fmt.Errorf("watch metrics certificate: %w", err)
		}

		metricsOptions.TLSOpts = append(metricsOptions.TLSOpts, func(config *tls.Config) { config.GetCertificate = watcher.GetCertificate })
	}

	options := ctrl.Options{Scheme: b.Scheme, Metrics: metricsOptions, LeaderElection: o.EnableLeaderElection, LeaderElectionID: leaseID, LeaderElectionNamespace: b.Namespace, HealthProbeBindAddress: healthProbeAddr, NewClient: func(config *rest.Config, options client.Options) (client.Client, error) {
		options.Cache.Unstructured = true

		return client.New(config, options)
	}}
	if o.LeaderElectionLeaseDuration > 0 {
		options.LeaseDuration = &o.LeaderElectionLeaseDuration
	}

	if o.LeaderElectionRenewDeadline > 0 {
		options.RenewDeadline = &o.LeaderElectionRenewDeadline
	}

	if o.LeaderElectionRetryPeriod > 0 {
		options.RetryPeriod = &o.LeaderElectionRetryPeriod
	}

	if o.EnablePprof {
		options.PprofBindAddress = o.PprofAddr
		if options.PprofBindAddress == "" {
			options.PprofBindAddress = ":8082"
		}
	}

	return options, watcher, nil
}

func AddHealthChecks(manager ctrl.Manager, watcher *certwatcher.CertWatcher) error {
	if watcher != nil {
		if err := manager.Add(watcher); err != nil {
			return fmt.Errorf("add metrics certificate watcher: %w", err)
		}
	}

	if err := manager.AddReadyzCheck("ping", healthz.Ping); err != nil {
		return err
	}

	return manager.AddHealthzCheck("ping", healthz.Ping)
}
