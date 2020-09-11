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
	"fmt"
)

type ingressClassForbidden struct {
	ingressClass string
}

func NewIngressClassForbidden(ingressClass string) error {
	return &ingressClassForbidden{ingressClass: ingressClass}
}

func (i ingressClassForbidden) Error() string {
	return fmt.Sprintf("Ingress Class %s is forbidden for the current Tenant", i.ingressClass)
}

type ingressClassNotValid struct{}

func NewIngressClassNotValid() error {
	return &ingressClassNotValid{}
}

func (ingressClassNotValid) Error() string {
	return "A valid Ingress Class must be used"
}
