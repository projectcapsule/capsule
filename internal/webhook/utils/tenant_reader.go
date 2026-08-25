// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

type tenantReadResult struct {
	tenant *capsulev1beta2.Tenant
	err    error
}

type tenantCachingReader struct {
	client.Reader

	results map[client.ObjectKey]tenantReadResult
}

// NewTenantCachingReader deduplicates direct Tenant reads within one admission
// request while preserving the API reader's fresh snapshot for all other
// objects and list operations.
func NewTenantCachingReader(reader client.Reader) client.Reader {
	return &tenantCachingReader{
		Reader:  reader,
		results: make(map[client.ObjectKey]tenantReadResult),
	}
}

func (r *tenantCachingReader) Get(
	ctx context.Context,
	key client.ObjectKey,
	obj client.Object,
	opts ...client.GetOption,
) error {
	tenant, ok := obj.(*capsulev1beta2.Tenant)
	if !ok || len(opts) > 0 {
		return r.Reader.Get(ctx, key, obj, opts...)
	}

	if result, found := r.results[key]; found {
		if result.tenant != nil {
			result.tenant.DeepCopyInto(tenant)
		}

		return result.err
	}

	err := r.Reader.Get(ctx, key, tenant)
	result := tenantReadResult{err: err}

	if err == nil {
		result.tenant = tenant.DeepCopy()
	}

	r.results[key] = result

	return err
}
