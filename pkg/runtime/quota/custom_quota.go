// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package quota

import (
	"k8s.io/apimachinery/pkg/api/resource"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

// Overlay for Global/Namespace CustomQuotas
type MatchedQuota struct {
	Key       string
	Name      string
	Namespace string
	Path      string
	Limit     resource.Quantity
	Used      resource.Quantity
	IsGlobal  bool
}

func MakeCustomQuotaCacheKey(namespace, name string) string {
	return namespace + "/" + name
}

func MakeGlobalCustomQuotaCacheKey(cq capsulev1beta2.GlobalCustomQuota) string {
	return "C/" + cq.Name
}
