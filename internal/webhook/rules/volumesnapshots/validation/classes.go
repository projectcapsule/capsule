// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"fmt"
	"slices"

	groupsnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumegroupsnapshot/v1"
	snapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

const (
	classSetName = "volume snapshot class"
	classPath    = "spec.volumeSnapshotClassName"
)

var volumeGroupSnapshotAPIVersion = groupsnapshotv1.SchemeGroupVersion.String()

func appliesToVolumeSnapshots(body *rules.NamespaceRuleEnforceBody) bool {
	return body.GetStorageClassTargets(rules.StorageTargetVolumeSnapshot)
}

func hasClassRules(bodies []*rules.NamespaceRuleEnforceBody) bool {
	return slices.ContainsFunc(bodies, func(body *rules.NamespaceRuleEnforceBody) bool {
		return body != nil && appliesToVolumeSnapshots(body)
	})
}

// readClass reads the VolumeSnapshotClass only when an applicable match
// selects classes by label. A missing class never matches a selector.
func readClass(
	ctx context.Context,
	reader client.Reader,
	obj *snapshotv1.VolumeSnapshot,
	enforce []*rules.NamespaceRuleEnforceBody,
) (*snapshotv1.VolumeSnapshotClass, error) {
	name := ptr.Deref(obj.Spec.VolumeSnapshotClassName, "")
	if name == "" || !hasClassSelector(enforce) {
		return nil, nil
	}

	class := &snapshotv1.VolumeSnapshotClass{}
	if err := reader.Get(ctx, types.NamespacedName{Name: name}, class); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("get VolumeSnapshotClass %q: %w", name, err)
	}

	return class, nil
}

func hasClassSelector(enforce []*rules.NamespaceRuleEnforceBody) bool {
	for _, body := range enforce {
		if body == nil || !appliesToVolumeSnapshots(body) {
			continue
		}

		if slices.ContainsFunc(body.Storage.Classes, rules.StorageClassMatch.HasSelector) {
			return true
		}
	}

	return false
}

func (h *volumeSnapshotRules) evaluate(
	obj *snapshotv1.VolumeSnapshot,
	class *snapshotv1.VolumeSnapshotClass,
	enforce []*rules.NamespaceRuleEnforceBody,
) (*ruleengine.Evaluation, error) {
	name := ptr.Deref(obj.Spec.VolumeSnapshotClassName, "")
	if name == "" {
		return missingClass(obj, enforce), nil
	}

	return ruleengine.EvaluateEnforce(obj, enforce, ruleengine.Set[rules.StorageClassMatch, *snapshotv1.VolumeSnapshot]{
		Name:        classSetName,
		EventReason: events.ReasonForbiddenVolumeSnapshotClass,
		Values: func(*snapshotv1.VolumeSnapshot) []ruleengine.Value {
			return []ruleengine.Value{{Value: name, Path: classPath}}
		},
		Rules: func(body *rules.NamespaceRuleEnforceBody) []rules.StorageClassMatch {
			if !appliesToVolumeSnapshots(body) {
				return nil
			}

			return body.Storage.Classes
		},
		Matches: func(match rules.StorageClassMatch, value ruleengine.Value) (ruleengine.Match, error) {
			if match.HasNameMatch() {
				matched, err := match.NameMatch().MatchesWithExpressionMatcher(h.regexCache, value.Value)
				if err != nil {
					return ruleengine.Match{}, err
				}

				if matched {
					return ruleengine.Match{Matched: true, MatchedValue: match.Describe()}, nil
				}
			}

			if !match.HasSelector() || class == nil {
				return ruleengine.Match{}, nil
			}

			selector, err := h.selectors.GetOrCompile(&match.LabelSelector)
			if err != nil {
				return ruleengine.Match{}, err
			}

			return ruleengine.Match{Matched: selector.Matches(labels.Set(class.Labels)), MatchedValue: match.Describe()}, nil
		},
		RuleDescription:    rules.StorageClassMatch.Describe,
		AllowedDescription: "Allowed classes",
	})
}

// missingClass handles a snapshot without a class like a missing ingress
// hostname: allow and deny rules reject it, audit rules record it. The
// snapshot-controller would otherwise set its driver's default class after
// admission, which no rule has checked.
func missingClass(obj *snapshotv1.VolumeSnapshot, enforce []*rules.NamespaceRuleEnforceBody) *ruleengine.Evaluation {
	evaluation := &ruleengine.Evaluation{}
	if !defaultsClass(obj.Spec.Source.VolumeSnapshotContentName, obj.OwnerReferences) {
		return evaluation
	}

	value := ruleengine.Value{Path: classPath}
	audited, enforced := false, false

	for _, body := range enforce {
		if body == nil || !appliesToVolumeSnapshots(body) {
			continue
		}

		if body.Action.OrDefault() == rules.ActionTypeAudit {
			audited = true
		} else {
			enforced = true
		}
	}

	if audited {
		evaluation.Audits = append(evaluation.Audits, &ruleengine.Decision{
			SetName:     classSetName,
			EventReason: events.ReasonNamespaceRuleAudit,
			Action:      rules.ActionTypeAudit,
			Value:       value,
			Message:     fmt.Sprintf("empty volume snapshot class detected at %s by audit namespace rule", classPath),
		})
	}

	if enforced {
		evaluation.Blocking = &ruleengine.Decision{
			SetName:     classSetName,
			EventReason: events.ReasonMissingVolumeSnapshotClass,
			Action:      rules.ActionTypeDeny,
			Value:       value,
			Message:     fmt.Sprintf("volume snapshot class is required at %s because class rules target %s", classPath, rules.StorageTargetVolumeSnapshot),
		}
	}

	return evaluation
}

// defaultsClass reports whether the snapshot-controller assigns a default
// class to a snapshot without one. It skips pre-provisioned snapshots and the
// members of a VolumeGroupSnapshot.
func defaultsClass(contentName *string, owners []metav1.OwnerReference) bool {
	return contentName == nil && !isVolumeGroupSnapshotMember(owners)
}

// isVolumeGroupSnapshotMember mirrors the snapshot-controller's own check:
// the first VolumeGroupSnapshot owner with its API version, v1 since v8.6,
// decides, and it needs a name. Owners with other versions are not exempt,
// because a controller that doesn't recognize them defaults the class.
func isVolumeGroupSnapshotMember(owners []metav1.OwnerReference) bool {
	for _, owner := range owners {
		if owner.Kind == "VolumeGroupSnapshot" && owner.APIVersion == volumeGroupSnapshotAPIVersion {
			return owner.Name != ""
		}
	}

	return false
}
