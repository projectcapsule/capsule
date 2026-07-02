// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package status

import (
	"context"

	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type ruleStatusHandler struct {
	configuration configuration.Configuration
	mapper        k8smeta.RESTMapper
}

func RuleStatusValidationHandler(mapper k8smeta.RESTMapper, configuration configuration.Configuration) handlers.Handler {
	return &ruleStatusHandler{
		configuration: configuration,
	}
}

func (r *ruleStatusHandler) OnCreate(
	_ client.Client,
	_ client.Reader,
	decoder admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		rs := &capsulev1beta2.RuleStatus{}
		if err := decoder.Decode(req, rs); err != nil {
			return ad.ErroredResponse(err)
		}

		return r.handle(rs)
	}
}

func (r *ruleStatusHandler) OnDelete(
	client.Client,
	client.Reader,
	admission.Decoder,
	events.EventRecorder,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (r *ruleStatusHandler) OnUpdate(
	_ client.Client,
	_ client.Reader,
	decoder admission.Decoder,
	recorder events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		rs := &capsulev1beta2.RuleStatus{}
		if err := decoder.Decode(req, rs); err != nil {
			return ad.ErroredResponse(err)
		}

		return r.handle(rs)
	}
}

func (r *ruleStatusHandler) handle(rs *capsulev1beta2.RuleStatus) *admission.Response {
	err := ruleengine.ValidateRuleStatusBody(r.mapper, rs.Spec)
	if err != nil {
		return ad.Deny(err.Error())
	}

	return nil
}
