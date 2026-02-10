// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenantresource_test

import (
	"reflect"
	"testing"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/indexers/tenantresource"
)

func TestGlobalServiceAccount_Object(t *testing.T) {
	var idx tenantresource.GlobalServiceAccount
	obj := idx.Object()

	_, ok := obj.(*capsulev1beta2.GlobalTenantResource)
	if !ok {
		t.Fatalf("expected *capsulev1beta2.GlobalTenantResource, got %T", obj)
	}
}

func TestGlobalServiceAccount_Field(t *testing.T) {
	var idx tenantresource.GlobalServiceAccount
	if idx.Field() != tenantresource.ServiceAccountIndexerFieldName {
		t.Fatalf("unexpected field: got %q want %q", idx.Field(), tenantresource.ServiceAccountIndexerFieldName)
	}
}

func TestGlobalServiceAccount_Func(t *testing.T) {
	var idx tenantresource.GlobalServiceAccount
	fn := idx.Func()

	t.Run("nil serviceAccount => nil", func(t *testing.T) {
		tgr := &capsulev1beta2.GlobalTenantResource{}
		tgr.Status.ServiceAccount = nil

		got := fn(tgr)
		if got != nil {
			t.Fatalf("expected nil, got %#v", got)
		}
	})

	t.Run("empty namespace => nil", func(t *testing.T) {
		tgr := &capsulev1beta2.GlobalTenantResource{}
		tgr.Status.ServiceAccount = &meta.NamespacedRFC1123ObjectReferenceWithNamespace{
			Name:      meta.RFC1123Name("sa"),
			Namespace: meta.RFC1123SubdomainName(""),
		}

		got := fn(tgr)
		if got != nil {
			t.Fatalf("expected nil, got %#v", got)
		}
	})

	t.Run("empty name => nil", func(t *testing.T) {
		tgr := &capsulev1beta2.GlobalTenantResource{}
		tgr.Status.ServiceAccount = &meta.NamespacedRFC1123ObjectReferenceWithNamespace{
			Name:      meta.RFC1123Name(""),
			Namespace: meta.RFC1123SubdomainName("kube-system"),
		}

		got := fn(tgr)
		if got != nil {
			t.Fatalf("expected nil, got %#v", got)
		}
	})

	t.Run("both set => returns ns/name key", func(t *testing.T) {
		tgr := &capsulev1beta2.GlobalTenantResource{}
		tgr.Status.ServiceAccount = &meta.NamespacedRFC1123ObjectReferenceWithNamespace{
			Name:      meta.RFC1123Name("default"),
			Namespace: meta.RFC1123SubdomainName("kube-system"),
		}

		got := fn(tgr)
		want := []string{"kube-system/default"}

		if !reflect.DeepEqual(got, want) {
			t.Fatalf("unexpected result\nwant=%#v\ngot =%#v", want, got)
		}
	})
}
