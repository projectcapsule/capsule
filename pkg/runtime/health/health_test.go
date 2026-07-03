// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package health_test

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/runtime/health"
)

const (
	syncedCurrent = "status.conditions.filter(e, e.type == 'Synced').all(e, e.status == 'True')"
	syncedFailed  = "status.conditions.filter(e, e.type == 'Synced').exists(e, e.status == 'False')"
)

func obj(apiVersion, kind string, content map[string]interface{}) *unstructured.Unstructured {
	u := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": apiVersion,
		"kind":       kind,
		"metadata":   map[string]interface{}{"name": "test", "namespace": "default"},
	}}
	for k, v := range content {
		u.Object[k] = v
	}

	return u
}

func condition(condType, condStatus string) map[string]interface{} {
	return map[string]interface{}{"type": condType, "status": condStatus}
}

func TestChecker_CEL(t *testing.T) {
	checks := []capsulev1beta2.HealthCheckSpec{{
		APIVersion: "example.com/v1",
		Kind:       "Foo",
		Current:    syncedCurrent,
		Failed:     syncedFailed,
	}}

	cases := []struct {
		name string
		obj  *unstructured.Unstructured
		want health.Status
	}{
		{
			name: "current matches -> healthy",
			obj: obj("example.com/v1", "Foo", map[string]interface{}{
				"status": map[string]interface{}{"conditions": []interface{}{condition("Synced", "True")}},
			}),
			want: health.StatusHealthy,
		},
		{
			name: "failed matches -> unhealthy",
			obj: obj("example.com/v1", "Foo", map[string]interface{}{
				"status": map[string]interface{}{"conditions": []interface{}{condition("Synced", "False")}},
			}),
			want: health.StatusUnhealthy,
		},
		{
			name: "neither matches -> progressing",
			obj: obj("example.com/v1", "Foo", map[string]interface{}{
				"status": map[string]interface{}{"conditions": []interface{}{condition("Synced", "Unknown")}},
			}),
			want: health.StatusProgressing,
		},
		{
			name: "absent status must not panic -> progressing",
			obj:  obj("example.com/v1", "Foo", nil),
			want: health.StatusProgressing,
		},
	}

	checker, err := health.NewChecker(checks)
	if err != nil {
		t.Fatalf("NewChecker: %v", err)
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := checker.Check(tc.obj).Status; got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestChecker_InProgress(t *testing.T) {
	// inProgress is evaluated before current: an object matching both must be
	// reported progressing, not healthy.
	checks := []capsulev1beta2.HealthCheckSpec{{
		APIVersion: "example.com/v1",
		Kind:       "Foo",
		Current:    "data['ready'] == 'true'",
		InProgress: "data['settling'] == 'true'",
	}}

	checker, err := health.NewChecker(checks)
	if err != nil {
		t.Fatalf("NewChecker: %v", err)
	}

	cases := []struct {
		name string
		data map[string]interface{}
		want health.Status
	}{
		{
			name: "current only -> healthy",
			data: map[string]interface{}{"ready": "true", "settling": "false"},
			want: health.StatusHealthy,
		},
		{
			name: "current and inProgress overlap -> progressing",
			data: map[string]interface{}{"ready": "true", "settling": "true"},
			want: health.StatusProgressing,
		},
		{
			name: "inProgress only -> progressing",
			data: map[string]interface{}{"ready": "false", "settling": "true"},
			want: health.StatusProgressing,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := obj("example.com/v1", "Foo", map[string]interface{}{"data": tc.data})
			if got := checker.Check(o).Status; got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestChecker_KstatusFallback(t *testing.T) {
	// No health check entry -> kstatus is used.
	checker, err := health.NewChecker(nil)
	if err != nil {
		t.Fatalf("NewChecker: %v", err)
	}

	ready := obj("apps/v1", "Deployment", map[string]interface{}{
		"metadata": map[string]interface{}{"name": "d", "namespace": "default", "generation": int64(1)},
		"spec":     map[string]interface{}{"replicas": int64(1)},
		"status": map[string]interface{}{
			"observedGeneration": int64(1),
			"replicas":           int64(1),
			"updatedReplicas":    int64(1),
			"readyReplicas":      int64(1),
			"availableReplicas":  int64(1),
			"conditions": []interface{}{
				map[string]interface{}{"type": "Available", "status": "True"},
			},
		},
	})
	if got := checker.Check(ready).Status; got != health.StatusHealthy {
		t.Fatalf("ready deployment: got %q, want Healthy", got)
	}

	unready := obj("apps/v1", "Deployment", map[string]interface{}{
		"metadata": map[string]interface{}{"name": "d", "namespace": "default", "generation": int64(2)},
		"spec":     map[string]interface{}{"replicas": int64(2)},
		"status": map[string]interface{}{
			"observedGeneration": int64(1),
			"replicas":           int64(1),
		},
	})
	if got := checker.Check(unready).Status; got != health.StatusProgressing {
		t.Fatalf("unready deployment: got %q, want Progressing", got)
	}
}

func TestChecker_CRReadyConvention(t *testing.T) {
	// A CR exposing a Ready condition is handled by the kstatus fallback.
	checker, err := health.NewChecker(nil)
	if err != nil {
		t.Fatalf("NewChecker: %v", err)
	}

	ready := obj("example.com/v1", "Widget", map[string]interface{}{
		"metadata": map[string]interface{}{"name": "w", "namespace": "default", "generation": int64(1)},
		"status": map[string]interface{}{
			"observedGeneration": int64(1),
			"conditions": []interface{}{
				map[string]interface{}{"type": "Ready", "status": "True"},
			},
		},
	})
	if got := checker.Check(ready).Status; got != health.StatusHealthy {
		t.Fatalf("ready CR: got %q, want Healthy", got)
	}
}

func TestNewChecker_Errors(t *testing.T) {
	cases := []struct {
		name  string
		check capsulev1beta2.HealthCheckSpec
	}{
		{
			name:  "syntax error",
			check: capsulev1beta2.HealthCheckSpec{APIVersion: "v1", Kind: "ConfigMap", Current: "status.conditions["},
		},
		{
			name:  "non-bool expression",
			check: capsulev1beta2.HealthCheckSpec{APIVersion: "v1", Kind: "ConfigMap", Current: "1 + 1"},
		},
		{
			name:  "unknown top-level field",
			check: capsulev1beta2.HealthCheckSpec{APIVersion: "v1", Kind: "ConfigMap", Current: "bogus.foo == true"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := health.NewChecker([]capsulev1beta2.HealthCheckSpec{tc.check}); err == nil {
				t.Fatalf("expected error, got nil")
			}
		})
	}
}

func TestValidate(t *testing.T) {
	// Entry with neither current nor failed is rejected.
	err := health.Validate([]capsulev1beta2.HealthCheckSpec{{APIVersion: "v1", Kind: "ConfigMap"}})
	if err == nil {
		t.Fatalf("expected error for entry without current/failed")
	}

	// Valid entry passes.
	if err := health.Validate([]capsulev1beta2.HealthCheckSpec{{
		APIVersion: "example.com/v1", Kind: "Foo", Current: syncedCurrent,
	}}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
