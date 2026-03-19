// Package healthcheck provides a unified health evaluation layer that supports:
// - Default kstatus evaluation (when no CEL expressions are configured)
// - Custom CEL evaluation (when any of Current/Failed/InProgress is configured)
//
// The three CEL expressions are optional as a set, but if CEL is enabled,
// this implementation requires Current to be set (recommended, deterministic).
package health

import (
	"context"
	"fmt"
	"sync"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	// These are the packages you already use in your upstream code:
	// - status: kstatus result type
	// - event: ResourceStatus wrapper
	// - engine: StatusReader interface + ClusterReader
	//
	// Keep these imports as-is in your repo and adjust paths accordingly.
	//
	// "github.com/fluxcd/pkg/runtime/status"
	// "github.com/fluxcd/pkg/runtime/event"
	// "github.com/fluxcd/pkg/ssa/engine"
	// kstatusreaders "github.com/fluxcd/pkg/ssa/engine/kstatus"
)

// ---- External types you already have (keep your real imports) ----

// Replace these with your real types.
type Status string

const (
	CurrentStatus    Status = "Current"
	InProgressStatus Status = "InProgress"
	FailedStatus     Status = "Failed"
)

type Result struct {
	Status Status
}

type ResourceStatus struct {
	Identifier any
	Status     Status
	Error      error
}

// engine.StatusReader in your repo.
type ClusterReader interface{}
type ObjMetadata struct {
	GroupKind schema.GroupKind
}
type StatusReader interface {
	Supports(gk schema.GroupKind) bool
	ReadStatus(ctx context.Context, reader ClusterReader, res ObjMetadata) (*ResourceStatus, error)
	ReadStatusForObject(ctx context.Context, reader ClusterReader, obj *unstructured.Unstructured) (*ResourceStatus, error)
}

// kstatus generic reader factory in your repo.
// In Flux this is: kstatusreaders.NewGenericStatusReader(mapper, statusFunc)
type GenericStatusFunc func(u *unstructured.Unstructured) (*Result, error)

func NewGenericStatusReader(_ meta.RESTMapper, _ GenericStatusFunc) StatusReader {
	panic("wire your repo's kstatus generic reader here")
}

// ---- CEL expression abstraction you already have ----

// Expression is your CEL wrapper.
type Expression struct{}

func NewExpression(_ string) (*Expression, error) { return &Expression{}, nil }
func (e *Expression) EvaluateBoolean(_ context.Context, _ map[string]any) (bool, error) {
	return false, nil
}

// ---- User-facing config ----

