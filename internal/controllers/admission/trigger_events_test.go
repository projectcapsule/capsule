// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package admission

import (
	"fmt"
	"slices"
	"testing"
	"time"

	registrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func TestTriggerRegistrationEvents(t *testing.T) {
	for _, global := range []bool{false, true} {
		for _, tc := range []struct {
			name   string
			change func(*capsulev1beta2.TenantResourceCommonSpec, client.Object)
			want   bool
		}{
			{"unrelated-resources", func(s *capsulev1beta2.TenantResourceCommonSpec, _ client.Object) {
				s.Resources = []capsulev1beta2.ResourceSpec{{}}
			}, false},
			{"resync", func(s *capsulev1beta2.TenantResourceCommonSpec, _ client.Object) { s.ResyncPeriod.Duration = time.Hour }, false},
			{"interval", func(s *capsulev1beta2.TenantResourceCommonSpec, _ client.Object) {
				s.TriggerMinInterval.Duration = time.Second
			}, false},
			{"audience", func(s *capsulev1beta2.TenantResourceCommonSpec, _ client.Object) {
				s.Triggers[0].Audience = []rules.Audience{{Kind: rules.AudienceKindUser, Name: "alice"}}
			}, false},
			{"name", func(s *capsulev1beta2.TenantResourceCommonSpec, _ client.Object) { s.Triggers[0].Name = "renamed" }, false},
			{"duplicate-predicate", func(s *capsulev1beta2.TenantResourceCommonSpec, _ client.Object) {
				other := *s.Triggers[0].DeepCopy()
				other.Name = "duplicate"
				s.Triggers = append(s.Triggers, other)
			}, false},
			{"object-selector", func(s *capsulev1beta2.TenantResourceCommonSpec, _ client.Object) {
				s.Triggers[0].ObjectSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"env": "prod"}}
			}, true},
			{"conditions", func(s *capsulev1beta2.TenantResourceCommonSpec, _ client.Object) {
				s.Triggers[0].MatchConditions = []registrationv1.MatchCondition{{Name: "match", Expression: "true"}}
			}, true},
			{"resource", func(s *capsulev1beta2.TenantResourceCommonSpec, _ client.Object) {
				s.Triggers[0].Resources = []string{"secrets"}
			}, true},
			{"last-trigger-removed", func(s *capsulev1beta2.TenantResourceCommonSpec, _ client.Object) { s.Triggers = nil }, true},
			{"cordon", func(s *capsulev1beta2.TenantResourceCommonSpec, _ client.Object) { s.Cordoned = new(true) }, true},
			{"deletion", func(_ *capsulev1beta2.TenantResourceCommonSpec, o client.Object) {
				stamp := metav1.Now()
				o.SetDeletionTimestamp(&stamp)
			}, true},
		} {
			t.Run(fmt.Sprintf("global=%t/%s", global, tc.name), func(t *testing.T) {
				var old client.Object = &capsulev1beta2.TenantResource{Name: "source", Namespace: "tenant-a", Generation: 1, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{Triggers: []capsulev1beta2.TenantResourceTrigger{eventTrigger()}}}}
				if global {
					old = &capsulev1beta2.GlobalTenantResource{Name: "source", Generation: 1, Spec: capsulev1beta2.GlobalTenantResourceSpec{TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{Triggers: []capsulev1beta2.TenantResourceTrigger{eventTrigger()}}}}
				}
				current := old.DeepCopyObject().(client.Object)
				current.SetGeneration(2)
				tc.change(triggerSpec(current), current)
				p := triggerSourceChanged()
				if got := p.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: current}); got != tc.want {
					t.Fatalf("registration rebuild=%t want=%t", got, tc.want)
				}
				if tc.name == "deletion" {
					current.SetGeneration(1)
					if !p.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: current}) {
						t.Fatal("unchanged-generation deletion discarded")
					}
				}
				if !p.Create(event.CreateEvent{Object: old}) || !p.Delete(event.DeleteEvent{Object: current}) {
					if tc.name != "last-trigger-removed" {
						t.Fatal("active lifecycle event discarded")
					}
				}
				inactive := old.DeepCopyObject().(client.Object)
				triggerSpec(inactive).Triggers = nil
				edited := inactive.DeepCopyObject().(client.Object)
				edited.SetGeneration(2)
				triggerSpec(edited).Resources = []capsulev1beta2.ResourceSpec{{}}
				if p.Create(event.CreateEvent{Object: inactive}) || p.Delete(event.DeleteEvent{Object: inactive}) || p.Update(event.UpdateEvent{ObjectOld: inactive, ObjectNew: edited}) {
					t.Fatal("triggerless activity enqueued rebuild")
				}
				if !p.Update(event.UpdateEvent{ObjectOld: inactive, ObjectNew: old}) {
					t.Fatal("first trigger addition discarded")
				}
				cordoned := old.DeepCopyObject().(client.Object)
				triggerSpec(cordoned).Cordoned = new(true)
				if !p.Update(event.UpdateEvent{ObjectOld: cordoned, ObjectNew: old}) {
					t.Fatal("uncordon discarded")
				}
				if p.Update(event.UpdateEvent{}) || p.Generic(event.GenericEvent{Object: old}) {
					t.Fatal("irrelevant event admitted")
				}
			})
		}
	}
}

func BenchmarkTriggerRegistrationEvents(b *testing.B) {
	for _, count := range []int{0, 32} {
		old := &capsulev1beta2.TenantResource{Name: "source", Namespace: "tenant-a", Generation: 1}
		for i := range count {
			trigger := eventTrigger()
			trigger.Name = fmt.Sprintf("t%d", i)
			for j := range 60 {
				trigger.MatchConditions = append(trigger.MatchConditions, registrationv1.MatchCondition{Name: fmt.Sprintf("c%d", j), Expression: fmt.Sprintf("request.name != '%d-%d'", i, j)})
			}
			old.Spec.Triggers = append(old.Spec.Triggers, trigger)
		}
		for _, kind := range []string{"status", "unrelated-spec", "reordered", "audience", "native-rule"} {
			b.Run(fmt.Sprintf("triggers=%d/%s", count, kind), func(b *testing.B) {
				current := old.DeepCopy()
				if kind != "status" {
					current.Generation++
				}
				current.Spec.ResyncPeriod.Duration = time.Hour
				if count > 0 && kind == "audience" {
					current.Spec.Triggers[0].Audience = []rules.Audience{{Kind: rules.AudienceKindUser, Name: "alice"}}
				}
				if count > 0 && kind == "native-rule" {
					current.Spec.Triggers[0].Resources = []string{"secrets"}
				}
				// Reordering preserves the same set of native predicates.
				if count > 1 && kind == "reordered" {
					slices.Reverse(current.Spec.Triggers)
				}
				p := triggerSourceChanged()
				e := event.UpdateEvent{ObjectOld: old, ObjectNew: current}
				expected := count > 0 && kind == "native-rule"
				b.ReportAllocs()
				for b.Loop() {
					if p.Update(e) != expected {
						b.Fatal("incorrect event selection")
					}
				}
			})
		}
	}
}
