// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package meta

import (
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNewManagedMetadata(t *testing.T) {
	t.Parallel()

	t.Run("contains default managed labels", func(t *testing.T) {
		t.Parallel()

		m := NewManagedMetadata(nil, nil)

		for _, key := range []string{
			ResourcesLabel,
			TenantNameLabel,
			TenantLabel,
			NewTenantLabel,
			ResourcePoolLabel,
			FreezeLabel,
			OwnerPromotionLabel,
			ServiceAccountPromotionLabel,
			CordonedLabel,
			CapsuleNameLabel,
			CreatedByCapsuleLabel,
			CustomResourcesLabel,
			NewManagedByCapsuleLabel,
			ManagedByCapsuleLabel,
			LimitRangeLabel,
			NetworkPolicyLabel,
			ResourceQuotaLabel,
			RolebindingLabel,
		} {
			if !m.HasLabel(key) {
				t.Fatalf("expected managed label %q", key)
			}
		}
	})

	t.Run("contains default managed annotations", func(t *testing.T) {
		t.Parallel()

		m := NewManagedMetadata(nil, nil)

		for _, key := range []string{
			ReleaseAnnotation,
			ReconcileAnnotation,
			AvailableIngressClassesAnnotation,
			AvailableIngressClassesRegexpAnnotation,
			AvailableStorageClassesAnnotation,
			AvailableStorageClassesRegexpAnnotation,
			AllowedRegistriesAnnotation,
			AllowedRegistriesRegexpAnnotation,
			ForbiddenNamespaceLabelsAnnotation,
			ForbiddenNamespaceLabelsRegexpAnnotation,
			ForbiddenNamespaceAnnotationsAnnotation,
			ForbiddenNamespaceAnnotationsRegexpAnnotation,
			ProtectedTenantAnnotation,
		} {
			if !m.HasAnnotation(key) {
				t.Fatalf("expected managed annotation %q", key)
			}
		}
	})

	t.Run("contains default managed annotation prefixes", func(t *testing.T) {
		t.Parallel()

		m := NewManagedMetadata(nil, nil)

		for _, key := range []string{
			ResourceQuotaAnnotationPrefix + "cpu",
			ResourceQuotaAnnotationPrefix + "memory",
			ResourceUsedAnnotationPrefix + "cpu",
			ResourceUsedAnnotationPrefix + "memory",
		} {
			if !m.HasAnnotation(key) {
				t.Fatalf("expected managed annotation prefix match for %q", key)
			}
		}
	})

	t.Run("adds custom managed labels and annotations", func(t *testing.T) {
		t.Parallel()

		m := NewManagedMetadata(
			[]string{
				"example.corp/label",
				" example.corp/trimmed-label ",
				"",
				"   ",
			},
			[]string{
				"example.corp/annotation",
				" example.corp/trimmed-annotation ",
				"",
				"   ",
			},
		)

		for _, key := range []string{
			"example.corp/label",
			"example.corp/trimmed-label",
		} {
			if !m.HasLabel(key) {
				t.Fatalf("expected custom managed label %q", key)
			}
		}

		for _, key := range []string{
			"example.corp/annotation",
			"example.corp/trimmed-annotation",
		} {
			if !m.HasAnnotation(key) {
				t.Fatalf("expected custom managed annotation %q", key)
			}
		}

		if m.HasLabel("") {
			t.Fatalf("empty label must not be managed")
		}

		if m.HasAnnotation("") {
			t.Fatalf("empty annotation must not be managed")
		}
	})

	t.Run("matching is exact and case-sensitive", func(t *testing.T) {
		t.Parallel()

		m := NewManagedMetadata(
			[]string{
				"example.corp/managed",
			},
			[]string{
				"example.corp/managed",
			},
		)

		if !m.HasLabel("example.corp/managed") {
			t.Fatalf("expected exact label match")
		}

		if m.HasLabel("Example.Corp/managed") {
			t.Fatalf("expected label lookup to be case-sensitive")
		}

		if m.HasLabel(" example.corp/managed ") {
			t.Fatalf("expected label lookup not to trim lookup key")
		}

		if !m.HasAnnotation("example.corp/managed") {
			t.Fatalf("expected exact annotation match")
		}

		if m.HasAnnotation("Example.Corp/managed") {
			t.Fatalf("expected annotation lookup to be case-sensitive")
		}

		if m.HasAnnotation(" example.corp/managed ") {
			t.Fatalf("expected annotation lookup not to trim lookup key")
		}
	})

	t.Run("unknown metadata is not managed", func(t *testing.T) {
		t.Parallel()

		m := NewManagedMetadata(nil, nil)

		if m.HasLabel("example.corp/not-managed") {
			t.Fatalf("unexpected managed label")
		}

		if m.HasAnnotation("example.corp/not-managed") {
			t.Fatalf("unexpected managed annotation")
		}
	})
}

func TestStringSet(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   []string
		want map[string]struct{}
	}{
		{
			name: "empty input returns nil",
			in:   nil,
			want: nil,
		},
		{
			name: "trims values skips blanks and deduplicates",
			in: []string{
				"alpha",
				" alpha ",
				"",
				"   ",
				"beta",
			},
			want: map[string]struct{}{
				"alpha": {},
				"beta":  {},
			},
		},
		{
			name: "case sensitive",
			in: []string{
				"alpha",
				"Alpha",
			},
			want: map[string]struct{}{
				"alpha": {},
				"Alpha": {},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := stringSet(tt.in...)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("expected %#v, got %#v", tt.want, got)
			}
		})
	}
}

