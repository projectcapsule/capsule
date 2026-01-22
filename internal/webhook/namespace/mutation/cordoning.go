// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"context"
	"encoding/json"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/configuration"
	capsuleutils "github.com/projectcapsule/capsule/pkg/utils"
)

type cordoningLabelHandler struct {
	cfg configuration.Configuration
}

func CordoningLabelHandler(cfg configuration.Configuration) capsulewebhook.TypedHandler[*corev1.Namespace] {
	return &cordoningLabelHandler{
		cfg: cfg,
	}
}

func (h *cordoningLabelHandler) OnCreate(client.Client, *corev1.Namespace, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *cordoningLabelHandler) OnDelete(client.Client, *corev1.Namespace, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *cordoningLabelHandler) OnUpdate(
	c client.Client,
	ns *corev1.Namespace,
	old *corev1.Namespace,
	decoder admission.Decoder,
	_ record.EventRecorder,
) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.handle(ctx, c, req, ns)
	}
}

func (h *cordoningLabelHandler) handle(
	ctx context.Context,
	c client.Client,
	req admission.Request,
	ns *corev1.Namespace,
) *admission.Response {
	tnt := &capsulev1beta2.Tenant{}

	ln, err := capsuleutils.GetTypeLabel(tnt)
	if err != nil {
		response := admission.Errored(http.StatusInternalServerError, err)

		return &response
	}

	if label, ok := ns.Labels[ln]; ok {
		if err = c.Get(ctx, types.NamespacedName{Name: label}, tnt); err != nil {
			response := admission.Errored(http.StatusInternalServerError, err)

			return &response
		}
	}

	condition := tnt.Status.Conditions.GetConditionByType(meta.CordonedCondition)
	if condition == nil {
		return nil
	}

	if condition.Status != metav1.ConditionTrue {
		return nil
	}

	labels := ns.GetLabels()
	if _, ok := labels[meta.CordonedLabel]; ok {
		return nil
	}

	ns.Labels[meta.CordonedLabel] = "true"

	marshaled, err := json.Marshal(ns)
	if err != nil {
		response := admission.Errored(http.StatusInternalServerError, err)

		return &response
	}

	response := admission.PatchResponseFromRaw(req.Object.Raw, marshaled)

	return &response
}
