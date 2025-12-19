// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"maps"
	"strings"

	corev1 "k8s.io/api/core/v1"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/template"
	"github.com/projectcapsule/capsule/pkg/utils"
)

func AddNamespaceNameLabels(labels map[string]string, ns *corev1.Namespace) {
	labels["kubernetes.io/metadata.name"] = ns.GetName()
}

func AddTenantNameLabel(labels map[string]string, ns *corev1.Namespace, tnt *capsulev1beta2.Tenant) {
	labels[meta.TenantLabel] = tnt.GetName()
}

func BuildInstanceMetadataForNamespace(ns *corev1.Namespace, tnt *capsulev1beta2.Tenant) (labels map[string]string, annotations map[string]string) {
	annotations = make(map[string]string)
	labels = make(map[string]string)

	instance := tnt.Status.GetInstance(&capsulev1beta2.TenantStatusNamespaceItem{
		Name: ns.GetName(),
		UID:  ns.GetUID(),
	})

	if instance == nil {
		return labels, annotations
	}

	annotations = instance.Metadata.Annotations
	labels = instance.Metadata.Labels

	return labels, annotations
}

func BuildNamespaceMetadataForTenant(ns *corev1.Namespace, tnt *capsulev1beta2.Tenant) (labels map[string]string, annotations map[string]string, err error) {
	annotations = BuildNamespaceAnnotationsForTenant(tnt)
	labels = BuildNamespaceLabelsForTenant(tnt)

	if opts := tnt.Spec.NamespaceOptions; opts != nil && len(opts.AdditionalMetadataList) > 0 {
		for _, md := range opts.AdditionalMetadataList {
			var ok bool

			ok, err = utils.IsNamespaceSelectedBySelector(ns, md.NamespaceSelector)
			if err != nil {
				return nil, nil, err
			}

			if !ok {
				continue
			}

			tLabels := template.TemplateForTenantAndNamespaceMap(md.Labels, tnt, ns)
			tAnnotations := template.TemplateForTenantAndNamespaceMap(md.Annotations, tnt, ns)

			utils.MapMergeNoOverrite(labels, tLabels)
			utils.MapMergeNoOverrite(annotations, tAnnotations)
		}
	}

	return labels, annotations, nil
}

func BuildNamespaceAnnotationsForTenant(tnt *capsulev1beta2.Tenant) map[string]string {
	annotations := make(map[string]string)

	//nolint:staticcheck
	if md := tnt.Spec.NamespaceOptions; md != nil && md.AdditionalMetadata != nil {
		maps.Copy(annotations, md.AdditionalMetadata.Annotations)
	}

	if tnt.Spec.NodeSelector != nil {
		annotations = utils.BuildNodeSelector(tnt, annotations)
	}

	if ic := tnt.Spec.IngressOptions.AllowedClasses; ic != nil {
		if len(ic.Exact) > 0 {
			annotations[meta.AvailableIngressClassesAnnotation] = strings.Join(ic.Exact, ",")
		}

		//nolint:staticcheck
		if len(ic.Regex) > 0 {
			annotations[meta.AvailableIngressClassesRegexpAnnotation] = ic.Regex
		}
	}

	if sc := tnt.Spec.StorageClasses; sc != nil {
		if len(sc.Exact) > 0 {
			annotations[meta.AvailableStorageClassesAnnotation] = strings.Join(sc.Exact, ",")
		}

		//nolint:staticcheck
		if len(sc.Regex) > 0 {
			annotations[meta.AvailableStorageClassesRegexpAnnotation] = sc.Regex
		}
	}

	if cr := tnt.Spec.ContainerRegistries; cr != nil {
		if len(cr.Exact) > 0 {
			annotations[meta.AllowedRegistriesAnnotation] = strings.Join(cr.Exact, ",")
		}

		//nolint:staticcheck
		if len(cr.Regex) > 0 {
			annotations[meta.AllowedRegistriesRegexpAnnotation] = cr.Regex
		}
	}

	for _, key := range []string{
		meta.ForbiddenNamespaceLabelsAnnotation,
		meta.ForbiddenNamespaceLabelsRegexpAnnotation,
		meta.ForbiddenNamespaceAnnotationsAnnotation,
		meta.ForbiddenNamespaceAnnotationsRegexpAnnotation,
	} {
		if value, ok := tnt.Annotations[key]; ok {
			annotations[key] = value
		}
	}

	return annotations
}

func BuildNamespaceLabelsForTenant(tnt *capsulev1beta2.Tenant) map[string]string {
	labels := make(map[string]string)

	//nolint:staticcheck
	if md := tnt.Spec.NamespaceOptions; md != nil && md.AdditionalMetadata != nil {
		maps.Copy(labels, md.AdditionalMetadata.Labels)
	}

	if tnt.Spec.Cordoned {
		labels[meta.CordonedLabel] = "true"
	}

	return labels
}
