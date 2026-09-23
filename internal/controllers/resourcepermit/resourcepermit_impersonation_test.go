// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resourcepermit

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/ssa"
	tpl "github.com/projectcapsule/capsule/pkg/template"
)

type impersonationTestConfiguration struct {
	configuration.Configuration
	properties capsulev1beta2.ServiceAccountClient
}

func (c impersonationTestConfiguration) ServiceAccountClientProperties() capsulev1beta2.ServiceAccountClient {
	return c.properties
}

func TestResolveTemplateServiceAccount(t *testing.T) {
	t.Parallel()

	configured := capsulev1beta2.ServiceAccountClient{
		GlobalDefaultServiceAccount:          "configured",
		GlobalDefaultServiceAccountNamespace: "configuration-ns",
	}
	r := &ResourcePermitReconciler{Configuration: impersonationTestConfiguration{properties: configured}}

	explicit := &meta.NamespacedRFC1123ObjectReferenceWithNamespace{
		Name:      "template",
		Namespace: "template-ns",
	}
	brt := &capsulev1beta2.GlobalResourcePermitTemplate{
		Spec: capsulev1beta2.GlobalResourcePermitTemplateSpec{Impersonation: explicit},
	}

	got := r.resolveTemplateServiceAccount(logr.Discard(), brt)
	if got == nil || got.Name != explicit.Name || got.Namespace != explicit.Namespace {
		t.Fatalf("explicit template ServiceAccount = %#v, want %#v", got, explicit)
	}
	if got == explicit {
		t.Fatal("resolved explicit ServiceAccount aliases the template spec")
	}

	brt.Spec.Impersonation = nil
	got = r.resolveTemplateServiceAccount(logr.Discard(), brt)
	if got == nil || got.Name != configured.GlobalDefaultServiceAccount ||
		got.Namespace != configured.GlobalDefaultServiceAccountNamespace {
		t.Fatalf("configured default ServiceAccount = %#v, want %s/%s", got,
			configured.GlobalDefaultServiceAccountNamespace,
			configured.GlobalDefaultServiceAccount,
		)
	}
}

func TestResolveTemplateServiceAccountWithoutCompleteDefault(t *testing.T) {
	t.Parallel()

	for _, properties := range []capsulev1beta2.ServiceAccountClient{
		{},
		{GlobalDefaultServiceAccount: "name-only"},
		{GlobalDefaultServiceAccountNamespace: "namespace-only"},
	} {
		r := &ResourcePermitReconciler{Configuration: impersonationTestConfiguration{properties: properties}}
		if got := r.resolveTemplateServiceAccount(logr.Discard(), &capsulev1beta2.GlobalResourcePermitTemplate{}); got != nil {
			t.Fatalf("resolved incomplete configured default %#v as %#v", properties, got)
		}
	}
}

func TestResolveNamespacedTemplateServiceAccount(t *testing.T) {
	t.Parallel()

	configured := capsulev1beta2.ServiceAccountClient{TenantDefaultServiceAccount: "tenant-default"}
	r := &ResourcePermitReconciler{Configuration: impersonationTestConfiguration{properties: configured}}
	brt := &capsulev1beta2.ResourcePermitTemplate{
		Namespace: "team-a",
		Spec: capsulev1beta2.ResourcePermitTemplateSpec{
			Impersonation: &meta.LocalRFC1123ObjectReference{Name: "template-runner"},
		},
	}

	got := r.resolveTemplateServiceAccount(logr.Discard(), brt)
	if got == nil || got.Name != "template-runner" || got.Namespace != "team-a" {
		t.Fatalf("explicit namespaced ServiceAccount = %#v, want team-a/template-runner", got)
	}

	brt.Spec.Impersonation = nil
	got = r.resolveTemplateServiceAccount(logr.Discard(), brt)
	if got == nil || got.Name != "tenant-default" || got.Namespace != "team-a" {
		t.Fatalf("default namespaced ServiceAccount = %#v, want team-a/tenant-default", got)
	}

	r.Configuration = impersonationTestConfiguration{}
	if got := r.resolveTemplateServiceAccount(logr.Discard(), brt); got != nil {
		t.Fatalf("namespaced ServiceAccount without explicit or configured default = %#v, want nil", got)
	}
}

