// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"crypto/tls"
	"testing"
	"time"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
)

func TestManagerFlagsPreserveDefaultsAndOverrides(t *testing.T) {
	var options Options
	flags := pflag.NewFlagSet("controller", pflag.ContinueOnError)
	options.BindFlags(flags)
	require.NoError(t, flags.Parse([]string{"--enable-leader-election", "--leader-election-lease-duration=40s", "--leader-election-renew-deadline=25s", "--leader-election-retry-period=3s", "--metrics-secure", "--enable-pprof"}))
	b := &Bootstrap{Namespace: "capsule-system", Scheme: NewScheme()}
	manager, watcher, err := options.ManagerOptions(b, "replications.projectcapsule.dev")
	require.NoError(t, err)
	require.Nil(t, watcher)
	require.Nil(t, manager.WebhookServer, "only core constructs a webhook server")
	require.Nil(t, manager.NewCache, "each command owns the normal manager cache")
	require.Equal(t, "replications.projectcapsule.dev", manager.LeaderElectionID)
	require.Equal(t, "capsule-system", manager.LeaderElectionNamespace)
	require.True(t, manager.LeaderElection)
	require.Equal(t, 40*time.Second, *manager.LeaseDuration)
	require.Equal(t, 25*time.Second, *manager.RenewDeadline)
	require.Equal(t, 3*time.Second, *manager.RetryPeriod)
	require.True(t, manager.Metrics.SecureServing)
	require.Equal(t, ":8080", manager.Metrics.BindAddress)
	require.Equal(t, ":10080", manager.HealthProbeBindAddress)
	require.Equal(t, ":8082", manager.PprofBindAddress)
	config := &tls.Config{}
	for _, option := range manager.Metrics.TLSOpts {
		option(config)
	}
	require.Equal(t, []string{"http/1.1"}, config.NextProtos)
	options.EnableHTTP2 = true
	require.Nil(t, options.TLSOptions())

	options.MetricsCertPath = t.TempDir()
	_, _, err = options.ManagerOptions(b, "core")
	require.ErrorContains(t, err, "watch metrics certificate")
}

func TestBootstrapRequiresControllerIdentityBeforeAPIInitialization(t *testing.T) {
	t.Setenv(configuration.EnvironmentControllerNamespace, "")
	_, err := NewBootstrap(t.Context(), Options{})
	require.ErrorContains(t, err, configuration.EnvironmentControllerNamespace)
	t.Setenv(configuration.EnvironmentControllerNamespace, "capsule-system")
	t.Setenv(configuration.EnvironmentServiceaccountName, "")
	_, err = NewBootstrap(t.Context(), Options{})
	require.ErrorContains(t, err, configuration.EnvironmentServiceaccountName)
	t.Setenv(configuration.EnvironmentServiceaccountName, "capsule")
	_, err = NewBootstrap(t.Context(), Options{})
	require.ErrorContains(t, err, "CapsuleConfiguration")
}

func TestManagerEndpointOverridesPreserveDefaults(t *testing.T) {
	for _, mode := range []string{"core", "replications"} {
		t.Run(mode, func(t *testing.T) {
			var options Options
			flags := pflag.NewFlagSet(mode, pflag.ContinueOnError)
			options.BindFlags(flags)
			require.NoError(t, flags.Parse([]string{"--metrics-addr=:8081", "--health-probe-bind-address=:10081", "--pprof-bind-address=:8083"}))
			manager, _, err := options.ManagerOptions(&Bootstrap{Scheme: NewScheme()}, mode)
			require.NoError(t, err)
			require.Equal(t, ":8081", manager.Metrics.BindAddress)
			require.Equal(t, ":10081", manager.HealthProbeBindAddress)
			require.Empty(t, manager.PprofBindAddress, "an address alone must not enable profiling")
			options.EnablePprof = true
			manager, _, err = options.ManagerOptions(&Bootstrap{Scheme: NewScheme()}, mode)
			require.NoError(t, err)
			require.Equal(t, ":8083", manager.PprofBindAddress)
			manager, _, err = (Options{EnablePprof: true}).ManagerOptions(&Bootstrap{Scheme: NewScheme()}, mode)
			require.NoError(t, err)
			require.Equal(t, ":10080", manager.HealthProbeBindAddress)
			require.Equal(t, ":8082", manager.PprofBindAddress)
		})
	}
}

func TestCachedClientConfigurationIsRetained(t *testing.T) {
	var options Options
	options.BindFlags(pflag.NewFlagSet("controller", pflag.ContinueOnError))
	manager, _, err := options.ManagerOptions(&Bootstrap{Namespace: "capsule-system", Scheme: NewScheme()}, "core")
	require.NoError(t, err)
	reader := fake.NewClientBuilder().WithScheme(NewScheme()).WithObjects(&corev1.ConfigMap{Name: "input", Namespace: "tenant-a", Data: map[string]string{"value": "cached"}}).Build()
	mapper := apimeta.NewDefaultRESTMapper([]schema.GroupVersion{corev1.SchemeGroupVersion})
	mapper.Add(corev1.SchemeGroupVersion.WithKind("ConfigMap"), apimeta.RESTScopeNamespace)
	c, err := manager.NewClient(&rest.Config{Host: "https://127.0.0.1:1"}, client.Options{Scheme: NewScheme(), Mapper: mapper, Cache: &client.CacheOptions{Reader: reader}})
	require.NoError(t, err)
	object := &unstructured.Unstructured{}
	object.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("ConfigMap"))
	require.NoError(t, c.Get(t.Context(), client.ObjectKey{Namespace: "tenant-a", Name: "input"}, object))
	value, _, err := unstructured.NestedString(object.Object, "data", "value")
	require.NoError(t, err)
	require.Equal(t, "cached", value)
}
