// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type requestReadKey struct {
	key        client.ObjectKey
	objectType reflect.Type
}

type requestReadResult struct {
	object client.Object
	err    error
	ready  chan struct{}
}

type requestCachingReader struct {
	client.Reader

	mu      sync.Mutex
	results map[requestReadKey]requestReadResult
}

// NewRequestCachingReader deduplicates identical direct reads made by handlers
// participating in one admission request. Results are deep-copied before they
// are returned so one handler cannot mutate the snapshot observed by another.
func NewRequestCachingReader(reader client.Reader) client.Reader {
	return &requestCachingReader{
		Reader:  reader,
		results: make(map[requestReadKey]requestReadResult),
	}
}

func (r *requestCachingReader) Get(
	ctx context.Context,
	key client.ObjectKey,
	obj client.Object,
	opts ...client.GetOption,
) error {
	if len(opts) > 0 {
		return r.Reader.Get(ctx, key, obj, opts...)
	}

	cacheKey := requestReadKey{
		key:        key,
		objectType: reflect.TypeOf(obj),
	}

	r.mu.Lock()
	if result, found := r.results[cacheKey]; found {
		r.mu.Unlock()
		<-result.ready

		r.mu.Lock()
		result = r.results[cacheKey]
		r.mu.Unlock()

		if result.object != nil {
			if err := copyClientObject(result.object, obj); err != nil {
				return err
			}
		}

		return result.err
	}

	result := requestReadResult{ready: make(chan struct{})}
	r.results[cacheKey] = result
	r.mu.Unlock()

	err := r.Reader.Get(ctx, key, obj)
	if err == nil {
		copied, ok := obj.DeepCopyObject().(client.Object)
		if !ok {
			result.err = fmt.Errorf("deep copy of %T does not implement client.Object", obj)

			r.mu.Lock()
			r.results[cacheKey] = result
			close(result.ready)
			r.mu.Unlock()

			return result.err
		}

		result.object = copied
	}

	result.err = err

	r.mu.Lock()
	r.results[cacheKey] = result
	close(result.ready)
	r.mu.Unlock()

	return err
}

func copyClientObject(src, dst client.Object) error {
	srcCopy, ok := src.DeepCopyObject().(client.Object)
	if !ok {
		return fmt.Errorf("deep copy of %T does not implement client.Object", src)
	}

	srcValue := reflect.ValueOf(srcCopy)
	dstValue := reflect.ValueOf(dst)

	if srcValue.Kind() != reflect.Pointer ||
		dstValue.Kind() != reflect.Pointer ||
		srcValue.Type() != dstValue.Type() ||
		srcValue.IsNil() ||
		dstValue.IsNil() {
		return fmt.Errorf("cannot copy cached %T into %T", src, dst)
	}

	dstValue.Elem().Set(srcValue.Elem())

	return nil
}
