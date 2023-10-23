// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

func IsUnsupportedAPI(err error) bool {
	missingAPIError, discoveryGropuError, discoveryResourceError := &meta.NoKindMatchError{}, &discovery.ErrGroupDiscoveryFailed{}, &apiutil.ErrResourceDiscoveryFailed{}

	return errors.As(err, &missingAPIError) || errors.As(err, &discoveryGropuError) || errors.As(err, &discoveryResourceError)
}
