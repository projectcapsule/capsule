package utils

import (
	"maps"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
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
		for key, _ := range allOldAnnotations {
			if _, ok := allNewAnnotations[key]; !ok {
				deletedAnnotations = append(deletedAnnotations, key)
			}
		}
		for key, _ := range allOldLabels {
			if _, ok := AllNewLabels[key]; !ok {
				deletedLabels = append(deletedLabels, key)
			}
		}
	}
	return deletedAnnotations, deletedLabels
}
