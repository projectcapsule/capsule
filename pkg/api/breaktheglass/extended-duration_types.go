// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package breaktheglass

import (
	"encoding/json"
	"time"

	"github.com/xhit/go-str2duration/v2"
)

// ExtendedDuration is a custom duration field type that supports weeks, days, hours and minutes.
// +k8s:openapi-gen=true
// +kubebuilder:validation:Type=string
// +kubebuilder:validation:Pattern="^(([0-9]+(\\.[0-9]+)?)+(m|h|d|w))+$"

type ExtendedDuration time.Duration

// UnmarshalJSON implements the json.Unmarshaller interface.
func (d *ExtendedDuration) UnmarshalJSON(b []byte) error {
	var str string

	err := json.Unmarshal(b, &str)
	if err != nil {
		return err
	}

	pd, err := str2duration.ParseDuration(str)
	if err != nil {
		return err
	}

	*d = ExtendedDuration(pd)

	return nil
}

// String tostring.
func (d ExtendedDuration) String() string {
	return str2duration.String(time.Duration(d))
}

// MarshalJSON implements the json.Marshaler interface.
func (d ExtendedDuration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

// ToUnstructured implements the value.UnstructuredConverter interface.
func (d ExtendedDuration) ToUnstructured() any {
	return d.String()
}

// OpenAPISchemaType is used by the kube-openapi generator when constructing
// the OpenAPI spec of this type.
//
// See: https://github.com/kubernetes/kube-openapi/tree/master/pkg/generators
func (ExtendedDuration) OpenAPISchemaType() []string { return []string{"string"} }

// OpenAPISchemaFormat is used by the kube-openapi generator when constructing
// the OpenAPI spec of this type.
func (ExtendedDuration) OpenAPISchemaFormat() string { return "" }
