// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
	"github.com/projectcapsule/capsule/internal/webhook/utils"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

type resourceCounterHandler struct {
	client client.Client
}

func ResourceCounterHandler(client client.Client) capsulewebhook.Handler {
	return &resourceCounterHandler{
		client: client,
	}
}

func (r *resourceCounterHandler) OnCreate(clt client.Client, _ admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		var tntName string

		var err error
		if tntName, err = r.getTenantName(ctx, clt, req); err != nil {
			return utils.ErroredResponse(err)
		}

		if len(tntName) == 0 {
			return nil
		}

		kgv := fmt.Sprintf("%s.%s_%s", req.Resource.Resource, req.Resource.Group, req.Resource.Version)

		tnt := &capsulev1beta2.Tenant{}

		var limit int64

		err = retry.RetryOnConflict(retry.DefaultRetry, func() (retryErr error) {
			if retryErr = clt.Get(ctx, types.NamespacedName{Name: tntName}, tnt); err != nil {
				return retryErr
			}

			if limit, retryErr = capsulev1beta2.GetLimitResourceFromTenant(*tnt, kgv); retryErr != nil {
				if errors.As(err, &capsulev1beta2.NonLimitedResourceError{}) {
					return nil
				}

				return err
			}

			used, _ := capsulev1beta2.GetUsedResourceFromTenant(*tnt, kgv)
			if used >= limit {
				return NewCustomResourceQuotaError(kgv, limit)
			}

			tnt.Annotations[capsulev1beta2.UsedAnnotationForResource(kgv)] = fmt.Sprintf("%d", used+1)

			return clt.Update(ctx, tnt)
		})
		if err != nil {
			if errors.As(err, &customResourceQuotaError{}) {
				recorder.Eventf(tnt, corev1.EventTypeWarning, "ResourceQuota", "Resource %s/%s in API group %s cannot be created, limit usage of %d has been reached", req.Namespace, req.Name, kgv, limit)
			}

			return utils.ErroredResponse(err)
		}

		return nil
	}
}

func (r *resourceCounterHandler) OnDelete(clt client.Client, _ admission.Decoder, _ record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		var tntName string

		var err error
		if tntName, err = r.getTenantName(ctx, clt, req); err != nil {
			return utils.ErroredResponse(err)
		}

		if len(tntName) == 0 {
			return nil
		}

		kgv := fmt.Sprintf("%s.%s_%s", req.Resource.Resource, req.Resource.Group, req.Resource.Version)

		err = retry.RetryOnConflict(retry.DefaultRetry, func() (retryErr error) {
			tnt := &capsulev1beta2.Tenant{}
			if retryErr = clt.Get(ctx, types.NamespacedName{Name: tntName}, tnt); err != nil {
				return retryErr
			}

			if tnt.Annotations == nil {
				return retryErr
			}

			if _, ok := tnt.Annotations[capsulev1beta2.UsedAnnotationForResource(kgv)]; !ok {
				return retryErr
			}

			used, _ := capsulev1beta2.GetUsedResourceFromTenant(*tnt, kgv)

			tnt.Annotations[capsulev1beta2.UsedAnnotationForResource(kgv)] = fmt.Sprintf("%d", used-1)

			return clt.Update(ctx, tnt)
		})
		if err != nil {
			return utils.ErroredResponse(err)
		}

		return nil
	}
}

func (r *resourceCounterHandler) OnUpdate(client.Client, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (r *resourceCounterHandler) getTenantName(ctx context.Context, clt client.Client, req admission.Request) (string, error) {
	tnt, err := tenant.TenantByStatusNamespace(ctx, clt, req.Namespace)
	if err != nil {
		return "", err
	}

	if tnt == nil {
		return "", nil
	}

	return tnt.GetName(), nil
}
