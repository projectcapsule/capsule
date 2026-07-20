// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"maps"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"google.golang.org/grpc/credentials"

	capsuleversion "github.com/projectcapsule/capsule/internal/version"
)

type tracingOptions struct {
	enabled               bool
	endpoint              string
	insecure              bool
	sampleRatio           float64
	headers               map[string]string
	basicAuthUsername     string
	basicAuthPassword     string
	timeout               time.Duration
	compression           string
	tlsServerName         string
	tlsInsecureSkipVerify bool
}

func setupTracing(ctx context.Context, options tracingOptions) (func(context.Context) error, error) {
	if !options.enabled {
		return func(context.Context) error { return nil }, nil
	}

	if options.sampleRatio < 0 || options.sampleRatio > 1 {
		return nil, fmt.Errorf("tracing sample ratio must be between 0 and 1, got %v", options.sampleRatio)
	}

	if (options.basicAuthUsername == "") != (options.basicAuthPassword == "") {
		return nil, fmt.Errorf("tracing basic auth username and password must be configured together")
	}

	headers := make(map[string]string, len(options.headers)+1)
	maps.Copy(headers, options.headers)

	if options.basicAuthUsername != "" {
		token := base64.StdEncoding.EncodeToString([]byte(options.basicAuthUsername + ":" + options.basicAuthPassword))
		headers["authorization"] = "Basic " + token
	}

	exporterOptions := make([]otlptracegrpc.Option, 0, 6)

	if options.endpoint != "" {
		exporterOptions = append(exporterOptions, otlptracegrpc.WithEndpoint(options.endpoint))
	}

	if options.insecure {
		exporterOptions = append(exporterOptions, otlptracegrpc.WithInsecure())
	} else if options.tlsServerName != "" || options.tlsInsecureSkipVerify {
		exporterOptions = append(exporterOptions, otlptracegrpc.WithTLSCredentials(credentials.NewTLS(&tls.Config{
			ServerName:         options.tlsServerName,
			InsecureSkipVerify: options.tlsInsecureSkipVerify, //nolint:gosec
		})))
	}

	if len(headers) > 0 {
		exporterOptions = append(exporterOptions, otlptracegrpc.WithHeaders(headers))
	}

	if options.timeout > 0 {
		exporterOptions = append(exporterOptions, otlptracegrpc.WithTimeout(options.timeout))
	}

	if options.compression != "" {
		if options.compression != "gzip" {
			return nil, fmt.Errorf("unsupported tracing compression %q, supported values: gzip", options.compression)
		}

		exporterOptions = append(exporterOptions, otlptracegrpc.WithCompressor(options.compression))
	}

	exporter, err := otlptracegrpc.New(ctx, exporterOptions...)
	if err != nil {
		return nil, fmt.Errorf("create OTLP trace exporter: %w", err)
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName("capsule"),
			semconv.ServiceVersion(capsuleversion.GitTag),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("create OpenTelemetry resource: %w", err)
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(options.sampleRatio))),
	)

	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return provider.Shutdown, nil
}

type tracingHeadersFlag map[string]string

func (f tracingHeadersFlag) String() string {
	if len(f) == 0 {
		return ""
	}

	items := make([]string, 0, len(f))
	for key, value := range f {
		items = append(items, key+"="+value)
	}

	return strings.Join(items, ",")
}

func (f tracingHeadersFlag) Type() string {
	return "key=value"
}

func (f tracingHeadersFlag) Set(value string) error {
	key, headerValue, found := strings.Cut(value, "=")
	if !found || key == "" {
		return fmt.Errorf("tracing header must use key=value format")
	}

	f[key] = headerValue

	return nil
}
