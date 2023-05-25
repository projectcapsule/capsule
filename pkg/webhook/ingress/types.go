// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package ingress

import (
	"sort"

	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	networkingv1 "k8s.io/api/networking/v1"
	networkingv1beta1 "k8s.io/api/networking/v1beta1"
	"k8s.io/apimachinery/pkg/util/sets"
)

const (
	annotationName = "kubernetes.io/ingress.class"
)

type Ingress interface {
	IngressClass() *string
	Namespace() string
	Name() string
	HostnamePathsPairs() map[string]sets.Set[string]
	SetIngressClass(string)
	SetNamespace(string)
}

type NetworkingV1 struct {
	*networkingv1.Ingress
}

func (n NetworkingV1) Name() string {
	return n.GetName()
}

func (n NetworkingV1) IngressClass() (res *string) {
	res = n.Spec.IngressClassName
	if res == nil {
		if a := n.GetAnnotations(); a != nil {
			if v, ok := a[annotationName]; ok {
				res = &v
			}
		}
	}

	return
}

func (n NetworkingV1) SetIngressClass(ingressClassName string) {
	if n.Spec.IngressClassName == nil {
		if a := n.GetAnnotations(); a != nil {
			if _, ok := a[annotationName]; ok {
				a[annotationName] = ingressClassName

				return
			}
		}
	}
	// Assign in case the IngressClassName property was not set
	n.Spec.IngressClassName = &ingressClassName
}

func (n NetworkingV1) Namespace() string {
	return n.GetNamespace()
}

func (n NetworkingV1) SetNamespace(ns string) {
	n.Ingress.SetNamespace(ns)
}

//nolint:dupl
func (n NetworkingV1) HostnamePathsPairs() (pairs map[string]sets.Set[string]) {
	pairs = make(map[string]sets.Set[string])

	for _, rule := range n.Spec.Rules {
		host := rule.Host

		if _, ok := pairs[host]; !ok {
			pairs[host] = sets.New[string]()
		}

		if http := rule.IngressRuleValue.HTTP; http != nil {
			for _, path := range http.Paths {
				pairs[host].Insert(path.Path)
			}
		}

		if http := rule.HTTP; http != nil {
			for _, path := range http.Paths {
				pairs[host].Insert(path.Path)
			}
		}
	}

	return pairs
}

type NetworkingV1Beta1 struct {
	*networkingv1beta1.Ingress
}

func (n NetworkingV1Beta1) Name() string {
	return n.GetName()
}

func (n NetworkingV1Beta1) IngressClass() (res *string) {
	res = n.Spec.IngressClassName
	if res == nil {
		if a := n.GetAnnotations(); a != nil {
			if v, ok := a[annotationName]; ok {
				res = &v
			}
		}
	}

	return
}

func (n NetworkingV1Beta1) SetIngressClass(ingressClassName string) {
	if n.Spec.IngressClassName == nil {
		if a := n.GetAnnotations(); a != nil {
			if _, ok := a[annotationName]; ok {
				a[annotationName] = ingressClassName

				return
			}
		}
	}
	// Assign in case the IngressClassName property was not set
	n.Annotations[annotationName] = ingressClassName
}

func (n NetworkingV1Beta1) Namespace() string {
	return n.GetNamespace()
}

func (n NetworkingV1Beta1) SetNamespace(ns string) {
	n.Ingress.SetNamespace(ns)
}

//nolint:dupl
func (n NetworkingV1Beta1) HostnamePathsPairs() (pairs map[string]sets.Set[string]) {
	pairs = make(map[string]sets.Set[string])

	for _, rule := range n.Spec.Rules {
		host := rule.Host

		if _, ok := pairs[host]; !ok {
			pairs[host] = sets.New[string]()
		}

		if http := rule.IngressRuleValue.HTTP; http != nil {
			for _, path := range http.Paths {
				pairs[host].Insert(path.Path)
			}
		}

		if http := rule.HTTP; http != nil {
			for _, path := range http.Paths {
				pairs[host].Insert(path.Path)
			}
		}
	}

	return pairs
}

type Extension struct {
	*extensionsv1beta1.Ingress
}

func (e Extension) Name() string {
	return e.GetName()
}

func (e Extension) SetNamespace(ns string) {
	e.Ingress.SetNamespace(ns)
}

func (e Extension) IngressClass() (res *string) {
	res = e.Spec.IngressClassName
	if res == nil {
		if a := e.GetAnnotations(); a != nil {
			if v, ok := a[annotationName]; ok {
				res = &v
			}
		}
	}

	return
}

func (e Extension) SetIngressClass(ingressClassName string) {
	if a := e.GetAnnotations(); a != nil {
		if _, ok := a[annotationName]; ok {
			a[annotationName] = ingressClassName

			return
		}
	}
	// Assign in case the IngressClassName property was not set
	e.Annotations[annotationName] = ingressClassName
}

func (e Extension) Namespace() string {
	return e.GetNamespace()
}

//nolint:dupl
func (e Extension) HostnamePathsPairs() (pairs map[string]sets.Set[string]) {
	pairs = make(map[string]sets.Set[string])

	for _, rule := range e.Spec.Rules {
		host := rule.Host

		if _, ok := pairs[host]; !ok {
			pairs[host] = sets.New[string]()
		}

		if http := rule.IngressRuleValue.HTTP; http != nil {
			for _, path := range http.Paths {
				pairs[host].Insert(path.Path)
			}
		}

		if http := rule.HTTP; http != nil {
			for _, path := range http.Paths {
				pairs[host].Insert(path.Path)
			}
		}
	}

	return pairs
}

type HostnamesList []string

func (h HostnamesList) Len() int {
	return len(h)
}

func (h HostnamesList) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

func (h HostnamesList) Less(i, j int) bool {
	return h[i] < h[j]
}

func (h HostnamesList) IsStringInList(value string) (ok bool) {
	sort.Sort(h)
	i := sort.SearchStrings(h, value)
	ok = i < h.Len() && h[i] == value

	return
}
