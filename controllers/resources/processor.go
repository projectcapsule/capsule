// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	finalizer = "capsule.clastix.io/resources"
)

type Processor struct {
	client             client.Client
	unstructuredClient client.Client
}
