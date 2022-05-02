package v1beta1

import "strings"

type NamespaceOptions struct {
	//+kubebuilder:validation:Minimum=1
	// Specifies the maximum number of namespaces allowed for that Tenant. Once the namespace quota assigned to the Tenant has been reached, the Tenant owner cannot create further namespaces. Optional.
	Quota *int32 `json:"quota,omitempty"`
	// Specifies additional labels and annotations the Capsule operator places on any Namespace resource in the Tenant. Optional.
	AdditionalMetadata *AdditionalMetadataSpec `json:"additionalMetadata,omitempty"`
}

func (t *Tenant) hasForbiddenNamespaceLabelsAnnotations() bool {
	if _, ok := t.Annotations[ForbiddenNamespaceLabelsAnnotation]; ok {
		return true
	}

	if _, ok := t.Annotations[ForbiddenNamespaceLabelsRegexpAnnotation]; ok {
		return true
	}

	return false
}

func (t *Tenant) hasForbiddenNamespaceAnnotationsAnnotations() bool {
	if _, ok := t.Annotations[ForbiddenNamespaceAnnotationsAnnotation]; ok {
		return true
	}

	if _, ok := t.Annotations[ForbiddenNamespaceAnnotationsRegexpAnnotation]; ok {
		return true
	}

	return false
}

func (t *Tenant) ForbiddenUserNamespaceLabels() *ForbiddenListSpec {
	if !t.hasForbiddenNamespaceLabelsAnnotations() {
		return nil
	}

	return &ForbiddenListSpec{
		Exact: strings.Split(t.Annotations[ForbiddenNamespaceLabelsAnnotation], ","),
		Regex: t.Annotations[ForbiddenNamespaceLabelsRegexpAnnotation],
	}
}

func (t *Tenant) ForbiddenUserNamespaceAnnotations() *ForbiddenListSpec {
	if !t.hasForbiddenNamespaceAnnotationsAnnotations() {
		return nil
	}

	return &ForbiddenListSpec{
		Exact: strings.Split(t.Annotations[ForbiddenNamespaceAnnotationsAnnotation], ","),
		Regex: t.Annotations[ForbiddenNamespaceAnnotationsRegexpAnnotation],
	}
}
