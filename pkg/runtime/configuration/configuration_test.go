// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package configuration_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsuleapi "github.com/projectcapsule/capsule/pkg/api"
	capsulemeta "github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestDefaultCapsuleConfiguration(t *testing.T) {
	t.Parallel()

	got := configuration.DefaultCapsuleConfiguration()

	if !reflect.DeepEqual(got.Users, rbac.UserListSpec{{Kind: rbac.GroupOwner, Name: "projectcapsule.dev"}}) {
		t.Fatalf("DefaultCapsuleConfiguration().Users = %#v", got.Users)
	}
	if got.CacheInvalidation.Duration != time.Hour {
		t.Fatalf("CacheInvalidation = %s, want 1h", got.CacheInvalidation.Duration)
	}
	if got.RBAC == nil || got.RBAC.DeleterClusterRole != "capsule-namespace-deleter" {
		t.Fatalf("RBAC defaults = %#v, want namespace deleter defaults", got.RBAC)
	}
}

func TestEnvironmentHelpers(t *testing.T) {
	t.Setenv(configuration.EnvironmentServiceaccountName, "capsule-controller")
	t.Setenv(configuration.EnvironmentControllerNamespace, "capsule-system")

	if got := configuration.ControllerNamespace(); got != "capsule-system" {
		t.Fatalf("ControllerNamespace() = %q, want capsule-system", got)
	}

	name, namespace := configuration.ControllerServiceAccount()
	if name != "capsule-controller" || namespace != "capsule-system" {
		t.Fatalf("ControllerServiceAccount() = %q/%q, want capsule-controller/capsule-system", namespace, name)
	}

	if !configuration.IsControllerServiceAccount("capsule-controller", "capsule-system") {
		t.Fatalf("IsControllerServiceAccount() = false, want true")
	}
	if configuration.IsControllerServiceAccount("other", "capsule-system") {
		t.Fatalf("IsControllerServiceAccount() = true, want false")
	}
}

func TestNewCapsuleConfigurationCreatesDefaultWhenMissing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cl := configurationFakeClient(t)
	cfg := configuration.NewCapsuleConfiguration(ctx, cl, cl, &rest.Config{Host: "https://kubernetes.default"}, "capsule")

	got := cfg.GetConfigObject()
	if got.Name != "capsule" {
		t.Fatalf("GetConfigObject().Name = %q, want capsule", got.Name)
	}
	if !reflect.DeepEqual(got.Spec.Users, configuration.DefaultCapsuleConfiguration().Users) {
		t.Fatalf("default config users = %#v, want %#v", got.Spec.Users, configuration.DefaultCapsuleConfiguration().Users)
	}

	var stored capsulev1beta2.CapsuleConfiguration
	if err := cl.Get(ctx, client.ObjectKey{Name: "capsule"}, &stored); err != nil {
		t.Fatalf("created configuration was not stored: %v", err)
	}
}

