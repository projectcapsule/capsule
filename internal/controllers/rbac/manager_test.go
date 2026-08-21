// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rbac

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
)

func TestEnsureClusterRoleBindingsProvisionerUsesAuthoritativePromotionState(t *testing.T) {
	t.Parallel()

	const (
		configurationName = "capsule"
		provisionerRole   = "capsule-namespace-provisioner"
		namespace         = "tenant-a"
		serviceAccount    = "builder"
	)

	ctx := context.Background()
	scheme := runtime.NewScheme()
	for _, addToScheme := range []func(*runtime.Scheme) error{
		corev1.AddToScheme,
		rbacv1.AddToScheme,
		capsulev1beta2.AddToScheme,
	} {
		if err := addToScheme(scheme); err != nil {
			t.Fatal(err)
		}
	}

	configurationObject := &capsulev1beta2.CapsuleConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: configurationName},
		Spec: capsulev1beta2.CapsuleConfigurationSpec{
			AllowServiceAccountPromotion: true,
			RBAC: &capsulev1beta2.RBACConfiguration{
				ProvisionerClusterRole: provisionerRole,
			},
		},
	}
	promoted := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceAccount,
			Namespace: namespace,
			Labels: map[string]string{
				meta.OwnerPromotionLabel: meta.ValueTrue,
			},
		},
	}

	// The cached client deliberately remains stale after demotion, reproducing
	// the ordering between the metadata-only watch and full-object cache.
	cachedClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(configurationObject, promoted.DeepCopy()).
		Build()
	authoritativeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(promoted.DeepCopy()).
		Build()
	cfg := configuration.NewCapsuleConfiguration(
		ctx,
		cachedClient,
		cachedClient,
		nil,
		configurationName,
	)
	manager := &Manager{
		Log:           logr.Discard(),
		Client:        cachedClient,
		Configuration: cfg,
		reader:        authoritativeClient,
	}

	if err := manager.EnsureClusterRoleBindingsProvisioner(ctx); err != nil {
		t.Fatalf("initial provisioner binding reconciliation: %v", err)
	}
	assertServiceAccountSubject(t, cachedClient, provisionerRole, namespace, serviceAccount, true)

	latest := &corev1.ServiceAccount{}
	if err := authoritativeClient.Get(ctx, client.ObjectKeyFromObject(promoted), latest); err != nil {
		t.Fatalf("get authoritative ServiceAccount: %v", err)
	}
	latest.Labels[meta.OwnerPromotionLabel] = "false"
	if err := authoritativeClient.Update(ctx, latest); err != nil {
		t.Fatalf("demote authoritative ServiceAccount: %v", err)
	}

	if err := manager.EnsureClusterRoleBindingsProvisioner(ctx); err != nil {
		t.Fatalf("demotion provisioner binding reconciliation: %v", err)
	}
	assertServiceAccountSubject(t, cachedClient, provisionerRole, namespace, serviceAccount, false)
}

func assertServiceAccountSubject(
	t *testing.T,
	kubeClient client.Client,
	bindingName string,
	namespace string,
	serviceAccount string,
	want bool,
) {
	t.Helper()

	binding := &rbacv1.ClusterRoleBinding{}
	if err := kubeClient.Get(context.Background(), client.ObjectKey{Name: bindingName}, binding); err != nil {
		t.Fatalf("get ClusterRoleBinding: %v", err)
	}

	found := false
	for _, subject := range binding.Subjects {
		if subject.Kind == rbacv1.ServiceAccountKind &&
			subject.Namespace == namespace &&
			subject.Name == serviceAccount {
			found = true
			break
		}
	}

	if found != want {
		t.Fatalf("ServiceAccount subject presence = %t, want %t; subjects: %#v", found, want, binding.Subjects)
	}
}
