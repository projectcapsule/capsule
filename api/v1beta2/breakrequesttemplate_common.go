// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"

	"github.com/projectcapsule/capsule/pkg/api/breaktheglass"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	tpl "github.com/projectcapsule/capsule/pkg/template"
)

// BreakRequestTemplateData is the common configuration exposed by namespaced
// and global BreakRequest templates.
// +kubebuilder:object:generate=false
type BreakRequestTemplateData struct {
	Resources       []apiruntime.ResourceTemplate
	ParamSchema     *k8sruntime.RawExtension
	Context         *tpl.TemplateContext
	DefaultDuration *metav1.Duration
	MaxDuration     *metav1.Duration
	KeepFor         *breaktheglass.ExtendedDuration
	Approvals       breaktheglass.ApprovalSpec
}

// BreakRequestTemplateSource is implemented by both supported template kinds.
// It lets BreakRequest lifecycle code treat their shared behavior uniformly.
// +kubebuilder:object:generate=false
type BreakRequestTemplateSource interface {
	metav1.Object

	TemplateData() BreakRequestTemplateData
	ValidateApprovalConditions() error
	EvaluateApprovalConditions(ctx context.Context, br *BreakRequest) (bool, error)
	CheckApprovalConditions(ctx context.Context, br *BreakRequest) error
}

func (brt *GlobalBreakRequestTemplate) TemplateData() BreakRequestTemplateData {
	return BreakRequestTemplateData{
		Resources:       brt.Spec.Resources,
		ParamSchema:     brt.Spec.ParamSchema,
		Context:         brt.Spec.Context,
		DefaultDuration: brt.Spec.DefaultDuration,
		MaxDuration:     brt.Spec.MaxDuration,
		KeepFor:         brt.Spec.KeepFor,
		Approvals:       brt.Spec.Approvals,
	}
}
