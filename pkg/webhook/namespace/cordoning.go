package namespace

import (
	"context"
	"fmt"
	"github.com/projectcapsule/capsule/pkg/configuration"
	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

type cordoningHandler struct {
	cfg configuration.Configuration
}

func CordoningHandler(cfg configuration.Configuration) capsulewebhook.Handler {
	return &cordoningHandler{
		cfg: cfg,
	}
}

func (h *cordoningHandler) OnCreate(client.Client, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	fmt.Printf("Cordoning handler in action")
	return func(ctx context.Context, r admission.Request) *admission.Response {
		return nil
	}
}

func (h *cordoningHandler) OnUpdate(client.Client, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *cordoningHandler) OnDelete(client.Client, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}
