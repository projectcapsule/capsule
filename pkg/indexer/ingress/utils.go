// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package ingress

import (
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	networkingv1 "k8s.io/api/networking/v1"
	networkingv1beta1 "k8s.io/api/networking/v1beta1"
	"k8s.io/apimachinery/pkg/util/sets"
)

func hostPathMapForExtensionsV1Beta1(ing *extensionsv1beta1.Ingress) map[string]sets.Set[string] {
	hostPathMap := make(map[string]sets.Set[string])

	for _, r := range ing.Spec.Rules {
		if r.HTTP == nil {
			continue
		}

		if _, ok := hostPathMap[r.Host]; !ok {
			hostPathMap[r.Host] = sets.New[string]()
		}

		for _, path := range r.HTTP.Paths {
			hostPathMap[r.Host].Insert(path.Path)
		}
	}

	return hostPathMap
}

func hostPathMapForNetworkingV1Beta1(ing *networkingv1beta1.Ingress) map[string]sets.Set[string] {
	hostPathMap := make(map[string]sets.Set[string])

	for _, r := range ing.Spec.Rules {
		if r.HTTP == nil {
			continue
		}

		if _, ok := hostPathMap[r.Host]; !ok {
			hostPathMap[r.Host] = sets.New[string]()
		}

		for _, path := range r.HTTP.Paths {
			hostPathMap[r.Host].Insert(path.Path)
		}
	}

	return hostPathMap
}

func hostPathMapForNetworkingV1(ing *networkingv1.Ingress) map[string]sets.Set[string] {
	hostPathMap := make(map[string]sets.Set[string])

	for _, r := range ing.Spec.Rules {
		if r.HTTP == nil {
			continue
		}

		if _, ok := hostPathMap[r.Host]; !ok {
			hostPathMap[r.Host] = sets.New[string]()
		}

		for _, path := range r.HTTP.Paths {
			hostPathMap[r.Host].Insert(path.Path)
		}
	}

	return hostPathMap
}
