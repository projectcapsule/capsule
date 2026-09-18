// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/rand"
)

func createResourcePermitTestNamespace(ctx context.Context) *corev1.Namespace {
	namespace := NewNamespace("e2e-resourcepermit-" + rand.String(10))
	// Namespace termination permits cleanup in every lifecycle phase. Deleting
	// an initializing or active ResourcePermit directly is rejected by admission.
	DeferCleanup(func() {
		ForceDeleteNamespace(ctx, namespace.Name)
	})
	NamespaceCreationAdmin(namespace, defaultTimeoutInterval).Should(Succeed())

	return namespace
}
