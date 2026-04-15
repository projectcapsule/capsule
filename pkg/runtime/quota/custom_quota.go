// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package quota

import (
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/projectcapsule/capsule/pkg/runtime/jsonpath"
)

// Overlay for Global/Namespace CustomQuotas
type MatchedQuota struct {
	Key          string
	Name         string
	Namespace    string
	Path         string
	CompiledPath *jsonpath.CompiledJSONPath
	Operation    Operation
	Limit        resource.Quantity
	Used         resource.Quantity
	IsGlobal     bool
	SourceRank   int
}

func MakeCustomQuotaCacheKey(namespace, name string) string {
	return namespace + "/" + name
}

func MakeGlobalCustomQuotaCacheKey(name string) string {
	return "C/" + name
}
