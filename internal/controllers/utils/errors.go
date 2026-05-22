// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"errors"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func IgnoreNotFoundOrTerminatingError(err error) bool {
	if err == nil {
		return false
	}

	if apierrors.IsNotFound(err) {
		return true
	}

	if apierrors.HasStatusCause(err, corev1.NamespaceTerminatingCause) {
		return true
	}

	var statusErr *apierrors.StatusError
	if errors.As(err, &statusErr) {
		if statusErr.ErrStatus.Reason == metav1.StatusReasonForbidden &&
			strings.Contains(statusErr.ErrStatus.Message, "because it is being terminated") {
			return true
		}
	}

	return false
}
