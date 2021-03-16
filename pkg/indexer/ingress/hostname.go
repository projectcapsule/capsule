/*
Copyright 2020 Clastix Labs.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

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
