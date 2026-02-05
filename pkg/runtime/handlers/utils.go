// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package handlers

import "sigs.k8s.io/controller-runtime/pkg/client"

type NewObjectFunc[T client.Object] func() T
