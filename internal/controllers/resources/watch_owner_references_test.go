// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"fmt"
	"testing"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

func TestParentOwnerReferenceChangesAreNotBookkeeping(t *testing.T) {
	owner := metav1.OwnerReference{APIVersion: "v1", Kind: "ConfigMap", Name: "external", UID: "owner-one", Controller: new(false), BlockOwnerDeletion: new(false)}
	for _, resource := range []string{"tenantresources", "globaltenantresources"} {
		for _, change := range []string{"add", "remove", "replace-uid", "name", "kind", "api-version", "controller", "controller-omitted", "block-deletion", "unchanged-copy", "empty-equivalent", "capsule-managed"} {
			t.Run(resource+"/"+change, func(t *testing.T) {
				before := sourceObject("a")
				if change != "add" && change != "empty-equivalent" {
					before.SetOwnerReferences([]metav1.OwnerReference{owner})
				}
				after := before.DeepCopy()
				after.SetResourceVersion("new")
				switch change {
				case "add":
					after.SetOwnerReferences([]metav1.OwnerReference{owner})
				case "remove":
					after.SetOwnerReferences(nil)
				case "replace-uid":
					next := owner
					next.UID = "owner-two"
					after.SetOwnerReferences([]metav1.OwnerReference{next})
				case "name":
					next := owner
					next.Name = "other"
					after.SetOwnerReferences([]metav1.OwnerReference{next})
				case "kind":
					next := owner
					next.Kind = "Secret"
					after.SetOwnerReferences([]metav1.OwnerReference{next})
				case "api-version":
					next := owner
					next.APIVersion = "external.example/v1"
					after.SetOwnerReferences([]metav1.OwnerReference{next})
				case "controller":
					next := owner
					next.Controller = new(true)
					after.SetOwnerReferences([]metav1.OwnerReference{next})
				case "controller-omitted":
					next := owner
					next.Controller = nil
					after.SetOwnerReferences([]metav1.OwnerReference{next})
				case "block-deletion":
					next := owner
					next.BlockOwnerDeletion = new(true)
					after.SetOwnerReferences([]metav1.OwnerReference{next})
				case "empty-equivalent":
					after.Object["metadata"].(map[string]any)["ownerReferences"] = []any{}
				case "capsule-managed":
					next := owner
					next.APIVersion = capsulev1beta2.GroupVersion.String()
					next.Kind = "Tenant"
					after.SetOwnerReferences([]metav1.OwnerReference{next})
				}
				gvr := schema.GroupVersionResource{Group: capsulev1beta2.GroupVersion.Group, Version: "v1beta2", Resource: resource}
				quiet := change == "unchanged-copy" || change == "empty-equivalent"
				if bookkeepingEvent(gvr, before, after) != quiet {
					t.Fatalf("owner-reference change %s classified incorrectly", change)
				}
				f := watchFixtureFor(t, 0)
				f.watcher.enqueueEvent(t.Context(), gvr, resourceWatchIdentity{}, admissionv1.Update, before, after)
				want := 1
				if quiet || change == "capsule-managed" {
					want = 0
				}
				if len(f.watcher.events) != want {
					t.Fatalf("queued %d events, want %d", len(f.watcher.events), want)
				}
				for len(f.watcher.events) > 0 {
					f.watcher.releaseEvent(<-f.watcher.events)
				}
				if change != "add" && change != "empty-equivalent" && before.GetOwnerReferences()[0].UID != "owner-one" {
					t.Fatal("comparison mutated old ownership")
				}
			})
		}
	}
}

func BenchmarkParentBookkeepingOwnership(b *testing.B) {
	for _, owners := range []int{0, 1, 8} {
		b.Run(fmt.Sprintf("owners=%d", owners), func(b *testing.B) {
			before := sourceObject("a")
			for i := range owners {
				before.SetOwnerReferences(append(before.GetOwnerReferences(), metav1.OwnerReference{APIVersion: "v1", Kind: "ConfigMap", Name: fmt.Sprint(i), UID: "owner", Controller: new(false)}))
			}
			after := before.DeepCopy()
			after.SetResourceVersion("new")
			gvr := schema.GroupVersionResource{Group: capsulev1beta2.GroupVersion.Group, Version: "v1beta2", Resource: "tenantresources"}
			b.ReportAllocs()
			for b.Loop() {
				if !bookkeepingEvent(gvr, before, after) {
					b.Fatal("bookkeeping unexpectedly eligible")
				}
			}
		})
	}
}
