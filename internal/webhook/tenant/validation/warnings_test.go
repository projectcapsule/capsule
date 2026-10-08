// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/api/runtime"
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

func TestDeprecatedSchedulerWarnings(t *testing.T) {
	legacy := &rules.NamespaceRuleBodyTenant{NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
		Enforce: &rules.NamespaceRuleEnforceBody{Workloads: rules.NamespaceRuleEnforceWorkloadsBody{Schedulers: []runtime.ExpressionMatch{{Exact: []string{"legacy"}}}}},
	}}
	preferred := &rules.NamespaceRuleBodyTenant{NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
		Enforce: &rules.NamespaceRuleEnforceBody{Workloads: rules.NamespaceRuleEnforceWorkloadsBody{Placement: rules.WorkloadPlacementEnforcement{Schedulers: []runtime.ExpressionMatch{{Exact: []string{"preferred"}}}}}},
	}}
	for _, deprecated := range []bool{false, true} {
		t.Run(fmt.Sprint(deprecated), func(t *testing.T) {
			tnt := &capsulev1beta2.Tenant{Spec: capsulev1beta2.TenantSpec{Rules: []*rules.NamespaceRuleBodyTenant{nil, {}, {NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{}}, preferred}}}
			if deprecated {
				tnt.Spec.Rules = append(tnt.Spec.Rules, legacy, legacy.DeepCopy())
			}
			before := tnt.DeepCopy()
			h := WarningHandler(nil)
			for _, response := range []*admission.Response{
				h.OnCreate(nil, nil, tnt, nil, nil)(t.Context(), admission.Request{}),
				h.OnUpdate(nil, nil, tnt, &capsulev1beta2.Tenant{}, nil, nil)(t.Context(), admission.Request{}),
			} {
				require.True(t, response.Allowed)
				if deprecated {
					require.Len(t, response.Warnings, 1)
					require.Contains(t, response.Warnings[0], "`spec.rules[].enforce.workloads.schedulers` is deprecated")
					require.Contains(t, response.Warnings[0], "`spec.rules[].enforce.workloads.placement.schedulers`")
				} else {
					require.Empty(t, response.Warnings)
				}
			}
			require.Nil(t, h.OnDelete(nil, nil, tnt, nil, nil)(t.Context(), admission.Request{}))
			require.Equal(t, before, tnt)
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

func BenchmarkSchedulerWarnings(b *testing.B) {
	for _, count := range []int{1, 20, 1000} {
		for _, legacy := range []bool{false, true} {
			b.Run(fmt.Sprintf("rules=%d/legacy=%t", count, legacy), func(b *testing.B) {
				tnt := &capsulev1beta2.Tenant{}
				for range count {
					tnt.Spec.Rules = append(tnt.Spec.Rules, &rules.NamespaceRuleBodyTenant{NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{}}})
				}
				if legacy {
					tnt.Spec.Rules[count-1].Enforce.Workloads.Schedulers = []runtime.ExpressionMatch{{Exact: []string{"legacy"}}}
				}
				h := WarningHandler(nil).OnCreate(nil, nil, tnt, nil, nil)
				b.ReportAllocs()
				for b.Loop() {
					response := h(b.Context(), admission.Request{})
					if !response.Allowed || (len(response.Warnings) == 1) != legacy {
						b.Fatalf("unexpected response: %v", response)
					}
				}
			})
		}
	}
}
