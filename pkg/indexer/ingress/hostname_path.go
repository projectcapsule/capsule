// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package ingress

import (
	"fmt"

	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	networkingv1 "k8s.io/api/networking/v1"
	networkingv1beta1 "k8s.io/api/networking/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	HostPathPair = "hostnamePathPair"
)

type HostnamePath struct {
	Obj metav1.Object
}

//nolint:forcetypeassert
func (s HostnamePath) Object() client.Object {
	return s.Obj.(client.Object)
}

func (s HostnamePath) Field() string {
	return HostPathPair
}

func (s HostnamePath) Func() client.IndexerFunc {
	return func(object client.Object) (entries []string) {
		hostPathMap := make(map[string]sets.Set[string])

		switch ing := object.(type) {
		case *networkingv1.Ingress:
			hostPathMap = hostPathMapForNetworkingV1(ing)
		case *networkingv1beta1.Ingress:
			hostPathMap = hostPathMapForNetworkingV1Beta1(ing)
		case *extensionsv1beta1.Ingress:
			hostPathMap = hostPathMapForExtensionsV1Beta1(ing)
		}

		for host, paths := range hostPathMap {
			for path := range paths {
				entries = append(entries, fmt.Sprintf("%s;%s", host, path))
			}
		}

		return
	}
}
