// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package breaktheglass

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	breaktheglassapi "github.com/projectcapsule/capsule/pkg/api/breaktheglass"
	apimeta "github.com/projectcapsule/capsule/pkg/api/meta"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
)

func TestRenderedResourceRows(t *testing.T) {
	t.Parallel()

	resource := apiruntime.RenderedResource{
		Policy: apiruntime.ResourceTemplatePolicy{
			Creation: apiruntime.ResourceCreationPolicyOwner,
			Protect:  ptr.To(true),
			Deletion: apiruntime.ResourceDeletionPolicyOrphan,
		},
		Targets: []runtime.RawExtension{
			{Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"first"}}`)},
			{Raw: []byte(`{"apiVersion":"v1","kind":"Secret","metadata":{"name":"second"}}`)},
		},
	}

	rows := renderedResourceRows(resource, false)
	require.Len(t, rows, 2)
	require.Len(t, rows[0], 2)
	require.Len(t, rows[1], 2)

	firstPolicy, ok := rows[0][0].(string)
	require.True(t, ok)
	secondPolicy, ok := rows[1][0].(string)
	require.True(t, ok)
	assert.Equal(t, firstPolicy, secondPolicy)
	assert.Contains(t, firstPolicy, "creation: Owner")
	assert.Contains(t, firstPolicy, "deletion: Orphan")
	assert.Contains(t, firstPolicy, "protect: true")

	firstManifest, ok := rows[0][1].(string)
	require.True(t, ok)
	secondManifest, ok := rows[1][1].(string)
	require.True(t, ok)
	assert.Contains(t, firstManifest, "kind: ConfigMap")
	assert.Contains(t, firstManifest, "name: first")
	assert.Contains(t, secondManifest, "kind: Secret")
	assert.Contains(t, secondManifest, "name: second")
}

func TestImpersonationOptionsApplyTo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		options    impersonationOptions
		config     rest.Config
		wantUser   string
		wantGroups []string
		wantError  string
	}{
		{
			name:       "preserves kubeconfig impersonation",
			config:     rest.Config{Impersonate: rest.ImpersonationConfig{UserName: "configured", Groups: []string{"configured-group"}}},
			wantUser:   "configured",
			wantGroups: []string{"configured-group"},
		},
		{
			name:       "command flags override kubeconfig impersonation",
			options:    impersonationOptions{User: "alice", Groups: []string{"developers", "on-call"}},
			config:     rest.Config{Impersonate: rest.ImpersonationConfig{UserName: "configured", Groups: []string{"configured-group"}}},
			wantUser:   "alice",
			wantGroups: []string{"developers", "on-call"},
		},
		{
			name:     "user flag clears kubeconfig impersonation groups",
			options:  impersonationOptions{User: "alice"},
			config:   rest.Config{Impersonate: rest.ImpersonationConfig{UserName: "configured", Groups: []string{"configured-group"}}},
			wantUser: "alice",
		},
		{
			name:       "group flags use kubeconfig impersonated user",
			options:    impersonationOptions{Groups: []string{"developers"}},
			config:     rest.Config{Impersonate: rest.ImpersonationConfig{UserName: "configured", Groups: []string{"configured-group"}}},
			wantUser:   "configured",
			wantGroups: []string{"developers"},
		},
		{
			name:       "groups require an impersonated user",
			options:    impersonationOptions{Groups: []string{"developers"}},
			wantGroups: []string{"developers"},
			wantError:  "--as-group requires --as or an impersonated user in the kubeconfig",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := tt.config
			err := tt.options.applyTo(&cfg)
			if tt.wantError != "" {
				require.EqualError(t, err, tt.wantError)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.wantUser, cfg.Impersonate.UserName)
			assert.Equal(t, tt.wantGroups, cfg.Impersonate.Groups)
		})
	}
}

func TestPatchBreakRequestStatusPreservesControllerManagedFields(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	managedResources := []apiruntime.RenderedResource{{
		Targets: []runtime.RawExtension{{Raw: []byte(`{
			"apiVersion":"v1",
			"kind":"ConfigMap",
			"metadata":{"name":"managed"}
		}`)}},
	}}
	serviceAccount := &apimeta.NamespacedRFC1123ObjectReferenceWithNamespace{
		Name:      "template-runner",
		Namespace: "operations",
	}
	approvals := &breaktheglassapi.ApprovalSpec{Conditions: []string{`requestor.name == "alice"`}}
	stored := &capsulev1beta2.BreakRequest{
		ObjectMeta: metav1.ObjectMeta{Name: "request", Namespace: "tenant"},
		Status: capsulev1beta2.BreakRequestStatus{
			Phase: capsulev1beta2.RequestPhaseActive,
			Request: &capsulev1beta2.BreakRequestStatusRequest{
				Impersonation: serviceAccount,
				Approvals:     approvals,
				Resources:     managedResources,
			},
		},
	}
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&capsulev1beta2.BreakRequest{}).
		WithObjects(stored).
		Build()

	// Simulate a CLI built against an older API which did not decode newer,
	// controller-owned status fields.
	partial := &capsulev1beta2.BreakRequest{}
	require.NoError(t, k8sClient.Get(ctx, ctrlclient.ObjectKeyFromObject(stored), partial))
	expectedManagedResources := partial.Status.Request.Resources
	expectedServiceAccount := partial.Status.Request.Impersonation
	expectedApprovals := partial.Status.Request.Approvals.DeepCopy()
	partial.Status.Request.Resources = nil
	partial.Status.Request.Impersonation = nil
	partial.Status.Request.Approvals = nil

	require.NoError(t, patchBreakRequestStatus(ctx, k8sClient, partial, func() error {
		partial.Status.Phase = capsulev1beta2.RequestPhaseExpired

		return nil
	}))

	current := &capsulev1beta2.BreakRequest{}
	require.NoError(t, k8sClient.Get(ctx, ctrlclient.ObjectKeyFromObject(stored), current))
	assert.Equal(t, capsulev1beta2.RequestPhaseExpired, current.Status.Phase)
	assert.Equal(t, expectedManagedResources, current.Status.Request.Resources)
	assert.Equal(t, expectedServiceAccount, current.Status.Request.Impersonation)
	assert.Equal(t, expectedApprovals, current.Status.Request.Approvals)
}
