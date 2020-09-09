package ingress

import (
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	networkingv1beta1 "k8s.io/api/networking/v1beta1"
)

const (
	annotationName = "kubernetes.io/ingress.class"
)

type Ingress interface {
	IngressClass() *string
	Namespace() string
}

type Networking struct {
	*networkingv1beta1.Ingress
}

func (n Networking) IngressClass() (res *string) {
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

func (n Networking) Namespace() string {
	return n.GetNamespace()
}

type Extension struct {
	*extensionsv1beta1.Ingress
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

func (e Extension) Namespace() string {
	return e.GetNamespace()
}
