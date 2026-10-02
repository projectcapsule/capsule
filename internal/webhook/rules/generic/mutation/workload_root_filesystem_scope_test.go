// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsule "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

func TestEphemeralMutationTenantProfilesAudienceAndReads(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, capsule.AddToScheme(scheme))
	var fixtures []client.Object
	for i, name := range []string{"tenant-a", "tenant-b"} {
		tnt := &capsule.Tenant{ObjectMeta: metav1.ObjectMeta{Name: name, UID: types.UID(name)}}
		fixtures = append(fixtures, tnt)
		for _, profile := range []string{"selected", "plain"} {
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name + "-" + profile, OwnerReferences: []metav1.OwnerReference{{APIVersion: capsule.GroupVersion.String(), Kind: "Tenant", Name: name, UID: tnt.UID}}}}
			status := &capsule.RuleStatus{ObjectMeta: metav1.ObjectMeta{Name: meta.NameForManagedRuleStatus(), Namespace: ns.Name}}
			if profile == "selected" {
				body := rootFilesystemBody(new(i == 0), rules.ValidatePod)
				body.Audience = []rules.Audience{{Kind: rules.AudienceKindUser, Name: name + "-owner"}}
				status.Status.Rules = []*rules.NamespaceRuleBodyNamespace{body}
			}
			fixtures = append(fixtures, ns, status)
		}
	}
	for _, tc := range []struct {
		name, namespace, user string
		want                  *bool
		readError             bool
	}{
		{"tenant A", "tenant-a-selected", "tenant-a-owner", new(true), false},
		{"tenant B", "tenant-b-selected", "tenant-b-owner", new(false), false},
		{"different namespace profile", "tenant-a-plain", "tenant-a-owner", nil, false},
		{"different audience", "tenant-a-selected", "tenant-b-owner", nil, false},
		{"rules unavailable fails closed", "tenant-a-selected", "tenant-a-owner", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var reads atomic.Int32
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(fixtures...).WithInterceptorFuncs(interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					reads.Add(1)
					if _, ok := obj.(*capsule.RuleStatus); ok && tc.readError {
						return fmt.Errorf("rules unavailable")
					}
					return c.Get(ctx, key, obj, opts...)
				},
				List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
					return fmt.Errorf("unexpected list")
				},
			}).Build()
			old, obj := ephemeralRootFilesystemObjects(t)
			old.SetNamespace(tc.namespace)
			obj.SetNamespace(tc.namespace)
			req := rootFilesystemAdmissionRequest(t, obj, old, admissionv1.Update, "ephemeralcontainers")
			req.UserInfo.Username = tc.user
			wrapper := handlers.TypedTenantWithRulesetHandler[*unstructured.Unstructured]{Factory: func() *unstructured.Unstructured { return &unstructured.Unstructured{} }, Handlers: []handlers.TypedHandlerWithTenantWithRuleset[*unstructured.Unstructured]{MetadataRules(nil)}}
			response := wrapper.OnUpdate(c, c, admission.NewDecoder(scheme), nil)(t.Context(), req)
			if tc.readError {
				require.NotNil(t, response)
				require.False(t, response.Allowed)
				require.Contains(t, response.Result.Message, "rules unavailable")
			} else if tc.want == nil {
				require.Nil(t, response)
			} else {
				require.NotNil(t, response)
				require.True(t, response.Allowed)
				require.Len(t, response.Patches, 1)
				require.Equal(t, "/spec/ephemeralContainers/1/securityContext", response.Patches[0].Path)
				require.Equal(t, map[string]any{"readOnlyRootFilesystem": *tc.want}, response.Patches[0].Value)
			}
			// Existing wrapper reads: namespace, its owning Tenant, and namespace RuleStatus.
			// The mutation adds no reads or cluster-wide lists, even on a skipped profile.
			require.EqualValues(t, 3, reads.Load())
		})
	}
}
