// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package errors

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

func IgnoreGone(err error) bool {
	return err == nil ||
		apierrors.IsNotFound(err) ||
		apierrors.HasStatusCause(err, corev1.NamespaceTerminatingCause) ||
		strings.Contains(err.Error(), " not found")
}
