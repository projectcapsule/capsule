// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"fmt"
	"strings"

	"github.com/projectcapsule/capsule/pkg/api"
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
	// Tenant of the referent.
	Tenant string `json:"tenant,omitempty"`
	// status of the condition, one of True, False, Unknown.
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=True;False;Unknown
	Status metav1.ConditionStatus `json:"status" protobuf:"bytes,2,opt,name=status"`
	// message is a human readable message indicating details about the transition.
	// This may be an empty string.
	// +kubebuilder:validation:MaxLength=32768
	Message string `json:"message,omitempty" protobuf:"bytes,6,opt,name=message"`
	// type of condition in CamelCase or in foo.example.com/CamelCase.
	// ---
	// Many .condition.type values are consistent across resources like Available, but because arbitrary conditions can be
	// useful (see .node.status.conditions), the ability to deconflict is important.
	// The regex it matches is (dns1123SubdomainFmt/)?(qualifiedNameFmt)
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^([a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*/)?(([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])$`
	// +kubebuilder:validation:MaxLength=316
	Type string `json:"type" protobuf:"bytes,1,opt,name=type"`
	// Resource Scope
	Scope api.ResourceScope `json:"scope,omitempty"`
}

type ObjectReference struct {
	ObjectReferenceAbstract `json:",inline"`

	// Label selector used to select the given resources in the given Namespace.
	Selector metav1.LabelSelector `json:"selector"`
}

func (in *ObjectReferenceStatus) String() string {
	return fmt.Sprintf(
		"Kind=%s,APIVersion=%s,Tenant=%s,Namespace=%s,Name=%s,Status=%s,Message=%s,Type=%s",
		in.Kind, in.APIVersion, in.Tenant, in.Namespace, in.Name, in.Status, in.Message, in.Type)
}

func (in *ObjectReferenceStatus) ParseFromString(value string) error {
	rawParts := strings.Split(value, ",")

	if len(rawParts) != 8 {
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
		case "Tenant":
			in.Tenant = v
		case "Namespace":
			in.Namespace = v
		case "Name":
			in.Name = v
		case "Status":
			switch metav1.ConditionStatus(v) {
			case metav1.ConditionTrue, metav1.ConditionFalse, metav1.ConditionUnknown:
				in.Status = metav1.ConditionStatus(v)
			default:
				return fmt.Errorf("invalid status value: %q", v)
			}
		case "Message":
			in.Message = v
		case "Type":
			in.Type = v
		case "Scope":
			in.Scope = api.ResourceScope(v)

		default:
			return fmt.Errorf("unrecognized marker: %s", k)
		}
	}

	return nil
}
