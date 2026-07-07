// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package selectors_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/projectcapsule/capsule/pkg/runtime/selectors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestMatchesSelectors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		labels    labels.Set
		selectors []metav1.LabelSelector
		want      bool
	}{
		{
			name: "empty selectors match",
			want: true,
		},
		{
			name:   "matching selector returns true",
			labels: labels.Set{"env": "prod"},
			selectors: []metav1.LabelSelector{{
				MatchLabels: map[string]string{"env": "prod"},
			}},
			want: true,
		},
		{
			name:   "any matching selector returns true",
			labels: labels.Set{"env": "prod"},
			selectors: []metav1.LabelSelector{
				{MatchLabels: map[string]string{"env": "dev"}},
				{MatchLabels: map[string]string{"env": "prod"}},
			},
			want: true,
		},
		{
			name:   "invalid selector is skipped",
			labels: labels.Set{"env": "prod"},
			selectors: []metav1.LabelSelector{
				{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "env", Operator: "invalid"}}},
				{MatchLabels: map[string]string{"env": "prod"}},
			},
			want: true,
		},
		{
			name:   "no selector matches",
			labels: labels.Set{"env": "prod"},
			selectors: []metav1.LabelSelector{{
				MatchLabels: map[string]string{"env": "dev"},
			}},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := selectors.MatchesSelectors(tt.labels, tt.selectors); got != tt.want {
				t.Fatalf("MatchesSelectors() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMatchesSelector(t *testing.T) {
	t.Parallel()

	t.Run("matches", func(t *testing.T) {
		t.Parallel()

		got, err := selectors.MatchesSelector(labels.Set{"tier": "frontend"}, metav1.LabelSelector{
			MatchLabels: map[string]string{"tier": "frontend"},
		})
		if err != nil {
			t.Fatalf("MatchesSelector() unexpected error: %v", err)
		}
		if !got {
			t.Fatalf("MatchesSelector() = false, want true")
		}
	})

	t.Run("does not match", func(t *testing.T) {
		t.Parallel()

		got, err := selectors.MatchesSelector(labels.Set{"tier": "backend"}, metav1.LabelSelector{
			MatchLabels: map[string]string{"tier": "frontend"},
		})
		if err != nil {
			t.Fatalf("MatchesSelector() unexpected error: %v", err)
		}
		if got {
			t.Fatalf("MatchesSelector() = true, want false")
		}
	})

	t.Run("invalid selector returns error", func(t *testing.T) {
		t.Parallel()

		got, err := selectors.MatchesSelector(labels.Set{"tier": "backend"}, metav1.LabelSelector{
			MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "tier", Operator: "invalid"}},
		})
		if err == nil {
			t.Fatalf("MatchesSelector() expected error")
		}
		if got {
			t.Fatalf("MatchesSelector() = true, want false on error")
		}
	})
}

func TestNamespaceSelectorGetMatchingNamespaces(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cl := fakeClient(
		namespace("alpha", map[string]string{"team": "a"}),
		namespace("beta", map[string]string{"team": "b"}),
		namespace("gamma", nil),
	)

	t.Run("nil selector returns nil", func(t *testing.T) {
		t.Parallel()

		got, err := (&selectors.NamespaceSelector{}).GetMatchingNamespaces(ctx, cl)
		if err != nil {
			t.Fatalf("GetMatchingNamespaces() unexpected error: %v", err)
		}
		if got != nil {
			t.Fatalf("GetMatchingNamespaces() = %#v, want nil", got)
		}
	})

	t.Run("filters namespaces by labels", func(t *testing.T) {
		t.Parallel()

		got, err := (&selectors.NamespaceSelector{
			LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"team": "a"}},
		}).GetMatchingNamespaces(ctx, cl)
		if err != nil {
			t.Fatalf("GetMatchingNamespaces() unexpected error: %v", err)
		}

		if names := namespaceNames(got); !reflect.DeepEqual(names, []string{"alpha"}) {
			t.Fatalf("GetMatchingNamespaces() names = %#v, want alpha", names)
		}
	})

	t.Run("invalid selector returns error", func(t *testing.T) {
		t.Parallel()

		_, err := (&selectors.NamespaceSelector{
			LabelSelector: &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "team", Operator: "invalid"}},
			},
		}).GetMatchingNamespaces(ctx, cl)
		if err == nil {
			t.Fatalf("GetMatchingNamespaces() expected error")
		}
	})
}

func TestGetNamespacesMatchingSelectors(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cl := fakeClient(
		namespace("zeta", map[string]string{"team": "a", "region": "eu"}),
		namespace("alpha", map[string]string{"team": "a", "region": "us"}),
		namespace("beta", map[string]string{"team": "b", "region": "eu"}),
	)

	got, err := selectors.GetNamespacesMatchingSelectors(ctx, cl, []selectors.NamespaceSelector{
		{LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"team": "a"}}},
		{LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"region": "eu"}}},
	})
	if err != nil {
		t.Fatalf("GetNamespacesMatchingSelectors() unexpected error: %v", err)
	}

	if names := namespaceNames(got); !reflect.DeepEqual(names, []string{"alpha", "beta", "zeta"}) {
		t.Fatalf("GetNamespacesMatchingSelectors() names = %#v, want sorted unique names", names)
	}

	gotNames, err := selectors.GetNamespacesMatchingSelectorsStrings(ctx, cl, []selectors.NamespaceSelector{
		{LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"team": "a"}}},
	})
	if err != nil {
		t.Fatalf("GetNamespacesMatchingSelectorsStrings() unexpected error: %v", err)
	}
	if !reflect.DeepEqual(gotNames, []string{"alpha", "zeta"}) {
		t.Fatalf("GetNamespacesMatchingSelectorsStrings() = %#v, want alpha and zeta", gotNames)
	}

	empty, err := selectors.GetNamespacesMatchingSelectors(ctx, cl, nil)
	if err != nil {
		t.Fatalf("GetNamespacesMatchingSelectors() unexpected error for nil selectors: %v", err)
	}
	if empty != nil {
		t.Fatalf("GetNamespacesMatchingSelectors() = %#v, want nil for no selectors", empty)
	}
}

