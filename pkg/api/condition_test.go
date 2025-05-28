// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestUpdateCondition(t *testing.T) {
	now := metav1.Now()

	t.Run("no update when all relevant fields match", func(t *testing.T) {
		c := &Condition{
			Type:    "Ready",
			Status:  "True",
			Reason:  "Success",
			Message: "All good",
		}

		updated := c.UpdateCondition(metav1.Condition{
			Type:               "Ready",
			Status:             "True",
			Reason:             "Success",
			Message:            "All good",
			LastTransitionTime: now,
		})

		assert.False(t, updated)
	})

	t.Run("update occurs on message change", func(t *testing.T) {
		c := &Condition{
			Type:    "Ready",
			Status:  "True",
			Reason:  "Success",
			Message: "Old message",
		}

		updated := c.UpdateCondition(metav1.Condition{
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
		c := &Condition{
			Type:    "Ready",
			Status:  "False",
			Reason:  "Pending",
			Message: "Not ready yet",
		}

		updated := c.UpdateCondition(metav1.Condition{
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
