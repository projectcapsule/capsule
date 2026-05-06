// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package invalidator

import (
	"context"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/indexers/tenantresource"
)

func (r *CacheInvalidator) rebuildImpersonationCache(
	ctx context.Context,
	log logr.Logger,
) error {
	var referencedServiceAccounts []meta.NamespacedRFC1123ObjectReferenceWithNamespace

	seen := make(map[string]struct{})

	var gtr capsulev1beta2.GlobalTenantResourceList
	if err := r.List(ctx, &gtr); err != nil {
		return err
	}

	for _, item := range gtr.Items {
		saName := item.Status.ServiceAccount.Name
		saNamespace := item.Status.ServiceAccount.Namespace

		key := saNamespace.String() + "/" + saName.String()
		if _, ok := seen[key]; ok {
			continue
		}

		sa := &corev1.ServiceAccount{}
		if err := r.Get(ctx, types.NamespacedName{
			Namespace: saNamespace.String(),
			Name:      saName.String(),
		}, sa); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}

			return err
		}

		seen[key] = struct{}{}

		referencedServiceAccounts = append(referencedServiceAccounts, meta.NamespacedRFC1123ObjectReferenceWithNamespace{
			Name:      meta.RFC1123Name(sa.Name),
			Namespace: meta.RFC1123SubdomainName(sa.Namespace),
		})
	}

	var ntr capsulev1beta2.TenantResourceList
	if err := r.List(ctx, &ntr); err != nil {
		return err
	}

	for _, item := range ntr.Items {
		saName := item.Status.ServiceAccount.Name
		saNamespace := item.Status.ServiceAccount.Namespace

		key := string(saNamespace) + "/" + string(saName)
		if _, ok := seen[key]; ok {
			continue
		}

		sa := &corev1.ServiceAccount{}
		if err := r.Get(ctx, types.NamespacedName{
			Namespace: saNamespace.String(),
			Name:      saName.String(),
		}, sa); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}

			return err
		}

		seen[key] = struct{}{}

		referencedServiceAccounts = append(referencedServiceAccounts, meta.NamespacedRFC1123ObjectReferenceWithNamespace{
			Name:      meta.RFC1123Name(sa.Name),
			Namespace: meta.RFC1123SubdomainName(sa.Namespace),
		})
	}

	log.V(5).Info("rebuilding impersonation cache",
		"serviceAccounts", len(referencedServiceAccounts),
		"cacheBefore", r.ImpersonationCache.Stats(),
	)

	r.ImpersonationCache.Reset()

	re, err := r.Configuration.ServiceAccountClient(ctx)
	if err != nil {
		log.Error(err, "failed to load impersonated rest client")

		return err
	}

	for _, sa := range referencedServiceAccounts {
		if _, err := r.ImpersonationCache.LoadOrCreate(
			ctx,
			log,
			re,
			r.Scheme(),
			sa,
		); err != nil {
			return err
		}
	}

	log.V(5).Info("rebuilt impersonation cache",
		"serviceAccounts", len(referencedServiceAccounts),
		"cacheAfter", r.ImpersonationCache.Stats(),
	)

	return nil
}

func (r *CacheInvalidator) invalidateServiceAccount(
	ctx context.Context,
	sa *corev1.ServiceAccount,
) error {
	hasReference, err := r.checkServiceAccountReferences(ctx, sa)
	if err != nil {
		return err
	}

	if !hasReference {
		r.Log.V(4).Info("invalidating cache for serviceaccount cache", "name", sa.GetNamespace(), "namespace", sa.GetName())

		r.ImpersonationCache.Invalidate(sa.GetNamespace(), sa.GetName())
	}

	return nil
}

func (r *CacheInvalidator) checkServiceAccountReferences(
	ctx context.Context,
	sa *corev1.ServiceAccount,
) (ref bool, err error) {
	key := sa.GetNamespace() + "/" + sa.GetName()

	var gtr capsulev1beta2.GlobalTenantResourceList
	if err := r.List(
		ctx,
		&gtr,
		client.MatchingFields{tenantresource.ServiceAccountIndexerFieldName: key},
	); err != nil {
		return false, err
	}

	if len(gtr.Items) > 0 {
		return true, nil
	}

	var ntr capsulev1beta2.TenantResourceList
	if err := r.List(
		ctx,
		&ntr,
		client.MatchingFields{tenantresource.ServiceAccountIndexerFieldName: key},
	); err != nil {
		return false, err
	}

	if len(ntr.Items) > 0 {
		return true, nil
	}

	return false, nil
}