func TestSelectorWithNamespaceSelectorMatchObjects(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cl := fakeClient(
		namespace("allowed", map[string]string{"tenant": "one"}),
		namespace("denied", map[string]string{"tenant": "two"}),
	)
	objects := []metav1.Object{
		configMap("allowed", "match", map[string]string{"app": "api"}),
		configMap("allowed", "skip-label", map[string]string{"app": "worker"}),
		configMap("denied", "skip-namespace", map[string]string{"app": "api"}),
	}

	got, err := (&selectors.SelectorWithNamespaceSelector{
		LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}},
		NamespaceSelector: &selectors.NamespaceSelector{
			LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"tenant": "one"}},
		},
	}).MatchObjects(ctx, cl, objects)
	if err != nil {
		t.Fatalf("MatchObjects() unexpected error: %v", err)
	}

	if names := objectNames(got); !reflect.DeepEqual(names, []string{"match"}) {
		t.Fatalf("MatchObjects() names = %#v, want match", names)
	}

	noNamespaceFilter, err := (&selectors.SelectorWithNamespaceSelector{
		LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}},
	}).MatchObjects(ctx, cl, objects)
	if err != nil {
		t.Fatalf("MatchObjects() unexpected error without namespace selector: %v", err)
	}
	if names := objectNames(noNamespaceFilter); !reflect.DeepEqual(names, []string{"match", "skip-namespace"}) {
		t.Fatalf("MatchObjects() names = %#v, want both app=api objects", names)
	}

	nilSelector, err := (*selectors.SelectorWithNamespaceSelector)(nil).MatchObjects(ctx, cl, objects)
	if err != nil {
		t.Fatalf("MatchObjects() unexpected error for nil selector: %v", err)
	}
	if nilSelector != nil {
		t.Fatalf("MatchObjects() = %#v, want nil for nil receiver", nilSelector)
	}
}

func TestListBySelectors(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cl := fakeClient(
		configMap("ns-a", "one", map[string]string{"app": "api"}),
		configMap("ns-a", "two", map[string]string{"app": "worker"}),
		configMap("ns-b", "three", nil),
	)

	got, err := selectors.ListBySelectors[*corev1.ConfigMap](ctx, cl, &corev1.ConfigMapList{}, []*metav1.LabelSelector{
		{MatchLabels: map[string]string{"app": "api"}},
		{MatchLabels: map[string]string{"app": "worker"}},
	})
	if err != nil {
		t.Fatalf("ListBySelectors() unexpected error: %v", err)
	}
	if names := configMapKeys(got); !reflect.DeepEqual(names, []string{"ns-a/one", "ns-a/two"}) {
		t.Fatalf("ListBySelectors() = %#v, want matching config maps", names)
	}

	empty, err := selectors.ListBySelectors[*corev1.ConfigMap](ctx, cl, &corev1.ConfigMapList{}, nil)
	if err != nil {
		t.Fatalf("ListBySelectors() unexpected error for nil selectors: %v", err)
	}
	if empty != nil {
		t.Fatalf("ListBySelectors() = %#v, want nil for nil selectors", empty)
	}

	empty, err = selectors.ListBySelectors[*corev1.ConfigMap](ctx, cl, &corev1.ConfigMapList{}, []*metav1.LabelSelector{nil})
	if err != nil {
		t.Fatalf("ListBySelectors() unexpected error for nil selector entry: %v", err)
	}
	if empty != nil {
		t.Fatalf("ListBySelectors() = %#v, want nil when all selector entries are nil", empty)
	}

	if _, err = selectors.ListBySelectors[*corev1.ConfigMap](ctx, cl, nil, []*metav1.LabelSelector{{}}); err == nil {
		t.Fatalf("ListBySelectors() expected error for nil list")
	}

	if _, err = selectors.ListBySelectors[*corev1.ConfigMap](ctx, cl, &corev1.ConfigMapList{}, []*metav1.LabelSelector{{
		MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "app", Operator: "invalid"}},
	}}); err == nil {
		t.Fatalf("ListBySelectors() expected error for invalid selector")
	}
}

func fakeClient(objects ...client.Object) client.Client {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		panic(err)
	}

	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}

func namespace(name string, lbls map[string]string) *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: lbls}}
}

func configMap(namespace, name string, lbls map[string]string) *corev1.ConfigMap {
	return &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name, Labels: lbls}}
}

func namespaceNames(namespaces []corev1.Namespace) []string {
	names := make([]string, 0, len(namespaces))
	for _, ns := range namespaces {
		names = append(names, ns.Name)
	}

	return names
}

func objectNames(objects []metav1.Object) []string {
	names := make([]string, 0, len(objects))
	for _, obj := range objects {
		names = append(names, obj.GetName())
	}

	return names
}

func configMapKeys(configMaps []*corev1.ConfigMap) []string {
	names := make([]string, 0, len(configMaps))
	for _, configMap := range configMaps {
		names = append(names, configMap.Namespace+"/"+configMap.Name)
	}

	return names
}
