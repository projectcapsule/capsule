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

package service_labels

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ServiceType interface {
	Namespace() string
	Labels() map[string]string
	Annotations() map[string]string
}

type Service struct {
	*corev1.Service
}

func (s Service) Namespace() string {
	return s.GetNamespace()
}

func (s Service) Labels() map[string]string {
	return s.GetLabels()
}

func (s Service) Annotations() map[string]string {
	return s.GetAnnotations()
}

type Endpoints struct {
	*corev1.Endpoints
}

func (ep Endpoints) Namespace() string {
	return ep.GetNamespace()
}

func (ep Endpoints) Labels() map[string]string {
	return ep.GetLabels()
}

func (ep Endpoints) Annotations() map[string]string {
	return ep.GetAnnotations()
}

type EndpointSlice struct {
	metav1.Object
}

func (eps EndpointSlice) Namespace() string {
	return eps.GetNamespace()
}

func (eps EndpointSlice) Labels() map[string]string {
	return eps.GetLabels()
}

func (eps EndpointSlice) Annotations() map[string]string {
	return eps.GetAnnotations()
}
