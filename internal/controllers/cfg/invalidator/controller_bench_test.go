// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package invalidator

import (
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	mockclient "github.com/projectcapsule/capsule/internal/mocks/client"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
)

func BenchmarkControllerCacheInvalidator(b *testing.B) {
	for _, count := range []int{1, 32} {
		b.Run(fmt.Sprintf("rebuild/tenants=%d", count), func(b *testing.B) {
			scheme := runtime.NewScheme()
			for _, add := range []func(*runtime.Scheme) error{corev1.AddToScheme, capsulev1beta2.AddToScheme} {
				if err := add(scheme); err != nil {
					b.Fatal(err)
				}
			}
			cfg := &capsulev1beta2.CapsuleConfiguration{Name: "capsule"}
			objects := []client.Object{cfg}
			for i := range count {
				name := fmt.Sprintf("tenant-%d", i)
				rule := &rules.NamespaceRuleBodyNamespace{
					Enforce: &rules.NamespaceRuleEnforceBody{
						Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
							Schedulers: []apiruntime.ExpressionMatch{{ExpressionRegex: apiruntime.ExpressionRegex{Expression: fmt.Sprintf("^scheduler-%d$", i)}}},
							Registries: []rules.OCIRegistry{
								{
									ExpressionMatch: apiruntime.ExpressionMatch{ExpressionRegex: apiruntime.ExpressionRegex{Expression: fmt.Sprintf("^registry-%d.example.com/", i)}},
								},
							},
						},
					},
				}
				objects = append(objects, &capsulev1beta2.Tenant{Name: name}, &capsulev1beta2.RuleStatus{
					Name:      "rules",
					Namespace: name,
					Labels:    map[string]string{meta.NewManagedByCapsuleLabel: meta.ValueController, meta.CapsuleNameLabel: meta.NameForManagedRuleStatus()},
					Spec:      []*rules.NamespaceRuleBodyNamespace{rule},
					Status:    capsulev1beta2.RuleStatusStatus{Rules: []*rules.NamespaceRuleBodyNamespace{rule}},
				}, &capsulev1beta2.CustomQuota{
					Name:      "quota",
					Namespace: name,
					Spec: capsulev1beta2.CustomQuotaSpec{
						Sources: []capsulev1beta2.CustomQuotaSpecSource{
							{
								VersionKind:                 apiruntime.VersionKind{APIVersion: "v1", Kind: "ConfigMap"},
								CustomQuotaSpecSourceConfig: capsulev1beta2.CustomQuotaSpecSourceConfig{Operation: "add", Path: ".data.units"},
							},
						},
					},
				})
			}
			calls := &mockclient.CallCounter{}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithInterceptorFuncs(calls.Interceptors()).Build()
			restConfig := &rest.Config{Host: "https://unused.example.com"}
			cel, err := cache.NewCELCache()
			if err != nil {
				b.Fatal(err)
			}
			regex := cache.NewRegexCache()
			r := &CacheInvalidator{
				Client:             c,
				reader:             c,
				Rest:               restConfig,
				Log:                logr.Discard(),
				Configuration:      configuration.NewCapsuleConfiguration(b.Context(), c, c, restConfig, cfg.Name),
				RegistryCache:      cache.NewRegistryRuleSetCache(regex),
				RegexCache:         regex,
				JSONPathCache:      cache.NewJSONPathCache(),
				CELCache:           cel,
				TargetsCache:       cache.NewCompiledTargetsCache[string](),
				ImpersonationCache: cache.NewImpersonationCache(),
			}
			req := reconcile.Request{Name: cfg.Name}
			if _, err := r.Reconcile(b.Context(), req); err != nil {
				b.Fatal(err)
			}
			if regex.Stats() != 2*count || r.RegistryCache.Stats() != count || r.TargetsCache.Stats() != count || r.JSONPathCache.Stats() != 1 {
				b.Fatal("cache rebuild did not populate expected entries")
			}
			calls.Reset()
			b.ReportAllocs()
			for b.Loop() {
				if _, err := r.Reconcile(b.Context(), req); err != nil {
					b.Fatal(err)
				}
			}
			calls.Report(b)
		})
	}
}
