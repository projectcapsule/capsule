// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package pvc

import (
	"context"
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func TestPVCSelectorUpdatesAreNotMutated(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	decoder := admission.NewDecoder(scheme)
	tnt := &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: "tenant-a"}}

	for _, phase := range []corev1.PersistentVolumeClaimPhase{"", corev1.ClaimPending, corev1.ClaimBound, corev1.ClaimLost} {
		for _, update := range []string{"metadata", "binding", "selector change"} {
			t.Run(string(phase)+"/"+update, func(t *testing.T) {
				oldPVC := restoredPVCForUpdate()
				oldPVC.Status.Phase = phase
				newPVC := oldPVC.DeepCopy()
				switch update {
				case "metadata":
					newPVC.Annotations = map[string]string{"volume.kubernetes.io/storage-provisioner": "example.com/csi"}
				case "binding":
					newPVC.Spec.VolumeName = "restored-pv"
				case "selector change":
					newPVC.Spec.Selector.MatchLabels[meta.TenantLabel] = "tenant-b"
				}
				oldSnapshot, newSnapshot := oldPVC.DeepCopy(), newPVC.DeepCopy()
				request := pvcUpdateAdmissionRequest(t, oldPVC, newPVC)

				response := PersistentVolumeMutatingVolume().OnUpdate(nil, nil, oldPVC, newPVC, decoder, nil, tnt)(t.Context(), request)
				if response != nil {
					t.Fatalf("mutating response = %#v, want nil", response)
				}
				if !apiequality.Semantic.DeepEqual(oldPVC, oldSnapshot) || !apiequality.Semantic.DeepEqual(newPVC, newSnapshot) {
					t.Fatal("update mutated the old or new PVC")
				}

				reader := &pvcCountingReader{Reader: fake.NewClientBuilder().WithScheme(scheme).Build()}
				response = MutatingHandler(PersistentVolumeMutatingVolume()).OnUpdate(nil, reader, decoder, nil)(t.Context(), request)
				if response != nil || reader.gets != 0 {
					t.Fatalf("response = %#v, gets = %d; want nil and no tenant lookup", response, reader.gets)
				}
			})
		}
	}
}

func TestRestoredPVCUpdateStillValidatesVolumeOwnership(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		volumeName string
		labels     map[string]string
		readErr    error
		wantError  string
		wantGets   int
	}{
		{name: "metadata only"},
		{name: "same tenant", volumeName: "restored-pv", labels: map[string]string{meta.TenantLabel: "tenant-a"}, wantGets: 1},
		{name: "other tenant", volumeName: "restored-pv", labels: map[string]string{meta.TenantLabel: "tenant-b"}, wantError: "cross-tenant", wantGets: 1},
		{name: "missing label", volumeName: "restored-pv", wantError: "missing the Tenant label", wantGets: 1},
		{name: "empty label", volumeName: "restored-pv", labels: map[string]string{meta.TenantLabel: ""}, wantError: "cross-tenant", wantGets: 1},
		{name: "missing PV", volumeName: "missing", wantError: "not yet existing PV", wantGets: 1},
		{name: "read failure", volumeName: "restored-pv", readErr: errors.New("PV reader unavailable"), wantError: "PV reader unavailable", wantGets: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			if err := corev1.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			pv := &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: "restored-pv", Labels: tt.labels}}
			base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pv).Build()
			reader := &pvcCountingReader{Reader: &pvcFailingReader{Reader: base, err: tt.readErr}}
			oldPVC := restoredPVCForUpdate()
			newPVC := oldPVC.DeepCopy()
			newPVC.Spec.VolumeName = tt.volumeName
			newPVC.Annotations = map[string]string{"restored": "true"}
			snapshot := newPVC.DeepCopy()
			tnt := &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: "tenant-a"}}
			response := PersistentVolumeValidatingVolume().OnUpdate(nil, reader, oldPVC, newPVC, admission.NewDecoder(scheme), nil, tnt)(
				t.Context(), pvcUpdateAdmissionRequest(t, oldPVC, newPVC),
			)
			if tt.wantError == "" {
				if response != nil {
					t.Fatalf("response = %#v, want nil to continue admission", response)
				}
			} else if response == nil || response.Allowed || response.Result == nil || !strings.Contains(response.Result.Message, tt.wantError) {
				t.Fatalf("response = %#v, want denial containing %q", response, tt.wantError)
			}
			if reader.gets != tt.wantGets {
				t.Fatalf("PV gets = %d, want %d", reader.gets, tt.wantGets)
			}
			if !apiequality.Semantic.DeepEqual(newPVC, snapshot) {
				t.Fatal("validation mutated the PVC")
			}
		})
	}
}

func TestPVCSelectorStillEnforcedOnCreate(t *testing.T) {
	t.Parallel()

	tnt := &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: "tenant-a"}}
	for _, selector := range []*metav1.LabelSelector{
		{MatchLabels: map[string]string{"velero.io/dynamic-pv-restore": "solar.restored.unique"}},
		{MatchLabels: map[string]string{meta.TenantLabel: "tenant-b", "storage-tier": "gold"}, MatchExpressions: []metav1.LabelSelectorRequirement{
			{Key: meta.TenantLabel, Operator: metav1.LabelSelectorOpNotIn, Values: []string{"tenant-a"}},
			{Key: "environment", Operator: metav1.LabelSelectorOpExists},
		}},
	} {
		pvc := &corev1.PersistentVolumeClaim{Spec: corev1.PersistentVolumeClaimSpec{Selector: selector.DeepCopy()}}
		if err := validatePVCSelector(pvc, tnt); err == nil {
			t.Fatal("selector without the tenant constraint was accepted before mutation")
		}
		response := PersistentVolumeMutatingVolume().OnCreate(nil, nil, pvc, nil, nil, tnt)(t.Context(), pvcAdmissionRequest(t, pvc))
		if response == nil || !response.Allowed || len(response.Patches) == 0 {
			t.Fatalf("create response = %#v, want tenant selector patch", response)
		}
		if err := validatePVCSelector(pvc, tnt); err != nil {
			t.Fatal(err)
		}
		for key, value := range selector.MatchLabels {
			if key != meta.TenantLabel && pvc.Spec.Selector.MatchLabels[key] != value {
				t.Fatalf("unrelated selector label %q was changed", key)
			}
		}
		for _, expression := range selector.MatchExpressions {
			if expression.Key != meta.TenantLabel && !apiequality.Semantic.DeepEqual(pvc.Spec.Selector.MatchExpressions[0], expression) {
				t.Fatal("unrelated selector expression was changed")
			}
		}
	}
}

func restoredPVCForUpdate() *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "restored", Namespace: "solar"},
		Spec: corev1.PersistentVolumeClaimSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{
			"velero.io/dynamic-pv-restore": "solar.restored.unique",
		}}},
		Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimPending},
	}
}

type pvcFailingReader struct {
	client.Reader
	err error
}

func (r *pvcFailingReader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if r.err != nil {
		return r.err
	}
	return r.Reader.Get(ctx, key, obj, opts...)
}
