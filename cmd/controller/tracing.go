// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"

	capsuleversion "github.com/projectcapsule/capsule/internal/version"
)

type tracingOptions struct {
	enabled     bool
	endpoint    string
	insecure    bool
	sampleRatio float64
}

func setupTracing(ctx context.Context, options tracingOptions) (func(context.Context) error, error) {
	if !options.enabled {
		return func(context.Context) error { return nil }, nil
	}

	if options.sampleRatio < 0 || options.sampleRatio > 1 {
		return nil, fmt.Errorf("tracing sample ratio must be between 0 and 1, got %v", options.sampleRatio)
	}

	exporterOptions := make([]otlptracegrpc.Option, 0, 2)
	if options.endpoint != "" {
		exporterOptions = append(exporterOptions, otlptracegrpc.WithEndpoint(options.endpoint))
	}
	if options.insecure {
		exporterOptions = append(exporterOptions, otlptracegrpc.WithInsecure())
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
