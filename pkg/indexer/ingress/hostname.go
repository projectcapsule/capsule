// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package ingress

import (
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	networkingv1 "k8s.io/api/networking/v1"
	networkingv1beta1 "k8s.io/api/networking/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Hostname struct {
	Obj metav1.Object
}

func (h Hostname) Object() client.Object {
	return h.Obj.(client.Object)
}

func (h Hostname) Field() string {
	return ".spec.rules[*].host"
}

func (h Hostname) Func() client.IndexerFunc {
	return func(object client.Object) (hostnames []string) {
		switch h.Obj.(type) {
		case *networkingv1.Ingress:
			ing := object.(*networkingv1.Ingress)
			for _, r := range ing.Spec.Rules {
				hostnames = append(hostnames, r.Host)
			}
			return
		case *networkingv1beta1.Ingress:
			ing := object.(*networkingv1beta1.Ingress)
			for _, r := range ing.Spec.Rules {
				hostnames = append(hostnames, r.Host)
			}
			return
		case *extensionsv1beta1.Ingress:
			ing := object.(*extensionsv1beta1.Ingress)
			for _, r := range ing.Spec.Rules {
				hostnames = append(hostnames, r.Host)
			}
			return
		default:
			return
		}
	}
}
