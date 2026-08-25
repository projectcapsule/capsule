// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package predicates

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const AdmissionStateHashAnnotation = "projectcapsule.dev/admission-state-hash"

type admissionState struct {
	Labels          map[string]string       `json:"labels,omitempty"`
	Annotations     map[string]string       `json:"annotations,omitempty"`
	OwnerReferences []metav1.OwnerReference `json:"ownerReferences,omitempty"`
	Webhooks        any                     `json:"webhooks"`
}

type ValidatingAdmissionConfigurationChangedPredicate struct{ predicate.Funcs }

func (ValidatingAdmissionConfigurationChangedPredicate) Create(event.CreateEvent) bool { return true }
func (ValidatingAdmissionConfigurationChangedPredicate) Delete(event.DeleteEvent) bool { return true }
func (ValidatingAdmissionConfigurationChangedPredicate) Generic(event.GenericEvent) bool {
	return false
}

func (ValidatingAdmissionConfigurationChangedPredicate) Update(e event.UpdateEvent) bool {
	oldObj, oldOK := e.ObjectOld.(*admissionv1.ValidatingWebhookConfiguration)

	newObj, newOK := e.ObjectNew.(*admissionv1.ValidatingWebhookConfiguration)

	if !oldOK || !newOK {
		return true
	}

	return ValidatingAdmissionStateHash(oldObj) != ValidatingAdmissionStateHash(newObj)
}

type MutatingAdmissionConfigurationChangedPredicate struct{ predicate.Funcs }

func (MutatingAdmissionConfigurationChangedPredicate) Create(event.CreateEvent) bool   { return true }
func (MutatingAdmissionConfigurationChangedPredicate) Delete(event.DeleteEvent) bool   { return true }
func (MutatingAdmissionConfigurationChangedPredicate) Generic(event.GenericEvent) bool { return false }
func (MutatingAdmissionConfigurationChangedPredicate) Update(e event.UpdateEvent) bool {
	oldObj, oldOK := e.ObjectOld.(*admissionv1.MutatingWebhookConfiguration)

	newObj, newOK := e.ObjectNew.(*admissionv1.MutatingWebhookConfiguration)

	if !oldOK || !newOK {
		return true
	}

	return MutatingAdmissionStateHash(oldObj) != MutatingAdmissionStateHash(newObj)
}

func ValidatingAdmissionStateHash(obj *admissionv1.ValidatingWebhookConfiguration) string {
	hooks := obj.DeepCopy().Webhooks
	for i := range hooks {
		hooks[i].ClientConfig.CABundle = nil
	}

	return admissionHash(obj.ObjectMeta, hooks)
}

func MutatingAdmissionStateHash(obj *admissionv1.MutatingWebhookConfiguration) string {
	hooks := obj.DeepCopy().Webhooks
	for i := range hooks {
		hooks[i].ClientConfig.CABundle = nil
	}

	return admissionHash(obj.ObjectMeta, hooks)
}

func admissionHash(metadata metav1.ObjectMeta, hooks any) string {
	annotations := make(map[string]string, len(metadata.Annotations))

	for key, value := range metadata.Annotations {
		if key != AdmissionStateHashAnnotation {
			annotations[key] = value
		}
	}

	payload, err := json.Marshal(admissionState{
		Labels:          metadata.Labels,
		Annotations:     annotations,
		OwnerReferences: metadata.OwnerReferences,
		Webhooks:        hooks,
	})
	if err != nil {
		return ""
	}

	sum := sha256.Sum256(payload)

	return hex.EncodeToString(sum[:])
}