func TestCompactStrings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "empty input returns nil",
			in:   nil,
			want: nil,
		},
		{
			name: "trims values and skips blanks",
			in: []string{
				"alpha",
				" alpha ",
				"",
				"   ",
				"beta",
			},
			want: []string{
				"alpha",
				"alpha",
				"beta",
			},
		},
		{
			name: "does not deduplicate prefixes",
			in: []string{
				"alpha/",
				"alpha/",
			},
			want: []string{
				"alpha/",
				"alpha/",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := compactStrings(tt.in...)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("expected %#v, got %#v", tt.want, got)
			}
		})
	}
}

func TestAddStrings(t *testing.T) {
	t.Parallel()

	set := map[string]struct{}{
		"existing": {},
	}

	addStrings(
		set,
		"alpha",
		" alpha ",
		"",
		"   ",
		"beta",
	)

	want := map[string]struct{}{
		"existing": {},
		"alpha":    {},
		"beta":     {},
	}

	if !reflect.DeepEqual(set, want) {
		t.Fatalf("expected %#v, got %#v", want, set)
	}
}

func TestManagedMetadataAddMethods(t *testing.T) {
	t.Parallel()

	m := ManagedMetadata{
		labels:      map[string]struct{}{},
		annotations: map[string]struct{}{},
	}

	m.addLabels("example.corp/label", " example.corp/trimmed-label ", "")
	m.addAnnotations("example.corp/annotation", " example.corp/trimmed-annotation ", "")

	if !m.HasLabel("example.corp/label") {
		t.Fatalf("expected added label")
	}

	if !m.HasLabel("example.corp/trimmed-label") {
		t.Fatalf("expected trimmed added label")
	}

	if !m.HasAnnotation("example.corp/annotation") {
		t.Fatalf("expected added annotation")
	}

	if !m.HasAnnotation("example.corp/trimmed-annotation") {
		t.Fatalf("expected trimmed added annotation")
	}

	if m.HasLabel("") {
		t.Fatalf("empty label must not be added")
	}

	if m.HasAnnotation("") {
		t.Fatalf("empty annotation must not be added")
	}
}

func TestManagedMetadataHasAnnotationPrefix(t *testing.T) {
	t.Parallel()

	m := ManagedMetadata{
		annotations: map[string]struct{}{
			"example.corp/exact": {},
		},
		annotationPrefixes: []string{
			"example.corp/prefix/",
		},
	}

	tests := []struct {
		name string
		key  string
		want bool
	}{
		{
			name: "exact annotation",
			key:  "example.corp/exact",
			want: true,
		},
		{
			name: "prefix annotation",
			key:  "example.corp/prefix/value",
			want: true,
		},
		{
			name: "prefix itself also matches",
			key:  "example.corp/prefix/",
			want: true,
		},
		{
			name: "similar prefix does not match",
			key:  "example.corp/prefix-other/value",
			want: false,
		},
		{
			name: "unknown annotation",
			key:  "example.corp/unknown",
			want: false,
		},
		{
			name: "case sensitive prefix",
			key:  "Example.Corp/prefix/value",
			want: false,
		},
		{
			name: "empty key",
			key:  "",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := m.HasAnnotation(tt.key)
			if got != tt.want {
				t.Fatalf("expected %t, got %t", tt.want, got)
			}
		})
	}
}