type HealthCheckConfig struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`

	// Optional CEL expressions. If any is set, CEL mode is enabled.
	Current    string `json:"current,omitempty"`
	Failed     string `json:"failed,omitempty"`
	InProgress string `json:"inProgress,omitempty"`
}

// ---- Compiled evaluator (CEL optional) ----

type CompiledHealthCheck struct {
	current    *Expression
	failed     *Expression
	inProgress *Expression

	// celEnabled is true if any expression was configured.
	celEnabled bool
}

// NewCompiledHealthCheck compiles expressions once.
// If none are set => CEL disabled => caller should fallback to kstatus.
func NewCompiledHealthCheck(cfg HealthCheckConfig) (*CompiledHealthCheck, error) {
	if cfg.Current == "" && cfg.Failed == "" && cfg.InProgress == "" {
		return &CompiledHealthCheck{celEnabled: false}, nil
	}

	// Recommended: require Current when CEL is enabled.
	// If you want “only Failed” semantics, relax this check.
	if cfg.Current == "" {
		return nil, fmt.Errorf("CEL enabled but expression Current not specified for %s/%s", cfg.APIVersion, cfg.Kind)
	}

	compile := func(name, src string) (*Expression, error) {
		if src == "" {
			return nil, nil
		}
		expr, err := NewExpression(src)
		if err != nil {
			return nil, fmt.Errorf("failed to parse expression %s: %w", name, err)
		}
		return expr, nil
	}

	current, err := compile("Current", cfg.Current)
	if err != nil {
		return nil, err
	}
	failed, err := compile("Failed", cfg.Failed)
	if err != nil {
		return nil, err
	}
	inProgress, err := compile("InProgress", cfg.InProgress)
	if err != nil {
		return nil, err
	}

	return &CompiledHealthCheck{
		current:    current,
		failed:     failed,
		inProgress: inProgress,
		celEnabled: true,
	}, nil
}

// Evaluate is the unified evaluator: CEL if enabled, otherwise kstatus fallback.
func (c *CompiledHealthCheck) Evaluate(
	ctx context.Context,
	u *unstructured.Unstructured,
	kstatusFallback func(*unstructured.Unstructured) (*Result, error),
) (*Result, error) {
	if c.celEnabled {
		return c.evalCEL(ctx, u)
	}
	return kstatusFallback(u)
}

// CEL evaluation semantics (RFC 0009 behavior).
func (c *CompiledHealthCheck) evalCEL(ctx context.Context, u *unstructured.Unstructured) (*Result, error) {
	unsObj := u.UnstructuredContent()

	// observedGeneration gate: if status.observedGeneration exists and differs from metadata.generation -> InProgress
	observedGeneration, ok, err := unstructured.NestedInt64(unsObj, "status", "observedGeneration")
	if err != nil {
		return nil, err
	}
	if ok {
		generation, ok, err := unstructured.NestedInt64(unsObj, "metadata", "generation")
		if err != nil {
			return nil, err
		}
		if ok && observedGeneration != generation {
			return &Result{Status: InProgressStatus}, nil
		}
	}

	// Order: InProgress -> Failed -> Current
	if c.inProgress != nil {
		ok, err := c.inProgress.EvaluateBoolean(ctx, unsObj)
		if err != nil {
			return nil, err
		}
		if ok {
			return &Result{Status: InProgressStatus}, nil
		}
	}
	if c.failed != nil {
		ok, err := c.failed.EvaluateBoolean(ctx, unsObj)
		if err != nil {
			return nil, err
		}
		if ok {
			return &Result{Status: FailedStatus}, nil
		}
	}
	ok, err = c.current.EvaluateBoolean(ctx, unsObj)
	if err != nil {
		return nil, err
	}
	if ok {
		return &Result{Status: CurrentStatus}, nil
	}

	return &Result{Status: InProgressStatus}, nil
}

// ---- Unified StatusReader (one struct for both modes) ----

type UnifiedStatusReader struct {
	mapper meta.RESTMapper

	// per GK compiled health config (CEL optional)
	checks map[schema.GroupKind]*CompiledHealthCheck

	// pooled per GK readers (perf)
	readers map[schema.GroupKind]StatusReader

	once sync.Once
}

// NewUnifiedStatusReader returns a factory compatible with “func(mapper) engine.StatusReader”.
func NewUnifiedStatusReader(cfgs []HealthCheckConfig) (func(meta.RESTMapper) StatusReader, error) {
	checks := make(map[schema.GroupKind]*CompiledHealthCheck, len(cfgs))

	for i, cfg := range cfgs {
		gk := schema.FromAPIVersionAndKind(cfg.APIVersion, cfg.Kind).GroupKind()
		if _, exists := checks[gk]; exists {
			return nil, fmt.Errorf("duplicate healthcheck config for GroupKind %s at index %d", gk.String(), i)
		}
		compiled, err := NewCompiledHealthCheck(cfg)
		if err != nil {
			return nil, fmt.Errorf("invalid healthcheck config for %s at index %d: %w", gk.String(), i, err)
		}
		checks[gk] = compiled
	}

	return func(mapper meta.RESTMapper) StatusReader {
		u := &UnifiedStatusReader{
			mapper:  mapper,
			checks:  checks,
			readers: make(map[schema.GroupKind]StatusReader, len(checks)),
		}
		// Build reader pool once.
		u.initReaders()
		return u
	}, nil
}

func (u *UnifiedStatusReader) initReaders() {
	u.once.Do(func() {
		for gk, chk := range u.checks {
			u.readers[gk] = u.newReaderForGK(gk, chk)
		}
	})
}

func (u *UnifiedStatusReader) Supports(gk schema.GroupKind) bool {
	_, ok := u.checks[gk]
	return ok
}

func (u *UnifiedStatusReader) ReadStatus(ctx context.Context, reader ClusterReader, res ObjMetadata) (*ResourceStatus, error) {
	if !u.Supports(res.GroupKind) {
		return nil, fmt.Errorf("GroupKind %s not supported", res.GroupKind.String())
	}
	u.initReaders()
	return u.readers[res.GroupKind].ReadStatus(ctx, reader, res)
}

func (u *UnifiedStatusReader) ReadStatusForObject(ctx context.Context, reader ClusterReader, obj *unstructured.Unstructured) (*ResourceStatus, error) {
	gk, err := groupKindFromUnstructured(obj)
	if err != nil {
		return nil, err
	}
	if !u.Supports(gk) {
		return nil, fmt.Errorf("GroupKind %s not supported", gk.String())
	}
	u.initReaders()
	return u.readers[gk].ReadStatusForObject(ctx, reader, obj)
}

func (u *UnifiedStatusReader) newReaderForGK(gk schema.GroupKind, chk *CompiledHealthCheck) StatusReader {
	// IMPORTANT:
	// This callback signature has no ctx (matches Flux’ GenericStatusReader).
	// To propagate cancellation into CEL evaluation, prefer changing your generic reader to accept ctx.
	//
	// Here we use context.Background() to keep the reader poolable.
	statusFunc := func(obj *unstructured.Unstructured) (*Result, error) {
		return chk.Evaluate(context.Background(), obj, func(o *unstructured.Unstructured) (*Result, error) {
			// Wire your real kstatus default evaluation here.
			// This MUST be the same default behavior you currently have.
			return defaultKStatusStatus(o)
		})
	}

	return NewGenericStatusReader(u.mapper, statusFunc)
}

// groupKindFromUnstructured is hardened (no interface{} assertions).
func groupKindFromUnstructured(u *unstructured.Unstructured) (schema.GroupKind, error) {
	obj := u.UnstructuredContent()

	apiVersion, ok, err := unstructured.NestedString(obj, "apiVersion")
	if err != nil {
		return schema.GroupKind{}, err
	}
	if !ok || apiVersion == "" {
		return schema.GroupKind{}, fmt.Errorf("resource is missing apiVersion field")
	}

	kind, ok, err := unstructured.NestedString(obj, "kind")
	if err != nil {
		return schema.GroupKind{}, err
	}
	if !ok || kind == "" {
		return schema.GroupKind{}, fmt.Errorf("resource is missing kind field")
	}

	return schema.FromAPIVersionAndKind(apiVersion, kind).GroupKind(), nil
}

// defaultKStatusStatus must call your existing kstatus evaluation logic.
// In Flux this is already encapsulated by their status readers; wire the same behavior here.
func defaultKStatusStatus(_ *unstructured.Unstructured) (*Result, error) {
	// Replace with your actual default evaluation.
	return &Result{Status: InProgressStatus}, nil
}
