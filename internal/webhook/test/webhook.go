// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// VerifyResponse is a helper function to verify the response of a webhook function.
func VerifyResponse(tb testing.TB, response *admission.Response, status int32, message string) {
	tb.Helper()
	require.NotNil(tb, response.Result)
	assert.Equal(tb, status, response.Result.Code)
	assert.Contains(tb, response.Result.Message, message, "expected message %q to contain %q", response.Result.Message, message)
}
