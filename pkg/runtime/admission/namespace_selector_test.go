// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package admission_test

import (
	"fmt"
	"reflect"
	"testing"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/projectcapsule/capsule/pkg/runtime/admission"
)

func TestWebhookNamespaceSelectorConjunction(t *testing.T) {
	for _, kind := range []string{"mutating", "validating"} {
		t.Run(kind, func(t *testing.T) {
			selector := &metav1.LabelSelector{
				MatchLabels:      map[string]string{"global": "yes", "webhook-type": kind, "hook": "yes"},
				MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "excluded", Operator: metav1.LabelSelectorOpDoesNotExist}},
			}
			before := selector.DeepCopy()
			url := "https://capsule.example.com"
			client := &admissionv1.WebhookClientConfig{URL: &url}
			var got *metav1.LabelSelector
			if kind == "mutating" {
				out, err := admission.NewMutatingWebhook(&admission.MutatingWebhook{NamespaceSelector: selector}, client, nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				got = out.NamespaceSelector
			} else {
				out, err := admission.NewValidatingWebhook(&admission.ValidatingWebhook{NamespaceSelector: selector}, client, nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				got = out.NamespaceSelector
			}
			if !reflect.DeepEqual(before, got) || !reflect.DeepEqual(before, selector) {
				t.Fatal("webhook conversion must preserve every selector constraint")
			}
			compiled, err := metav1.LabelSelectorAsSelector(got)
			if err != nil {
				t.Fatal(err)
			}
			for _, tc := range []struct {
				name   string
				labels labels.Set
				want   bool
			}{
				{"all scopes", labels.Set{"global": "yes", "webhook-type": kind, "hook": "yes"}, true},
				{"missing global", labels.Set{"webhook-type": kind, "hook": "yes"}, false},
				{"different type", labels.Set{"global": "yes", "webhook-type": "other", "hook": "yes"}, false},
				{"missing hook", labels.Set{"global": "yes", "webhook-type": kind}, false},
				{"expression exclusion", labels.Set{"global": "yes", "webhook-type": kind, "hook": "yes", "excluded": ""}, false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					if compiled.Matches(tc.labels) != tc.want {
						t.Fatalf("selector match for %v must be %t", tc.labels, tc.want)
					}
				})
			}
		})
	}
}

func BenchmarkWebhookNamespaceSelectorConjunction(b *testing.B) {
	for _, count := range []int{0, 3, 32} {
		for _, selected := range []bool{true, false} {
			b.Run(fmt.Sprintf("requirements=%d/selected=%t", count, selected), func(b *testing.B) {
				selector := &metav1.LabelSelector{}
				values := labels.Set{}
				for i := range count {
					key := fmt.Sprintf("scope-%02d", i)
					selector.MatchExpressions = append(selector.MatchExpressions, metav1.LabelSelectorRequirement{Key: key, Operator: metav1.LabelSelectorOpIn, Values: []string{"yes"}})
					values[key] = "yes"
				}
				if !selected && count > 0 {
					delete(values, "scope-00")
				}
				compiled, err := metav1.LabelSelectorAsSelector(selector)
				if err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				for b.Loop() {
					if compiled.Matches(values) != (selected || count == 0) {
						b.Fatal("unexpected namespace selection")
					}
				}
			})
		}
	}
}
