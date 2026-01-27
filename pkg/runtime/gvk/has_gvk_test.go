// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package gvk_test

import (
	"errors"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/projectcapsule/capsule/pkg/runtime/gvk"
)

// stubRESTMapper implements meta.RESTMapper (a "fat" interface), but we only
// care about RESTMapping() for these tests.
type stubRESTMapper struct {
	mapping *meta.RESTMapping
	err     error

	lastGK      schema.GroupKind
	lastVersion string
	calls       int
}

func (s *stubRESTMapper) KindFor(resource schema.GroupVersionResource) (schema.GroupVersionKind, error) {
	return schema.GroupVersionKind{}, errors.New("not implemented")
}

func (s *stubRESTMapper) KindsFor(resource schema.GroupVersionResource) ([]schema.GroupVersionKind, error) {
	return nil, errors.New("not implemented")
}

func (s *stubRESTMapper) ResourceFor(input schema.GroupVersionResource) (schema.GroupVersionResource, error) {
	return schema.GroupVersionResource{}, errors.New("not implemented")
}

func (s *stubRESTMapper) ResourcesFor(input schema.GroupVersionResource) ([]schema.GroupVersionResource, error) {
	return nil, errors.New("not implemented")
}

func (s *stubRESTMapper) RESTMapping(gk schema.GroupKind, versions ...string) (*meta.RESTMapping, error) {
	s.calls++
	s.lastGK = gk
	if len(versions) > 0 {
		s.lastVersion = versions[0]
	}
	if s.err != nil {
		return nil, s.err
	}
	return s.mapping, nil
}

func (s *stubRESTMapper) RESTMappings(gk schema.GroupKind, versions ...string) ([]*meta.RESTMapping, error) {
	return nil, errors.New("not implemented")
}

func (s *stubRESTMapper) ResourceSingularizer(resource string) (string, error) {
	return "", errors.New("not implemented")
}

func TestHasGVK(t *testing.T) {
	t.Parallel()

	gvkT := schema.GroupVersionKind{
		Group:   "capsule.clastix.io",
		Version: "v1beta2",
		Kind:    "RuleStatus",
	}

	t.Run("returns true when RESTMapping succeeds", func(t *testing.T) {
		t.Parallel()

		m := &stubRESTMapper{
			mapping: &meta.RESTMapping{
				Resource: schema.GroupVersionResource{
					Group:    gvkT.Group,
					Version:  gvkT.Version,
					Resource: "rulestatuses",
				},
				GroupVersionKind: gvkT,
			},
		}

		got := gvk.HasGVK(m, gvkT)
		if got != true {
			t.Fatalf("expected true, got %v", got)
		}
		if m.calls != 1 {
			t.Fatalf("expected RESTMapping to be called once, calls=%d", m.calls)
		}
		if m.lastGK != gvkT.GroupKind() {
			t.Fatalf("expected GroupKind=%v, got %v", gvkT.GroupKind(), m.lastGK)
		}
		if m.lastVersion != gvkT.Version {
			t.Fatalf("expected version=%q, got %q", gvkT.Version, m.lastVersion)
		}
	})

	t.Run("returns false on NoMatchError", func(t *testing.T) {
		t.Parallel()

		noMatch := &meta.NoKindMatchError{
			GroupKind:        gvkT.GroupKind(),
			SearchedVersions: []string{gvkT.Version},
		}

		m := &stubRESTMapper{err: noMatch}

		got := gvk.HasGVK(m, gvkT)
		if got != false {
			t.Fatalf("expected false, got %v", got)
		}
		if m.calls != 1 {
			t.Fatalf("expected RESTMapping to be called once, calls=%d", m.calls)
		}
	})

	t.Run("returns false on generic error (and does not panic)", func(t *testing.T) {
		t.Parallel()

		m := &stubRESTMapper{err: errors.New("boom")}

		got := gvk.HasGVK(m, gvkT)
		if got != false {
			t.Fatalf("expected false, got %v", got)
		}
		if m.calls != 1 {
			t.Fatalf("expected RESTMapping to be called once, calls=%d", m.calls)
		}
	})
}
