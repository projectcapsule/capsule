// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package namespace_test

import (
	"reflect"
	"testing"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/runtime/indexers/namespace"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestOwnerReferenceIndexer(t *testing.T) {
	t.Parallel()

	idx := namespace.OwnerReference{}
	got := idx.Func()(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{OwnerReferences: []metav1.OwnerReference{
		{APIVersion: "v1", Kind: "ConfigMap", Name: "ignored"},
		{APIVersion: capsulev1beta2.GroupVersion.String(), Kind: "Tenant", Name: "tenant-a"},
	}}})

	if idx.Object() == nil || idx.Field() != namespace.OwnerReferenceIndex {
		t.Fatalf("unexpected object/field")
	}
	if !reflect.DeepEqual(got, []string{"tenant-a"}) {
		t.Fatalf("Func() = %#v, want tenant-a", got)
	}
}
