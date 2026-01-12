// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package utils_test

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/projectcapsule/capsule/pkg/utils"
)

func TestIsNamespaceSelectedBySelector_NilSelectorMatchesAll(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "ns1",
			Labels: map[string]string{"env": "prod"},
		},
	}

	ok, err := utils.IsNamespaceSelectedBySelector(ns, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !ok {
		t.Fatalf("expected match=true for nil selector, got false")
	}
}

func TestIsNamespaceSelectedBySelector_MatchLabels(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "ns1",
			Labels: map[string]string{"env": "prod", "team": "a"},
		},
	}

	selector := &metav1.LabelSelector{
		MatchLabels: map[string]string{"env": "prod"},
	}

	ok, err := utils.IsNamespaceSelectedBySelector(ns, selector)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !ok {
		t.Fatalf("expected match=true, got false")
	}
}

func TestIsNamespaceSelectedBySelector_NoMatchLabels(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "ns1",
			Labels: map[string]string{"env": "dev"},
		},
	}

	selector := &metav1.LabelSelector{
		MatchLabels: map[string]string{"env": "prod"},
	}

	ok, err := utils.IsNamespaceSelectedBySelector(ns, selector)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if ok {
		t.Fatalf("expected match=false, got true")
	}
}

func TestIsNamespaceSelectedBySelector_MatchExpressions_In(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "ns1",
			Labels: map[string]string{"tier": "backend"},
		},
	}

	selector := &metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{
				Key:      "tier",
				Operator: metav1.LabelSelectorOpIn,
				Values:   []string{"backend", "worker"},
			},
		},
	}

	ok, err := utils.IsNamespaceSelectedBySelector(ns, selector)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !ok {
		t.Fatalf("expected match=true, got false")
	}
}

func TestIsNamespaceSelectedBySelector_MatchExpressions_NotIn(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "ns1",
			Labels: map[string]string{"tier": "frontend"},
		},
	}

	selector := &metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{
				Key:      "tier",
				Operator: metav1.LabelSelectorOpNotIn,
				Values:   []string{"backend", "worker"},
			},
		},
	}

	ok, err := utils.IsNamespaceSelectedBySelector(ns, selector)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !ok {
		t.Fatalf("expected match=true (frontend not in backend/worker), got false")
	}
}

func TestIsNamespaceSelectedBySelector_EmptyLabels_NoMatch(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "ns1",
			Labels: nil,
		},
	}

	selector := &metav1.LabelSelector{
		MatchLabels: map[string]string{"env": "prod"},
	}

	ok, err := utils.IsNamespaceSelectedBySelector(ns, selector)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if ok {
		t.Fatalf("expected match=false with missing label, got true")
	}
}

func TestIsNamespaceSelectedBySelector_InvalidSelectorReturnsError(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "ns1",
			Labels: map[string]string{"env": "prod"},
		},
	}

	// Invalid: In operator requires non-empty Values
	selector := &metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{
				Key:      "env",
				Operator: metav1.LabelSelectorOpIn,
				Values:   nil,
			},
		},
	}

	ok, err := utils.IsNamespaceSelectedBySelector(ns, selector)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if ok {
		t.Fatalf("expected match=false on error, got true")
	}
}
