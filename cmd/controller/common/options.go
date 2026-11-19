// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package common

import (
	goflag "flag"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/pflag"
	"go.uber.org/zap/zapcore"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/projectcapsule/capsule/internal/controllers/utils"
)

type Options struct {
	Controller                   utils.ControllerOptions
	Logging                      zap.Options
	MetricsAddr                  string
	MetricsCertPath              string
	MetricsCertName              string
	MetricsCertKey               string
	WebhookCertPath              string
	WebhookCertName              string
	WebhookCertKey               string
	EnableLeaderElection         bool
	EnablePprof                  bool
	EnableTracing                bool
	Version                      bool
	SecureMetrics                bool
	EnableHTTP2                  bool
	TracingEndpoint              string
	TracingSampleRatio           float64
	TracingInsecure              bool
	TracingHeaders               Headers
	TracingBasicAuthUsername     string
	TracingBasicAuthPassword     string
	TracingTimeout               time.Duration
	TracingCompression           string
	TracingTLSServerName         string
	TracingTLSInsecureSkipVerify bool
	ClientConnectionQPS          float32
	ClientConnectionBurst        int32
	WebhookPort                  int
	CacheSyncTimeout             time.Duration
	LeaderElectionLeaseDuration  time.Duration
	LeaderElectionRenewDeadline  time.Duration
	LeaderElectionRetryPeriod    time.Duration
}

