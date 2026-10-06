// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

func TestLabelSelectorCache(t *testing.T) {
	c := NewLabelSelectorCache()
	selector := &metav1.LabelSelector{MatchLabels: map[string]string{"app": "one"}}
	compiled, err := c.GetOrCompile(selector)
	require.NoError(t, err)
	again, err := c.GetOrCompile(selector.DeepCopy())
	require.NoError(t, err)
	require.Equal(t, 1, len(c.selectors))
	require.True(t, again.Matches(labels.Set{"app": "one"}))
	// Mutating a caller's selector must not modify published compiled entries.
	selector.MatchLabels["app"] = "two"
	updated, err := c.GetOrCompile(selector)
	require.NoError(t, err)
	require.True(t, compiled.Matches(labels.Set{"app": "one"}))
	require.False(t, compiled.Matches(labels.Set{"app": "two"}))
	require.True(t, updated.Matches(labels.Set{"app": "two"}))
	for _, tc := range []struct {
		selector *metav1.LabelSelector
		matches  bool
	}{
		{nil, false}, {&metav1.LabelSelector{}, true},
		{&metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "app", Operator: metav1.LabelSelectorOpNotIn, Values: []string{"blocked"}}}}, true},
	} {
		got, err := c.GetOrCompile(tc.selector)
		require.NoError(t, err)
		require.Equal(t, tc.matches, got.Matches(labels.Set{}))
	}
	_, err = c.GetOrCompile(&metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "app", Operator: "Invalid"}}})
	require.Error(t, err)
	for i := 0; i < maxLabelSelectors+10; i++ {
		_, err := c.GetOrCompile(&metav1.LabelSelector{MatchLabels: map[string]string{"app": fmt.Sprint(i)}})
		require.NoError(t, err)
	}
	require.LessOrEqual(t, len(c.selectors), maxLabelSelectors)
	recompiled, err := c.GetOrCompile(&metav1.LabelSelector{MatchLabels: map[string]string{"app": "one"}})
	require.NoError(t, err)
	require.True(t, recompiled.Matches(labels.Set{"app": "one"}))
}

func TestLabelSelectorConcurrentPublication(t *testing.T) {
	c := NewLabelSelectorCache()
	selector := &metav1.LabelSelector{MatchLabels: map[string]string{"app": "a"}}
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			for range 50 {
				got, err := c.GetOrCompile(selector)
				if err != nil || !got.Matches(labels.Set{"app": "a"}) {
					t.Errorf("compile: %v", err)
					return
				}
			}
		})
	}
	wg.Wait()
	require.Len(t, c.selectors, 1)
}

func BenchmarkLabelSelectorCache(b *testing.B) {
	selector := &metav1.LabelSelector{MatchLabels: map[string]string{"app": "a", "tier": "frontend"}}
	for _, mode := range []string{"cold", "warm", "parallel"} {
		b.Run(mode, func(b *testing.B) {
			c := NewLabelSelectorCache()
			_, err := c.GetOrCompile(selector)
			require.NoError(b, err)
			run := func() {
				got, err := c.GetOrCompile(selector)
				if err != nil || !got.Matches(labels.Set{"app": "a", "tier": "frontend"}) {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			if mode == "parallel" {
				b.RunParallel(func(pb *testing.PB) {
					for pb.Next() {
						run()
					}
				})
				return
			}
			for b.Loop() {
				if mode == "cold" {
					c = NewLabelSelectorCache()
				}
				run()
			}
		})
	}
}
