// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package generic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func TestReplicationMarkerIntroduction(t *testing.T) {
	for _, global := range []bool{false, true} {
		for _, create := range []bool{false, true} {
			for key, value := range map[string]string{
				meta.ReplicationProtectionLabel: meta.ValueTrue,
				meta.ProtectedByCapsuleLabel:    meta.ValueControllerReplications,
				meta.CreatedByCapsuleLabel:      meta.ValueControllerReplications,
				meta.NewManagedByCapsuleLabel:   meta.ValueControllerReplications,
			} {
				for _, state := range []string{"owner", "first apply", "other tenant", "missing parent", "replaced parent", "deleted parent", "read error", "no manager", "unchanged"} {
					t.Run(fmt.Sprintf("global=%t/create=%t/%s/%s", global, create, key, state), func(t *testing.T) {
						c, req := replicationAdmissionFixture(t, global, false, 2)
						next := &metav1.PartialObjectMetadata{}
						require.NoError(t, json.Unmarshal(req.Object.Raw, next))
						next.Labels = map[string]string{key: value}
						if state != "owner" {
							req.UserInfo.Username = "system:serviceaccount:tenant-a:runner"
						}
						if state == "other tenant" {
							req.UserInfo.Username = "system:serviceaccount:other-tenant-1:runner"
						}
						if state == "no manager" {
							next.ManagedFields = nil
						}
						if state == "unchanged" {
							next.Labels = nil
						}
						var err error
						req.Object.Raw, err = json.Marshal(next)
						require.NoError(t, err)
						reads := 0
						reader := interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
							reads++
							if state == "read error" {
								return errors.New("parent unavailable")
							}
							if state == "missing parent" {
								key.Name = "missing"
							}
							if err := c.Get(ctx, key, obj, opts...); err != nil {
								return err
							}
							if state == "replaced parent" {
								obj.SetUID("replacement")
							}
							if state == "deleted parent" {
								obj.SetDeletionTimestamp(new(metav1.Now()))
							}
							// The first write must not depend on target status being published.
							switch obj := obj.(type) {
							case *capsulev1beta2.GlobalTenantResource:
								obj.Status.ProcessedItems = nil
							case *capsulev1beta2.TenantResource:
								obj.Status.ProcessedItems = nil
							}
							return nil
						}})
						call := ReplicaHandler().OnUpdate(c, reader, admission.NewDecoder(c.Scheme()), nil)
						if create {
							call = ReplicaHandler().OnCreate(c, reader, admission.NewDecoder(c.Scheme()), nil)
						}
						response := call(t.Context(), req)
						if state == "first apply" || state == "unchanged" {
							require.Nil(t, response)
						} else {
							require.NotNil(t, response)
							require.False(t, response.Allowed)
							if state == "read error" {
								require.EqualValues(t, 500, response.Result.Code)
							} else {
								require.EqualValues(t, 403, response.Result.Code)
								require.Contains(t, response.Result.Message, "replication metadata")
							}
						}
						if state == "owner" || state == "unchanged" || state == "no manager" {
							require.Zero(t, reads)
						} else {
							require.Equal(t, 1, reads)
						}
					})
				}
			}
		}
	}
}
