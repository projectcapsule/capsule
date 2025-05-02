package tenant

import (
	"context"
	"fmt"
	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/configuration"
	capsuleutils "github.com/projectcapsule/capsule/pkg/utils"
	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
	"github.com/projectcapsule/capsule/pkg/webhook/utils"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"net/http"
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

func (h *namespaceCordoningHandler) OnCreate(client client.Client, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	fmt.Printf("HELLO from OnCreate\n")
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.removeCordonedLabel(ctx, req, client, decoder)
	}
}

func (h *namespaceCordoningHandler) OnUpdate(client client.Client, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	fmt.Printf("HELLO from OnUpdate\n")
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.removeCordonedLabel(ctx, req, client, decoder)
	}
}

func (h *namespaceCordoningHandler) OnDelete(client.Client, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *namespaceCordoningHandler) removeCordonedLabel(ctx context.Context, req admission.Request, c client.Client, decoder admission.Decoder) *admission.Response {
	tnt := &capsulev1beta2.Tenant{}
	ns := &v1.Namespace{}
	if err := decoder.Decode(req, ns); err != nil {
		return utils.ErroredResponse(err)
	}
	ln, err := capsuleutils.GetTypeLabel(&capsulev1beta2.Tenant{})
	if err != nil {
		response := admission.Errored(http.StatusBadRequest, err)

		return &response
	}
	if label, ok := ns.Labels[ln]; ok {
		if err = c.Get(ctx, types.NamespacedName{Name: label}, tnt); err != nil {
			response := admission.Errored(http.StatusBadRequest, err)

			return &response
		}
	}
	delete(ns.Labels, "projectcapsule.dev/cordoned")
	if err := c.Update(ctx, ns); err != nil {
		return utils.ErroredResponse(err)
	}
	response := admission.Allowed(fmt.Sprintf("Cordoning label has been removed from %s namespace", ns.GetName()))
	return &response
}
