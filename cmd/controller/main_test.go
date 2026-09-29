// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

func TestControllerSchemeDecodesDRA(t *testing.T) {
	t.Parallel()
	decoder := serializer.NewCodecFactory(scheme).UniversalDeserializer()
	for _, version := range []string{"v1", "v1beta2"} {
		for _, kind := range []string{"DeviceClass", "ResourceClaim", "ResourceClaimTemplate"} {
			for _, suffix := range []string{"", "List"} {
				t.Run(version+"/"+kind+suffix, func(t *testing.T) {
					data := []byte(fmt.Sprintf(`{"apiVersion":"resource.k8s.io/%s","kind":%q}`, version, kind+suffix))
					obj, err := runtime.Decode(decoder, data)
					require.NoError(t, err)
					require.Equal(t, "resource.k8s.io/"+version, obj.GetObjectKind().GroupVersionKind().GroupVersion().String())
					require.Equal(t, kind+suffix, obj.GetObjectKind().GroupVersionKind().Kind)
				})
			}
		}
	}
}
