// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"errors"
	"io"
	"net/http"
	"sync/atomic"

	"k8s.io/client-go/rest"
)

const maxWatchListResponseBytes = 4 * 1024 * 1024

var errWatchListResponseLimit = errors.New("replication watch LIST response exceeds byte budget")

type watchListLimitKey struct{}

type watchListRoundTripper struct{ http.RoundTripper }

func limitWatchListResponses(config *rest.Config) {
	config.Wrap(func(transport http.RoundTripper) http.RoundTripper {
		return watchListRoundTripper{transport}
	})
}

func (t watchListRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.RoundTripper.RoundTrip(request)
	if err != nil {
		return nil, err
	}

	if exceeded, ok := request.Context().Value(watchListLimitKey{}).(*atomic.Bool); ok {
		response.Body = &watchListBody{ReadCloser: response.Body, remaining: maxWatchListResponseBytes, exceeded: exceeded}
	}

	return response, nil
}

type watchListBody struct {
	io.ReadCloser

	remaining int
	exceeded  *atomic.Bool
}

func (b *watchListBody) Read(buffer []byte) (int, error) {
	buffer = buffer[:min(len(buffer), b.remaining+1)]

	n, err := b.ReadCloser.Read(buffer)
	if n > b.remaining {
		b.exceeded.Store(true)
		n = b.remaining
		b.remaining = 0

		return n, errWatchListResponseLimit
	}

	b.remaining -= n

	return n, err
}
