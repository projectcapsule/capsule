// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package webhook

type Webhook interface {
	GetPath() string
	GetHandlers() []Handler
}
