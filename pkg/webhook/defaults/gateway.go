// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package defaults

import (
	"context"

	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/util/version"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/webhook/utils"
)

func mutateGatewayDefaults(ctx context.Context, req admission.Request, version *version.Version, c client.Client, decoder admission.Decoder, recorder record.EventRecorder, namespace string) *admission.Response {
	const (
		annotationName = "kubernetes.io/gateway.class"
	)
	var (
	//gatewayClass client.Object
	//mutate bool
	)
	tntList := &capsulev1beta2.TenantList{}
	gatewayObj := &gatewayv1.Gateway{}
	tnt := &capsulev1beta2.Tenant{}

	if err := decoder.Decode(req, gatewayObj); err != nil {
		return utils.ErroredResponse(err)
	}
	gatewayObj.SetNamespace(namespace)

	if err := c.List(ctx, tntList, client.MatchingFieldsSelector{
		Selector: fields.OneTermEqualSelector(".status.namespaces", gatewayObj.Namespace),
	}); err != nil {
		return utils.ErroredResponse(err)
	}

	if tnt = &tntList.Items[0]; tnt == nil {
		return nil
	}

	allowed := tnt.Spec.GatewayOptions.AllowedClasses
	if allowed == nil || allowed.Default == "" {
		return nil
	}
	gatewayClassName := &gatewayObj.Spec.GatewayClassName
	if gatewayClassName != nil && *gatewayClassName != gatewayv1.ObjectName(allowed.Default) {
		return nil
	}
	return nil
}
