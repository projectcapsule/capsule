// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package invalidator

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/cel/environment"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/internal/controllers/customquotas"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/quota"
	"github.com/projectcapsule/capsule/pkg/runtime/selectors"
)

func quotaCELSource(expression, namespace string) capsulev1beta2.CustomQuotaSpecSource {
	return capsulev1beta2.CustomQuotaSpecSource{
		VersionKind: apiruntime.VersionKind{APIVersion: "v1", Kind: "ConfigMap"},
		CustomQuotaSpecSourceConfig: capsulev1beta2.CustomQuotaSpecSourceConfig{
			Operation: quota.OpAdd, CEL: expression,
			Selectors: []selectors.SelectorWithFields{{CELExpressions: []string{fmt.Sprintf("object.metadata.namespace == '%s'", namespace)}}},
		},
	}
}

func newCELInvalidator(t testing.TB, objects ...client.Object) *CacheInvalidator {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, capsulev1beta2.AddToScheme(scheme))
	conditions, err := cache.NewCELCache()
	require.NoError(t, err)
	return &CacheInvalidator{
		Client:   fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build(),
		CELCache: conditions, JSONPathCache: cache.NewJSONPathCache(), TargetsCache: cache.NewCompiledTargetsCache[string](),
	}
}

func TestRebuildTargetsInvalidatesCEL(t *testing.T) {
	local := &capsulev1beta2.CustomQuota{Name: "local", Namespace: "tenant-a", Spec: capsulev1beta2.CustomQuotaSpec{
		Sources: []capsulev1beta2.CustomQuotaSpecSource{quotaCELSource(`quantity('1')`, "tenant-a")},
	}}
	global := &capsulev1beta2.GlobalCustomQuota{Name: "global", Spec: capsulev1beta2.GlobalCustomQuotaSpec{
		Sources: []capsulev1beta2.CustomQuotaSpecSource{quotaCELSource(`quantity('1')`, "tenant-b")},
	}}
	r := newCELInvalidator(t, local, global)
	old, err := r.CELCache.GetOrCompileQuantity(`quantity('1')`, environment.StoredExpressions)
	require.NoError(t, err)
	_, err = r.CELCache.GetOrCompileBoolean("false", environment.NewExpressions)
	require.NoError(t, err)
	condition, err := r.CELCache.GetOrCompileResourceCondition("object == null", environment.StoredExpressions)
	require.NoError(t, err)
	require.NoError(t, r.rebuildTargetsCache(t.Context(), logr.Discard()))
	require.Equal(t, 4, r.CELCache.Stats(), "active quota expressions should be warmed without evicting resource conditions")
	reused, err := r.CELCache.GetOrCompileResourceCondition("object == null", environment.StoredExpressions)
	require.NoError(t, err)
	require.Same(t, condition, reused)
	keys := []string{customquotas.MakeCustomQuotaCacheKey(local.Namespace, local.Name), customquotas.MakeGlobalCustomQuotaCacheKey(global.Name)}
	for i, key := range keys {
		targets, ok := r.TargetsCache.Get(key)
		require.True(t, ok)
		require.Len(t, targets, 1)
		require.NotSame(t, old, targets[0].CompiledCEL, "target rebuild must use a newly compiled program")
		for j, namespace := range []string{"tenant-a", "tenant-b"} {
			allowed, err := targets[0].CompiledSelectors[0].CELMatchers[0].EvaluateBoolean(t.Context(), unstructured.Unstructured{Object: map[string]any{"metadata": map[string]any{"namespace": namespace}}})
			require.NoError(t, err)
			require.Equal(t, i == j, allowed)
		}
	}
	// Updates and deletion retire expressions on the next invalidation cycle.
	local.Spec.Sources[0].CEL = `quantity('2')`
	require.NoError(t, r.Update(t.Context(), local))
	require.NoError(t, r.Delete(t.Context(), global))
	require.NoError(t, r.rebuildTargetsCache(t.Context(), logr.Discard()))
	require.Equal(t, 3, r.CELCache.Stats())
	_, ok := r.TargetsCache.Get(keys[1])
	require.False(t, ok)
	targets, ok := r.TargetsCache.Get(keys[0])
	require.True(t, ok)
	value, err := targets[0].CompiledCEL.EvaluateQuantity(t.Context(), unstructured.Unstructured{})
	require.NoError(t, err)
	require.Equal(t, "2", value.String())
	require.NoError(t, r.Delete(t.Context(), local))
	require.NoError(t, r.rebuildTargetsCache(t.Context(), logr.Discard()))
	require.Equal(t, 1, r.CELCache.Stats())
	reused, err = r.CELCache.GetOrCompileResourceCondition("object == null", environment.StoredExpressions)
	require.NoError(t, err)
	require.Same(t, condition, reused)
}

func TestRebuildTargetsPreservesCELOnListError(t *testing.T) {
	for _, failAt := range []int{1, 2} {
		t.Run(fmt.Sprintf("list-%d", failAt), func(t *testing.T) {
			r := newCELInvalidator(t)
			compiled, err := r.CELCache.GetOrCompileResourceCondition("object == null", environment.StoredExpressions)
			require.NoError(t, err)
			failure := errors.New("unavailable")
			calls := 0
			r.Client = interceptor.NewClient(r.Client.(client.WithWatch), interceptor.Funcs{
				List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					calls++
					if calls == failAt {
						return failure
					}
					return c.List(ctx, list, opts...)
				},
			})
			require.ErrorIs(t, r.rebuildTargetsCache(t.Context(), logr.Discard()), failure)
			reused, err := r.CELCache.GetOrCompileResourceCondition("object == null", environment.StoredExpressions)
			require.NoError(t, err)
			require.Same(t, compiled, reused)
		})
	}
}

func BenchmarkRebuildCELTargets(b *testing.B) {
	for _, tenants := range []int{1, 4} {
		for _, quotas := range []int{1, 25} {
			b.Run(fmt.Sprintf("tenants=%d/quotas=%d", tenants, quotas), func(b *testing.B) {
				var objects []client.Object
				for tenant := range tenants {
					for item := range quotas {
						namespace := fmt.Sprintf("tenant-%d", tenant)
						objects = append(objects, &capsulev1beta2.CustomQuota{
							Name: fmt.Sprintf("quota-%d", item), Namespace: namespace,
							Spec: capsulev1beta2.CustomQuotaSpec{Sources: []capsulev1beta2.CustomQuotaSpecSource{quotaCELSource(fmt.Sprintf("quantity('%d')", item+1), namespace)}},
						})
					}
				}
				r := newCELInvalidator(b, objects...)
				conditions := make([]string, tenants*quotas)
				for i := range conditions {
					conditions[i] = fmt.Sprintf("object == null || object.metadata.namespace == 'tenant-%d'", i)
					_, err := r.CELCache.GetOrCompileResourceCondition(conditions[i], environment.StoredExpressions)
					require.NoError(b, err)
				}
				require.NoError(b, r.rebuildTargetsCache(b.Context(), logr.Discard()))
				b.ReportAllocs()
				for b.Loop() {
					if err := r.rebuildTargetsCache(b.Context(), logr.Discard()); err != nil {
						b.Fatal(err)
					}
					// Include the next use of resource conditions after quota invalidation.
					for _, expression := range conditions {
						if _, err := r.CELCache.GetOrCompileResourceCondition(expression, environment.StoredExpressions); err != nil {
							b.Fatal(err)
						}
					}
					if r.TargetsCache.Stats() != tenants*quotas {
						b.Fatal("missing compiled targets")
					}
				}
			})
		}
	}
}
