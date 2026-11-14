// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package replication

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
)

// DeliveryKey signs predicate-specific bearer capabilities. Only the controller
// Secret and administrators who can read webhook configurations may access them.
// The key is immutable and shared across replicas; admission needs no API reads.
type DeliveryKey struct{ key [32]byte }

func NewDeliveryKey(data []byte) (*DeliveryKey, error) {
	if len(data) != 32 {
		return nil, fmt.Errorf("trigger delivery key must contain 32 bytes")
	}

	k := &DeliveryKey{}
	copy(k.key[:], data)

	return k, nil
}

func (k *DeliveryKey) Path(id string) string {
	return TriggerPath + id + "/" + k.signature(id)
}

type authenticatedDeliveryKey struct{}

func AuthenticatedDelivery(ctx context.Context) bool {
	value, _ := ctx.Value(authenticatedDeliveryKey{}).(bool)

	return value
}

// WrapHandler authenticates before decoding, tracing or processing the supplied
// AdmissionReview. Never pass the credential to logs, metrics or tracing.
func (k *DeliveryKey) WrapHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, signature, found := strings.Cut(strings.TrimPrefix(r.URL.Path, TriggerPath), "/")
		if k == nil || !strings.HasPrefix(r.URL.Path, TriggerPath) || !found || len(id) != 64 || len(signature) != 64 {
			http.Error(w, "unauthorized trigger delivery", http.StatusUnauthorized)

			return
		}

		if _, err := hex.DecodeString(id); err != nil || subtle.ConstantTimeCompare([]byte(signature), []byte(k.signature(id))) != 1 {
			http.Error(w, "unauthorized trigger delivery", http.StatusUnauthorized)

			return
		}

		r = r.Clone(context.WithValue(r.Context(), authenticatedDeliveryKey{}, true))
		r.URL.Path = TriggerPath + id
		r.URL.RawPath = ""
		r.URL.RawQuery = ""
		r.RequestURI = r.URL.Path
		next.ServeHTTP(w, r)
	})
}

func (k *DeliveryKey) signature(id string) string {
	h := hmac.New(sha256.New, k.key[:])
	h.Write([]byte("projectcapsule.dev/replication-trigger/"))
	h.Write([]byte(id))

	return hex.EncodeToString(h.Sum(nil))
}
