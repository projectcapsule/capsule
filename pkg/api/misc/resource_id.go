// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package misc

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// ResourceID represents the decomposed parts of a Kubernetes resource identity.
type ResourceID struct {
	Group     string `json:"group,omitempty"`
	Version   string `json:"version,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Tenant    string `json:"tenant,omitempty"`
	Index     string `json:"index,omitempty"`
}

// ResourceKey builds the canonical key string used for maps/sets.
// Non-namespaced objects will have "_" as the namespace component.
func NewResourceID(u *unstructured.Unstructured, tenant string, index string) ResourceID {
	gvk := u.GroupVersionKind()

	return ResourceID{
		Group:     gvk.Group,
		Version:   gvk.Version,
		Kind:      gvk.Kind,
		Name:      u.GetName(),
		Namespace: u.GetNamespace(),
		Tenant:    tenant,
		Index:     index,
	}
}

// ParseResourceKey parses a key created by ResourceKey back into structured form.
func ParseResourceKey(key string) (ResourceID, error) {
	parts := strings.Split(key, ",")
	if len(parts) != 5 {
		return ResourceID{}, fmt.Errorf("invalid resource key: %q", key)
	}
	id := ResourceID{
		Group:     parts[0],
		Version:   parts[1],
		Kind:      parts[2],
		Namespace: parts[3],
		Name:      parts[4],
	}
	if id.Namespace == "_" {
		id.Namespace = ""
	}
	return id, nil
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

// Key returns the string key form again (inverse of ParseResourceKey).
func (r ResourceID) GetIndex() string {
	i := r.Index
	if i == "" {
		i = r.GetKey()
	}

	return i
}

// Key returns the string key form again (inverse of ParseResourceKey).
func (r ResourceID) GetKey() string {
	ns := r.Namespace
	if ns == "" {
		ns = "_"
	}
	return fmt.Sprintf("%s/%s/%s/%s/%s",
		r.Group, r.Version, r.Kind, ns, r.Name)
}
