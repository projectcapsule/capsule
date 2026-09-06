// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

type Decoder[T interface {
	runtime.Object
	DeepCopyInto(T)
}] struct {
	Object    T
	OldObject T
}

func copyInto[T interface {
	runtime.Object
	DeepCopyInto(T)
}](source T, into runtime.Object) {
	if into == nil || any(source) == nil {
		return
	}

	target, ok := into.(T)
	if !ok {
		return
	}

	source.DeepCopyInto(target)
}

func (d *Decoder[T]) Decode(_ admission.Request, into runtime.Object) error {
	copyInto[T](d.Object, into)

	return nil
}

func (d *Decoder[T]) DecodeRaw(_ runtime.RawExtension, into runtime.Object) error {
	copyInto[T](d.OldObject, into)

	return nil
}