func TestObjectSkipRuleShouldSkip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		rule        ObjectSkipRule
		labels      map[string]string
		annotations map[string]string
		want        bool
	}{
		{
			name: "empty rule does not skip",
			rule: ObjectSkipRule{},
			want: false,
		},
		{
			name: "matching label skips",
			rule: ObjectSkipRule{
				Labels: map[string]string{
					"managed-by": "controller",
				},
			},
			labels: map[string]string{
				"managed-by": "controller",
			},
			want: true,
		},
		{
			name: "missing label does not skip",
			rule: ObjectSkipRule{
				Labels: map[string]string{
					"managed-by": "controller",
				},
			},
			labels: nil,
			want:   false,
		},
		{
			name: "non matching label value does not skip",
			rule: ObjectSkipRule{
				Labels: map[string]string{
					"managed-by": "controller",
				},
			},
			labels: map[string]string{
				"managed-by": "human",
			},
			want: false,
		},
		{
			name: "label match is case-sensitive",
			rule: ObjectSkipRule{
				Labels: map[string]string{
					"managed-by": "controller",
				},
			},
			labels: map[string]string{
				"managed-by": "Controller",
			},
			want: false,
		},
		{
			name: "matching annotation skips",
			rule: ObjectSkipRule{
				Annotations: map[string]string{
					"example.corp/skip": "true",
				},
			},
			annotations: map[string]string{
				"example.corp/skip": "true",
			},
			want: true,
		},
		{
			name: "missing annotation does not skip",
			rule: ObjectSkipRule{
				Annotations: map[string]string{
					"example.corp/skip": "true",
				},
			},
			annotations: nil,
			want:        false,
		},
		{
			name: "non matching annotation value does not skip",
			rule: ObjectSkipRule{
				Annotations: map[string]string{
					"example.corp/skip": "true",
				},
			},
			annotations: map[string]string{
				"example.corp/skip": "false",
			},
			want: false,
		},
		{
			name: "all labels must match",
			rule: ObjectSkipRule{
				Labels: map[string]string{
					"managed-by": "controller",
					"owner":      "capsule",
				},
			},
			labels: map[string]string{
				"managed-by": "controller",
				"owner":      "other",
			},
			want: false,
		},
		{
			name: "all annotations must match",
			rule: ObjectSkipRule{
				Annotations: map[string]string{
					"example.corp/skip":  "true",
					"example.corp/owner": "capsule",
				},
			},
			annotations: map[string]string{
				"example.corp/skip":  "true",
				"example.corp/owner": "other",
			},
			want: false,
		},
		{
			name: "labels and annotations must both match",
			rule: ObjectSkipRule{
				Labels: map[string]string{
					"managed-by": "controller",
				},
				Annotations: map[string]string{
					"example.corp/skip": "true",
				},
			},
			labels: map[string]string{
				"managed-by": "controller",
			},
			annotations: map[string]string{
				"example.corp/skip": "true",
			},
			want: true,
		},
		{
			name: "matching label but missing required annotation does not skip",
			rule: ObjectSkipRule{
				Labels: map[string]string{
					"managed-by": "controller",
				},
				Annotations: map[string]string{
					"example.corp/skip": "true",
				},
			},
			labels: map[string]string{
				"managed-by": "controller",
			},
			annotations: nil,
			want:        false,
		},
		{
			name: "missing key must not match empty expected value",
			rule: ObjectSkipRule{
				Labels: map[string]string{
					"empty": "",
				},
			},
			labels: nil,
			want:   false,
		},
		{
			name: "present empty value matches empty expected value",
			rule: ObjectSkipRule{
				Labels: map[string]string{
					"empty": "",
				},
			},
			labels: map[string]string{
				"empty": "",
			},
			want: true,
		},
		{
			name: "extra labels and annotations do not prevent match",
			rule: ObjectSkipRule{
				Labels: map[string]string{
					"managed-by": "controller",
				},
			},
			labels: map[string]string{
				"managed-by": "controller",
				"extra":      "value",
			},
			annotations: map[string]string{
				"extra": "value",
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.rule.ShouldSkip(tt.labels, tt.annotations)
			if got != tt.want {
				t.Fatalf("expected %t, got %t", tt.want, got)
			}
		})
	}
}

func TestObjectSkipRuleShouldSkipNilReceiver(t *testing.T) {
	t.Parallel()

	var rule *ObjectSkipRule

	if rule.ShouldSkip(
		map[string]string{
			"managed-by": "controller",
		},
		nil,
	) {
		t.Fatalf("nil rule must not skip")
	}
}

