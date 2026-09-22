// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package defaults

import (
	"fmt"
	"strconv"

	"gomodules.xyz/jsonpatch/v2"
	"k8s.io/apiserver/pkg/cel/environment"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	celruntime "github.com/projectcapsule/capsule/pkg/runtime/cel"
)

// Convert only missing policies. Narrow patches retain deprecated settings and
// avoid rewriting raw manifests, template context, metadata, or status.
//
//nolint:staticcheck // Admission intentionally converts the deprecated compatibility fields.
func mutateReplicationPolicy(req admission.Request, decoder admission.Decoder, conditions celruntime.ResourceConditionCompiler) *admission.Response {
	if req.SubResource != "" {
		return nil
	}

	var spec *capsulev1beta2.TenantResourceCommonSpec

	if req.Resource.Resource == "tenantresources" {
		var resource capsulev1beta2.TenantResource
		if err := decoder.Decode(req, &resource); err != nil {
			return ad.ErroredResponse(err)
		}

		spec = &resource.Spec.TenantResourceCommonSpec
	} else {
		var resource capsulev1beta2.GlobalTenantResource
		if err := decoder.Decode(req, &resource); err != nil {
			return ad.ErroredResponse(err)
		}

		spec = &resource.Spec.TenantResourceCommonSpec
	}

	policy := apiruntime.ResourceTemplatePolicy{
		Creation: apiruntime.ResourceCreationPolicyOwner,
		Deletion: apiruntime.ResourceDeletionPolicyRemove,
		Protect:  new(true),
		Force:    spec.Settings.Force != nil && *spec.Settings.Force,
	}
	if spec.Settings.Adopt != nil && *spec.Settings.Adopt {
		policy.Creation = apiruntime.ResourceCreationPolicyMerge
	}

	if spec.PruningOnDelete != nil && !*spec.PruningOnDelete {
		policy.Deletion = apiruntime.ResourceDeletionPolicyOrphan
	}

	var patches []jsonpatch.JsonPatchOperation

	for i := range spec.Resources {
		if policy := spec.Resources[i].Policy; policy != nil && policy.Condition != "" {
			if conditions == nil {
				return ad.ErroredResponse(fmt.Errorf("resource condition compiler is not configured"))
			}

			if _, err := conditions.GetOrCompileResourceCondition(policy.Condition, environment.NewExpressions); err != nil {
				return ad.Denyf("spec.resources[%d].policy.condition: %v", i, err)
			}
		}

		if spec.Resources[i].Policy == nil {
			patches = append(patches, jsonpatch.NewOperation("add", "/spec/resources/"+strconv.Itoa(i)+"/policy", policy))
		}
	}

	if len(patches) == 0 {
		return nil
	}

	response := admission.Patched("Converted deprecated replication settings to per-resource policies", patches...)

	return &response
}
