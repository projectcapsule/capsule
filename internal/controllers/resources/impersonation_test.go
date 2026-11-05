// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
)

type replicationTestConfiguration struct {
	configuration.Configuration
}

func (replicationTestConfiguration) ServiceAccountClient(context.Context) (*rest.Config, error) {
	return &rest.Config{Host: "https://unused.example"}, nil
}

func TestReplicationTargetReadsPreserveImpersonation(t *testing.T) {
	controllerReads, impersonatedReads := 0, 0
	base := fake.NewClientBuilder().Build()
	controller := interceptor.NewClient(base, interceptor.Funcs{Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
		controllerReads++
		return c.Get(ctx, key, obj, opts...)
	}})
	denial := apierrors.NewForbidden(corev1.Resource("configmaps"), "target", nil)
	restricted := interceptor.NewClient(base, interceptor.Funcs{Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
		impersonatedReads++
		return denial
	}})
	sa := meta.NamespacedRFC1123ObjectReferenceWithNamespace{Name: "replicator", Namespace: "tenant-a"}
	identities := cache.NewImpersonationCache()
	identities.Set(sa.Namespace.String(), sa.Name.String(), restricted)
	loader := impersonatedClientLoader[*capsulev1beta2.GlobalTenantResource]{
		client: replicationClient{Client: controller, reader: controller}, configuration: replicationTestConfiguration{}, impersonation: identities,
		resolve: func(configuration.Configuration, logr.Logger, *capsulev1beta2.GlobalTenantResource) *meta.NamespacedRFC1123ObjectReferenceWithNamespace {
			return &sa
		},
	}
	loaded, identity, err := loader.Load(t.Context(), logr.Discard(), &capsulev1beta2.GlobalTenantResource{})
	require.NoError(t, err)
	require.Equal(t, &sa, identity)
	err = loaded.Get(t.Context(), client.ObjectKey{Namespace: "tenant-a", Name: "target"}, &corev1.ConfigMap{})
	require.ErrorIs(t, err, denial)
	require.Equal(t, 1, impersonatedReads)
	require.Zero(t, controllerReads, "must not read a denied target through the controller identity")
}
