// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package configuration

import (
	"context"
	"k8s.io/apimachinery/pkg/util/sets"
	"regexp"
	"sort"
	"strings"

	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1alpha1 "github.com/clastix/capsule/api/v1alpha1"
	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
)

// capsuleConfiguration is the Capsule Configuration retrieval mode
// using a closure that provides the desired configuration.
type capsuleConfiguration struct {
	retrievalFn func() *capsulev1alpha1.CapsuleConfiguration
}

func NewCapsuleConfiguration(ctx context.Context, client client.Client, name string) Configuration {
	return &capsuleConfiguration{retrievalFn: func() *capsulev1alpha1.CapsuleConfiguration {
		config := &capsulev1alpha1.CapsuleConfiguration{}

		if err := client.Get(ctx, types.NamespacedName{Name: name}, config); err != nil {
			if apierrors.IsNotFound(err) {
				return &capsulev1alpha1.CapsuleConfiguration{
					Spec: capsulev1alpha1.CapsuleConfigurationSpec{
						UserGroups:                     []string{"capsule.clastix.io"},
						ForceTenantPrefix:              false,
						ProtectedNamespaceRegexpString: "",
					},
				}
			}
			panic(errors.Wrap(err, "Cannot retrieve Capsule configuration with name "+name))
		}

		if !isUserGroupsMutuallyExclusive(config.Annotations[capsulev1alpha1.IgnoredUserGroupsAnnotation], config.Spec.UserGroups) {
			panic(errors.New("User Groups and Ignore User Groups are mutually exclusive, cannot bootstrap with current Capsule configuration"))
		}
		return config
	}}
}

func (c capsuleConfiguration) ProtectedNamespaceRegexp() (*regexp.Regexp, error) {
	expr := c.retrievalFn().Spec.ProtectedNamespaceRegexpString
	if len(expr) == 0 {
		return nil, nil // nolint:nilnil
	}

	r, err := regexp.Compile(expr)
	if err != nil {
		return nil, errors.Wrap(err, "Cannot compile the protected namespace regexp")
	}

	return r, nil
}

func (c capsuleConfiguration) ForceTenantPrefix() bool {
	return c.retrievalFn().Spec.ForceTenantPrefix
}

func (c capsuleConfiguration) CASecretName() (name string) {
	name = CASecretName

	if c.retrievalFn().Annotations == nil {
		return
	}

	v, ok := c.retrievalFn().Annotations[capsulev1alpha1.CASecretNameAnnotation]
	if ok {
		return v
	}

	return
}

func (c capsuleConfiguration) TLSSecretName() (name string) {
	name = TLSSecretName

	if c.retrievalFn().Annotations == nil {
		return
	}

	v, ok := c.retrievalFn().Annotations[capsulev1alpha1.TLSSecretNameAnnotation]
	if ok {
		return v
	}

	return
}

func (c capsuleConfiguration) MutatingWebhookConfigurationName() (name string) {
	name = MutatingWebhookConfigurationName

	if c.retrievalFn().Annotations == nil {
		return
	}

	v, ok := c.retrievalFn().Annotations[capsulev1alpha1.MutatingWebhookConfigurationName]
	if ok {
		return v
	}

	return
}

func (c capsuleConfiguration) ValidatingWebhookConfigurationName() (name string) {
	name = ValidatingWebhookConfigurationName

	if c.retrievalFn().Annotations == nil {
		return
	}

	v, ok := c.retrievalFn().Annotations[capsulev1alpha1.ValidatingWebhookConfigurationName]
	if ok {
		return v
	}

	return
}

func (c capsuleConfiguration) UserGroups() []string {
	return c.retrievalFn().Spec.UserGroups
}

func (c capsuleConfiguration) hasForbiddenNodeLabelsAnnotations() bool {
	if _, ok := c.retrievalFn().Annotations[capsulev1alpha1.ForbiddenNodeLabelsAnnotation]; ok {
		return true
	}

	if _, ok := c.retrievalFn().Annotations[capsulev1alpha1.ForbiddenNodeLabelsRegexpAnnotation]; ok {
		return true
	}

	return false
}

func (c capsuleConfiguration) hasForbiddenNodeAnnotationsAnnotations() bool {
	if _, ok := c.retrievalFn().Annotations[capsulev1alpha1.ForbiddenNodeAnnotationsAnnotation]; ok {
		return true
	}

	if _, ok := c.retrievalFn().Annotations[capsulev1alpha1.ForbiddenNodeAnnotationsRegexpAnnotation]; ok {
		return true
	}

	return false
}

func (c *capsuleConfiguration) ForbiddenUserNodeLabels() *capsulev1beta1.ForbiddenListSpec {
	if !c.hasForbiddenNodeLabelsAnnotations() {
		return nil
	}

	return &capsulev1beta1.ForbiddenListSpec{
		Exact: strings.Split(c.retrievalFn().Annotations[capsulev1alpha1.ForbiddenNodeLabelsAnnotation], ","),
		Regex: c.retrievalFn().Annotations[capsulev1alpha1.ForbiddenNodeLabelsRegexpAnnotation],
	}
}

func (c *capsuleConfiguration) ForbiddenUserNodeAnnotations() *capsulev1beta1.ForbiddenListSpec {
	if !c.hasForbiddenNodeAnnotationsAnnotations() {
		return nil
	}

	return &capsulev1beta1.ForbiddenListSpec{
		Exact: strings.Split(c.retrievalFn().Annotations[capsulev1alpha1.ForbiddenNodeAnnotationsAnnotation], ","),
		Regex: c.retrievalFn().Annotations[capsulev1alpha1.ForbiddenNodeAnnotationsRegexpAnnotation],
	}
}

func (c *capsuleConfiguration) IgnoredUserGroupsAnnotations() sets.String {
	ignoredUserGroupSet := sets.NewString()
	ignoredUserGroups, ok := c.retrievalFn().Annotations[capsulev1alpha1.IgnoredUserGroupsAnnotation]
	if !ok {
		return nil
	}
	return ignoredUserGroupSet.Insert(strings.Split(ignoredUserGroups, ",")...)
}

func isUserGroupsMutuallyExclusive(ignoredUserGroups string, userGroups []string) bool {
	if len(ignoredUserGroups) > 0 {
		sort.Strings(userGroups)
		userGroupsLen := len(userGroups)

		ignoredUserGrpSlice := strings.Split(ignoredUserGroups, ",")
		sort.Strings(ignoredUserGrpSlice)

		for _, ignoredGroup := range ignoredUserGrpSlice {
			idx := sort.SearchStrings(userGroups, ignoredGroup)
			if idx <= userGroupsLen && userGroups[idx] == ignoredGroup {
				return false
			}
		}
	}
	return true
}
