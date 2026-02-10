// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package users

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/authentication/serviceaccount"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
)

// This function resolves the tenant based on the serviceaccount given via username
// if a serviceaccount is in a tenant namespace they will return the tenant.
func ResolveServiceAccountActor(
	ctx context.Context,
	c client.Client,
	ns *corev1.Namespace,
	username string,
	cfg configuration.Configuration,
) (tnt *capsulev1beta2.Tenant, err error) {
	namespace, name, err := serviceaccount.SplitUsername(username)
	if err != nil {
		return nil, err
	}

	sa := &corev1.ServiceAccount{}
	if err = c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, sa); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}

		return tnt, err
	}

	if meta.OwnerPromotionLabelTriggers(ns) {
		return tnt, err
	}

	tntList := &capsulev1beta2.TenantList{}
	if err = c.List(ctx, tntList, client.MatchingFields{".status.namespaces": namespace}); err != nil {
		return tnt, err
	}

	if len(tntList.Items) > 0 {
		tnt = &tntList.Items[0]
	}

	return tnt, err
}

// GetServiceAccountFullName return the full qualified name for the serviceaccount
func GetServiceAccountFullName(ref meta.NamespacedRFC1123ObjectReferenceWithNamespace) string {
	return fmt.Sprintf("%s%s:%s", serviceaccount.ServiceAccountUsernamePrefix, ref.Namespace, ref.Name)
}

// GetServiceAccountGroups returns all groups associated with a ServiceAccount
func GetServiceAccountGroups(namespace string) []string {
	return []string{
		fmt.Sprintf("%s%s", serviceaccount.ServiceAccountGroupPrefix, namespace),
		serviceaccount.AllServiceAccountsGroup,
		user.AllAuthenticated,
	}
}

// ImpersonatedKubernetesClientForServiceAccount returns a controller-runtime client.Client that impersonates a given ServiceAccount.
func ImpersonatedKubernetesClientForServiceAccount(
	base *rest.Config,
	scheme *runtime.Scheme,
	reference meta.NamespacedRFC1123ObjectReferenceWithNamespace,
) (client.Client, error) {
	imp := rest.CopyConfig(base)
	imp.Impersonate = rest.ImpersonationConfig{
		UserName: GetServiceAccountFullName(reference),
	}

	k8sClient, err := client.New(imp, client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("failed to create impersonated client: %w", err)
	}
	return k8sClient, nil
}
