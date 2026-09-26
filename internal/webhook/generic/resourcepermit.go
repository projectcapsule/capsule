// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package generic

import (
	"context"
	"encoding/json"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/authentication/serviceaccount"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	indexes "github.com/projectcapsule/capsule/pkg/runtime/indexers/serviceaccount"
	"github.com/projectcapsule/capsule/pkg/users"
)

type resourcePermitResourceHandler struct{}

func ResourcePermitResourceHandler() handlers.Handler {
	return &resourcePermitResourceHandler{}
}

func (h *resourcePermitResourceHandler) OnCreate(
	c client.Client,
	reader client.Reader,
	decoder admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.handle(ctx, req, c, reader, decoder, false)
	}
}

func (h *resourcePermitResourceHandler) OnDelete(
	c client.Client,
	reader client.Reader,
	decoder admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return allowTerminatingNamespaceDeletion(ctx, reader, req, h.handle(ctx, req, c, reader, decoder, true))
	}
}

func (h *resourcePermitResourceHandler) OnUpdate(
	c client.Client,
	reader client.Reader,
	decoder admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.handle(ctx, req, c, reader, decoder, true)
	}
}

func (h *resourcePermitResourceHandler) handle(
	ctx context.Context,
	req admission.Request,
	indexed client.Reader,
	reader client.Reader,
	decoder admission.Decoder,
	preferOld bool,
) *admission.Response {
	if users.IsControllerServiceAccount(req.UserInfo.Username) {
		return nil
	}

	deny := ad.Deny("resources protected by a ResourcePermit can only be changed by the Capsule controller or the template ServiceAccount")
	if decoder == nil {
		return deny
	}

	target := &metav1.PartialObjectMetadata{}
	if preferOld {
		if err := decoder.DecodeRaw(req.OldObject, target); err != nil {
			return ad.ErroredResponse(err)
		}
	}

	if !preferOld || !permitProtected(target) {
		if err := decoder.Decode(req, target); err != nil {
			return ad.ErroredResponse(err)
		}
	}

	// A caller-supplied authorization annotation never establishes authority.
	if _, _, err := serviceaccount.SplitUsername(req.UserInfo.Username); err != nil || indexed == nil || reader == nil {
		return deny
	}

	seen := map[string]struct{}{}

	for _, field := range target.ManagedFields {
		if !strings.HasPrefix(field.Manager, meta.ResourceFieldOwner("resourcepermit/")) {
			continue
		}

		if _, ok := seen[field.Manager]; ok {
			continue
		}

		seen[field.Manager] = struct{}{}

		parents := &capsulev1beta2.ResourcePermitList{}
		if err := indexed.List(ctx, parents, client.MatchingFields{indexes.ResourcePermitFieldOwnerIndex: field.Manager}); err != nil {
			return ad.ErroredResponse(err)
		}

		for i := range parents.Items {
			parent := &parents.Items[i]
			// The index supplies immutable identity candidates; status may lag.
			uid := parent.UID

			if err := reader.Get(ctx, client.ObjectKeyFromObject(parent), parent); err != nil {
				if client.IgnoreNotFound(err) != nil {
					return ad.ErroredResponse(err)
				}

				continue
			}

			if parent.UID != uid || meta.ResourcePermitFieldOwner(parent) != field.Manager || !permitServiceAccount(parent, req.UserInfo.Username) {
				continue
			}

			allowed, err := permitProtectsTarget(parent, req)
			if err != nil {
				return ad.ErroredResponse(err)
			}

			if allowed {
				return nil
			}
		}
	}

	return deny
}

func permitProtected(target *metav1.PartialObjectMetadata) bool {
	return target.Labels[meta.ProtectedByCapsuleLabel] == meta.ValueControllerResourcePermit || target.Labels[meta.ResourcePermitProtectionLabel] == meta.ValueTrue
}

func permitServiceAccount(parent *capsulev1beta2.ResourcePermit, username string) bool {
	if parent.Status.Request == nil || parent.Status.Request.Impersonation == nil {
		return false
	}

	identity := parent.Status.Request.Impersonation

	return serviceaccount.MatchesUsername(identity.Namespace.String(), identity.Name.String(), username)
}

func permitProtectsTarget(parent *capsulev1beta2.ResourcePermit, req admission.Request) (bool, error) {
	for _, resource := range parent.Status.Request.Resources {
		if !resource.Policy.IsProtected() {
			continue
		}

		for _, raw := range resource.Targets {
			target := &metav1.PartialObjectMetadata{}
			if err := permitTargetMetadata(raw, target); err != nil {
				return false, err
			}
			// Cluster-scoped requests have no namespace even when the rendered
			// input contained one. The API server establishes the request scope.
			if target.Name == req.Name && (req.Namespace == "" || target.Namespace == req.Namespace) && target.GroupVersionKind().Group == req.Kind.Group && target.Kind == req.Kind.Kind {
				return true, nil
			}
		}
	}

	return false, nil
}

func permitTargetMetadata(raw runtime.RawExtension, target *metav1.PartialObjectMetadata) error {
	if obj, ok := raw.Object.(client.Object); ok {
		target.Name, target.Namespace = obj.GetName(), obj.GetNamespace()
		target.SetGroupVersionKind(obj.GetObjectKind().GroupVersionKind())

		return nil
	}

	return json.Unmarshal(raw.Raw, target)
}
