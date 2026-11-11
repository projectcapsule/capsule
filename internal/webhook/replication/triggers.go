// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package replication

import (
	"context"
	"encoding/hex"
	"strings"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	triggers "github.com/projectcapsule/capsule/pkg/runtime/replication"
	"github.com/projectcapsule/capsule/pkg/runtime/ssa"
	runtimewebhook "github.com/projectcapsule/capsule/pkg/runtime/webhook"
	"github.com/projectcapsule/capsule/pkg/runtime/workloads"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

type triggerHandler struct {
	configuration configuration.Configuration
	notifier      triggers.Notifier
	log           logr.Logger
}

func TriggerHandler(cfg configuration.Configuration, notifier triggers.Notifier, log logr.Logger) handlers.Handler {
	return &triggerHandler{configuration: cfg, notifier: notifier, log: log}
}

func (h *triggerHandler) OnCreate(c client.Client, reader client.Reader, decoder admission.Decoder, _ events.EventRecorder) handlers.Func {
	return h.handle(c, reader, decoder)
}

func (h *triggerHandler) OnUpdate(c client.Client, reader client.Reader, decoder admission.Decoder, _ events.EventRecorder) handlers.Func {
	return h.handle(c, reader, decoder)
}

func (h *triggerHandler) OnDelete(c client.Client, reader client.Reader, decoder admission.Decoder, _ events.EventRecorder) handlers.Func {
	return h.handle(c, reader, decoder)
}

func (h *triggerHandler) handle(c client.Client, reader client.Reader, decoder admission.Decoder) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		if !triggers.AuthenticatedDelivery(ctx) {
			return nil
		}

		if req.Namespace == "" || (req.DryRun != nil && *req.DryRun) || triggers.BookkeepingRequest(&req.AdmissionRequest) {
			return nil
		}

		id := strings.TrimPrefix(runtimewebhook.RequestPath(ctx), triggers.TriggerPath)
		if len(id) != 64 {
			return nil
		}

		if _, err := hex.DecodeString(id); err != nil {
			return nil
		}

		managed, sourceUID := h.managedRequest(req, decoder)
		if managed {
			return nil
		}

		local := &capsulev1beta2.TenantResourceList{}
		global := &capsulev1beta2.GlobalTenantResourceList{}

		filter := client.MatchingFields{triggers.TriggerIndexerField: id}
		if err := c.List(ctx, local, filter, client.InNamespace(req.Namespace), client.UnsafeDisableDeepCopy); err != nil {
			h.log.Error(err, "cannot resolve local triggers")

			return nil
		}

		if err := c.List(ctx, global, filter, client.UnsafeDisableDeepCopy); err != nil {
			h.log.Error(err, "cannot resolve global triggers")

			return nil
		}

		audience := &triggerAudience{handler: h, client: c, request: req, results: make(map[rules.Audience]bool)}
		parentChecked, parentManaged := req.SubResource == "", false
		eligible := func(obj client.Object, spec *capsulev1beta2.TenantResourceCommonSpec) bool {
			if spec.IsCordoned() || !obj.GetDeletionTimestamp().IsZero() {
				return false
			}

			for i := range spec.Triggers {
				trigger := &spec.Triggers[i]
				if triggers.MatchID(trigger) == id && audience.matches(ctx, trigger.Audience) {
					if !parentChecked {
						parentChecked = true
						parentManaged = h.managedParent(ctx, c, reader, req, sourceUID)
					}

					return !parentManaged
				}
			}

			return false
		}

		for i := range local.Items {
			obj := &local.Items[i]
			if eligible(obj, &obj.Spec.TenantResourceCommonSpec) {
				h.notifier.Notify(triggers.Target{Name: obj.Name, Namespace: obj.Namespace, UID: obj.UID}, triggers.Interval(&obj.Spec.TenantResourceCommonSpec))
			}
		}

		for i := range global.Items {
			obj := &global.Items[i]
			if eligible(obj, &obj.Spec.TenantResourceCommonSpec) {
				h.notifier.Notify(triggers.Target{Name: obj.Name, UID: obj.UID}, triggers.Interval(&obj.Spec.TenantResourceCommonSpec))
			}
		}

		return nil
	}
}

