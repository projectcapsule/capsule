// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package serviceaccount_test

import (
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	serviceaccountindexer "github.com/projectcapsule/capsule/pkg/runtime/indexers/serviceaccount"
)

func TestBreakRequestReference(t *testing.T) {
	t.Parallel()

	indexer := serviceaccountindexer.BreakRequestReference{}
	if _, ok := indexer.Object().(*capsulev1beta2.BreakRequest); !ok {
		t.Fatalf("Object() = %T, want *BreakRequest", indexer.Object())
	}
	if indexer.Field() != serviceaccountindexer.ReferenceFieldName {
		t.Fatalf("Field() = %q, want %q", indexer.Field(), serviceaccountindexer.ReferenceFieldName)
	}

	request := &capsulev1beta2.BreakRequest{
		ObjectMeta: metav1.ObjectMeta{Name: "access", Namespace: "team-a"},
		Status: capsulev1beta2.BreakRequestStatus{Request: &capsulev1beta2.BreakRequestStatusRequest{
			Impersonation: &meta.NamespacedRFC1123ObjectReferenceWithNamespace{
				Name:      "runner",
				Namespace: "capsule-system",
			},
		}},
	}
	if got := indexer.Func()(request); !reflect.DeepEqual(got, []string{"capsule-system/runner"}) {
		t.Fatalf("Func() = %#v, want ServiceAccount reference key", got)
	}

	request.Status.Request.Impersonation = nil
	if got := indexer.Func()(request); got != nil {
		t.Fatalf("Func() without ServiceAccount = %#v, want nil", got)
	}

	request.Status.Request = nil
	if got := indexer.Func()(request); got != nil {
		t.Fatalf("Func() without request status = %#v, want nil", got)
	}
}

func TestReferenceKey(t *testing.T) {
	t.Parallel()

	if got := serviceaccountindexer.ReferenceKey("capsule-system", "runner"); got != "capsule-system/runner" {
		t.Fatalf("ReferenceKey() = %q", got)
	}
	if got := serviceaccountindexer.ReferenceKey("", "runner"); got != "" {
		t.Fatalf("ReferenceKey() with empty namespace = %q, want empty", got)
	}
}
