// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
)

//nolint:staticcheck
func TestDeprecatedTenantFieldsWarnings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		configure func(*capsulev1beta2.Tenant)
		field     string
	}{
		{
			name: "resource quota items",
			configure: func(tnt *capsulev1beta2.Tenant) {
				tnt.Spec.ResourceQuota.Items = []corev1.ResourceQuotaSpec{{}}
			},
			field: "`resourceQuotas`",
		},
		{
			name: "resource quota scope",
			configure: func(tnt *capsulev1beta2.Tenant) {
				tnt.Spec.ResourceQuota.Scope = api.ResourceQuotaScopeTenant
			},
			field: "`resourceQuotas`",
		},
		{
			name: "service options",
			configure: func(tnt *capsulev1beta2.Tenant) {
				tnt.Spec.ServiceOptions = &api.ServiceOptions{}
			},
			field: "`serviceOptions`",
		},
		{
			name: "pod options",
			configure: func(tnt *capsulev1beta2.Tenant) {
				tnt.Spec.PodOptions = &api.PodOptions{}
			},
			field: "`podOptions`",
		},
		{
			name: "additional metadata list",
			configure: func(tnt *capsulev1beta2.Tenant) {
				tnt.Spec.NamespaceOptions = &capsulev1beta2.NamespaceOptions{
					AdditionalMetadataList: []api.AdditionalMetadataSelectorSpec{{}},
				}
			},
			field: "`additionalMetadataList`",
		},
		{
			name: "required metadata",
			configure: func(tnt *capsulev1beta2.Tenant) {
				tnt.Spec.NamespaceOptions = &capsulev1beta2.NamespaceOptions{
					RequiredMetadata: &capsulev1beta2.RequiredMetadata{},
				}
			},
			field: "`requiredMetadata`",
		},
		{
			name: "forbidden labels exact",
			configure: func(tnt *capsulev1beta2.Tenant) {
				tnt.Spec.NamespaceOptions = &capsulev1beta2.NamespaceOptions{
					ForbiddenLabels: api.ForbiddenListSpec{Exact: []string{"blocked"}},
				}
			},
			field: "`forbiddenLabels`",
		},
		{
			name: "forbidden labels regex",
			configure: func(tnt *capsulev1beta2.Tenant) {
				tnt.Spec.NamespaceOptions = &capsulev1beta2.NamespaceOptions{
					ForbiddenLabels: api.ForbiddenListSpec{Regex: "blocked-.*"},
				}
			},
			field: "`forbiddenLabels`",
		},
		{
			name: "forbidden annotations exact",
			configure: func(tnt *capsulev1beta2.Tenant) {
				tnt.Spec.NamespaceOptions = &capsulev1beta2.NamespaceOptions{
					ForbiddenAnnotations: api.ForbiddenListSpec{Exact: []string{"blocked"}},
				}
			},
			field: "`forbiddenAnnotations`",
		},
		{
			name: "forbidden annotations regex",
			configure: func(tnt *capsulev1beta2.Tenant) {
				tnt.Spec.NamespaceOptions = &capsulev1beta2.NamespaceOptions{
					ForbiddenAnnotations: api.ForbiddenListSpec{Regex: "blocked-.*"},
				}
			},
			field: "`forbiddenAnnotations`",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tnt := &capsulev1beta2.Tenant{}
			tt.configure(tnt)

			response := (&warningHandler{}).handle(tnt, admission.Request{})
			if len(response.Warnings) != 1 {
				t.Fatalf("warnings = %v, want exactly one warning", response.Warnings)
			}

			if !strings.Contains(response.Warnings[0], tt.field) {
				t.Fatalf("warning = %q, want field %s", response.Warnings[0], tt.field)
			}
		})
	}
}

func TestDeprecatedTenantFieldsWarningsAreAbsentForZeroValues(t *testing.T) {
	t.Parallel()

	tnt := &capsulev1beta2.Tenant{
		Spec: capsulev1beta2.TenantSpec{
			NamespaceOptions: &capsulev1beta2.NamespaceOptions{},
		},
	}

	response := (&warningHandler{}).handle(tnt, admission.Request{})
	if len(response.Warnings) != 0 {
		t.Fatalf("warnings = %v, want no warnings", response.Warnings)
	}
}
