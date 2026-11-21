// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package template

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPreparedGeneratorsReuseParsingAndFreshContexts(t *testing.T) {
	source := `apiVersion: v1
kind: ConfigMap
metadata:
  name: '{{ .namespace }}'
data:
  value: '{{ .value }}'
  nonce: '{{ randAlphaNum 32 }}'
`
	prepared, err := prepareGenerator(MissingKeyError, source)
	require.NoError(t, err)
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Go(func() {
			cached, err := prepareGenerator(MissingKeyError, source)
			require.NoError(t, err)
			require.Same(t, prepared, cached)
			name := fmt.Sprintf("namespace-%d", i)
			context := ReferenceContext{"namespace": name, "value": name}
			first, err := RenderUnstructuredItems(context, MissingKeyError, source)
			require.NoError(t, err)
			require.Len(t, first, 1)
			require.Equal(t, name, first[0].GetName())
			second, err := RenderUnstructuredItems(context, MissingKeyError, source)
			require.NoError(t, err)
			require.NotEqual(t, first[0].Object["data"], second[0].Object["data"], "outputs and random functions must not be cached")
		})
	}
	wg.Wait()
	_, err = RenderUnstructuredItems(ReferenceContext{}, MissingKeyError, source)
	require.Error(t, err)
	other, err := prepareGenerator(MissingKeyZero, source)
	require.NoError(t, err)
	require.NotSame(t, prepared, other)
	_, err = prepareGenerator(MissingKeyError, "{{ malformed")
	require.Error(t, err)
}

func TestPreparedGeneratorsBoundRetention(t *testing.T) {
	source := strings.Repeat(" ", maxCachedRuleSourceSize+1)
	first, err := prepareGenerator(MissingKeyError, source)
	require.NoError(t, err)
	second, err := prepareGenerator(MissingKeyError, source)
	require.NoError(t, err)
	require.NotSame(t, first, second, "oversized sources must not be retained")
	for i := range 300 {
		_, err := prepareGenerator(MissingKeyError, fmt.Sprint(i))
		require.NoError(t, err)
	}
	require.LessOrEqual(t, generatorTemplates.cache.Len(), 256)
}

func BenchmarkPrepareGenerator(b *testing.B) {
	for _, mode := range []string{"cold", "warm", "concurrent"} {
		b.Run(mode, func(b *testing.B) {
			source := `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"{{ .namespace }}"}}`
			_, err := prepareGenerator(MissingKeyError, source)
			require.NoError(b, err)
			b.ReportAllocs()
			if mode == "concurrent" {
				b.RunParallel(func(pb *testing.PB) {
					for pb.Next() {
						if _, err := prepareGenerator(MissingKeyError, source); err != nil {
							b.Error(err)
						}
					}
				})
				return
			}
			i := 0
			for b.Loop() {
				input := source
				if mode == "cold" {
					i++
					input += fmt.Sprint(i)
				}
				if _, err := prepareGenerator(MissingKeyError, input); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
