// Copyright 2020-2025 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package meta_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/projectcapsule/capsule/pkg/api/meta"
)

// helper
func makeCond(tpe, status, reason, msg string, gen int64) meta.Condition {
	return meta.Condition{
		Type:               tpe,
		Status:             metav1.ConditionStatus(status),
		Reason:             reason,
		Message:            msg,
		ObservedGeneration: gen,
		LastTransitionTime: metav1.NewTime(time.Unix(0, 0)),
	}
}

func TestConditionList_GetConditionByType(t *testing.T) {
	t.Run("returns matching condition", func(t *testing.T) {
		list := meta.ConditionList{
			makeCond("Ready", "False", "Init", "starting", 1),
			makeCond("Synced", "True", "Ok", "done", 2),
		}

		got := list.GetConditionByType("Synced")
		assert.NotNil(t, got)
		assert.Equal(t, "Synced", got.Type)
		assert.Equal(t, metav1.ConditionTrue, got.Status)
		assert.Equal(t, "Ok", got.Reason)
		assert.Equal(t, "done", got.Message)
	})

	t.Run("returns nil when not found", func(t *testing.T) {
		list := meta.ConditionList{
			makeCond("Ready", "False", "Init", "starting", 1),
		}
		assert.Nil(t, list.GetConditionByType("Missing"))
	})

	t.Run("returned pointer refers to slice element (not copy)", func(t *testing.T) {
		list := meta.ConditionList{
			makeCond("Ready", "False", "Init", "starting", 1),
			makeCond("Synced", "True", "Ok", "done", 2),
		}
		ptr := list.GetConditionByType("Ready")
		assert.NotNil(t, ptr)

		ptr.Message = "mutated"
		// This asserts GetConditionByType returns &list[i] (via index),
		// not &cond where cond is the range variable copy.
		assert.Equal(t, "mutated", list[0].Message)
	})
}

func TestConditionList_UpdateConditionByType(t *testing.T) {
	now := metav1.Now()

	t.Run("updates existing condition in place", func(t *testing.T) {
		list := meta.ConditionList{
			makeCond("Ready", "False", "Init", "starting", 1),
			makeCond("Synced", "True", "Ok", "done", 2),
		}
		beforeLen := len(list)

		list.UpdateConditionByType(meta.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionTrue,
			Reason:             "Reconciled",
			Message:            "ready now",
			ObservedGeneration: 3,
			LastTransitionTime: now,
		})

		assert.Equal(t, beforeLen, len(list))
		got := list.GetConditionByType("Ready")
		assert.NotNil(t, got)
		assert.Equal(t, metav1.ConditionTrue, got.Status)
		assert.Equal(t, "Reconciled", got.Reason)
		assert.Equal(t, "ready now", got.Message)
		assert.Equal(t, int64(3), got.ObservedGeneration)
	})

	t.Run("appends when condition type not present", func(t *testing.T) {
		list := meta.ConditionList{
			makeCond("Ready", "True", "Ok", "ready", 1),
		}
		beforeLen := len(list)

		list.UpdateConditionByType(meta.Condition{
			Type:               "Synced",
			Status:             metav1.ConditionTrue,
			Reason:             "Done",
			Message:            "synced",
			ObservedGeneration: 2,
			LastTransitionTime: now,
		})

		assert.Equal(t, beforeLen+1, len(list))
		got := list.GetConditionByType("Synced")
		assert.NotNil(t, got)
		assert.Equal(t, metav1.ConditionTrue, got.Status)
		assert.Equal(t, "Done", got.Reason)
		assert.Equal(t, "synced", got.Message)
		assert.Equal(t, int64(2), got.ObservedGeneration)
	})
}

func TestConditionList_RemoveConditionByType(t *testing.T) {
	t.Run("removes all conditions with matching type", func(t *testing.T) {
		list := meta.ConditionList{
			makeCond("A", "True", "x", "m1", 1),
			makeCond("B", "True", "y", "m2", 1),
			makeCond("A", "False", "z", "m3", 2),
		}
		list.RemoveConditionByType(meta.Condition{Type: "A"})

		assert.Len(t, list, 1)
		assert.Equal(t, "B", list[0].Type)
	})

	t.Run("no-op when type not present", func(t *testing.T) {
		orig := meta.ConditionList{
			makeCond("A", "True", "x", "m1", 1),
		}
		list := append(meta.ConditionList{}, orig...) // copy

		list.RemoveConditionByType(meta.Condition{Type: "Missing"})

		assert.Equal(t, orig, list)
	})

	t.Run("nil receiver is safe", func(t *testing.T) {
		var list *meta.ConditionList // nil receiver
		assert.NotPanics(t, func() {
			list.RemoveConditionByType(meta.Condition{Type: "X"})
		})
	})
}

func TestUpdateCondition(t *testing.T) {
	now := metav1.Now()

	t.Run("no update when all relevant fields match", func(t *testing.T) {
		c := &meta.Condition{
			Type:    "Ready",
			Status:  "True",
			Reason:  "Success",
			Message: "All good",
		}

		updated := c.UpdateCondition(meta.Condition{
			Type:               "Ready",
			Status:             "True",
			Reason:             "Success",
			Message:            "All good",
			LastTransitionTime: now,
		})

		assert.False(t, updated)
	})

	t.Run("update occurs on message change", func(t *testing.T) {
		c := &meta.Condition{
			Type:    "Ready",
			Status:  "True",
			Reason:  "Success",
			Message: "Old message",
		}

		updated := c.UpdateCondition(meta.Condition{
			Type:               "Ready",
			Status:             "True",
			Reason:             "Success",
			Message:            "New message",
			LastTransitionTime: now,
		})

		assert.True(t, updated)
		assert.Equal(t, "New message", c.Message)
	})

	t.Run("update occurs on status change", func(t *testing.T) {
		c := &meta.Condition{
			Type:    "Ready",
			Status:  "False",
			Reason:  "Pending",
			Message: "Not ready yet",
		}

		updated := c.UpdateCondition(meta.Condition{
			Type:               "Ready",
			Status:             "True",
			Reason:             "Success",
			Message:            "Ready",
			LastTransitionTime: now,
		})

		assert.True(t, updated)
		assert.Equal(t, "True", string(c.Status))
		assert.Equal(t, "Success", c.Reason)
		assert.Equal(t, "Ready", c.Message)
	})
}