func (h *triggerHandler) managedRequest(req admission.Request, decoder admission.Decoder) (bool, types.UID) {
	var uid types.UID

	for _, raw := range [][]byte{req.Object.Raw, req.OldObject.Raw} {
		if len(raw) == 0 || string(raw) == "null" {
			continue
		}

		var obj metav1.PartialObjectMetadata
		if err := decoder.DecodeRaw(runtime.RawExtension{Raw: raw}, &obj); err != nil {
			h.log.Error(err, "cannot decode trigger metadata")

			return true, uid
		}

		if uid != "" && obj.UID != "" && uid != obj.UID {
			return true, uid
		}

		if obj.UID != "" {
			uid = obj.UID
		}

		if managedMetadata(&obj) {
			return true, uid
		}
	}

	return false, uid
}

// Subresources can project away all management metadata (notably Scale).
// Read the named parent through the admission reader only after an applicable
// subscription and audience have matched. A missing/uncertain parent never
// creates a notification, and never denies the original Kubernetes operation.
func (h *triggerHandler) managedParent(ctx context.Context, c client.Client, reader client.Reader, req admission.Request, uid types.UID) bool {
	if reader == nil {
		return true
	}

	gvk := schema.GroupVersionKind{}
	if req.SubResource == "scale" {
		gvk = workloads.ScaleWorkloadGVK(req.Resource)
	}

	if gvk.Empty() {
		var err error

		gvk, err = c.RESTMapper().KindFor(schema.GroupVersionResource(req.Resource))
		if err != nil {
			h.log.Error(err, "cannot resolve trigger subresource parent kind")

			return true
		}
	}

	parent := &metav1.PartialObjectMetadata{}
	parent.SetGroupVersionKind(gvk)

	if err := reader.Get(ctx, client.ObjectKey{Namespace: req.Namespace, Name: req.Name}, parent); err != nil {
		if !apierrors.IsNotFound(err) {
			h.log.Error(err, "cannot read trigger subresource parent")
		}

		return true
	}

	return (uid != "" && uid != parent.UID) || managedMetadata(parent)
}

func managedMetadata(obj *metav1.PartialObjectMetadata) bool {
	if triggers.Managed(obj) || len(ssa.ReplicationOwnerPrefixes(obj, "")) > 0 {
		return true
	}

	for _, field := range obj.ManagedFields {
		if strings.HasPrefix(field.Manager, meta.FieldManagerCapsulePrefix+"/") {
			return true
		}
	}

	return false
}

type triggerAudience struct {
	handler   *triggerHandler
	client    client.Client
	request   admission.Request
	tenant    *capsulev1beta2.Tenant
	resolved  bool
	tenantErr error
	results   map[rules.Audience]bool
}

func (a *triggerAudience) matches(ctx context.Context, audience []rules.Audience) bool {
	if len(audience) == 0 {
		return true
	}

	for _, subject := range audience {
		matched, known := a.results[subject]
		if !known {
			matched = a.matchSubject(ctx, subject)
			a.results[subject] = matched
		}

		if matched {
			return true
		}
	}

	return false
}

func (a *triggerAudience) matchSubject(ctx context.Context, subject rules.Audience) bool {
	if subject.Kind == rules.AudienceKindCustom && subject.Name == string(rules.CustomAudienceTenantOwner) {
		if !a.resolved {
			a.resolved = true
			ns := &corev1.Namespace{}

			a.tenantErr = a.client.Get(ctx, client.ObjectKey{Name: a.request.Namespace}, ns)
			if a.tenantErr == nil {
				a.tenant, a.tenantErr = tenant.ResolveNamespaceTenant(ctx, a.client, ns)
			}
		}

		if a.tenantErr != nil {
			a.handler.log.Error(a.tenantErr, "cannot resolve trigger source tenant")

			return false
		}
	}

	matched, err := ruleengine.MatchesAudience(ctx, a.client, a.handler.configuration, a.tenant, a.request, []rules.Audience{subject})
	if err != nil {
		a.handler.log.Error(err, "cannot evaluate trigger audience")

		return false
	}

	return matched
}
