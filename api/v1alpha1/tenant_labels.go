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

package v1alpha1

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func GetTypeLabel(t runtime.Object) (label string, err error) {
	switch v := t.(type) {
	case *Tenant:
		return "capsule.clastix.io/tenant", nil
	case *corev1.LimitRange:
		return "capsule.clastix.io/limit-range", nil
	case *networkingv1.NetworkPolicy:
		return "capsule.clastix.io/network-policy", nil
	case *corev1.ResourceQuota:
		return "capsule.clastix.io/resource-quota", nil
	default:
		err = fmt.Errorf("type %T is not mapped as Capsule label recognized", v)
	}
	return
}