func TestCapsuleConfigurationGetters(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	stored := &capsulev1beta2.CapsuleConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: "capsule"},
		Spec: capsulev1beta2.CapsuleConfigurationSpec{
			ProtectedNamespaceRegexpString: "^(kube|openshift)-",
			ForceTenantPrefix:              true,
			EnableTLSReconciler:            true,
			AllowServiceAccountPromotion:   true,
			CapsuleResources: capsulev1beta2.CapsuleResources{
				TLSSecretName: "capsule-tls",
			},
			Users: rbac.UserListSpec{
				{Kind: rbac.UserOwner, Name: "alice"},
				{Kind: rbac.GroupOwner, Name: "developers"},
				{Kind: rbac.ServiceAccountOwner, Name: "system:serviceaccount:tenant-a:builder"},
			},
			UserNames:            []string{"legacy-user"},
			UserGroups:           []string{"legacy-group"},
			IgnoreUserWithGroups: []string{"ignored"},
			Administrators:       rbac.UserListSpec{{Kind: rbac.UserOwner, Name: "admin"}},
			NodeMetadata: &capsulev1beta2.NodeMetadata{
				ForbiddenLabels:      capsuleapi.ForbiddenListSpec{Exact: []string{"node-role.kubernetes.io/control-plane"}},
				ForbiddenAnnotations: capsuleapi.ForbiddenListSpec{Regex: "internal/.*"},
			},
			RBAC: &capsulev1beta2.RBACConfiguration{DeleterClusterRole: "delete-role"},
			CacheInvalidation: metav1.Duration{
				Duration: 2 * time.Hour,
			},
			Impersonation: capsulev1beta2.ServiceAccountClient{
				Endpoint:      "https://impersonation.example",
				SkipTLSVerify: true,
			},
			Events: capsulev1beta2.EventsConfiguration{ClusterEventNamespace: "audit"},
		},
		Status: capsulev1beta2.CapsuleConfigurationStatus{
			Users: rbac.UserListSpec{{Kind: rbac.GroupOwner, Name: "status-group"}},
		},
	}
	cl := configurationFakeClient(t, stored)
	cfg := configuration.NewCapsuleConfiguration(ctx, cl, cl, &rest.Config{
		Host: "https://kubernetes.default",
		TLSClientConfig: rest.TLSClientConfig{
			CAData: []byte("ca"),
			CAFile: "ca-file",
		},
	}, "capsule")

	regex, err := cfg.ProtectedNamespaceRegexp()
	if err != nil {
		t.Fatalf("ProtectedNamespaceRegexp() unexpected error: %v", err)
	}
	if !regex.MatchString("kube-system") {
		t.Fatalf("ProtectedNamespaceRegexp() did not match kube-system")
	}

	if !cfg.ForceTenantPrefix() || !cfg.EnableTLSConfiguration() || !cfg.AllowServiceAccountPromotion() {
		t.Fatalf("boolean getters did not return configured true values")
	}
	if cfg.TLSSecretName() != "capsule-tls" {
		t.Fatalf("TLSSecretName() = %q, want capsule-tls", cfg.TLSSecretName())
	}
	if cfg.TenantCRDName() != configuration.TenantCRDName {
		t.Fatalf("TenantCRDName() = %q, want %q", cfg.TenantCRDName(), configuration.TenantCRDName)
	}
	if !reflect.DeepEqual(cfg.IgnoreUserWithGroups(), []string{"ignored"}) {
		t.Fatalf("IgnoreUserWithGroups() = %#v", cfg.IgnoreUserWithGroups())
	}
	if !reflect.DeepEqual(cfg.GetUsersByStatus(), rbac.UserListSpec{{Kind: rbac.GroupOwner, Name: "status-group"}}) {
		t.Fatalf("GetUsersByStatus() = %#v", cfg.GetUsersByStatus())
	}
	if cfg.ForbiddenUserNodeLabels() == nil || cfg.ForbiddenUserNodeLabels().Exact[0] != "node-role.kubernetes.io/control-plane" {
		t.Fatalf("ForbiddenUserNodeLabels() = %#v", cfg.ForbiddenUserNodeLabels())
	}
	if cfg.ForbiddenUserNodeAnnotations() == nil || cfg.ForbiddenUserNodeAnnotations().Regex != "internal/.*" {
		t.Fatalf("ForbiddenUserNodeAnnotations() = %#v", cfg.ForbiddenUserNodeAnnotations())
	}
	if cfg.Administrators()[0].Name != "admin" {
		t.Fatalf("Administrators() = %#v", cfg.Administrators())
	}
	if cfg.Events().ClusterEventNamespace != "audit" {
		t.Fatalf("Events() = %#v", cfg.Events())
	}
	if cfg.RBAC().DeleterClusterRole != "delete-role" {
		t.Fatalf("RBAC() = %#v", cfg.RBAC())
	}
	if cfg.CacheInvalidation().Duration != 2*time.Hour {
		t.Fatalf("CacheInvalidation() = %s", cfg.CacheInvalidation().Duration)
	}

	wantUsers := rbac.UserListSpec{
		{Kind: rbac.GroupOwner, Name: "developers"},
		{Kind: rbac.GroupOwner, Name: "legacy-group"},
		{Kind: rbac.ServiceAccountOwner, Name: "system:serviceaccount:tenant-a:builder"},
		{Kind: rbac.UserOwner, Name: "alice"},
		{Kind: rbac.UserOwner, Name: "legacy-user"},
	}
	if got := cfg.Users(); !reflect.DeepEqual(got, wantUsers) {
		t.Fatalf("Users() = %#v, want %#v", got, wantUsers)
	}

	saClient, err := cfg.ServiceAccountClient(ctx)
	if err != nil {
		t.Fatalf("ServiceAccountClient() unexpected error: %v", err)
	}
	if saClient.Host != "https://impersonation.example" ||
		!saClient.Insecure ||
		len(saClient.TLSClientConfig.CAData) != 0 ||
		saClient.TLSClientConfig.CAFile != "" {
		t.Fatalf("ServiceAccountClient() = %#v, want endpoint with insecure TLS", saClient)
	}
}

