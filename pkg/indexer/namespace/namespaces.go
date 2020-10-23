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

package namespace

import (
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clastix/capsule/api/v1alpha1"
)

type OwnerReference struct {
}

func (o OwnerReference) Object() client.Object {
	return &v1.Namespace{}
}

func (o OwnerReference) Field() string {
	return ".metadata.ownerReferences[*].capsule"
}

func (o OwnerReference) Func() client.IndexerFunc {
	return func(object client.Object) []string {
		var res []string
		ns := object.(*v1.Namespace)
		for _, or := range ns.OwnerReferences {
			if or.APIVersion == v1alpha1.GroupVersion.String() {
				res = append(res, or.Name)
			}
		}
		return res
	}
}
