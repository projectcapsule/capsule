package utils

import (
	"maps"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsule "github.com/projectcapsule/capsule/controllers/tenant"
	"github.com/projectcapsule/capsule/pkg/api"
)

func FindDeletedMetadataKeys(oldTenant *capsulev1beta2.Tenant, newTenant *capsulev1beta2.Tenant) (deletedAnnotations []string, deletedLabels []string) {
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
				deletedAnnotations = append(deletedAnnotations, key)
			}
		}
		for key := range allOldLabels {
			if _, ok := AllNewLabels[key]; !ok {
				deletedLabels = append(deletedLabels, key)
			}
		}
	}

	if ic := newTenant.Spec.IngressOptions.AllowedClasses; ic == nil || len(ic.Exact) < 0 {
		deletedAnnotations = append(deletedAnnotations, capsule.AvailableIngressClassesAnnotation)
	}
	if ic := newTenant.Spec.IngressOptions.AllowedClasses; ic == nil || len(ic.Regex) < 0 {
		deletedAnnotations = append(deletedAnnotations, capsule.AvailableIngressClassesRegexpAnnotation)
	}

	if sc := newTenant.Spec.StorageClasses; sc == nil || len(sc.Exact) < 0 {
		deletedAnnotations = append(deletedAnnotations, capsule.AvailableStorageClassesAnnotation)
	}
	if sc := newTenant.Spec.StorageClasses; sc == nil || len(sc.Regex) < 0 {
		deletedAnnotations = append(deletedAnnotations, capsule.AvailableStorageClassesRegexpAnnotation)
	}

	if cr := newTenant.Spec.ContainerRegistries; cr == nil || len(cr.Exact) < 0 {
		deletedAnnotations = append(deletedAnnotations, capsule.AllowedRegistriesAnnotation)
	}
	if cr := newTenant.Spec.ContainerRegistries; cr == nil || len(cr.Regex) < 0 {
		deletedAnnotations = append(deletedAnnotations, capsule.AllowedRegistriesRegexpAnnotation)
	}

	for _, key := range []string{
		api.ForbiddenNamespaceLabelsAnnotation,
		api.ForbiddenNamespaceLabelsRegexpAnnotation,
		api.ForbiddenNamespaceAnnotationsAnnotation,
		api.ForbiddenNamespaceAnnotationsRegexpAnnotation,
	} {
		if _, ok := newTenant.Annotations[key]; !ok {
			deletedAnnotations = append(deletedAnnotations, key)
		}
	}
	return deletedAnnotations, deletedLabels
}
