// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package meta

import (
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ManagedMetadata struct {
	labels             map[string]struct{}
	annotations        map[string]struct{}
	annotationPrefixes []string
}

func NewManagedMetadata(
	labels []string,
	annotations []string,
) ManagedMetadata {
	m := ManagedMetadata{
		labels: stringSet(
			ResourcesLabel,
			TenantNameLabel,
			TenantLabel,
			NewTenantLabel,
			ResourcePoolLabel,
			FreezeLabel,
			OwnerPromotionLabel,
			ServiceAccountPromotionLabel,
			CordonedLabel,
			CapsuleNameLabel,
			CreatedByCapsuleLabel,
			CustomResourcesLabel,
			NewManagedByCapsuleLabel,
			ManagedByCapsuleLabel,
			LimitRangeLabel,
			NetworkPolicyLabel,
			ResourceQuotaLabel,
			RolebindingLabel,
		),
		annotations: stringSet(
			ReleaseAnnotation,
			ReconcileAnnotation,
			AvailableIngressClassesAnnotation,
			AvailableIngressClassesRegexpAnnotation,
			AvailableStorageClassesAnnotation,
			AvailableStorageClassesRegexpAnnotation,
			AllowedRegistriesAnnotation,
			AllowedRegistriesRegexpAnnotation,
			ForbiddenNamespaceLabelsAnnotation,
			ForbiddenNamespaceLabelsRegexpAnnotation,
			ForbiddenNamespaceAnnotationsAnnotation,
			ForbiddenNamespaceAnnotationsRegexpAnnotation,
			ProtectedTenantAnnotation,
		),
		annotationPrefixes: compactStrings(
			ResourceQuotaAnnotationPrefix,
			ResourceUsedAnnotationPrefix,
		),
	}

	m.addLabels(labels...)
	m.addAnnotations(annotations...)

	return m
}

func (m ManagedMetadata) HasLabel(key string) bool {
	_, ok := m.labels[key]

	return ok
}

func (m ManagedMetadata) HasAnnotation(key string) bool {
	if _, ok := m.annotations[key]; ok {
		return true
	}

	for _, prefix := range m.annotationPrefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}

	return false
}

func (m ManagedMetadata) addLabels(values ...string) {
	addStrings(m.labels, values...)
}

func (m ManagedMetadata) addAnnotations(values ...string) {
	addStrings(m.annotations, values...)
}

func addStrings(set map[string]struct{}, values ...string) {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}

		set[value] = struct{}{}
	}
}

func stringSet(values ...string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}

	out := make(map[string]struct{}, len(values))

	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}

		out[value] = struct{}{}
	}

	return out
}

func compactStrings(values ...string) []string {
	if len(values) == 0 {
		return nil
	}

	out := make([]string, 0, len(values))

	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}

		out = append(out, value)
	}

	return out
}

type ObjectSkipRule struct {
	// Labels with values which indicate a skip condition.
	Labels map[string]string

	// Annotations with values which indicate a skip condition.
	Annotations map[string]string
}

func (s *ObjectSkipRule) ShouldSkip(
	labels map[string]string,
	annotations map[string]string,
) bool {
	if s == nil {
		return false
	}

	for key, expected := range s.Labels {
		value, ok := labels[key]
		if !ok || value != expected {
			return false
		}
	}

	for key, expected := range s.Annotations {
		value, ok := annotations[key]
		if !ok || value != expected {
			return false
		}
	}

	return len(s.Labels) > 0 || len(s.Annotations) > 0
}

func DefaultObjectSkipRules() []ObjectSkipRule {
	return []ObjectSkipRule{
		{
			Labels: map[string]string{
				NewManagedByCapsuleLabel: ValueController,
			},
		},
		{
			Labels: map[string]string{
				NewManagedByCapsuleLabel: ValueControllerResources,
			},
		},
	}
}

func ShouldSkipObjectByRules(
	obj metav1.Object,
	rules []ObjectSkipRule,
) bool {
	if obj == nil || len(rules) == 0 {
		return false
	}

	labels := obj.GetLabels()
	annotations := obj.GetAnnotations()

	for _, rule := range rules {
		if rule.ShouldSkip(labels, annotations) {
			return true
		}
	}

	return false
}
