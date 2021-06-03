// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package webhook

type Webhook interface {
	GetName() string
	GetPath() string
	GetHandler() Handler
}
