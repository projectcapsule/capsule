// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

// cacheFirstReader serves the lookups that every admission request performs to
// resolve its Tenant (Namespace, Tenant, managed RuleStatus) from the manager
// cache, which already holds these objects for the controllers. When the cache
// cannot answer, typically for a Namespace created a few milliseconds ago and not
// yet observed by the informer, it falls back to a direct read from the API
// server, so a fresh object is never reported as missing. Every other type, and
// every read with options, keeps the direct read.
type cacheFirstReader struct {
	cache client.Reader
	api   client.Reader
}

// NewCacheFirstReader returns a reader that answers Namespace, Tenant and
// RuleStatus lookups from cache and everything else from api.
func NewCacheFirstReader(cache, api client.Reader) client.Reader {
	return &cacheFirstReader{cache: cache, api: api}
}

func (r *cacheFirstReader) Get(
	ctx context.Context,
	key client.ObjectKey,
	obj client.Object,
	opts ...client.GetOption,
) error {
	if len(opts) == 0 && servedFromCache(obj) {
		if err := r.cache.Get(ctx, key, obj); err == nil {
			return nil
		}
	}

	return r.api.Get(ctx, key, obj, opts...)
}

func (r *cacheFirstReader) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	return r.api.List(ctx, list, opts...)
}

func servedFromCache(obj client.Object) bool {
	switch obj.(type) {
	case *corev1.Namespace, *capsulev1beta2.Tenant, *capsulev1beta2.RuleStatus:
		return true
	default:
		return false
	}
}
