// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ssa

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func TestProtectionComposesAcrossControllers(t *testing.T) {
	const replicationOwner = "2lclct9cwq6mg/default/tenant-a/0/raw-0/"
	const permitAccount = "system:serviceaccount:test:permit-runner"
	owners := map[string]string{
		meta.ValueControllerReplications: replicationOwner,
		"example-addon":                  meta.ResourceFieldOwner("example-addon/request"),
	}
	for _, first := range []string{meta.ValueControllerReplications, "example-addon"} {
		second := "example-addon"
		if first == second {
			second = meta.ValueControllerReplications
		}
		for _, condition := range []string{"false", "true", ""} {
			for _, cleanup := range []string{"disable", "disown", "orphan"} {
				t.Run(fmt.Sprintf("first=%s/condition=%s/cleanup=%s", first, condition, cleanup), func(t *testing.T) {
					existing := configMap("shared", map[string]any{"value": "retained"})
					// The first owner may predate independent protection markers.
					existing.SetLabels(map[string]string{meta.ProtectedByCapsuleLabel: first, meta.NewManagedByCapsuleLabel: first})
					existing.SetAnnotations(map[string]string{"example.org/service-account": permitAccount})
					fields := []metav1.ManagedFieldsEntry{managedField(owners[first]), managedField(owners[second])}
					for i := range fields {
						fields[i].FieldsV1 = &metav1.FieldsV1{Raw: []byte(`{"f:data":{"f:value":{}}}`)}
					}
					existing.SetManagedFields(fields)
					c := fake.NewClientBuilder().WithObjects(existing).WithReturnManagedFields().Build()
					manager := skippedPolicyManager(t)
					manager.ReplicationOwners = knownReplicationOwners(replicationOwner)
					manager.Metadata = Metadata{CreatedByValue: second, ManagedByValue: second, ProtectedByValue: second, ProtectionFieldOwnerPrefix: protectionOwnerPrefix(second)}
					if second == "example-addon" {
						manager.Metadata.ProtectedByServiceAccountAnnotation = "example.org/service-account"
						manager.Metadata.ProtectedByServiceAccount = permitAccount
					}
					_, err := manager.Apply(t.Context(), c, configMap("shared", map[string]any{"value": "retained"}), ApplyOptions{
						FieldOwner: owners[second], Condition: condition, Adopt: true, Force: true, Protect: true,
					})
					require.NoError(t, err)
					actual := existing.DeepCopy()
					require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(existing), actual))
					require.Equal(t, first, actual.GetLabels()[meta.ProtectedByCapsuleLabel], "must not replace another controller's legacy identity")
					require.Equal(t, meta.ValueTrue, actual.GetLabels()[meta.ProtectionLabelPrefix+second])
					require.Equal(t, permitAccount, actual.GetAnnotations()["example.org/service-account"])
					switch cleanup {
					case "disable":
						_, err = manager.Apply(t.Context(), c, configMap("shared", nil), ApplyOptions{FieldOwner: owners[second], Condition: "false", Adopt: true})
					case "disown":
						err = manager.Disown(t.Context(), c, existing, owners[second], nil)
					case "orphan":
						err = manager.Orphan(t.Context(), c, existing, owners[second], nil)
					}
					require.NoError(t, err)
					require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(existing), actual))
					require.Equal(t, first, actual.GetLabels()[meta.ProtectedByCapsuleLabel])
					require.NotContains(t, actual.GetLabels(), meta.ProtectionLabelPrefix+second)
					if first == "example-addon" {
						require.Equal(t, permitAccount, actual.GetAnnotations()["example.org/service-account"])
					} else {
						require.NotContains(t, actual.GetAnnotations(), "example.org/service-account")
					}
				})
			}
		}
	}
}

