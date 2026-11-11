// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package replication_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/projectcapsule/capsule/pkg/runtime/replication"
)

type unreadableBody struct{ testing.TB }

func (r unreadableBody) Read([]byte) (int, error) {
	r.Fatal("unauthorized body was read")
	return 0, nil
}
func (r unreadableBody) Close() error { return nil }

func TestTriggerDeliveryAuthentication(t *testing.T) {
	data := make([]byte, 32)
	key, err := replication.NewDeliveryKey(data)
	if err != nil {
		t.Fatal(err)
	}
	id, other := strings.Repeat("a", 64), strings.Repeat("b", 64)
	path := key.Path(id)
	data[0] = 1
	if key.Path(id) != path {
		t.Fatal("key retained mutable input")
	}
	wrong, _ := replication.NewDeliveryKey(data)
	for _, tc := range []struct {
		name, path string
		key        *replication.DeliveryKey
		allowed    bool
	}{
		{"valid", path, key, true},
		{"missing", replication.TriggerPath + id, key, false},
		{"wrong-key", wrong.Path(id), key, false},
		{"other-predicate", strings.Replace(path, id, other, 1), key, false},
		{"malformed", replication.TriggerPath + "invalid/" + strings.Repeat("a", 64), key, false},
		{"trailing-segment", path + "/extra", key, false},
		{"wrong-prefix", "/other/" + id + "/" + strings.Repeat("a", 64), key, false},
		{"unconfigured", path, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			h := tc.key.WrapHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				if !replication.AuthenticatedDelivery(r.Context()) || r.URL.Path != replication.TriggerPath+id || r.RequestURI != r.URL.Path || r.URL.RawPath != "" || r.URL.RawQuery != "" {
					t.Fatal("credential was not redacted before inner handler")
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			req := httptest.NewRequest(http.MethodPost, tc.path+"?sensitive=unused", nil)
			req.Body = unreadableBody{t}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if called != tc.allowed || (!tc.allowed && w.Code != http.StatusUnauthorized) {
				t.Fatalf("called=%v status=%d", called, w.Code)
			}
		})
	}
	for _, size := range []int{0, 31, 33} {
		if _, err := replication.NewDeliveryKey(make([]byte, size)); err == nil {
			t.Fatalf("accepted %d-byte key", size)
		}
	}
}

func BenchmarkTriggerDeliveryAuthentication(b *testing.B) {
	key := testDeliveryKey(b)
	id := strings.Repeat("a", 64)
	for _, state := range []string{"unprotected", "valid", "forged", "missing"} {
		path := key.Path(id)
		if state == "forged" {
			path = strings.Replace(key.Path(strings.Repeat("b", 64)), strings.Repeat("b", 64), id, 1)
		}
		if state == "missing" {
			path = replication.TriggerPath + id
		}
		for _, parallel := range []bool{false, true} {
			mode := "serial"
			if parallel {
				mode = "parallel"
			}
			b.Run(state+"/"+mode, func(b *testing.B) {
				var h http.Handler = http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
				if state != "unprotected" {
					h = key.WrapHandler(h)
				}
				req := httptest.NewRequest(http.MethodPost, path, nil)
				b.ReportAllocs()
				if parallel {
					b.RunParallel(func(pb *testing.PB) {
						for pb.Next() {
							h.ServeHTTP(httptest.NewRecorder(), req)
						}
					})
					return
				}
				for b.Loop() {
					h.ServeHTTP(httptest.NewRecorder(), req)
				}
			})
		}
	}
}
