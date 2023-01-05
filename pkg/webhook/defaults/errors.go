// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package defaults

import (
	"fmt"
)

type StorageClassError struct {
	storageClass string
	msg          error
}

func NewStorageClassError(class string, msg error) error {
	return &StorageClassError{
		storageClass: class,
		msg:          msg,
	}
}

func (e StorageClassError) Error() string {
	return fmt.Sprintf("Failed to resolve Storage Class %s: %s", e.storageClass, e.msg)
}

type IngressClassError struct {
	ingressClass string
	msg          error
}

func NewIngressClassError(class string, msg error) error {
	return &IngressClassError{
		ingressClass: class,
		msg:          msg,
	}
}

func (e IngressClassError) Error() string {
	return fmt.Sprintf("Failed to resolve Ingress Class %s: %s", e.ingressClass, e.msg)
}

type PriorityClassError struct {
	priorityClass string
	msg           error
}

func NewPriorityClassError(class string, msg error) error {
	return &PriorityClassError{
		priorityClass: class,
		msg:           msg,
	}
}

func (e PriorityClassError) Error() string {
	return fmt.Sprintf("Failed to resolve Priority Class %s: %s", e.priorityClass, e.msg)
}
