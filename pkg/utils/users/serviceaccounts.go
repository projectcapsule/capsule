// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package users

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apiserver/pkg/authentication/serviceaccount"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/configuration"
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
	if err = c.List(ctx, tntList, client.MatchingFieldsSelector{
		Selector: fields.OneTermEqualSelector(".status.namespaces", namespace),
	}); err != nil {
		return tnt, err
	}

	if len(tntList.Items) > 0 {
		tnt = &tntList.Items[0]
	}

	return tnt, err
}
