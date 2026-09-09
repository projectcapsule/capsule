// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"

	"github.com/projectcapsule/capsule/pkg/api/resourcepermit"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	tpl "github.com/projectcapsule/capsule/pkg/template"
)

// ResourcePermitTemplateData is the common configuration exposed by namespaced
// and global ResourcePermit templates.
// +kubebuilder:object:generate=false
type ResourcePermitTemplateData struct {
	Resources       []apiruntime.ResourceTemplate
	ParamSchema     *k8sruntime.RawExtension
	Context         *tpl.TemplateContext
	DefaultDuration *metav1.Duration
	MaxDuration     *metav1.Duration
	KeepFor         *resourcepermit.ExtendedDuration
	Approvals       resourcepermit.ApprovalSpec
}

// ResourcePermitTemplateSource is implemented by both supported template kinds.
// It lets ResourcePermit lifecycle code treat their shared behavior uniformly.
// +kubebuilder:object:generate=false
type ResourcePermitTemplateSource interface {
	metav1.Object

	TemplateData() ResourcePermitTemplateData
	ValidateApprovalConditions() error
	EvaluateApprovalConditions(ctx context.Context, br *ResourcePermit) (bool, error)
	CheckApprovalConditions(ctx context.Context, br *ResourcePermit) error
}

func (brt *GlobalResourcePermitTemplate) TemplateData() ResourcePermitTemplateData {
	return ResourcePermitTemplateData{
		Resources:       brt.Spec.Resources,
		ParamSchema:     brt.Spec.ParamSchema,
		Context:         brt.Spec.Context,
		DefaultDuration: brt.Spec.DefaultDuration,
		MaxDuration:     brt.Spec.MaxDuration,
		KeepFor:         brt.Spec.KeepFor,
		Approvals:       brt.Spec.Approvals,
	}
}

// ValidateResourcePermitTemplate checks the static configuration shared by
// admission and template readiness reconciliation. Rendering and context loading
// depend on request parameters and are checked when a ResourcePermit is submitted.
func ValidateResourcePermitTemplate(brt ResourcePermitTemplateSource) error {
	templateData := brt.TemplateData()

	if err := brt.ValidateApprovalConditions(); err != nil {
		return fmt.Errorf("approval conditions are invalid: %w", err)
	}

	for i, approver := range templateData.Approvals.Approvers {
		if approver.Name == "" {
			return fmt.Errorf("approvals.approvers[%d].name must not be empty", i)
		}
	}

	if templateData.MaxDuration != nil && templateData.MaxDuration.Duration > 0 && templateData.DefaultDuration != nil &&
		templateData.DefaultDuration.Duration > templateData.MaxDuration.Duration {
		return fmt.Errorf(
			"defaultDuration %s exceeds maxDuration %s",
			templateData.DefaultDuration.Duration,
			templateData.MaxDuration.Duration,
		)
	}

	if err := tpl.ValidateResourceTemplates(templateData.ParamSchema, templateData.Resources); err != nil {
		return fmt.Errorf("invalid resources: %w", err)
	}

	return nil
}
