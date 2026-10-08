// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package events

import (
	"testing"

	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func TestDiscardRecorder(t *testing.T) {
	recorder := NewDiscardRecorder()
	// No client, queue or underlying Kubernetes recorder is needed, even for nil objects.
	recorder.Eventf(nil, nil, "Warning", "Denied", "Validate", "request %s", "dry")
	recorder.LabeledEvent(nil, "", "", "", "").Emit(t.Context())
	for _, name := range []string{"tenant-a", "tenant-b"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			obj := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "config", Namespace: name}}
			tnt := &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: name}}
			req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{UID: "request", UserInfo: authenticationv1.UserInfo{Username: name}}}
			event := recorder.LabeledEvent(obj, "Warning", "Denied", "Validate", "note").
				WithRelated(tnt).WithLabels(map[string]string{"custom": name}).
				WithAnnotations(map[string]string{"custom": name}).WithTenantLabel(tnt).WithRequestAnnotations(req)
			require.Nil(t, event.(*labeledEvent).emitter)
			require.Equal(t, obj, event.Regarding())
			require.Equal(t, tnt, event.Related())
			require.Equal(t, "Denied", event.Reason())
			require.Equal(t, "Validate", event.Action())
			require.Equal(t, "Warning", event.EventType())
			require.Equal(t, "note", event.Note())
			require.Equal(t, name, event.Labels()[meta.NewTenantLabel])
			require.Equal(t, name, event.Annotations()[meta.AuditUsername])
			require.Equal(t, "request", event.Annotations()[meta.AuditRequestUID])
			event.Labels()["custom"] = "changed"
			event.Annotations()["custom"] = "changed"
			require.Equal(t, name, event.Labels()["custom"])
			require.Equal(t, name, event.Annotations()["custom"])
			event.Emit(t.Context())
		})
	}
}
