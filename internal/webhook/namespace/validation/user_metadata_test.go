// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/users"
	"github.com/projectcapsule/capsule/pkg/utils"
)

func TestUserMetadataAllowsOnlyTenantNodeSelectorRepair(t *testing.T) {
	t.Parallel()

	for _, userType := range []users.AdmissionUserType{users.AdmissionUserCapsule, users.AdmissionUserAdmin, users.AdmissionUserUnknown} {
		for _, previous := range []string{"", "pool=foreign", "pool=oil"} {
			for _, replacement := range []string{"", "pool=other", "pool=oil"} {
				t.Run(string(userType)+"/"+previous+"->"+replacement, func(t *testing.T) {
					tnt, oldNs := namespaceSecurityObjects()
					tnt.Spec.NodeSelector = map[string]string{"pool": "oil"}
					if previous != "" {
						oldNs.Annotations[utils.NodeSelectorAnnotation] = previous
					}
					newNs := oldNs.DeepCopy()
					delete(newNs.Annotations, utils.NodeSelectorAnnotation)
					if replacement != "" {
						newNs.Annotations[utils.NodeSelectorAnnotation] = replacement
					}
					denial := ""
					if replacement == "" {
						denial = "cannot be removed"
					} else if replacement != "pool=oil" {
						denial = "cannot be updated"
					}
					recorder := events.NewEventRecorder(nil, logr.Discard(), nil, nil)
					response := UserMetadataHandler().OnUpdate(nil, nil, users.AdmissionUser{Type: userType}, newNs, oldNs, nil, recorder, tnt)(context.Background(), admission.Request{})
					assertNamespaceResponse(t, response, denial)
				})
			}
		}
	}
}