func TestDefaultObjectSkipRules(t *testing.T) {
	t.Parallel()

	rules := DefaultObjectSkipRules()
	if len(rules) != 2 {
		t.Fatalf("expected three default skip rules, got %d", len(rules))
	}

	if got := rules[0].Labels[NewManagedByCapsuleLabel]; got != ValueController {
		t.Fatalf("expected default skip label %q=%q, got %q", NewManagedByCapsuleLabel, ValueController, got)
	}

	if got := rules[1].Labels[NewManagedByCapsuleLabel]; got != ValueControllerResources {
		t.Fatalf("expected legacy default skip label %q=%q, got %q", NewManagedByCapsuleLabel, ValueControllerResources, got)
	}

	for _, rule := range rules {
		if len(rule.Annotations) != 0 {
			t.Fatalf("expected no default skip annotations, got %#v", rule.Annotations)
		}
	}
}

func TestShouldSkipObjectByRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		obj   *metav1.PartialObjectMetadata
		rules []ObjectSkipRule
		want  bool
	}{
		{
			name:  "nil object does not skip",
			obj:   nil,
			rules: DefaultObjectSkipRules(),
			want:  false,
		},
		{
			name: "nil rules do not skip",
			obj: objectWithMetadata(
				map[string]string{
					NewManagedByCapsuleLabel: ValueController,
				},
				nil,
			),
			rules: nil,
			want:  false,
		},
		{
			name: "empty rules do not skip",
			obj: objectWithMetadata(
				map[string]string{
					NewManagedByCapsuleLabel: ValueController,
				},
				nil,
			),
			rules: []ObjectSkipRule{},
			want:  false,
		},
		{
			name: "default Capsule controller-managed object skips",
			obj: objectWithMetadata(
				map[string]string{
					NewManagedByCapsuleLabel: ValueController,
				},
				nil,
			),
			rules: DefaultObjectSkipRules(),
			want:  true,
		},
		{
			name: "legacy Capsule resources object skips",
			obj: objectWithMetadata(
				map[string]string{
					NewManagedByCapsuleLabel: ValueControllerResources,
				},
				nil,
			),
			rules: DefaultObjectSkipRules(),
			want:  true,
		},
		{
			name: "default skip is case-sensitive",
			obj: objectWithMetadata(
				map[string]string{
					NewManagedByCapsuleLabel: "Controller",
				},
				nil,
			),
			rules: DefaultObjectSkipRules(),
			want:  false,
		},
		{
			name: "plain managed-by controller is not skipped by default",
			obj: objectWithMetadata(
				map[string]string{
					"managed-by": "controller",
				},
				nil,
			),
			rules: DefaultObjectSkipRules(),
			want:  false,
		},
		{
			name: "non matching object does not skip",
			obj: objectWithMetadata(
				map[string]string{
					NewManagedByCapsuleLabel: "human",
				},
				nil,
			),
			rules: DefaultObjectSkipRules(),
			want:  false,
		},
		{
			name: "any matching rule skips",
			obj: objectWithMetadata(
				map[string]string{
					"app": "demo",
				},
				map[string]string{
					"example.corp/skip": "true",
				},
			),
			rules: []ObjectSkipRule{
				{
					Labels: map[string]string{
						"app": "other",
					},
				},
				{
					Annotations: map[string]string{
						"example.corp/skip": "true",
					},
				},
			},
			want: true,
		},
		{
			name: "object without labels and annotations does not skip",
			obj:  objectWithMetadata(nil, nil),
			rules: []ObjectSkipRule{
				{
					Labels: map[string]string{
						"managed-by": "controller",
					},
				},
			},
			want: false,
		},
		{
			name: "empty skip rule does not skip object",
			obj: objectWithMetadata(
				map[string]string{
					"app": "demo",
				},
				nil,
			),
			rules: []ObjectSkipRule{
				{},
			},
			want: false,
		},
		{
			name: "custom plain managed-by rule skips",
			obj: objectWithMetadata(
				map[string]string{
					"managed-by": "controller",
				},
				nil,
			),
			rules: []ObjectSkipRule{
				{
					Labels: map[string]string{
						"managed-by": "controller",
					},
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ShouldSkipObjectByRules(tt.obj, tt.rules)
			if got != tt.want {
				t.Fatalf("expected %t, got %t", tt.want, got)
			}
		})
	}
}

func objectWithMetadata(
	labels map[string]string,
	annotations map[string]string,
) *metav1.PartialObjectMetadata {
	return &metav1.PartialObjectMetadata{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      labels,
			Annotations: annotations,
		},
	}
}