func TestProtectionRestoresSurvivingControllerIdentity(t *testing.T) {
	for _, departing := range []string{meta.ValueControllerReplications, "example-addon"} {
		t.Run(departing, func(t *testing.T) {
			survivor := "example-addon"
			if survivor == departing {
				survivor = meta.ValueControllerReplications
			}
			existing := configMap("shared", nil)
			existing.SetLabels(map[string]string{
				meta.ProtectedByCapsuleLabel:                   departing,
				meta.ReplicationProtectionLabel:                meta.ValueTrue,
				(meta.ProtectionLabelPrefix + "example-addon"): meta.ValueTrue,
			})
			existing.SetManagedFields([]metav1.ManagedFieldsEntry{managedField("departing")})
			c := fake.NewClientBuilder().WithObjects(existing).WithReturnManagedFields().Build()
			manager := Manager{Metadata: Metadata{ProtectedByValue: departing}}
			require.NoError(t, manager.Disown(t.Context(), c, existing, "departing", nil))
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(existing), existing))
			require.Equal(t, survivor, existing.GetLabels()[meta.ProtectedByCapsuleLabel])
			require.Equal(t, meta.ValueTrue, existing.GetLabels()[meta.ProtectionLabelPrefix+survivor])
			require.NotContains(t, existing.GetLabels(), meta.ProtectionLabelPrefix+departing)
		})
	}
}

func TestSharedRenderedResourceProtection(t *testing.T) {
	for _, cleanup := range []string{"orphan", "disown"} {
		t.Run(cleanup, func(t *testing.T) {
			a, b := meta.ResourceFieldOwner("example-addon/a"), meta.ResourceFieldOwner("example-addon/b")
			obj := configMap("shared", map[string]any{"outside": "retained"})
			obj.SetManagedFields([]metav1.ManagedFieldsEntry{managedField(a), managedField(b)})
			obj.SetLabels(map[string]string{(meta.ProtectionLabelPrefix + "example-addon"): meta.ValueTrue, meta.ProtectedByCapsuleLabel: "example-addon"})
			obj.SetAnnotations(map[string]string{"example.org/service-account": "system:serviceaccount:test:runner"})
			c := fake.NewClientBuilder().WithObjects(obj).WithReturnManagedFields().Build()
			m := Manager{Metadata: Metadata{ProtectedByValue: "example-addon",
				ProtectionFieldOwnerPrefix: meta.ResourceFieldOwner("example-addon/"), ProtectedByServiceAccountAnnotation: "example.org/service-account"}}
			require.True(t, m.hasOtherProtectionOwners(map[string]struct{}{a: {}, b: {}}, a))
			var err error
			if cleanup == "orphan" {
				err = m.Orphan(t.Context(), c, obj, a, nil)
			} else {
				err = m.Disown(t.Context(), c, obj, a, nil)
			}
			require.NoError(t, err)
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(obj), obj))
			require.Equal(t, meta.ValueTrue, obj.GetLabels()[(meta.ProtectionLabelPrefix+"example-addon")])
			require.Equal(t, "system:serviceaccount:test:runner", obj.GetAnnotations()["example.org/service-account"])
		})
	}
}

func protectionOwnerPrefix(controller string) string {
	if controller == "example-addon" {
		return meta.ResourceFieldOwner("example-addon/")
	}
	return ""
}

func TestProtectionMarkersAreControllerIndependent(t *testing.T) {
	manager := Manager{Metadata: Metadata{ProtectedByValue: "addon-a", ProtectionFieldOwnerPrefix: meta.ResourceFieldOwner("addon-a/")}}
	labels := map[string]string{meta.ProtectedByCapsuleLabel: "addon-a", meta.ProtectionLabelPrefix + "addon-a": meta.ValueTrue, meta.ProtectionLabelPrefix + "addon-z": meta.ValueTrue, meta.ProtectionLabelPrefix + "addon-b": meta.ValueTrue, "example.org/keep": "true"}
	obj := configMap("shared", nil)
	obj.SetLabels(labels)
	obj.SetManagedFields([]metav1.ManagedFieldsEntry{managedField(meta.ResourceFieldOwner("addon-a/uid"))})
	c := fake.NewClientBuilder().WithObjects(obj).WithReturnManagedFields().Build()
	require.NoError(t, manager.Disown(t.Context(), c, obj, meta.ResourceFieldOwner("addon-a/uid"), nil))
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(obj), obj))
	require.Equal(t, "addon-b", obj.GetLabels()[meta.ProtectedByCapsuleLabel])
	require.Equal(t, meta.ValueTrue, obj.GetLabels()[meta.ProtectionLabelPrefix+"addon-z"])
	require.Equal(t, "true", obj.GetLabels()["example.org/keep"])
	require.NotContains(t, obj.GetLabels(), meta.ProtectionLabelPrefix+"addon-a")
}
