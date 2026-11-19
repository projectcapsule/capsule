// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package processor

import (
	"k8s.io/apimachinery/pkg/types"

	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/gvk"
	"github.com/projectcapsule/capsule/pkg/runtime/ssa"
)

type healthTarget struct {
	group, kind, namespace, name string
	uid                          types.UID
}

type appliedHealthTargets map[healthTarget]*meta.ResourceHealthReference

func healthTargetFor(id gvk.ResourceID, uid types.UID) healthTarget {
	return healthTarget{group: id.Group, kind: id.Kind, namespace: id.Namespace, name: id.Name, uid: uid}
}

// Contributions keep separate ownership and pruning records, but health must
// describe the final physical target after all field managers finish applying.
// Successful content writes remain observations even if their metadata failed;
// application errors still prevent the source from completing its health phase.
func (targets appliedHealthTargets) record(id gvk.ResourceID, result ssa.ApplyResult, previous *meta.ResourceHealthReference) *meta.ResourceHealthReference {
	key := healthTargetFor(id, result.UID)
	if current := targets[key]; current != nil {
		previous = current
	}

	targets[key] = appliedHealthReference(result, previous)

	return targets[key]
}

func (targets appliedHealthTargets) finalize(processed meta.ProcessedItems) {
	for i := range processed {
		item := &processed[i]
		if item.Health == nil {
			continue
		}

		if final := targets[healthTargetFor(item.ResourceID, item.Health.UID)]; final != nil && *final != *item.Health {
			item.Health = new(*final)
		}
	}
}
