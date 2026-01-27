// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package utils_test

import (
	"errors"
	"fmt"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	"github.com/projectcapsule/capsule/pkg/utils"
)

func TestIsUnsupportedAPI_NoKindMatchError(t *testing.T) {
	err := &meta.NoKindMatchError{}

	if !utils.IsUnsupportedAPI(err) {
		t.Fatalf("expected true for NoKindMatchError")
	}
}

func TestIsUnsupportedAPI_GroupDiscoveryFailed(t *testing.T) {
	err := &discovery.ErrGroupDiscoveryFailed{}

	if !utils.IsUnsupportedAPI(err) {
		t.Fatalf("expected true for ErrGroupDiscoveryFailed")
	}
}

func TestIsUnsupportedAPI_ResourceDiscoveryFailed(t *testing.T) {
	err := &apiutil.ErrResourceDiscoveryFailed{}

	if !utils.IsUnsupportedAPI(err) {
		t.Fatalf("expected true for ErrResourceDiscoveryFailed")
	}
}

func TestIsUnsupportedAPI_WrappedError(t *testing.T) {
	base := &meta.NoKindMatchError{}
	err := fmt.Errorf("wrapped: %w", base)

	if !utils.IsUnsupportedAPI(err) {
		t.Fatalf("expected true for wrapped NoKindMatchError")
	}
}

func TestIsUnsupportedAPI_OtherError(t *testing.T) {
	err := errors.New("some other error")

	if utils.IsUnsupportedAPI(err) {
		t.Fatalf("expected false for unrelated error")
	}
}

func TestIsUnsupportedAPI_NilError(t *testing.T) {
	if utils.IsUnsupportedAPI(nil) {
		t.Fatalf("expected false for nil error")
	}
}
