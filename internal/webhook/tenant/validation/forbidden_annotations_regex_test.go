// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	tenantvalidation "github.com/projectcapsule/capsule/internal/webhook/tenant/validation"
	"github.com/projectcapsule/capsule/pkg/api"
)

func TestForbiddenMetadataRegexAdmission(t *testing.T) {
	t.Parallel()
	handler := tenantvalidation.ForbiddenAnnotationsRegexHandler()
	for _, scope := range []string{"namespaceOptions", "serviceOptions"} {
		for _, field := range []string{"forbiddenLabels", "forbiddenAnnotations"} {
			path := "spec." + scope + "." + field + ".deniedRegex"
			t.Run(path, func(t *testing.T) {
				makeTenant := func(expression string) *capsulev1beta2.Tenant {
					tnt := &capsulev1beta2.Tenant{}
					var labels, annotations *api.ForbiddenListSpec
					if scope == "namespaceOptions" {
						tnt.Spec.NamespaceOptions = &capsulev1beta2.NamespaceOptions{}
						labels, annotations = &tnt.Spec.NamespaceOptions.ForbiddenLabels, &tnt.Spec.NamespaceOptions.ForbiddenAnnotations
					} else {
						// Keep NamespaceOptions nil: Service options must be checked independently.
						tnt.Spec.ServiceOptions = &api.ServiceOptions{}
						labels, annotations = &tnt.Spec.ServiceOptions.ForbiddenLabels, &tnt.Spec.ServiceOptions.ForbiddenAnnotations
					}
					if field == "forbiddenLabels" {
						labels.Regex = expression
					} else {
						annotations.Regex = expression
					}
					return tnt
				}
				for _, expression := range []string{"[", "(", "*invalid", "(?P<"} {
					invalid := makeTenant(expression)
					for _, response := range []*admission.Response{
						handler.OnCreate(nil, nil, invalid, nil, nil)(t.Context(), admission.Request{}),
						handler.OnUpdate(nil, nil, invalid, makeTenant("^blocked-"), nil, nil)(t.Context(), admission.Request{}),
					} {
						require.NotNil(t, response, expression)
						require.False(t, response.Allowed)
						require.Contains(t, response.Result.Message, "unable to compile regex")
						require.Contains(t, response.Result.Message, path)
					}
					require.Nil(t, handler.OnDelete(nil, nil, invalid, nil, nil)(t.Context(), admission.Request{}))
				}
				for _, expression := range []string{"", "^blocked-.*$", " "} {
					valid := makeTenant(expression)
					before := valid.DeepCopy()
					require.Nil(t, handler.OnCreate(nil, nil, valid, nil, nil)(t.Context(), admission.Request{}))
					require.Nil(t, handler.OnUpdate(nil, nil, valid, makeTenant("["), nil, nil)(t.Context(), admission.Request{}), "must permit repairing a malformed stored pattern")
					require.Equal(t, before, valid)
				}
			})
		}
	}
	for _, tnt := range []*capsulev1beta2.Tenant{nil, {}} {
		require.Nil(t, handler.OnCreate(nil, nil, tnt, nil, nil)(t.Context(), admission.Request{}))
	}
}

func BenchmarkForbiddenMetadataRegexAdmission(b *testing.B) {
	handler := tenantvalidation.ForbiddenAnnotationsRegexHandler()
	for _, scope := range []string{"skip", "namespace", "service"} {
		b.Run(scope, func(b *testing.B) {
			tnt := &capsulev1beta2.Tenant{}
			switch scope {
			case "namespace":
				tnt.Spec.NamespaceOptions = &capsulev1beta2.NamespaceOptions{ForbiddenLabels: api.ForbiddenListSpec{Regex: "^blocked-.*$"}, ForbiddenAnnotations: api.ForbiddenListSpec{Regex: "^private/"}}
			case "service":
				tnt.Spec.ServiceOptions = &api.ServiceOptions{ForbiddenLabels: api.ForbiddenListSpec{Regex: "^blocked-.*$"}, ForbiddenAnnotations: api.ForbiddenListSpec{Regex: "^private/"}}
			}
			b.ReportAllocs()
			for b.Loop() {
				if response := handler.OnCreate(nil, nil, tnt, nil, nil)(b.Context(), admission.Request{}); response != nil {
					b.Fatal(response)
				}
			}
		})
	}
}
