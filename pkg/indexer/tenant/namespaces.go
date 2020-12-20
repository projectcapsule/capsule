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
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clastix/capsule/api/v1alpha1"
)

type NamespacesReference struct {
}

func (o NamespacesReference) Object() client.Object {
	return &v1alpha1.Tenant{}
}

func (o NamespacesReference) Field() string {
	return ".status.namespaces"
}

func (o NamespacesReference) Func() client.IndexerFunc {
	return func(object client.Object) []string {
		return object.(*v1alpha1.Tenant).DeepCopy().Status.Namespaces
	}
}