func TestResourceClientPinsTemplateIdentity(t *testing.T) {
	t.Parallel()

	base := fake.NewClientBuilder().Build()
	impersonated := fake.NewClientBuilder().Build()
	clients := cache.NewImpersonationCache()
	clients.Set("template-ns", "template", impersonated)

	r := &ResourcePermitReconciler{
		Client:             base,
		ImpersonationCache: clients,
	}
	br := &capsulev1beta2.ResourcePermit{}
	brt := &capsulev1beta2.GlobalResourcePermitTemplate{
		Spec: capsulev1beta2.GlobalResourcePermitTemplateSpec{
			Impersonation: &meta.NamespacedRFC1123ObjectReferenceWithNamespace{
				Name:      "template",
				Namespace: "template-ns",
			},
		},
	}

	got, err := r.resourceClient(context.Background(), logr.Discard(), br, brt)
	if err != nil {
		t.Fatalf("resourceClient() error = %v", err)
	}
	if got != impersonated {
		t.Fatalf("resourceClient() = %T, want cached impersonated client", got)
	}
	if br.Status.Request.Impersonation == nil || br.Status.Request.Impersonation.Name != "template" ||
		br.Status.Request.Impersonation.Namespace != "template-ns" {
		t.Fatalf("pinned ServiceAccount = %#v", br.Status.Request.Impersonation)
	}

	// A request keeps using its initially resolved identity even if its template
	// is changed while the request is active.
	brt.Spec.Impersonation = &meta.NamespacedRFC1123ObjectReferenceWithNamespace{
		Name:      "changed",
		Namespace: "changed-ns",
	}
	got, err = r.resourceClient(context.Background(), logr.Discard(), br, brt)
	if err != nil {
		t.Fatalf("resourceClient() with pinned identity error = %v", err)
	}
	if got != impersonated {
		t.Fatal("resourceClient() did not retain the pinned impersonated client")
	}
}

func TestResourceClientPinsControllerIdentityWithoutImpersonation(t *testing.T) {
	t.Setenv(configuration.EnvironmentServiceaccountName, "capsule-controller")
	t.Setenv(configuration.EnvironmentControllerNamespace, "capsule-system")

	base := fake.NewClientBuilder().Build()
	direct := fake.NewClientBuilder().Build()
	r := &ResourcePermitReconciler{Client: base, ControllerClient: direct}

	for _, tt := range []struct {
		name     string
		template *capsulev1beta2.GlobalResourcePermitTemplate
	}{
		{name: "while resolving a template", template: &capsulev1beta2.GlobalResourcePermitTemplate{}},
		{name: "without reloading a template", template: nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			br := &capsulev1beta2.ResourcePermit{}
			got, err := r.resourceClient(context.Background(), logr.Discard(), br, tt.template)
			if err != nil {
				t.Fatalf("resourceClient() error = %v", err)
			}
			if got != direct {
				t.Fatalf("resourceClient() = %T, want uncached controller client", got)
			}
			if br.Status.Request.Impersonation == nil {
				t.Fatal("controller ServiceAccount was not posted to ResourcePermit status")
			}
			if br.Status.Request.Impersonation.Name != "capsule-controller" ||
				br.Status.Request.Impersonation.Namespace != "capsule-system" {
				t.Fatalf("controller ServiceAccount = %#v, want capsule-system/capsule-controller", br.Status.Request.Impersonation)
			}
		})
	}
}