func TestProtectedNamespaceRegexpErrors(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cl := configurationFakeClient(t, &capsulev1beta2.CapsuleConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: "capsule"},
		Spec: capsulev1beta2.CapsuleConfigurationSpec{
			ProtectedNamespaceRegexpString: "[",
		},
	})
	cfg := configuration.NewCapsuleConfiguration(ctx, cl, cl, &rest.Config{}, "capsule")

	if _, err := cfg.ProtectedNamespaceRegexp(); err == nil {
		t.Fatalf("ProtectedNamespaceRegexp() expected error")
	}
}

func TestServiceAccountClientLoadsCASecret(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cl := configurationFakeClient(t,
		&capsulev1beta2.CapsuleConfiguration{
			ObjectMeta: metav1.ObjectMeta{Name: "capsule"},
			Spec: capsulev1beta2.CapsuleConfigurationSpec{
				Impersonation: capsulev1beta2.ServiceAccountClient{
					Endpoint:          "https://impersonation.example",
					CASecretNamespace: capsulemeta.RFC1123SubdomainName("capsule-system"),
					CASecretName:      capsulemeta.RFC1123Name("ca"),
					CASecretKey:       "ca.crt",
				},
			},
		},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "capsule-system", Name: "ca"}, Data: map[string][]byte{"ca.crt": []byte("secret-ca")}},
	)
	cfg := configuration.NewCapsuleConfiguration(ctx, cl, cl, &rest.Config{
		Host:            "https://kubernetes.default",
		TLSClientConfig: rest.TLSClientConfig{CAFile: "old"},
	}, "capsule")

	got, err := cfg.ServiceAccountClient(ctx)
	if err != nil {
		t.Fatalf("ServiceAccountClient() unexpected error: %v", err)
	}
	if got.Host != "https://impersonation.example" || string(got.TLSClientConfig.CAData) != "secret-ca" || got.TLSClientConfig.CAFile != "" {
		t.Fatalf("ServiceAccountClient() = %#v, want CA data from secret", got)
	}
}

func TestServiceAccountClientMissingCASecretKey(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cl := configurationFakeClient(t,
		&capsulev1beta2.CapsuleConfiguration{
			ObjectMeta: metav1.ObjectMeta{Name: "capsule"},
			Spec: capsulev1beta2.CapsuleConfigurationSpec{
				Impersonation: capsulev1beta2.ServiceAccountClient{
					CASecretNamespace: capsulemeta.RFC1123SubdomainName("capsule-system"),
					CASecretName:      capsulemeta.RFC1123Name("ca"),
					CASecretKey:       "missing",
				},
			},
		},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "capsule-system", Name: "ca"}, Data: map[string][]byte{"ca.crt": []byte("secret-ca")}},
	)
	cfg := configuration.NewCapsuleConfiguration(ctx, cl, cl, &rest.Config{}, "capsule")

	if _, err := cfg.ServiceAccountClient(ctx); err == nil {
		t.Fatalf("ServiceAccountClient() expected error for missing CA key")
	}
}

func configurationFakeClient(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("adding core scheme: %v", err)
	}
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatalf("adding capsule scheme: %v", err)
	}

	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}
