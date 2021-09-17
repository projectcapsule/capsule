package route

import (
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
)

// +kubebuilder:webhook:path=/namespace-owner-reference,mutating=true,sideEffects=None,admissionReviewVersions=v1,failurePolicy=fail,groups="",resources=namespaces,verbs=create;update,versions=v1,name=owner.namespace.capsule.clastix.io

type webhook struct {
	handlers []capsulewebhook.Handler
}

func OwnerReference(handlers ...capsulewebhook.Handler) capsulewebhook.Webhook {
	return &webhook{handlers: handlers}
}

func (w *webhook) GetHandlers() []capsulewebhook.Handler {
	return w.handlers
}

func (w *webhook) GetPath() string {
	return "/namespace-owner-reference"
}
