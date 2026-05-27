// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	gherrors "github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

func IsUnsupportedAPI(err error) bool {
	missingAPIError, discoveryGropuError, discoveryResourceError := &meta.NoKindMatchError{}, &discovery.ErrGroupDiscoveryFailed{}, &apiutil.ErrResourceDiscoveryFailed{}

	return gherrors.As(err, &missingAPIError) || gherrors.As(err, &discoveryGropuError) || gherrors.As(err, &discoveryResourceError)
}

func IgnoreWrappedNotFound(err error) error {
	if err == nil {
		return nil
	}

	if apierrors.IsNotFound(err) {
		return nil
	}

	if apierrors.IsNotFound(gherrors.Cause(err)) {
		return nil
	}

	return err
}
