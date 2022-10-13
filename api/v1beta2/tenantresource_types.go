// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ObjectReferenceAbstract struct {
	// Kind of the referent.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
	Kind string `json:"kind"`
	// Namespace of the referent.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/namespaces/
	Namespace string `json:"namespace"`
	// API version of the referent.
	APIVersion string `json:"apiVersion,omitempty"`
}

type ObjectReferenceStatus struct {
	ObjectReferenceAbstract `json:",inline"`
	// Name of the referent.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#names
	Name string `json:"name"`
}

type ObjectReference struct {
	ObjectReferenceAbstract `json:",inline"`
	// Label selector used to select the given resources in the given Namespace.
	Selector metav1.LabelSelector `json:"selector"`
}

func (in *ObjectReferenceStatus) String() string {
	return fmt.Sprintf("Kind=%s,APIVersion=%s,Namespace=%s,Name=%s", in.Kind, in.APIVersion, in.Namespace, in.Name)
}

func (in *ObjectReferenceStatus) ParseFromString(value string) error {
	rawParts := strings.Split(value, ",")

	if len(rawParts) != 4 {
		return fmt.Errorf("unexpected raw parts")
	}

	for _, i := range rawParts {
		parts := strings.Split(i, "=")

		if len(parts) != 2 {
			return fmt.Errorf("unrecognized separator")
		}

		k, v := parts[0], parts[1]

		switch k {
		case "Kind":
			in.Kind = v
		case "APIVersion":
			in.APIVersion = v
		case "Namespace":
			in.Namespace = v
		case "Name":
			in.Name = v
		default:
			return fmt.Errorf("unrecognized marker: %s", k)
		}
	}

	return nil
}
