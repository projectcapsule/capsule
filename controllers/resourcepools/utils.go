// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package resourcepools

import (
	"fmt"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

// Returns for an item it's name as Kubernetes object
func resourceQuotaItemName(quota *capsulev1beta2.ResourcePool) string {
	// Generate a name using the tenant name and item name
	return fmt.Sprintf("capsule-pool-%s", quota.Name)
}

//func (r *Manager) emitEvent(object runtime.Object, namespace string, res controllerutil.OperationResult, msg string, err error) {
//	eventType := corev1.EventTypeNormal
//
//	if err != nil {
//		eventType = corev1.EventTypeWarning
//		res = "Error"
//	}
//
//	r.Recorder.AnnotatedEventf(object, map[string]string{"OperationResult": string(res)}, eventType, namespace, msg)
//}
