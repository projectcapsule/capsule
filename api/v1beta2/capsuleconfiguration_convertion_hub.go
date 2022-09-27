// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"fmt"
	"strconv"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/conversion"

	capsulev1alpha1 "github.com/clastix/capsule/api/v1alpha1"
)

func (in *CapsuleConfiguration) ConvertTo(raw conversion.Hub) error {
	dst, ok := raw.(*capsulev1alpha1.CapsuleConfiguration)
	if !ok {
		return fmt.Errorf("expected type *capsulev1alpha1.CapsuleConfiguration, got %T", dst)
	}

	dst.ObjectMeta = in.ObjectMeta
	dst.Spec.ProtectedNamespaceRegexpString = in.Spec.ProtectedNamespaceRegexpString
	dst.Spec.UserGroups = in.Spec.UserGroups
	dst.Spec.ProtectedNamespaceRegexpString = in.Spec.ProtectedNamespaceRegexpString

	annotations := dst.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}

	if in.Spec.NodeMetadata != nil {
		if len(in.Spec.NodeMetadata.ForbiddenLabels.Exact) > 0 {
			annotations[capsulev1alpha1.ForbiddenNodeLabelsAnnotation] = strings.Join(in.Spec.NodeMetadata.ForbiddenLabels.Exact, ",")
		}

		if len(in.Spec.NodeMetadata.ForbiddenLabels.Regex) > 0 {
			annotations[capsulev1alpha1.ForbiddenNodeLabelsRegexpAnnotation] = in.Spec.NodeMetadata.ForbiddenLabels.Regex
		}

		if len(in.Spec.NodeMetadata.ForbiddenAnnotations.Exact) > 0 {
			annotations[capsulev1alpha1.ForbiddenNodeAnnotationsAnnotation] = strings.Join(in.Spec.NodeMetadata.ForbiddenAnnotations.Exact, ",")
		}

		if len(in.Spec.NodeMetadata.ForbiddenAnnotations.Regex) > 0 {
			annotations[capsulev1alpha1.ForbiddenNodeAnnotationsRegexpAnnotation] = in.Spec.NodeMetadata.ForbiddenAnnotations.Regex
		}
	}

	annotations[capsulev1alpha1.EnableTLSConfigurationAnnotationName] = fmt.Sprintf("%t", in.Spec.EnableTLSReconciler)
	annotations[capsulev1alpha1.TLSSecretNameAnnotation] = in.Spec.CapsuleResources.TLSSecretName
	annotations[capsulev1alpha1.MutatingWebhookConfigurationName] = in.Spec.CapsuleResources.MutatingWebhookConfigurationName
	annotations[capsulev1alpha1.ValidatingWebhookConfigurationName] = in.Spec.CapsuleResources.ValidatingWebhookConfigurationName

	dst.SetAnnotations(annotations)

	return nil
}

func (in *CapsuleConfiguration) ConvertFrom(raw conversion.Hub) error {
	src, ok := raw.(*capsulev1alpha1.CapsuleConfiguration)
	if !ok {
		return fmt.Errorf("expected type *capsulev1alpha1.CapsuleConfiguration, got %T", src)
	}

	in.ObjectMeta = src.ObjectMeta
	in.Spec.ProtectedNamespaceRegexpString = src.Spec.ProtectedNamespaceRegexpString
	in.Spec.UserGroups = src.Spec.UserGroups
	in.Spec.ProtectedNamespaceRegexpString = src.Spec.ProtectedNamespaceRegexpString

	annotations := src.GetAnnotations()

	if value, found := annotations[capsulev1alpha1.ForbiddenNodeLabelsAnnotation]; found {
		if in.Spec.NodeMetadata == nil {
			in.Spec.NodeMetadata = &NodeMetadata{}
		}

		in.Spec.NodeMetadata.ForbiddenLabels.Exact = strings.Split(value, ",")

		delete(annotations, capsulev1alpha1.ForbiddenNodeLabelsAnnotation)
	}

	if value, found := annotations[capsulev1alpha1.ForbiddenNodeLabelsRegexpAnnotation]; found {
		if in.Spec.NodeMetadata == nil {
			in.Spec.NodeMetadata = &NodeMetadata{}
		}

		in.Spec.NodeMetadata.ForbiddenLabels.Regex = value

		delete(annotations, capsulev1alpha1.ForbiddenNodeLabelsRegexpAnnotation)
	}

	if value, found := annotations[capsulev1alpha1.ForbiddenNodeAnnotationsAnnotation]; found {
		if in.Spec.NodeMetadata == nil {
			in.Spec.NodeMetadata = &NodeMetadata{}
		}

		in.Spec.NodeMetadata.ForbiddenAnnotations.Exact = strings.Split(value, ",")

		delete(annotations, capsulev1alpha1.ForbiddenNodeAnnotationsAnnotation)
	}

	if value, found := annotations[capsulev1alpha1.ForbiddenNodeAnnotationsRegexpAnnotation]; found {
		if in.Spec.NodeMetadata == nil {
			in.Spec.NodeMetadata = &NodeMetadata{}
		}

		in.Spec.NodeMetadata.ForbiddenAnnotations.Regex = value

		delete(annotations, capsulev1alpha1.ForbiddenNodeAnnotationsRegexpAnnotation)
	}

	if value, found := annotations[capsulev1alpha1.EnableTLSConfigurationAnnotationName]; found {
		v, _ := strconv.ParseBool(value)

		in.Spec.EnableTLSReconciler = v

		delete(annotations, capsulev1alpha1.EnableTLSConfigurationAnnotationName)
	}

	if value, found := annotations[capsulev1alpha1.TLSSecretNameAnnotation]; found {
		in.Spec.CapsuleResources.TLSSecretName = value

		delete(annotations, capsulev1alpha1.TLSSecretNameAnnotation)
	}

	if value, found := annotations[capsulev1alpha1.MutatingWebhookConfigurationName]; found {
		in.Spec.CapsuleResources.MutatingWebhookConfigurationName = value

		delete(annotations, capsulev1alpha1.MutatingWebhookConfigurationName)
	}

	if value, found := annotations[capsulev1alpha1.ValidatingWebhookConfigurationName]; found {
		in.Spec.CapsuleResources.ValidatingWebhookConfigurationName = value

		delete(annotations, capsulev1alpha1.ValidatingWebhookConfigurationName)
	}

	in.SetAnnotations(annotations)

	return nil
}
