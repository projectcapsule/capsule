// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"fmt"
	"strings"

	"github.com/projectcapsule/capsule/pkg/api"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
)

type GeneratorItemSpec struct {
	// Template contains any amount of yaml which is applied to Kubernetes.
	// This can be a single resource or multiple resources
	Template string `json:"template,omitempty"`
}

type ProcessedItems []ObjectReferenceStatus

// Adds a condition by type.
func (p *ProcessedItems) UpdateItem(item ObjectReferenceStatus) {
	for i, stat := range *p {
		if p.isEqual(stat, item) {
			(*p)[i].ObjectReferenceStatusCondition = item.ObjectReferenceStatusCondition

			return
		}
	}

	*p = append(*p, item)
}

// Removes a condition by type.
func (p *ProcessedItems) RemoveItem(item ObjectReferenceStatus) {
	filtered := make(ProcessedItems, 0, len(*p))

	for _, stat := range *p {
		if !p.isEqual(stat, item) {
			filtered = append(filtered, stat)
		}
	}

	*p = filtered
}

func (p *ProcessedItems) isEqual(a, b ObjectReferenceStatus) bool {
	return a.Owner == b.Owner && a.APIVersion == b.APIVersion && a.Kind == b.Kind && a.Name == b.Name && a.Namespace == b.Namespace
}

func (p *ProcessedItems) AsSet() sets.Set[string] {
	set := sets.New[string]()

	for _, i := range *p {
		set.Insert(i.String())
	}

	return set
}

type ObjectReferenceAbstract struct {
	// Kind of the referent.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
	Kind string `json:"kind"`
	// Namespace of the referent.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/namespaces/
	Namespace string `json:"namespace,omitempty"`
	// API version of the referent.
	APIVersion string `json:"apiVersion,omitempty"`
}

type ObjectReferenceStatus struct {
	// Name of the referent.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#names
	Name string `json:"name"`
	// Tenant of the referent.
	Owner ObjectReferenceStatusOwner `json:"owner,omitempty"`

	ObjectReferenceAbstract        `json:",inline"`
	ObjectReferenceStatusCondition `json:",inline"`
}

type ObjectReferenceStatusOwner struct {
	// Name of the owning object.
	Name string `json:"name,omitempty"`
	// UID of the owning object.
	k8stypes.UID `json:"uid,omitempty" protobuf:"bytes,5,opt,name=uid"`
	// Scope of the owning object.
	Scope api.ResourceScope `json:"scope,omitempty"`
}

type ObjectReferenceStatusCondition struct {
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
}

type ObjectReference struct {
	ObjectReferenceAbstract `json:",inline"`

	// Label selector used to select the given resources in the given Namespace.
	Selector metav1.LabelSelector `json:"selector"`
}

func (in *ObjectReferenceStatus) String() string {
	return fmt.Sprintf(
		"Kind=%s,APIVersion=%s,Namespace=%s,Name=%s,Message=%s,Type=%s,Owner=%s,UID=%s,Scope=%s",
		in.Kind, in.APIVersion, in.Namespace, in.Name, in.Message, in.Type, in.Owner.Name, in.Owner.UID, in.Owner.Scope)
}

func (in *ObjectReferenceStatus) ParseFromString(value string) error {
	rawParts := strings.Split(value, ",")

	if len(rawParts) != 9 {
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
		case "Owner":
			in.Owner.Name = v
		case "UID":
			in.Owner.UID = k8stypes.UID(v)
		case "Scope":
			in.Owner.Scope = api.ResourceScope(v)

		default:
			return fmt.Errorf("unrecognized marker: %s", k)
		}
	}

	return nil
}
