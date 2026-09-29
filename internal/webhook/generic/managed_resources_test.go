// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package generic

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	validation "github.com/projectcapsule/capsule/internal/webhook/rules/generic/validation"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

func TestManagedResourceProtectionDoesNotExemptMetadata(t *testing.T) {
	t.Setenv(configuration.EnvironmentControllerNamespace, "capsule-system")
	t.Setenv(configuration.EnvironmentServiceaccountName, "capsule-controller")
	const denied = "example.org/denied"
	bodies := []*rules.NamespaceRuleBodyNamespace{{Enforce: &rules.NamespaceRuleEnforceBody{
		Action: rules.ActionTypeDeny,
		Metadata: []rules.MetadataRule{{Kinds: []string{"ConfigMap"}, Labels: map[string]rules.MetadataValueRule{
			denied: {Values: []runtime.ExpressionMatch{{Exact: []string{"true"}}}},
		}}},
	}}}
	for _, source := range []string{meta.ValueControllerResourcePermit, meta.ValueControllerReplications} {
		for _, op := range []admissionv1.Operation{admissionv1.Create, admissionv1.Update} {
			for _, marker := range []string{source, meta.ValueController, meta.ValueControllerResources} {
				for _, removeProtection := range []bool{false, true} {
					if removeProtection && op == admissionv1.Create {
						continue
					}
					t.Run(fmt.Sprintf("%s/%s/marker=%s/remove=%t", source, op, marker, removeProtection), func(t *testing.T) {
						var c client.Client
						var req admission.Request
						var guard handlers.Handler
						protection := meta.ResourcePermitProtectionLabel
						if source == meta.ValueControllerResourcePermit {
							c, req = permitAdmissionFixture(t, 2, false)
							guard = ResourcePermitResourceHandler()
						} else {
							c, req = replicationAdmissionFixture(t, true, true, 2)
							guard = ReplicaHandler()
							protection = meta.ReplicationProtectionLabel
							req.UserInfo.Username = "system:serviceaccount:tenant-a:runner"
						}
						req.Operation = op
						old := &metav1.PartialObjectMetadata{}
						require.NoError(t, json.Unmarshal(req.Object.Raw, old))
						old.Labels = map[string]string{meta.CreatedByCapsuleLabel: source, meta.NewManagedByCapsuleLabel: source, protection: meta.ValueTrue}
						var err error
						req.OldObject.Raw, err = json.Marshal(old)
						require.NoError(t, err)
						next := old.DeepCopy()
						next.Labels[meta.NewManagedByCapsuleLabel] = marker
						if removeProtection {
							delete(next.Labels, protection)
						}
						req.Object.Raw, err = json.Marshal(next)
						require.NoError(t, err)
						call := guard.OnUpdate(c, c, admission.NewDecoder(c.Scheme()), nil)
						if op == admissionv1.Create {
							call = guard.OnCreate(c, c, admission.NewDecoder(c.Scheme()), nil)
						}
						// The execution identity may update its target, but that
						// authorization must not override the namespace's rules.
						require.Nil(t, call(t.Context(), req), "authorized protection handler must continue admission")
						recorder := events.NewEventRecorder(nil, logr.Discard(), nil, nil)
						tenant := &capsulev1beta2.Tenant{Name: req.Namespace}
						evaluate := validation.GenericRules(nil).OnUpdate(nil, nil, old, next, nil, recorder, tenant, bodies)
						if op == admissionv1.Create {
							evaluate = validation.GenericRules(nil).OnCreate(nil, nil, next, nil, recorder, tenant, bodies)
						}
						require.Nil(t, evaluate(t.Context(), req), "valid metadata must remain allowed")
						next.Labels[denied] = "true"
						req.Object.Raw, err = json.Marshal(next)
						require.NoError(t, err)
						require.Nil(t, call(t.Context(), req), "protection and metadata enforcement are independent")
						response := evaluate(t.Context(), req)
						require.NotNil(t, response)
						require.False(t, response.Allowed)
						require.Contains(t, response.Result.Message, "denied by namespace rule")

						// Retaining or stripping markers cannot authorize an owner
						// or another tenant's execution identity on the protected target.
						for _, actor := range []string{"alice", "system:serviceaccount:other-tenant:runner"} {
							req.UserInfo.Username = actor
							response = call(t.Context(), req)
							require.NotNil(t, response)
							require.False(t, response.Allowed)
							require.EqualValues(t, 403, response.Result.Code)
						}
					})
				}
			}
		}
	}
}
