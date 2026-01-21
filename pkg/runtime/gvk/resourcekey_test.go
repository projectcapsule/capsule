// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package gvk_test

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/projectcapsule/capsule/pkg/runtime/gvk"
)

func TestKeyFromUnstructured(t *testing.T) {
	t.Parallel()

	t.Run("nil object returns false", func(t *testing.T) {
		t.Parallel()

		_, ok := gvk.KeyFromUnstructured(nil)
		if ok {
			t.Fatalf("expected ok=false")
		}
	})

	t.Run("missing kind returns false", func(t *testing.T) {
		t.Parallel()

		u := &unstructured.Unstructured{}
		u.SetAPIVersion("apps/v1")
		u.SetName("demo")
		u.SetNamespace("default")

		_, ok := gvk.KeyFromUnstructured(u)
		if ok {
			t.Fatalf("expected ok=false")
		}
	})

	t.Run("missing name returns false", func(t *testing.T) {
		t.Parallel()

		u := &unstructured.Unstructured{}
		u.SetAPIVersion("apps/v1")
		u.SetKind("Deployment")
		u.SetNamespace("default")

		_, ok := gvk.KeyFromUnstructured(u)
		if ok {
			t.Fatalf("expected ok=false")
		}
	})

	t.Run("returns key for namespaced object", func(t *testing.T) {
		t.Parallel()

		u := &unstructured.Unstructured{}
		u.SetAPIVersion("apps/v1")
		u.SetKind("Deployment")
		u.SetNamespace("default")
		u.SetName("demo")

		key, ok := gvk.KeyFromUnstructured(u)
		if !ok {
			t.Fatalf("expected ok=true")
		}

		if key.Group != "apps" {
			t.Fatalf("expected group=apps, got %q", key.Group)
		}
		if key.Version != "v1" {
			t.Fatalf("expected version=v1, got %q", key.Version)
		}
		if key.Kind != "Deployment" {
			t.Fatalf("expected kind=Deployment, got %q", key.Kind)
		}
		if key.Namespace != "default" {
			t.Fatalf("expected namespace=default, got %q", key.Namespace)
		}
		if key.Name != "demo" {
			t.Fatalf("expected name=demo, got %q", key.Name)
		}
	})

	t.Run("returns key for cluster-scoped object (empty namespace)", func(t *testing.T) {
		t.Parallel()

		u := &unstructured.Unstructured{}
		u.SetAPIVersion("rbac.authorization.k8s.io/v1")
		u.SetKind("ClusterRole")
		// no namespace
		u.SetName("admin")

		key, ok := gvk.KeyFromUnstructured(u)
		if !ok {
			t.Fatalf("expected ok=true")
		}

		if key.Namespace != "" {
			t.Fatalf("expected empty namespace, got %q", key.Namespace)
		}
	})
}
