// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"fmt"
	"reflect"

	"k8s.io/apimachinery/pkg/runtime"
)

func MapMergeNoOverrite(dst, src map[string]string) {
	if len(src) == 0 {
		return
	}

	for k, v := range src {
		if _, exists := dst[k]; !exists {
			dst[k] = v
		}
	}
}

func MapEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}

	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}

	return true
}

func ToUnstructuredMap(obj any) (map[string]any, error) {
	m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	return m, nil
}

func Mapify(data any) map[string]any {
	result := make(map[string]any)
	v := reflect.ValueOf(data)

	// If the provided data is a pointer, resolve to the underlying value
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return result // Return empty map for nil pointers
		}

		v = v.Elem()
	}

	// Ensure we're working with a struct
	if v.Kind() == reflect.Struct {
		for i := range v.NumField() {
			field := v.Type().Field(i)

			// Skip unexported fields
			if field.PkgPath != "" {
				continue
			}

			value := v.Field(i)
			// Handle different types with recursive or base handling
			switch value.Kind() {
			case reflect.Ptr:
				if !value.IsNil() {
					result[field.Name] = Mapify(value.Interface())
				}
			case reflect.Struct:
				result[field.Name] = Mapify(value.Interface())
			case reflect.Slice:
				var slice []any

				for j := range value.Len() {
					item := value.Index(j)
					if item.Kind() == reflect.Struct {
						slice = append(slice, Mapify(item.Interface()))
					} else {
						slice = append(slice, item.Interface())
					}
				}

				result[field.Name] = slice
			case reflect.Map:
				mapResult := make(map[string]any)
				for _, key := range value.MapKeys() {
					mapResult[fmt.Sprint(key)] = value.MapIndex(key).Interface()
				}

				result[field.Name] = mapResult
			default:
				result[field.Name] = value.Interface()
			}
		}
	}

	return result
}