func (o *Options) BindFlags(flags *pflag.FlagSet) {
	var goFlagSet goflag.FlagSet

	o.TracingHeaders = Headers{}
	flags.StringVar(
		&o.Controller.ConfigurationName,
		"configuration-name",
		"default",
		"The CapsuleConfiguration resource name to use",
	)

	flags.BoolVar(
		&o.EnableLeaderElection,
		"enable-leader-election",
		false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.",
	)
	flags.BoolVar(
		&o.EnableTracing,
		"enable-tracing",
		false,
		"Enable OpenTelemetry tracing for admission webhook requests.",
	)
	flags.StringVar(
		&o.TracingEndpoint,
		"tracing-otlp-endpoint",
		"",
		"OTLP gRPC endpoint for exporting traces, for example otel-collector.observability.svc:4317. If unset, OpenTelemetry environment variables are used.",
	)
	flags.BoolVar(
		&o.TracingInsecure,
		"tracing-otlp-insecure",
		true,
		"Disable transport security for the OTLP gRPC trace exporter.",
	)
	flags.Float64Var(
		&o.TracingSampleRatio,
		"tracing-sample-ratio",
		1.0,
		"Trace sampling ratio for admission webhook requests. Must be between 0 and 1.",
	)
	flags.Var(
		o.TracingHeaders,
		"tracing-otlp-header",
		"OTLP gRPC metadata header in key=value format. Can be set multiple times.",
	)
	flags.StringVar(
		&o.TracingBasicAuthUsername,
		"tracing-otlp-basic-auth-username",
		"",
		"Basic auth username for the OTLP gRPC trace exporter. Can also be set with CAPSULE_TRACING_OTLP_BASIC_AUTH_USERNAME.",
	)
	flags.StringVar(
		&o.TracingBasicAuthPassword,
		"tracing-otlp-basic-auth-password",
		"",
		"Basic auth password for the OTLP gRPC trace exporter. Can also be set with CAPSULE_TRACING_OTLP_BASIC_AUTH_PASSWORD.",
	)
	flags.DurationVar(
		&o.TracingTimeout,
		"tracing-otlp-timeout",
		0,
		"Timeout for exporting a batch of spans. Empty or 0 uses OpenTelemetry's default.",
	)
	flags.StringVar(
		&o.TracingCompression,
		"tracing-otlp-compression",
		"",
		"Compression for OTLP gRPC trace exports. Supported value: gzip. Empty disables compression.",
	)
	flags.StringVar(
		&o.TracingTLSServerName,
		"tracing-otlp-tls-server-name",
		"",
		"TLS server name override for the OTLP gRPC trace exporter.",
	)
	flags.BoolVar(
		&o.TracingTLSInsecureSkipVerify,
		"tracing-otlp-tls-insecure-skip-verify",
		false,
		"Skip OTLP gRPC trace exporter TLS certificate verification. Not recommended for production.",
	)
	flags.IntVar(
		&o.Controller.Runtime.MaxConcurrentReconciles,
		"workers",
		1,
		"MaxConcurrentReconciles is the maximum number of concurrent Reconciles which can be run.",
	)
	flags.DurationVar(
		&o.CacheSyncTimeout,
		"cache-sync-timeout",
		0,
		"The timeout used when waiting for controller cache synchronization. If unset or 0, the controller-runtime default is used.",
	)
	flags.DurationVar(
		&o.LeaderElectionLeaseDuration,
		"leader-election-lease-duration",
		0,
		"The duration that non-leader candidates wait to force acquire leadership. If unset or 0, the controller-runtime default is used.",
	)
	flags.DurationVar(
		&o.LeaderElectionRenewDeadline,
		"leader-election-renew-deadline",
		0,
		"The duration that the acting leader retries refreshing leadership before giving up. If unset or 0, the controller-runtime default is used.",
	)
	flags.DurationVar(
		&o.LeaderElectionRetryPeriod,
		"leader-election-retry-period",
		0,
		"The duration that leader election clients wait between retries. If unset or 0, the controller-runtime default is used.",
	)
	flags.StringVar(
		&o.MetricsAddr,
		"metrics-addr",
		":8080",
		"The address the metric endpoint binds to.",
	)
	flags.BoolVar(
		&o.SecureMetrics,
		"metrics-secure",
		false,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.",
	)
	flags.StringVar(
		&o.MetricsCertPath,
		"metrics-cert-path",
		"",
		"The directory that contains the metrics server certificate.",
	)
	flags.StringVar(
		&o.MetricsCertName,
		"metrics-cert-name",
		"tls.crt",
		"The name of the metrics server certificate file.",
	)
	flags.StringVar(
		&o.MetricsCertKey,
		"metrics-cert-key",
		"tls.key",
		"The name of the metrics server key file.",
	)
	flags.IntVar(
		&o.WebhookPort,
		"webhook-port",
		9443,
		"The port the webhook server binds to.",
	)
	flags.StringVar(
		&o.WebhookCertPath,
		"webhook-cert-path",
		"/tmp/k8s-webhook-server/serving-certs",
		"The directory that contains the webhook certificate.",
	)
	flags.StringVar(
		&o.WebhookCertName,
		"webhook-cert-name",
		"tls.crt",
		"The name of the webhook certificate file.",
	)
	flags.StringVar(
		&o.WebhookCertKey,
		"webhook-cert-key",
		"tls.key",
		"The name of the webhook key file.",
	)
	flags.BoolVar(
		&o.EnableHTTP2,
		"enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers",
	)
	flags.Float32Var(
		&o.ClientConnectionQPS,
		"client-connection-qps",
		20.0,
		"QPS to use for interacting with kubernetes apiserver.",
	)
	flags.Int32Var(
		&o.ClientConnectionBurst,
		"client-connection-burst",
		30,
		"Burst to use for interacting with kubernetes apiserver.",
	)

	flags.BoolVar(
		&o.EnablePprof,
		"enable-pprof",
		false,
		"Enables Pprof endpoint for profiling (not recommend in production)",
	)
	flags.BoolVar(
		&o.Version,
		"version",
		false,
		"Print the Capsule version and exit",
	)

	o.Logging = zap.Options{
		EncoderConfigOptions: append([]zap.EncoderConfigOption{}, func(config *zapcore.EncoderConfig) {
			config.EncodeTime = zapcore.ISO8601TimeEncoder
		}),
	}

	o.Logging.BindFlags(&goFlagSet)
	flags.AddGoFlagSet(&goFlagSet)
}

type Headers map[string]string

func (f Headers) String() string {
	if len(f) == 0 {
		return ""
	}

	items := make([]string, 0, len(f))
	for key, value := range f {
		items = append(items, key+"="+value)
	}

	return strings.Join(items, ",")
}

func (f Headers) Type() string {
	return "key=value"
}

func (f Headers) Set(value string) error {
	key, headerValue, found := strings.Cut(value, "=")
	if !found || key == "" {
		return fmt.Errorf("tracing header must use key=value format")
	}

	f[key] = headerValue

	return nil
}
