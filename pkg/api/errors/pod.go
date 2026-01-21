// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package errors

import (
	"fmt"
	"strings"

	"github.com/projectcapsule/capsule/internal/webhook/utils"
	"github.com/projectcapsule/capsule/pkg/api"
)

type NoPodMetadataError struct {
	objectName string
}

func NewNoPodMetadata(objectName string) error {
	return &NoPodMetadataError{objectName: objectName}
}

func (n NoPodMetadataError) Error() string {
	return fmt.Sprintf("Skipping labels sync for %s because no AdditionalLabels or AdditionalAnnotations presents in Tenant spec", n.objectName)
}

type missingContainerRegistryError struct {
	fqci string
}

func (m missingContainerRegistryError) Error() string {
	return fmt.Sprintf("container image %s is missing repository, please, use a fully qualified container image name", m.fqci)
}

func NewMissingContainerRegistryError(image string) error {
	return &missingContainerRegistryError{fqci: image}
}

type RegistryClassForbiddenError struct {
	fqci string
	spec api.AllowedListSpec
}

func NewContainerRegistryForbidden(image string, spec api.AllowedListSpec) error {
	return &RegistryClassForbiddenError{
		fqci: image,
		spec: spec,
	}
}

func (f RegistryClassForbiddenError) Error() (err string) {
	err = fmt.Sprintf("Container image %s registry is forbidden for the current Tenant: ", f.fqci)

	var extra []string

	if len(f.spec.Exact) > 0 {
		extra = append(extra, fmt.Sprintf("use one from the following list (%s)", strings.Join(f.spec.Exact, ", ")))
	}

	//nolint:staticcheck
	if len(f.spec.Regex) > 0 {
		extra = append(extra, fmt.Sprintf(" use one matching the following regex (%s)", f.spec.Regex))
	}

	err += strings.Join(extra, " or ")

	return err
}

type ImagePullPolicyForbiddenError struct {
	usedPullPolicy      string
	allowedPullPolicies []string
	containerName       string
}

func NewImagePullPolicyForbidden(usedPullPolicy, containerName string, allowedPullPolicies []string) error {
	return &ImagePullPolicyForbiddenError{
		usedPullPolicy:      usedPullPolicy,
		containerName:       containerName,
		allowedPullPolicies: allowedPullPolicies,
	}
}

func (f ImagePullPolicyForbiddenError) Error() (err string) {
	return fmt.Sprintf("ImagePullPolicy %s for container %s is forbidden, use one of the followings: %s", f.usedPullPolicy, f.containerName, strings.Join(f.allowedPullPolicies, ", "))
}

type PodPriorityClassForbiddenError struct {
	priorityClassName string
	spec              api.DefaultAllowedListSpec
}

func NewPodPriorityClassForbidden(priorityClassName string, spec api.DefaultAllowedListSpec) error {
	return &PodPriorityClassForbiddenError{
		priorityClassName: priorityClassName,
		spec:              spec,
	}
}

func (f PodPriorityClassForbiddenError) Error() (err string) {
	msg := fmt.Sprintf("Pod Priority Class %s is forbidden for the current Tenant: ", f.priorityClassName)

	return utils.DefaultAllowedValuesErrorMessage(f.spec, msg)
}

type PodRuntimeClassForbiddenError struct {
	runtimeClassName string
	spec             api.DefaultAllowedListSpec
}

func NewPodRuntimeClassForbidden(runtimeClassName string, spec api.DefaultAllowedListSpec) error {
	return &PodRuntimeClassForbiddenError{
		runtimeClassName: runtimeClassName,
		spec:             spec,
	}
}

func (f PodRuntimeClassForbiddenError) Error() (err string) {
	err = fmt.Sprintf("Pod Runtime Class %s is forbidden for the current Tenant: ", f.runtimeClassName)

	return utils.DefaultAllowedValuesErrorMessage(f.spec, err)
}
