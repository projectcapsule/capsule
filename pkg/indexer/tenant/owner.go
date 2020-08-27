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

package tenant

import (
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clastix/capsule/api/v1alpha1"
	"github.com/clastix/capsule/pkg/utils"
)

type OwnerReference struct {
}

func (o OwnerReference) Object() runtime.Object {
	return &v1alpha1.Tenant{}
}

func (o OwnerReference) Field() string {
	return ".spec.owner.ownerkind"
}

func (o OwnerReference) Func() client.IndexerFunc {
	return func(object runtime.Object) []string {
		tenant := object.(*v1alpha1.Tenant)
		return []string{utils.GetOwnerWithKind(tenant)}
	}
}
