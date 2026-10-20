// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package generic

import (
	"context"
	"errors"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	caperrors "github.com/projectcapsule/capsule/pkg/api/errors"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

type resourceCounterHandler struct{}

func ResourceCounterHandler(_ client.Client) handlers.Handler {
	return &resourceCounterHandler{}
}

func (r *resourceCounterHandler) OnCreate(
	c client.Client,
	reader client.Reader,
	decoder admission.Decoder,
	recorder events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		kgv := fmt.Sprintf("%s.%s_%s", req.Resource.Resource, req.Resource.Group, req.Resource.Version)

		var limit int64

		name := req.Name

		tnt, err := r.updateUsage(ctx, c, reader, req, func(tnt *capsulev1beta2.Tenant, ns *corev1.Namespace) (bool, error) {
			var limitErr error
			if limit, limitErr = capsulev1beta2.GetLimitResourceFromTenant(*tnt, kgv); limitErr != nil {
				if _, ok := errors.AsType[*capsulev1beta2.NonLimitedResourceError](limitErr); ok {
					return false, nil
				}

				return false, limitErr
			}

			// Generated-name CREATEs can omit request.name even though the
			// object already has its final name. Decode only those limited
			// requests, once across conflict retries, to identify the reservation.
			if name == "" {
				obj := &metav1.PartialObjectMetadata{}
				if err := decoder.Decode(req, obj); err != nil {
					return false, err
				}

				name = obj.Name
				if name == "" {
					return false, errors.New("cannot reserve quota for a resource without a name")
				}
			}

			reservations, err := tenant.ReadLegacyQuotaReservations(tnt.Annotations)
			if err != nil {
				return false, err
			}

			now := metav1.Now()

			used, _ := capsulev1beta2.GetUsedResourceFromTenant(*tnt, kgv)

			for _, pending := range reservations.Pending[kgv] {
				if req.UID != "" && pending.ID == string(req.UID) && pending.NamespaceUID == ns.UID && pending.ObjectRef.Namespace == req.Namespace && pending.ObjectRef.Name == name && pending.ExpiresAt.After(now.Time) {
					if used > limit {
						return false, caperrors.NewCustomResourceQuotaError(kgv, limit)
					}

					return false, nil
				}
			}

			if used >= limit {
				return false, caperrors.NewCustomResourceQuotaError(kgv, limit)
			}

			expires := metav1.NewTime(now.Add(2 * time.Minute))

			id := string(req.UID)
			if id == "" {
				id = string(uuid.NewUUID())
			}

			reservations.Pending[kgv] = append(reservations.Pending[kgv], tenant.LegacyQuotaReservation{
				ID: id, Usage: *resource.NewQuantity(1, resource.DecimalSI), CreatedAt: now, UpdatedAt: now, ExpiresAt: &expires,
				ObjectRef: capsulev1beta2.QuantityLedgerObjectRef{APIGroup: req.Resource.Group, APIVersion: req.Resource.Version, Kind: req.Kind.Kind, Namespace: req.Namespace, Name: name}, NamespaceUID: ns.UID,
			})

			reservations.Namespaces[ns.Name] = ns.UID

			if err := tenant.WriteLegacyQuotaReservations(tnt.Annotations, reservations); err != nil {
				return false, err
			}

			tnt.Annotations[capsulev1beta2.UsedAnnotationForResource(kgv)] = fmt.Sprintf("%d", used+1)

			return true, nil
		})
		if err != nil {
			if _, ok := errors.AsType[*caperrors.CustomResourceQuotaError](err); ok {
				recorder.LabeledEvent(
					tnt,
					corev1.EventTypeWarning,
					events.ReasonOverprovision,
					events.ActionValidationDenied,
					fmt.Sprintf("Resource %s/%s in API group %s cannot be created, limit usage of %d has been reached", req.Namespace, name, kgv, limit),
				).
					WithRelated(tnt).
					WithTenantLabel(tnt).
					WithRequestAnnotations(req).
					Emit(ctx)
			}

			return ad.ErroredResponse(err)
		}

		return nil
	}
}

func (r *resourceCounterHandler) OnDelete(
	c client.Client,
	reader client.Reader,
	_ admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		kgv := fmt.Sprintf("%s.%s_%s", req.Resource.Resource, req.Resource.Group, req.Resource.Version)

		_, err := r.updateUsage(ctx, c, reader, req, func(tnt *capsulev1beta2.Tenant, ns *corev1.Namespace) (bool, error) {
			if _, ok := tnt.Annotations[capsulev1beta2.UsedAnnotationForResource(kgv)]; !ok {
				return false, nil
			}

			used, _ := capsulev1beta2.GetUsedResourceFromTenant(*tnt, kgv)

			reservations, err := tenant.ReadLegacyQuotaReservations(tnt.Annotations)
			if err != nil {
				return false, err
			}

			active := reservations.Pending[kgv][:0]

			for _, pending := range reservations.Pending[kgv] {
				if pending.NamespaceUID != ns.UID || pending.ObjectRef.Namespace != req.Namespace || pending.ObjectRef.Name != req.Name {
					active = append(active, pending)
				}
			}

			reservations.Pending[kgv] = active
			if err := tenant.WriteLegacyQuotaReservations(tnt.Annotations, reservations); err != nil {
				return false, err
			}

			tnt.Annotations[capsulev1beta2.UsedAnnotationForResource(kgv)] = fmt.Sprintf("%d", max(used-1, 0))

			return true, nil
		})
		if err != nil {
			return ad.ErroredResponse(err)
		}

		return nil
	}
}

func (r *resourceCounterHandler) OnUpdate(
	client.Client,
	client.Reader,
	admission.Decoder,
	events.EventRecorder,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

// Resolve ownership through the API reader: namespace membership in Tenant status
// can lag behind the permission queue. Each conflict retry refreshes both objects,
// without issuing another Tenant GET on the successful first attempt.
func (r *resourceCounterHandler) updateUsage(
	ctx context.Context,
	clt client.Client,
	reader client.Reader,
	req admission.Request,
	mutate func(*capsulev1beta2.Tenant, *corev1.Namespace) (bool, error),
) (*capsulev1beta2.Tenant, error) {
	if req.Namespace == "" || req.SubResource != "" {
		return nil, nil
	}

	var original, current *capsulev1beta2.Tenant

	var namespaceUID types.UID

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var err error

		var ns *corev1.Namespace

		current, ns, err = tenant.GetTenantByNamespaceWithSnapshot(ctx, reader, req.Namespace)
		if err != nil {
			return err
		}

		if original != nil && (current == nil || current.Name != original.Name || current.UID != original.UID || ns.UID != namespaceUID) {
			return fmt.Errorf("namespace %q tenant ownership changed while updating quota usage", req.Namespace)
		}

		if current == nil {
			if ns != nil {
				if _, owned := tenant.GetTenantNameByOwnerreferences(ns.OwnerReferences); owned {
					return fmt.Errorf("cannot resolve tenant owner for namespace %q", req.Namespace)
				}
			}

			return nil
		}

		original = current
		namespaceUID = ns.UID

		changed, err := mutate(current, ns)
		if err != nil || !changed || (req.DryRun != nil && *req.DryRun) {
			return err
		}

		return clt.Update(ctx, current)
	})
	if err == nil {
		err = ctx.Err()
	}

	return current, err
}
