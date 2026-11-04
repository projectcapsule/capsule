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
	"k8s.io/apimachinery/pkg/runtime/schema"
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

func appliesToVolumeSnapshots(body *rules.NamespaceRuleEnforceBody) bool {
	return body.GetStorageClassTargets(rules.StorageTargetVolumeSnapshot)
}

func hasClassRules(bodies []*rules.NamespaceRuleEnforceBody) bool {
	return slices.ContainsFunc(bodies, func(body *rules.NamespaceRuleEnforceBody) bool {
		return body != nil && appliesToVolumeSnapshots(body)
	})
}

// everyRequest returns the enforcement rules without an audience or
// conditions. They apply to every request in the namespace, so also to the
// patch with which the snapshot-controller sets a default class.
func everyRequest(bodies []*rules.NamespaceRuleBodyNamespace) map[*rules.NamespaceRuleEnforceBody]bool {
	out := make(map[*rules.NamespaceRuleEnforceBody]bool, len(bodies))

	for _, body := range bodies {
		if body != nil && body.Enforce != nil && len(body.Audience) == 0 && len(body.Enforce.Conditions) == 0 {
			out[body.Enforce] = true
		}
	}

	return out
}

// classReader reads the snapshot's VolumeSnapshotClass at most once, and only
// when a selector has to be matched. A missing class never matches a selector.
type classReader struct {
	reader client.Reader
	name   string

	read  bool
	class *snapshotv1.VolumeSnapshotClass
	err   error
}

func (c *classReader) get(ctx context.Context) (*snapshotv1.VolumeSnapshotClass, error) {
	if c.read {
		return c.class, c.err
	}

	c.read = true

	class := &snapshotv1.VolumeSnapshotClass{}
	if err := c.reader.Get(ctx, types.NamespacedName{Name: c.name}, class); err != nil {
		if !apierrors.IsNotFound(err) {
			c.err = fmt.Errorf("get VolumeSnapshotClass %q: %w", c.name, err)
		}

		return nil, c.err
	}

	c.class = class

	return class, nil
}

func (h *volumeSnapshotRules) evaluate(
	ctx context.Context,
	obj, old *snapshotv1.VolumeSnapshot,
	classes *classReader,
	bodies []*rules.NamespaceRuleBodyNamespace,
	enforce []*rules.NamespaceRuleEnforceBody,
) (*ruleengine.Evaluation, error) {
	name := ptr.Deref(obj.Spec.VolumeSnapshotClassName, "")
	if name == "" {
		return missingClass(obj, old, enforce, everyRequest(bodies)), nil
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

			return rules.StorageClassesToMatch(body.Action, body.Storage.Classes, h.regexCache, name)
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

			if !match.HasSelector() {
				return ruleengine.Match{}, nil
			}

			selector, err := h.selectors.GetOrCompile(&match.LabelSelector)
			if err != nil {
				return ruleengine.Match{}, err
			}

			class, err := classes.get(ctx)
			if err != nil || class == nil {
				return ruleengine.Match{}, err
			}

			return ruleengine.Match{Matched: selector.Matches(labels.Set(class.Labels)), MatchedValue: match.Describe()}, nil
		},
		RuleDescription:    rules.StorageClassMatch.Describe,
		AllowedDescription: "Allowed classes",
	})
}

// missingClass handles a snapshot without a class like a missing ingress
// hostname: allow and deny rules reject it, audit rules record it, because
// the snapshot-controller would set its driver's default class after
// admission.
//
// A pre-provisioned snapshot never gets a default class. A VolumeGroupSnapshot
// member doesn't either, but the snapshot-controller only recognizes an owner
// with its own API version (v1beta1, v1beta2 or v1, depending on its release),
// and the requester sets the owners. So only rules without an audience or
// conditions, which also check the patch that sets a default class, let
// members through, and updates that keep the class missing.
func missingClass(obj, old *snapshotv1.VolumeSnapshot, enforce []*rules.NamespaceRuleEnforceBody, everyRequest map[*rules.NamespaceRuleEnforceBody]bool) *ruleengine.Evaluation {
	evaluation := &ruleengine.Evaluation{}
	if obj.Spec.Source.VolumeSnapshotContentName != nil {
		return evaluation
	}

	exempt := isVolumeGroupSnapshotMember(obj.OwnerReferences) || (old != nil && old.Spec.VolumeSnapshotClassName == nil)
	value := ruleengine.Value{Path: classPath}
	audited, enforced := false, false

	for _, body := range enforce {
		if body == nil || !appliesToVolumeSnapshots(body) || (exempt && everyRequest[body]) {
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

// isVolumeGroupSnapshotMember reports whether a VolumeGroupSnapshot of any
// API version owns the snapshot.
func isVolumeGroupSnapshotMember(owners []metav1.OwnerReference) bool {
	return slices.ContainsFunc(owners, func(owner metav1.OwnerReference) bool {
		gv, err := schema.ParseGroupVersion(owner.APIVersion)

		return err == nil && gv.Group == groupsnapshotv1.GroupName && owner.Kind == "VolumeGroupSnapshot"
	})
}
