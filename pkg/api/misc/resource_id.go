// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package misc

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type TenantResourceID struct {
	Tenant string `json:"tenant,omitempty"`
}

type TenantResourceIDWithOrigin struct {
	TenantResourceID `json:",inline"`

	Origin string `json:"origin,omitempty"`
}

// ResourceID represents the decomposed parts of a Kubernetes resource identity.
type ResourceID struct {
	TenantResourceID `json:",inline"`

	Group     string `json:"group,omitempty"`
	Version   string `json:"version,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
}

// ResourceKey builds the canonical key string used for maps/sets.
// Non-namespaced objects will have "_" as the namespace component.
func NewResourceID(u *unstructured.Unstructured, tenant string, origin string) ResourceID {
	gvk := u.GroupVersionKind()

	return ResourceID{
		Group:     gvk.Group,
		Version:   gvk.Version,
		Kind:      gvk.Kind,
		Name:      u.GetName(),
		Namespace: u.GetNamespace(),
		TenantResourceID: TenantResourceID{
			Tenant: tenant,
		},
	}
}

func (r ResourceID) GetName() string {
	return r.Name
}

func (r ResourceID) GetNamespace() string {
	return r.Namespace
}

// GVK returns the schema.GroupVersionKind of the resource.
func (r ResourceID) GetGVK() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   r.Group,
		Version: r.Version,
		Kind:    r.Kind,
	}
}

func (r ResourceID) GetKey(sep string) string {
	// Use a delimiter that won’t appear in fields normally; '\x1f' (unit separator) is great.
	if sep == "" {
		sep = "\x1f"
	}
	return fmt.Sprintf("%s%s%s%s%s%s%s%s%s%s%s%s",
		r.Group, sep,
		r.Version, sep,
		r.Kind, sep,
		r.Namespace, sep,
		r.Name, sep,
		r.Tenant, sep,
	)
}

func (r ResourceID) FieldOwner(sep string) string {
	// Use a delimiter that won’t appear in fields normally; '\x1f' (unit separator) is great.
	if sep == "" {
		sep = "/"
	}
	return fmt.Sprintf("%s%s%s%s",
		r.Namespace, sep,
		r.Tenant, sep,
	)
}
