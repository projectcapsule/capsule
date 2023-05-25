// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery"
)

func IsUnsupportedAPI(err error) bool {
	missingAPIError, discoveryError := &meta.NoKindMatchError{}, &discovery.ErrGroupDiscoveryFailed{}

	return errors.As(err, &missingAPIError) || errors.As(err, &discoveryError)
}