func TestTemplateContextUsesImpersonatedClient(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	base := fake.NewClientBuilder().WithScheme(scheme).Build()
	impersonated := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&corev1.ConfigMap{
		Name: "source", Namespace: "team-a",
		Data: map[string]string{"value": "loaded-with-template-client"},
	}).Build()
	clients := cache.NewImpersonationCache()
	clients.Set("operations", "template-runner", impersonated)

	mapper := k8smeta.NewDefaultRESTMapper([]schema.GroupVersion{corev1.SchemeGroupVersion})
	mapper.Add(corev1.SchemeGroupVersion.WithKind("ConfigMap"), k8smeta.RESTScopeNamespace)

	r := &ResourcePermitReconciler{
		Client:             base,
		ImpersonationCache: clients,
		resources:          ssa.Manager{Mapper: mapper},
	}
	br := &capsulev1beta2.ResourcePermit{
		Name: "request", Namespace: "team-a",
	}
	brt := &capsulev1beta2.GlobalResourcePermitTemplate{
		Spec: capsulev1beta2.GlobalResourcePermitTemplateSpec{
			Impersonation: &meta.NamespacedRFC1123ObjectReferenceWithNamespace{
				Name:      "template-runner",
				Namespace: "operations",
			},
			Context: &tpl.TemplateContext{Resources: []*tpl.TemplateResourceReference{{
				APIVersion: "v1", Kind: "ConfigMap",
				Name:  "source",
				Index: "settings",
			}}},
			Resources: []apiruntime.ResourceTemplate{{
				Targets: []runtime.RawExtension{{Raw: []byte(
					`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"rendered"},"data":{"value":"{{ (index .settings 0).data.value }}"}}`,
				)}},
			}},
		},
	}

	resourceClient, err := r.resourceClient(context.Background(), logr.Discard(), br, brt)
	if err != nil {
		t.Fatalf("resourceClient() error = %v", err)
	}
	if err := r.renderResources(context.Background(), br, brt, resourceClient); err != nil {
		t.Fatalf("renderResources() error = %v", err)
	}

	if br.Status.Request == nil ||
		len(br.Status.Request.Resources) != 1 ||
		len(br.Status.Request.Resources[0].Targets) != 1 {
		t.Fatalf("rendered resources = %#v", br.Status.Request)
	}
	obj, err := object(br.Status.Request.Resources[0].Targets[0])
	if err != nil {
		t.Fatalf("decoding rendered target: %v", err)
	}
	data, found, err := unstructured.NestedStringMap(obj.Object, "data")
	if err != nil || !found || data["value"] != "loaded-with-template-client" {
		t.Fatalf("rendered data = %#v, found=%v, error=%v", data, found, err)
	}
}

func TestControllerResourceClientReadsAppliedTarget(t *testing.T) {
	t.Setenv(configuration.EnvironmentServiceaccountName, "capsule-controller")
	t.Setenv(configuration.EnvironmentControllerNamespace, "capsule-system")
	r, permits, direct := permitPolicyStatusFixture(t, 2, 1, "")
	permit := permits[0]
	permit.Status.Request.Resources[0].Policy.Protect = new(true)
	snapshot := &unstructured.Unstructured{}
	snapshot.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("ConfigMap"))
	require.NoError(t, direct.Get(t.Context(), client.ObjectKey{Namespace: permit.Namespace, Name: "target-0"}, snapshot))
	cachedReads := 0
	r.Client = interceptor.NewClient(direct, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			cachedReads++
			if target, ok := obj.(*unstructured.Unstructured); ok {
				// A manager-cache read can still hold the pre-apply version.
				snapshot.DeepCopyInto(target)
				return nil
			}
			return c.Get(ctx, key, obj, opts...)
		},
	})
	r.ControllerClient = direct
	execution, err := r.resourceClient(t.Context(), logr.Discard(), permit, nil)
	require.NoError(t, err)
	require.NoError(t, r.reconcileItems(t.Context(), permit, execution))
	require.Zero(t, cachedReads, "SSA execution must not read snapshots from the manager cache")
	require.False(t, permit.Status.ProcessedItems[0].LastApply.IsZero())
	require.True(t, permit.Status.ProcessedItems[0].Policy.IsProtected())
	target := &corev1.ConfigMap{}
	require.NoError(t, direct.Get(t.Context(), client.ObjectKeyFromObject(snapshot), target))
	require.Equal(t, meta.ValueTrue, target.Labels[meta.ResourcePermitProtectionLabel])
	require.Equal(t, "system:serviceaccount:capsule-system:capsule-controller", target.Annotations[meta.ResourcePermitServiceAccountAnnotation])
	require.NoError(t, direct.Get(t.Context(), client.ObjectKey{Namespace: permits[1].Namespace, Name: "target-0"}, target))
	require.NotContains(t, target.Labels, meta.ResourcePermitProtectionLabel)
}

func TestControllerResourceClientRequiresDirectClient(t *testing.T) {
	r := &ResourcePermitReconciler{Client: fake.NewClientBuilder().Build()}
	_, err := r.resourceClient(t.Context(), logr.Discard(), &capsulev1beta2.ResourcePermit{}, nil)
	require.ErrorContains(t, err, "direct controller resource client is not configured")
}
