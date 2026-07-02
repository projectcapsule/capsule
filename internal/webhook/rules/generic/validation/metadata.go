// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"fmt"
	"sort"

	"k8s.io/apimachinery/pkg/runtime/schema"

	apirules "github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

type metadataField string

const (
	metadataFieldLabel      metadataField = "label"
	metadataFieldAnnotation metadataField = "annotation"
)

type metadataEntry struct {
	Field    metadataField
	Key      string
	Value    string
	Path     string
	Present  bool
	Required bool
}

func (h *genericRules) validateMetadata(
	obj genericObject,
	gvk schema.GroupVersionKind,
	enforceBodies []*apirules.NamespaceRuleEnforceBody,
) (*ruleengine.Evaluation, error) {
	if obj == nil || len(enforceBodies) == 0 {
		return nil, nil
	}

	entries := h.controlledMetadataEntries(obj, gvk, enforceBodies)
	if len(entries) == 0 {
		return nil, nil
	}

	out := &ruleengine.Evaluation{}

	for i := range entries {
		entry := entries[i]

		if !entry.Present {
			if entry.Required {
				out.Blocking = metadataRequiredDecision(entry)

				return out, nil
			}

			continue
		}

		evaluation, err := evaluateGenericRules(
			obj,
			enforceBodies,
			h.metadataSet(gvk, entry),
		)
		if err != nil {
			return out, err
		}

		out.Append(evaluation)

		//nolint:nilerr
		if evaluation != nil && evaluation.BlockingError() != nil {
			return out, nil
		}
	}

	return out, nil
}

func metadataRequiredDecision(entry metadataEntry) *ruleengine.Decision {
	return &ruleengine.Decision{
		SetName:     metadataSetName(entry.Field),
		EventReason: events.ReasonForbiddenMetadata,
		Action:      apirules.ActionTypeDeny,
		Value: ruleengine.Value{
			Value: "",
			Path:  entry.Path,
		},
		Message: fmt.Sprintf(
			"metadata %s %q is required at %s",
			entry.Field,
			entry.Key,
			entry.Path,
		),
	}
}

func (h *genericRules) metadataSet(
	gvk schema.GroupVersionKind,
	entry metadataEntry,
) genericRuleSet[runtime.ExpressionMatch] {
	return genericRuleSet[runtime.ExpressionMatch]{
		Name:        metadataSetName(entry.Field),
		EventReason: events.ReasonForbiddenMetadata,

		Values: func(_ genericObject) []ruleengine.Value {
			return []ruleengine.Value{
				{
					Value: entry.Value,
					Path:  entry.Path,
				},
			}
		},

		Rules: func(enforce *apirules.NamespaceRuleEnforceBody) []runtime.ExpressionMatch {
			if enforce == nil || len(enforce.Metadata) == 0 {
				return nil
			}

			var out []runtime.ExpressionMatch

			for i := range enforce.Metadata {
				rule := enforce.Metadata[i]
				if !rule.MatchesGroupVersionKind(gvk) {
					continue
				}

				switch entry.Field {
				case metadataFieldLabel:
					policy, ok := rule.Labels[entry.Key]
					if ok {
						out = append(out, policy.Values...)
					}
				case metadataFieldAnnotation:
					policy, ok := rule.Annotations[entry.Key]
					if ok {
						out = append(out, policy.Values...)
					}
				}
			}

			return out
		},
		Matches: func(match runtime.ExpressionMatch, value ruleengine.Value) (ruleengine.Match, error) {
			matched, err := match.MatchesWithExpressionMatcher(h.regexCache, value.Value)
			if err != nil {
				return ruleengine.Match{}, err
			}

			return ruleengine.Match{
				Matched: matched,
			}, nil
		},
		RuleDescription:    runtime.DescribeExpressionMatch,
		AllowedDescription: "Allowed metadata values",
	}
}

func (h *genericRules) controlledMetadataEntries(
	obj genericObject,
	gvk schema.GroupVersionKind,
	enforceBodies []*apirules.NamespaceRuleEnforceBody,
) []metadataEntry {
	labels := obj.GetLabels()
	annotations := obj.GetAnnotations()

	seen := make(map[string]metadataEntry)

	for _, enforce := range enforceBodies {
		if enforce == nil || len(enforce.Metadata) == 0 {
			continue
		}

		action := enforce.Action.OrDefault()

		for i := range enforce.Metadata {
			rule := enforce.Metadata[i]
			if !rule.MatchesGroupVersionKind(gvk) {
				continue
			}

			for key, policy := range rule.Labels {
				if h.managedMetadata.HasLabel(key) {
					continue
				}

				value, exists := labels[key]
				required := action == apirules.ActionTypeAllow && policy.Required

				if !exists && !required {
					continue
				}

				path := metadataLabelPath(key)

				entry := seen[path]
				entry.Field = metadataFieldLabel
				entry.Key = key
				entry.Path = path
				entry.Present = exists
				entry.Value = value
				entry.Required = entry.Required || required

				seen[path] = entry
			}

			for key, policy := range rule.Annotations {
				if h.managedMetadata.HasAnnotation(key) {
					continue
				}

				value, exists := annotations[key]
				required := action == apirules.ActionTypeAllow && policy.Required

				if !exists && !required {
					continue
				}

				path := metadataAnnotationPath(key)

				entry := seen[path]
				entry.Field = metadataFieldAnnotation
				entry.Key = key
				entry.Path = path
				entry.Present = exists
				entry.Value = value
				entry.Required = entry.Required || required

				seen[path] = entry
			}
		}
	}

	if len(seen) == 0 {
		return nil
	}

	out := make([]metadataEntry, 0, len(seen))
	for _, entry := range seen {
		out = append(out, entry)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Path < out[j].Path
	})

	return out
}

func metadataSetName(field metadataField) string {
	switch field {
	case metadataFieldLabel:
		return "metadata label"
	case metadataFieldAnnotation:
		return "metadata annotation"
	default:
		return "metadata"
	}
}

func metadataLabelPath(key string) string {
	return fmt.Sprintf("metadata.labels[%q]", key)
}

func metadataAnnotationPath(key string) string {
	return fmt.Sprintf("metadata.annotations[%q]", key)
}
