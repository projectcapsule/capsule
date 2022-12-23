// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"fmt"
	"sort"
	"strings"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
)

const (
	NodeSelectorAnnotation = "scheduler.alpha.kubernetes.io/node-selector"
)

func BuildNodeSelector(tnt *capsulev1beta2.Tenant, nsAnnotations map[string]string) map[string]string {
	if nsAnnotations == nil {
		nsAnnotations = make(map[string]string)
	}

	selector := make([]string, 0, len(tnt.Spec.NodeSelector))

	for k, v := range tnt.Spec.NodeSelector {
		selector = append(selector, fmt.Sprintf("%s=%s", k, v))
	}
	// Sorting the resulting slice: iterating over maps is randomized, and we could end-up
	// in multiple reconciliations upon multiple node selectors.
	sort.Strings(selector)

	nsAnnotations[NodeSelectorAnnotation] = strings.Join(selector, ",")

	return nsAnnotations
}
