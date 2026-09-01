// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package breaktheglass

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

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
	r := &BreakRequestReconciler{Configuration: impersonationTestConfiguration{properties: configured}}

	explicit := &meta.NamespacedRFC1123ObjectReferenceWithNamespace{
		Name:      "template",
		Namespace: "template-ns",
	}
	brt := &capsulev1beta2.GlobalBreakRequestTemplate{
		Spec: capsulev1beta2.GlobalBreakRequestTemplateSpec{Impersonation: explicit},
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
		r := &BreakRequestReconciler{Configuration: impersonationTestConfiguration{properties: properties}}
		if got := r.resolveTemplateServiceAccount(logr.Discard(), &capsulev1beta2.GlobalBreakRequestTemplate{}); got != nil {
			t.Fatalf("resolved incomplete configured default %#v as %#v", properties, got)
		}
	}
}

func TestResolveNamespacedTemplateServiceAccount(t *testing.T) {
	t.Parallel()

	configured := capsulev1beta2.ServiceAccountClient{TenantDefaultServiceAccount: "tenant-default"}
	r := &BreakRequestReconciler{Configuration: impersonationTestConfiguration{properties: configured}}
	brt := &capsulev1beta2.BreakRequestTemplate{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team-a"},
		Spec: capsulev1beta2.BreakRequestTemplateSpec{
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

	r := &BreakRequestReconciler{
		Client:             base,
		ImpersonationCache: clients,
	}
	br := &capsulev1beta2.BreakRequest{}
	brt := &capsulev1beta2.GlobalBreakRequestTemplate{
		Spec: capsulev1beta2.GlobalBreakRequestTemplateSpec{
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
	if br.Status.ServiceAccount == nil || br.Status.ServiceAccount.Name != "template" ||
		br.Status.ServiceAccount.Namespace != "template-ns" {
		t.Fatalf("pinned ServiceAccount = %#v", br.Status.ServiceAccount)
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
	r := &BreakRequestReconciler{Client: base}

	for _, tt := range []struct {
		name     string
		template *capsulev1beta2.GlobalBreakRequestTemplate
	}{
		{name: "while resolving a template", template: &capsulev1beta2.GlobalBreakRequestTemplate{}},
		{name: "without reloading a template", template: nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			br := &capsulev1beta2.BreakRequest{}
			got, err := r.resourceClient(context.Background(), logr.Discard(), br, tt.template)
			if err != nil {
				t.Fatalf("resourceClient() error = %v", err)
			}
			if got != base {
				t.Fatalf("resourceClient() = %T, want controller client", got)
			}
			if br.Status.ServiceAccount == nil {
				t.Fatal("controller ServiceAccount was not posted to BreakRequest status")
			}
			if br.Status.ServiceAccount.Name != "capsule-controller" ||
				br.Status.ServiceAccount.Namespace != "capsule-system" {
				t.Fatalf("controller ServiceAccount = %#v, want capsule-system/capsule-controller", br.Status.ServiceAccount)
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
		ObjectMeta: metav1.ObjectMeta{Name: "source", Namespace: "team-a"},
		Data:       map[string]string{"value": "loaded-with-template-client"},
	}).Build()
	clients := cache.NewImpersonationCache()
	clients.Set("operations", "template-runner", impersonated)

	mapper := k8smeta.NewDefaultRESTMapper([]schema.GroupVersion{corev1.SchemeGroupVersion})
	mapper.Add(corev1.SchemeGroupVersion.WithKind("ConfigMap"), k8smeta.RESTScopeNamespace)

	r := &BreakRequestReconciler{
		Client:             base,
		ImpersonationCache: clients,
		resources:          ssa.Manager{Mapper: mapper},
	}
	br := &capsulev1beta2.BreakRequest{
		ObjectMeta: metav1.ObjectMeta{Name: "request", Namespace: "team-a"},
	}
	brt := &capsulev1beta2.GlobalBreakRequestTemplate{
		Spec: capsulev1beta2.GlobalBreakRequestTemplateSpec{
			Impersonation: &meta.NamespacedRFC1123ObjectReferenceWithNamespace{
				Name:      "template-runner",
				Namespace: "operations",
			},
			Context: &tpl.TemplateContext{Resources: []*tpl.TemplateResourceReference{{
				ResourceReference: tpl.ResourceReference{
					VersionKind: apiruntime.VersionKind{APIVersion: "v1", Kind: "ConfigMap"},
					Name:        "source",
				},
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

	if br.Status.Approved == nil ||
		len(br.Status.Approved.Resources) != 1 ||
		len(br.Status.Approved.Resources[0].Targets) != 1 {
		t.Fatalf("rendered resources = %#v", br.Status.Approved)
	}
	obj, err := object(br.Status.Approved.Resources[0].Targets[0])
	if err != nil {
		t.Fatalf("decoding rendered target: %v", err)
	}
	data, found, err := unstructured.NestedStringMap(obj.Object, "data")
	if err != nil || !found || data["value"] != "loaded-with-template-client" {
		t.Fatalf("rendered data = %#v, found=%v, error=%v", data, found, err)
	}
}
