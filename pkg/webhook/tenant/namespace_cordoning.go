package tenant

import (
	"context"
	"fmt"
	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/configuration"
	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
	"github.com/projectcapsule/capsule/pkg/webhook/utils"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

type namespaceCordoningHandler struct {
	cfg configuration.Configuration
}

func NamespaceCordoningHandler(cfg configuration.Configuration) capsulewebhook.Handler {
	return &namespaceCordoningHandler{
		cfg: cfg,
	}
}

func (h *namespaceCordoningHandler) OnCreate(client.Client, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *namespaceCordoningHandler) OnUpdate(client client.Client, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.setCordonedLabel(ctx, req, client, decoder, recorder)
	}
}

func (h *namespaceCordoningHandler) OnDelete(client.Client, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *namespaceCordoningHandler) setCordonedLabel(ctx context.Context, req admission.Request, c client.Client, decoder admission.Decoder, recorder record.EventRecorder) *admission.Response {
	tnt := &capsulev1beta2.Tenant{}
	ns := &v1.Namespace{}
	var response admission.Response
	if err := decoder.Decode(req, tnt); err != nil {
		return utils.ErroredResponse(err)
	}

	for item := range tnt.Status.Namespaces {
		if err := c.Get(ctx, types.NamespacedName{Name: tnt.Status.Namespaces[item]}, ns); err != nil {
			return utils.ErroredResponse(err)
		}
		fmt.Printf("Patching namespace %s\n", ns.GetName())
		if tnt.Spec.Cordoned {
			ns.Labels["capsule.clastix.io/cordoned"] = "true"
			if err := c.Update(ctx, ns); err != nil {
				return utils.ErroredResponse(err)
			}
		} else {
			delete(ns.Labels, "capsule.clastix.io/cordoned")
			if err := c.Update(ctx, ns); err != nil {
				return utils.ErroredResponse(err)
			}
		}
	}
	response = admission.Allowed(fmt.Sprintf("Cordoning label has been applied to all namespaces in %s tenant", tnt.GetName()))
	return &response
}
