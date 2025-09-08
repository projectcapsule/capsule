// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"
	"maps"

	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsule "github.com/projectcapsule/capsule/controllers/tenant"
	"github.com/projectcapsule/capsule/pkg/api"
)

type DeletedMetadataKeys struct {
	annotations []string
	labels      []string
}

func findDeletedStaticMetadataKeys(deletedMetadataKey *DeletedMetadataKeys, oldTenant *capsulev1beta2.Tenant, newTenant *capsulev1beta2.Tenant) {
	if oldIc := oldTenant.Spec.IngressOptions.AllowedClasses; oldIc != nil {
		if ic := newTenant.Spec.IngressOptions.AllowedClasses; ic == nil || len(ic.Exact) == 0 {
			deletedMetadataKey.annotations = append(deletedMetadataKey.annotations, capsule.AvailableIngressClassesAnnotation)
		}

		if ic := newTenant.Spec.IngressOptions.AllowedClasses; ic == nil || len(ic.Regex) == 0 {
			deletedMetadataKey.annotations = append(deletedMetadataKey.annotations, capsule.AvailableIngressClassesRegexpAnnotation)
		}
	}

	if oldSc := oldTenant.Spec.StorageClasses; oldSc != nil {
		if sc := newTenant.Spec.StorageClasses; sc == nil || len(sc.Exact) == 0 {
			deletedMetadataKey.annotations = append(deletedMetadataKey.annotations, capsule.AvailableStorageClassesAnnotation)
		}

		if sc := newTenant.Spec.StorageClasses; sc == nil || len(sc.Regex) == 0 {
			deletedMetadataKey.annotations = append(deletedMetadataKey.annotations, capsule.AvailableStorageClassesRegexpAnnotation)
		}
	}

	if oldCr := oldTenant.Spec.ContainerRegistries; oldCr != nil {
		if cr := newTenant.Spec.ContainerRegistries; cr == nil || len(cr.Exact) == 0 {
			deletedMetadataKey.annotations = append(deletedMetadataKey.annotations, capsule.AllowedRegistriesAnnotation)
		}

		if cr := newTenant.Spec.ContainerRegistries; cr == nil || len(cr.Regex) == 0 {
			deletedMetadataKey.annotations = append(deletedMetadataKey.annotations, capsule.AllowedRegistriesRegexpAnnotation)
		}
	}

	for _, key := range []string{
		api.ForbiddenNamespaceLabelsAnnotation,
		api.ForbiddenNamespaceLabelsRegexpAnnotation,
		api.ForbiddenNamespaceAnnotationsAnnotation,
		api.ForbiddenNamespaceAnnotationsRegexpAnnotation,
	} {
		if _, ok := newTenant.Annotations[key]; !ok {
			if _, exist := oldTenant.Annotations[key]; !exist {
				continue
			}

			deletedMetadataKey.annotations = append(deletedMetadataKey.annotations, key)
		}
	}
}

func findDeletedMetadataListKeys(deletedMetadataKey *DeletedMetadataKeys, oldTenant *capsulev1beta2.Tenant, newTenant *capsulev1beta2.Tenant) {
	allOldAnnotations := map[string]string{}
	allOldLabels := map[string]string{}
	allNewAnnotations := map[string]string{}
	AllNewLabels := map[string]string{}

	if opts := oldTenant.Spec.NamespaceOptions; opts != nil && len(opts.AdditionalMetadataList) > 0 {
		for _, v := range oldTenant.Spec.NamespaceOptions.AdditionalMetadataList {
			maps.Copy(allOldAnnotations, v.Annotations)
			maps.Copy(allOldLabels, v.Labels)
		}
	}

	if opts := newTenant.Spec.NamespaceOptions; opts != nil && len(opts.AdditionalMetadataList) > 0 {
		for _, v := range newTenant.Spec.NamespaceOptions.AdditionalMetadataList {
			maps.Copy(allNewAnnotations, v.Annotations)
			maps.Copy(AllNewLabels, v.Labels)
		}
	}

	if !maps.Equal(allOldAnnotations, allNewAnnotations) {
		for key := range allOldAnnotations {
			if _, ok := allNewAnnotations[key]; !ok {
				deletedMetadataKey.annotations = append(deletedMetadataKey.annotations, key)
			}
		}

		for key := range allOldLabels {
			if _, ok := AllNewLabels[key]; !ok {
				deletedMetadataKey.labels = append(deletedMetadataKey.labels, key)
			}
		}
	}
}

// FindDeletedMetadataKeys returns the deleted metadata keys.
func FindDeletedMetadataKeys(oldTenant *capsulev1beta2.Tenant, newTenant *capsulev1beta2.Tenant) (deletedMetadata *DeletedMetadataKeys) {
	deletedMetadata = &DeletedMetadataKeys{}
	findDeletedStaticMetadataKeys(deletedMetadata, oldTenant, newTenant)
	findDeletedMetadataListKeys(deletedMetadata, oldTenant, newTenant)

	return deletedMetadata
}

// StoreObsoleteMetadata Saves the deleted metadata in the Tenant status.
func StoreObsoleteMetadata(client client.Client, ctx context.Context, oldTenant *capsulev1beta2.Tenant, newTenant *capsulev1beta2.Tenant) (err error) {
	err = retry.RetryOnConflict(retry.DefaultBackoff, func() (conflictErr error) {
		deletedMetadataKeys := FindDeletedMetadataKeys(oldTenant, newTenant)
		if deletedMetadataKeys.annotations == nil && deletedMetadataKeys.labels == nil {
			return nil
		}

		newTenant.Status.ObsoleteMetadata.Annotations = deletedMetadataKeys.annotations
		newTenant.Status.ObsoleteMetadata.Labels = deletedMetadataKeys.labels

		return client.Status().Update(ctx, newTenant)
	})

	return err
}
