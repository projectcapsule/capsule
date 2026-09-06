// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resourcelease

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	k8svalidation "k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/internal/controllers/utils"
	"github.com/projectcapsule/capsule/internal/metrics"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/resourcelease"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	evt "github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/ssa"
	"github.com/projectcapsule/capsule/pkg/users"
)

const controllerName = "resourcelease"

const (
	templateResolutionFailedReason = "TemplateResolutionFailed"
	templateContextFailedReason    = "TemplateContextFailed"
	templateRenderingFailedReason  = "TemplateRenderingFailed"
	impersonationFailedReason      = "ImpersonationFailed"
	resourceDryRunFailedReason     = "ResourceDryRunFailed"
	resourcesNotReadyReason        = "ResourcesNotReady"
	resourceApplyFailedReason      = "ResourceApplyFailed"
)

type reconcileStatusError struct {
	reason string
	err    error
}

func (e *reconcileStatusError) Error() string {
	return e.err.Error()
}

func (e *reconcileStatusError) Unwrap() error {
	return e.err
}

func statusError(reason string, err error) error {
	return &reconcileStatusError{reason: reason, err: err}
}

func statusReason(err error) string {
	reason := meta.FailedReason

	var statusErr *reconcileStatusError
	if errors.As(err, &statusErr) {
		reason = statusErr.reason
	}

	return reason
}

func markRequestFailed(
	br *capsulev1beta2.ResourceLease,
	stage capsulev1beta2.ResourceLeaseFailureStage,
	retryPhase capsulev1beta2.ResourceLeasePhase,
	err error,
) error {
	if failureErr := br.FailLease(stage, retryPhase, statusReason(err), err.Error()); failureErr != nil {
		return errors.Join(err, failureErr)
	}

	return err
}

func setReconcileReady(br *capsulev1beta2.ResourceLease, err error) {
	switch {
	case err != nil:
		br.SetReady(metav1.ConditionFalse, statusReason(err), err.Error())
	case br.Status.Phase == capsulev1beta2.ResourceLeasePhaseFailed && br.Status.Failure != nil:
		br.SetReady(metav1.ConditionFalse, br.Status.Failure.Reason, br.Status.Failure.Message)
	case br.Status.Phase == capsulev1beta2.ResourceLeasePhaseFailed:
		br.SetReady(metav1.ConditionFalse, meta.FailedReason, "request failed and may be retried or expired")
	default:
		br.SetReady(metav1.ConditionTrue, meta.SucceededReason, readyMessage(br))
	}
}

type ResourceLeaseReconciler struct {
	client.Client

	Metrics   metrics.ResourceLeasesRecorder
	recorder  evt.EventRecorder
	Log       logr.Logger
	resources ssa.Manager

	Configuration      configuration.Configuration
	ImpersonationCache *cache.ImpersonationCache
}

// SetupWithManager sets up the controller with the Manager.
func (r *ResourceLeaseReconciler) SetupWithManager(mgr ctrl.Manager, _ utils.ControllerOptions) error {
	r.Client = mgr.GetClient()

	r.recorder = evt.NewEventRecorder(
		mgr.GetClient(),
		r.Log.WithName("events"),
		mgr.GetEventRecorder(controllerName),
		r.Configuration,
	)
	if r.ImpersonationCache == nil {
		r.ImpersonationCache = cache.NewImpersonationCache()
	}

	r.resources = ssa.Manager{
		Reader: mgr.GetAPIReader(),
		Mapper: mgr.GetRESTMapper(),
		Metadata: ssa.Metadata{
			CreatedByValue:                      meta.ValueControllerResourceLease,
			ManagedByValue:                      meta.ValueControllerResourceLease,
			ProtectedByValue:                    meta.ValueControllerResourceLease,
			ProtectedByServiceAccountAnnotation: meta.ResourceLeaseServiceAccountAnnotation,
			AppManagedByValue:                   meta.ValueAppResourceLeaseManager,
		},
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&capsulev1beta2.ResourceLease{}).
		Named(controllerName).
		Complete(r)
}

// Reconcile the request.
func (r *ResourceLeaseReconciler) Reconcile(
	ctx context.Context,
	req ctrl.Request,
) (ctrl.Result, error) {
	log := r.Log.WithValues("Request.Name", req.Name).WithValues("Request.Namespace", req.Namespace)

	br := &capsulev1beta2.ResourceLease{}
	if err := r.Get(ctx, req.NamespacedName, br); err != nil {
		if apierrors.IsNotFound(err) {
			// ensure metrics for this object are removed
			r.Metrics.DeleteResourceLeaseMetrics(&capsulev1beta2.ResourceLease{ObjectMeta: metav1.ObjectMeta{Name: req.Name, Namespace: req.Namespace}})
			log.V(5).
				Info("Request object not found, could have been deleted after reconcile request")

			return reconcile.Result{}, nil
		}

		r.Log.Error(err, "Error reading the object")

		return reconcile.Result{}, err
	}

	defer func() {
		r.Metrics.RecordResourceLeasePhase(br)
	}()

	return r.reconcile(ctx, log, br)
}

// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.4/pkg/reconcile
func (r *ResourceLeaseReconciler) reconcile(
	ctx context.Context,
	log logr.Logger,
	br *capsulev1beta2.ResourceLease,
) (res ctrl.Result, err error) {
	defer func() {
		setReconcileReady(br, err)

		r.updateStatus(ctx, log, br)()
	}()

	if !br.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, log, br)
	}

	switch br.Status.Phase {
	case capsulev1beta2.ResourceLeasePhasePending:
		log.V(5).Info("ResourceLease is pending, waiting for TTL")

		return ctrl.Result{}, nil

	case capsulev1beta2.ResourceLeasePhaseApproved:
		r.recordTransitionEvent(
			ctx,
			br,
			capsulev1beta2.ResourceLeasePhaseApproved,
			evt.ReasonResourceLeaseApproved,
			evt.ActionApproved,
		)

		result, activationErr := r.reconcileApproved(ctx, log, br)
		if activationErr != nil {
			return result, markRequestFailed(
				br,
				capsulev1beta2.ResourceLeaseFailureStageActivation,
				capsulev1beta2.ResourceLeasePhaseApproved,
				activationErr,
			)
		}

		return result, nil

	case capsulev1beta2.ResourceLeasePhaseFailed:
		log.V(5).Info("ResourceLease failed and is waiting for retry or expiry")

		return ctrl.Result{}, nil

	case capsulev1beta2.ResourceLeasePhaseRetrying:
		return r.reconcileRetrying(ctx, log, br)

	case capsulev1beta2.ResourceLeasePhaseDenied:
		r.recordTransitionEvent(
			ctx,
			br,
			capsulev1beta2.ResourceLeasePhaseDenied,
			evt.ReasonResourceLeaseDenied,
			evt.ActionDenied,
		)

		log.V(5).Info("ResourceLease is denied, handling denied state")

		return ctrl.Result{}, nil

	case capsulev1beta2.ResourceLeasePhaseActive:
		if err := r.addFinalizer(ctx, log, br); err != nil {
			return ctrl.Result{}, err
		}

		if br.Status.Active == nil || br.Status.Active.ActiveUntil == nil {
			return ctrl.Result{}, nil
		}

		if !metav1.Now().After(br.Status.Active.ActiveUntil.Time) {
			log.V(5).Info("Re-queueing when expiration is due")

			return ctrl.Result{
				RequeueAfter: time.Until(br.Status.Active.ActiveUntil.Time),
			}, nil
		}

		if err := br.ExpireLease(nil); err != nil {
			return ctrl.Result{}, err
		}

		r.recordTransitionEvent(
			ctx,
			br,
			capsulev1beta2.ResourceLeasePhaseExpired,
			evt.ReasonResourceLeaseExpired,
			evt.ActionExpired,
		)

		return ctrl.Result{}, nil

	// When the ResourceLease has expired
	case capsulev1beta2.ResourceLeasePhaseExpired:
		r.recordTransitionEvent(
			ctx,
			br,
			capsulev1beta2.ResourceLeasePhaseExpired,
			evt.ReasonResourceLeaseExpired,
			evt.ActionExpired,
		)

		if len(br.Status.ProcessedItems) > 0 {
			resourceClient, loadErr := r.resourceClient(ctx, log, br, nil)
			if loadErr != nil {
				return ctrl.Result{}, statusError(impersonationFailedReason, loadErr)
			}

			if err := r.pruneItems(ctx, br, resourceClient); err != nil {
				return ctrl.Result{}, err
			}
		}

		if br.Status.KeepUntil == nil ||
			time.Until(br.Status.KeepUntil.Time) <= 0 {
			log.V(5).Info("ResourceLease is expired, deleting br")
			br.DeleteLease()

			if err := r.Update(ctx, br); err != nil {
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, r.Delete(ctx, br)
		}

		log.V(5).WithValues("keep-date", br.Status.KeepUntil.Time).
			Info("ResourceLease is expired, Holding expired state until keep date is reached")

		return ctrl.Result{RequeueAfter: time.Until(br.Status.KeepUntil.Time)}, nil

	// The case when the ResourceLease is newly created
	case "", capsulev1beta2.ResourceLeasePhaseCreated:
		return r.reconcileNew(ctx, log, br)

	case capsulev1beta2.ResourceLeasePhaseRequested:
		return ctrl.Result{}, nil
	default:
		log.WithValues("phase", br.Status.Phase).Info("Unhandled phase")

		return ctrl.Result{}, nil
	}
}

// recordTransitionEvent emits a Kubernetes lifecycle event once and records
// its emission time on the corresponding audit transition.
func (r *ResourceLeaseReconciler) recordTransitionEvent(
	ctx context.Context,
	br *capsulev1beta2.ResourceLease,
	phase capsulev1beta2.ResourceLeasePhase,
	reason,
	action string,
) {
	transition := br.LatestTransition(phase)
	if transition == nil || transition.EventTime != nil || r.recorder == nil {
		return
	}

	r.recorder.LabeledEvent(
		br,
		corev1.EventTypeNormal,
		reason,
		action,
		transition.Message,
	).WithLabels(map[string]string{
		meta.EventActorLabel:     eventActorLabelValue(transition.Actor.Name),
		meta.EventActorKindLabel: transition.Actor.Type.String(),
	}).Emit(ctx)

	now := metav1.Now()
	transition.EventTime = &now
}

// eventActorLabelValue preserves ordinary actor names and converts identities
// such as service-account usernames and email addresses into a stable value
// accepted by Kubernetes label validation. The exact identity remains recorded
// in status.transitions[].actor.name.
func eventActorLabelValue(actor string) string {
	if len(k8svalidation.IsValidLabelValue(actor)) == 0 {
		return actor
	}

	var normalized strings.Builder

	for _, char := range actor {
		switch {
		case char >= 'a' && char <= 'z',
			char >= 'A' && char <= 'Z',
			char >= '0' && char <= '9',
			char == '-', char == '_', char == '.':
			normalized.WriteRune(char)
		default:
			normalized.WriteByte('_')
		}
	}

	prefix := strings.Trim(normalized.String(), "-_.")
	if prefix == "" {
		prefix = "actor"
	}

	hash := sha256.Sum256([]byte(actor))
	suffix := fmt.Sprintf("-%x", hash[:4])
	maxLength := 63 - len(suffix)

	if len(prefix) > maxLength {
		prefix = strings.TrimRight(prefix[:maxLength], "-_.")
	}

	return prefix + suffix
}

func (r *ResourceLeaseReconciler) reconcileApproved(
	ctx context.Context,
	log logr.Logger,
	br *capsulev1beta2.ResourceLease,
) (ctrl.Result, error) {
	log.V(5).Info("ResourceLease is approved, checking if duration can be started")

	if br.Status.Request == nil {
		return ctrl.Result{}, fmt.Errorf("ResourceLease is in Approved phase but status.request is nil")
	}

	if !k8smeta.IsStatusConditionTrue(br.Status.Conditions, meta.ReadyCondition) {
		return ctrl.Result{}, statusError(
			resourcesNotReadyReason,
			errors.New("rendered resources are not ready for activation"),
		)
	}

	brt, err := r.loadTemplate(ctx, br)
	if err != nil {
		return ctrl.Result{}, statusError(templateResolutionFailedReason, fmt.Errorf(
			"failed to get ResourceLease Template %s: %w",
			br.Spec.Template.Name,
			err,
		))
	}

	if err := br.CheckApprovalConditions(ctx, brt); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to verify approval for ResourceLease %s: %w", br.Name, err)
	}

	approvals := br.ApprovalPolicy(brt)
	if len(approvals.Approvers) > 0 {
		if br.Status.Review == nil || br.Status.Review.Reviewer == nil {
			return ctrl.Result{}, fmt.Errorf("ResourceLease %s has no reviewer", br.Name)
		}

		reviewer := br.Status.Review.Reviewer
		if reviewer.Type != resourcelease.AccessEntityTypeSystem &&
			!approvals.IsApprover(reviewer.Name, reviewer.Groups) {
			return ctrl.Result{}, fmt.Errorf("reviewer %q is not permitted to approve ResourceLease %s", reviewer.Name, br.Name)
		}
	}

	if err := r.addFinalizer(ctx, log, br); err != nil {
		return ctrl.Result{}, err
	}

	if err := br.ResolveLeaseStatus(brt); err != nil {
		return ctrl.Result{}, err
	}

	if br.Status.Request.StartTime != nil {
		if wait := time.Until(br.Status.Request.StartTime.Time); wait > 0 {
			log.V(5).Info("ResourceLease is approved, waiting for startTime", "startTime", br.Status.Request.StartTime.Time)

			return ctrl.Result{RequeueAfter: wait}, nil
		}
	}

	resourceClient, err := r.resourceClient(ctx, log, br, brt)
	if err != nil {
		return ctrl.Result{}, statusError(impersonationFailedReason, err)
	}

	if err := r.validateResolvedServiceAccount(ctx, br); err != nil {
		return ctrl.Result{}, statusError(impersonationFailedReason, err)
	}

	log.V(5).Info("ResourceLease is approved, activating br")

	if err := r.transitionRequestActivation(ctx, br, resourceClient); err != nil {
		return ctrl.Result{}, fmt.Errorf(
			"failed to activate ResourceLease %s: %w",
			br.Name,
			err,
		)
	}

	log.V(5).Info("ResourceLease activated successfully")

	r.recordTransitionEvent(
		ctx,
		br,
		capsulev1beta2.ResourceLeasePhaseActive,
		evt.ReasonResourceLeaseActivated,
		evt.ActionActivated,
	)

	return ctrl.Result{}, nil
}

func (r *ResourceLeaseReconciler) reconcileNew(
	ctx context.Context,
	log logr.Logger,
	br *capsulev1beta2.ResourceLease,
) (ctrl.Result, error) {
	if err := br.SetCreated(&br.Spec.Requestor); err != nil {
		return ctrl.Result{}, err
	}

	brt, err := r.loadTemplate(ctx, br)
	if err != nil {
		return ctrl.Result{}, statusError(templateResolutionFailedReason, fmt.Errorf(
			"failed to get ResourceLease Template %s: %w",
			br.Spec.Template.Name,
			err,
		))
	}

	properties, err := br.GenerateRequestStatus(brt)
	if err != nil {
		return ctrl.Result{}, err
	}

	properties.Template = &capsulev1beta2.ResolvedResourceLeaseTemplateReference{
		ResourceLeaseTemplateReference: br.Spec.Template,
		ResourceVersion:                brt.GetResourceVersion(),
	}
	br.Status.Request = properties

	resourceClient, err := r.resourceClient(ctx, log, br, brt)
	if err != nil {
		return ctrl.Result{}, statusError(impersonationFailedReason, err)
	}

	if err := r.renderResources(ctx, br, brt, resourceClient); err != nil {
		return ctrl.Result{}, err
	}

	retryPhase := capsulev1beta2.ResourceLeasePhaseRequested
	autoApprove := false

	if br.ApprovalPolicy(brt).Auto {
		approved, approvalErr := br.EvaluateApprovalConditions(ctx, brt)
		if approvalErr != nil {
			return ctrl.Result{}, fmt.Errorf(
				"auto approval could not be evaluated for ResourceLease %s: %w",
				br.Name,
				approvalErr,
			)
		}

		if approved {
			retryPhase = capsulev1beta2.ResourceLeasePhaseApproved
			autoApprove = true
		}
	}

	if err := r.validateResolvedServiceAccount(ctx, br); err != nil {
		return ctrl.Result{}, markRequestFailed(
			br,
			capsulev1beta2.ResourceLeaseFailureStagePreflight,
			retryPhase,
			statusError(impersonationFailedReason, err),
		)
	}

	if dryRunErr := r.dryRunItems(ctx, br, resourceClient); dryRunErr != nil {
		return ctrl.Result{}, markRequestFailed(
			br,
			capsulev1beta2.ResourceLeaseFailureStagePreflight,
			retryPhase,
			statusError(resourceDryRunFailedReason, fmt.Errorf("resource preflight failed: %w", dryRunErr)),
		)
	}

	if err := br.SetRequestedBy(&br.Spec.Requestor); err != nil {
		return ctrl.Result{}, err
	}

	if !autoApprove {
		return r.requestReview(ctx, log, br)
	}

	return ctrl.Result{}, br.ApproveLease(&resourcelease.AccessEntity{
		Type: resourcelease.AccessEntityTypeSystem,
	}, br.Status.Request, "Auto Approved")
}

func (r *ResourceLeaseReconciler) reconcileRetrying(
	ctx context.Context,
	log logr.Logger,
	br *capsulev1beta2.ResourceLease,
) (ctrl.Result, error) {
	if br.Status.Failure == nil {
		return ctrl.Result{}, errors.New("ResourceLease is Retrying but status.failure is nil")
	}

	failure := *br.Status.Failure

	switch failure.Stage {
	case capsulev1beta2.ResourceLeaseFailureStagePreflight:
		resourceClient, err := r.resourceClient(ctx, log, br, nil)
		if err != nil {
			return ctrl.Result{}, markRequestFailed(
				br,
				failure.Stage,
				failure.RetryPhase,
				statusError(impersonationFailedReason, err),
			)
		}

		if err := r.validateResolvedServiceAccount(ctx, br); err != nil {
			return ctrl.Result{}, markRequestFailed(
				br,
				failure.Stage,
				failure.RetryPhase,
				statusError(impersonationFailedReason, err),
			)
		}

		if dryRunErr := r.dryRunItems(ctx, br, resourceClient); dryRunErr != nil {
			return ctrl.Result{}, markRequestFailed(
				br,
				failure.Stage,
				failure.RetryPhase,
				statusError(resourceDryRunFailedReason, fmt.Errorf("resource preflight failed: %w", dryRunErr)),
			)
		}

		return ctrl.Result{}, br.CompleteRetry()

	case capsulev1beta2.ResourceLeaseFailureStageActivation:
		if err := br.CompleteRetry(); err != nil {
			return ctrl.Result{}, err
		}

		// reconcileApproved requires the snapshot to be Ready. The deferred
		// status update records the final result after this attempt.
		br.SetReady(metav1.ConditionTrue, meta.ReconcilingReason, "retrying resource activation")

		result, activationErr := r.reconcileApproved(ctx, log, br)
		if activationErr != nil {
			return result, markRequestFailed(
				br,
				capsulev1beta2.ResourceLeaseFailureStageActivation,
				capsulev1beta2.ResourceLeasePhaseApproved,
				activationErr,
			)
		}

		return result, nil
	default:
		return ctrl.Result{}, fmt.Errorf("unsupported retry failure stage %q", failure.Stage)
	}
}

func (r *ResourceLeaseReconciler) loadTemplate(
	ctx context.Context,
	br *capsulev1beta2.ResourceLease,
) (capsulev1beta2.ResourceLeaseTemplateSource, error) {
	switch br.Spec.Template.Kind {
	case capsulev1beta2.ResourceLeaseTemplateKind:
		brt := &capsulev1beta2.ResourceLeaseTemplate{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: br.Namespace, Name: br.Spec.Template.Name}, brt); err != nil {
			return nil, err
		}

		return brt, nil
	case capsulev1beta2.GlobalResourceLeaseTemplateKind:
		brt := &capsulev1beta2.GlobalResourceLeaseTemplate{}
		if err := r.Get(ctx, client.ObjectKey{Name: br.Spec.Template.Name}, brt); err != nil {
			return nil, err
		}

		return brt, nil
	default:
		return nil, fmt.Errorf("unsupported ResourceLease template kind %q", br.Spec.Template.Kind)
	}
}

func (r *ResourceLeaseReconciler) renderResources(
	ctx context.Context,
	br *capsulev1beta2.ResourceLease,
	brt capsulev1beta2.ResourceLeaseTemplateSource,
	resourceClient client.Client,
) error {
	templateData := brt.TemplateData()

	if br.Status.Request == nil {
		properties, err := br.GenerateRequestStatus(brt)
		if err != nil {
			return err
		}

		br.Status.Request = properties
	}

	loadedContext, err := br.LoadTemplateContext(
		ctx,
		resourceClient,
		r.managedResourceManager(resourceClient, br).Mapper,
		templateData.ParamSchema,
		templateData.Context,
	)
	if err != nil {
		br.Status.Request.Resources = nil

		return statusError(templateContextFailedReason, fmt.Errorf("loading template context: %w", err))
	}

	rendered, renderErr := br.RenderResources( //nolint:contextcheck // rendering has no context-aware public API
		templateData.ParamSchema,
		templateData.Resources,
		loadedContext,
	)
	manager := r.managedResourceManager(resourceClient, br)

	var preparationErr error

	for resourceIndex := range rendered {
		for targetIndex, raw := range rendered[resourceIndex].Targets {
			obj, targetErr := object(raw)
			if targetErr != nil {
				preparationErr = errors.Join(preparationErr, fmt.Errorf(
					"preparing resource %d target %d: %w",
					resourceIndex,
					targetIndex,
					targetErr,
				))

				continue
			}

			defaultTargetNamespace(obj, br.Namespace)

			labels := obj.GetLabels()
			if labels == nil {
				labels = map[string]string{}
			}

			labels[meta.AppManagedByLabel] = meta.ValueAppResourceLeaseManager
			obj.SetLabels(labels)
			rendered[resourceIndex].Targets[targetIndex] = runtime.RawExtension{Object: obj}

			if _, targetErr := managedResourceStatus(manager, obj); targetErr != nil {
				preparationErr = errors.Join(preparationErr, fmt.Errorf(
					"preparing resource %d target %d: %w",
					resourceIndex,
					targetIndex,
					targetErr,
				))
			}
		}
	}

	// Persist successful render results even when another target failed. This
	// gives users a useful status preview while Ready=False prevents approval or
	// application of a partial result.
	br.Status.Request.Resources = rendered

	if err := errors.Join(renderErr, preparationErr); err != nil {
		return statusError(templateRenderingFailedReason, err)
	}

	return nil
}

func readyMessage(br *capsulev1beta2.ResourceLease) string {
	switch br.Status.Phase {
	case capsulev1beta2.ResourceLeasePhaseCreated:
		return "request is being prepared"
	case capsulev1beta2.ResourceLeasePhaseRequested:
		return "rendered resources are ready for review"
	case capsulev1beta2.ResourceLeasePhasePending:
		return "request is ready for review"
	case capsulev1beta2.ResourceLeasePhaseDenied:
		return "request was denied"
	case capsulev1beta2.ResourceLeasePhaseApproved:
		return "rendered resources are ready for activation"
	case capsulev1beta2.ResourceLeasePhaseActive:
		return "managed resources are ready"
	case capsulev1beta2.ResourceLeasePhaseFailed:
		return "request failed and may be retried or expired"
	case capsulev1beta2.ResourceLeasePhaseRetrying:
		return "request retry is in progress"
	case capsulev1beta2.ResourceLeasePhaseExpired:
		return "managed resources were pruned"
	default:
		return "reconciled"
	}
}

func (r *ResourceLeaseReconciler) requestReview(
	ctx context.Context,
	log logr.Logger,
	br *capsulev1beta2.ResourceLease,
) (ctrl.Result, error) {
	log.V(5).Info("ResourceLease is ready for review")

	r.recordTransitionEvent(
		ctx,
		br,
		capsulev1beta2.ResourceLeasePhaseRequested,
		evt.ReasonResourceLeaseReviewNeeded,
		evt.ActionPendingReview,
	)

	return ctrl.Result{}, nil
}

func (r *ResourceLeaseReconciler) updateStatus(
	ctx context.Context,
	log logr.Logger,
	br *capsulev1beta2.ResourceLease,
) func() {
	return func() {
		err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			current := &capsulev1beta2.ResourceLease{}
			if err := r.Get(ctx, client.ObjectKeyFromObject(br), current); err != nil {
				return fmt.Errorf("failed to refetch instance before update: %w", err)
			}

			current.Status = br.Status

			log.V(7).Info("updating status", "status", current.Status)

			if err := r.Client.Status().Update(ctx, current); err != nil {
				return fmt.Errorf("failed to update status: %w", err)
			}

			return nil
		})
		if err != nil {
			if apierrors.IsNotFound(err) {
				// if the br is deleted, we cannot find it anymore
				return
			}

			log.Error(err, "failed updating status")
		} else {
			log.V(7).Info("successful update", "status", br.Status)
		}
	}
}

// Add a finalizer so managed resources are pruned before deletion and the
// ResourceLease can be retained for its configured audit period.
func (r *ResourceLeaseReconciler) addFinalizer(
	ctx context.Context,
	log logr.Logger,
	br *capsulev1beta2.ResourceLease,
) error {
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, br, func() error {
		finalizerName := meta.ControllerFinalizer
		if controllerutil.ContainsFinalizer(br, finalizerName) {
			log.V(5).Info("Finalizer already exists", "name", br.Name)

			return nil
		}

		log.V(5).Info("Adding finalizer to ResourceLease", "name", br.Name)
		controllerutil.AddFinalizer(br, finalizerName)

		return nil
	}); err != nil {
		return fmt.Errorf("failed to add finalizer to ResourceLease %s: %w", br.Name, err)
	}

	return r.Get(ctx, client.ObjectKeyFromObject(br), br)
}

func (r *ResourceLeaseReconciler) reconcileDelete(
	ctx context.Context,
	log logr.Logger,
	br *capsulev1beta2.ResourceLease,
) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(br, meta.ControllerFinalizer) {
		return ctrl.Result{}, nil
	}

	if len(br.Status.ProcessedItems) > 0 {
		resourceClient, err := r.resourceClient(ctx, log, br, nil)
		if err != nil {
			return ctrl.Result{}, statusError(impersonationFailedReason, err)
		}

		if err := r.pruneItems(ctx, br, resourceClient); err != nil {
			return ctrl.Result{}, err
		}
	}

	namespaceTerminating, err := r.namespaceTerminating(ctx, br.Namespace)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("checking namespace termination: %w", err)
	}

	if !namespaceTerminating && br.Status.KeepUntil != nil {
		if wait := time.Until(br.Status.KeepUntil.Time); wait > 0 {
			return ctrl.Result{RequeueAfter: wait}, nil
		}
	}

	controllerutil.RemoveFinalizer(br, meta.ControllerFinalizer)

	return ctrl.Result{}, r.Update(ctx, br)
}

func (r *ResourceLeaseReconciler) namespaceTerminating(
	ctx context.Context,
	namespace string,
) (bool, error) {
	if namespace == "" {
		return false, nil
	}

	ns := &corev1.Namespace{}
	if err := r.Get(ctx, client.ObjectKey{Name: namespace}, ns); err != nil {
		if apierrors.IsNotFound(err) {
			return true, nil
		}

		return false, err
	}

	return ns.DeletionTimestamp != nil || ns.Status.Phase == corev1.NamespaceTerminating, nil
}

// When a request is approved, it can be activated immediately or after a certain duration.
func (r *ResourceLeaseReconciler) transitionRequestActivation(
	ctx context.Context,
	br *capsulev1beta2.ResourceLease,
	resourceClient client.Client,
) error {
	// Avoid persisting the Active phase when item reconciliation fails.
	brCopy := br.DeepCopy()

	if err := brCopy.ActivateLease(nil); err != nil {
		return err
	}

	// Reflect Binding
	if err := r.reconcileItems(ctx, brCopy, resourceClient); err != nil {
		// Persist the rendered identities so partially applied resources can be
		// pruned if activation is cancelled or the request is deleted.
		br.Status.Request.Resources = brCopy.Status.Request.Resources
		br.Status.ManagedResourcesStatus = brCopy.Status.ManagedResourcesStatus

		return statusError(resourceApplyFailedReason, fmt.Errorf(
			"failed to create ResourceLease items %s: %w",
			brCopy.Name,
			err,
		))
	}

	br.Status = brCopy.Status
	br.Finalizers = brCopy.Finalizers

	return nil
}

// dryRunItems exercises the exact server-side apply request for every rendered
// target with the resolved execution identity. It does not persist resources,
// tracking metadata, or processed-item status.
func (r *ResourceLeaseReconciler) dryRunItems(
	ctx context.Context,
	br *capsulev1beta2.ResourceLease,
	resourceClient client.Client,
) error {
	if br.Status.Request == nil {
		return errors.New("request status is nil")
	}

	if err := r.validateResolvedServiceAccount(ctx, br); err != nil {
		return err
	}

	var dryRunErr error

	manager := r.managedResourceManager(resourceClient, br)
	fieldOwner := meta.ResourceLeaseFieldOwner(br)

	for resourceIndex, resource := range br.Status.Request.Resources {
		for targetIndex, raw := range resource.Targets {
			obj, err := object(raw)
			if err != nil {
				dryRunErr = errors.Join(dryRunErr, fmt.Errorf(
					"resource %d target %d: %w",
					resourceIndex,
					targetIndex,
					err,
				))

				continue
			}

			defaultTargetNamespace(obj, br.Namespace)

			_, err = manager.Apply(ctx, resourceClient, obj, ssa.ApplyOptions{
				FieldOwner: fieldOwner,
				Force:      resource.Policy.Force,
				Adopt:      resource.Policy.AllowsAdoption(),
				Protect:    resource.Policy.IsProtected(),
				DryRun:     true,
			})
			if err != nil {
				dryRunErr = errors.Join(dryRunErr, fmt.Errorf(
					"resource %d target %d: %w",
					resourceIndex,
					targetIndex,
					err,
				))
			}
		}
	}

	return dryRunErr
}

// Creates the necessary items resources for the ResourceLease.
func (r *ResourceLeaseReconciler) reconcileItems(
	ctx context.Context,
	br *capsulev1beta2.ResourceLease,
	resourceClient client.Client,
) (err error) {
	var syncErr error

	if br.Status.Request == nil {
		return errors.New("request status is nil")
	}

	if br.Status.Active == nil {
		return errors.New("active status is nil")
	}

	currentItems := br.Status.ProcessedItems
	processedItems := make(meta.ProcessedItems, 0)

	manager := r.managedResourceManager(resourceClient, br)
	fieldOwner := meta.ResourceLeaseFieldOwner(br)

	for resourceIndex := range br.Status.Request.Resources {
		resource := &br.Status.Request.Resources[resourceIndex]

		for targetIndex, raw := range resource.Targets {
			obj, decodeErr := object(raw)
			if decodeErr != nil {
				syncErr = errors.Join(syncErr, decodeErr)

				continue
			}

			defaultTargetNamespace(obj, br.Namespace)

			if br.Status.Active.ActiveUntil != nil {
				ann := obj.GetAnnotations()
				if ann == nil {
					ann = map[string]string{}
				}

				ann[meta.ResourceLeaseActiveUntilAnnotation] = br.Status.Active.ActiveUntil.Format(time.RFC3339)
				obj.SetAnnotations(ann)
			}

			labels := obj.GetLabels()
			if labels == nil {
				labels = map[string]string{}
			}

			labels[meta.AppManagedByLabel] = meta.ValueAppResourceLeaseManager
			obj.SetLabels(labels)

			// Persist the exact SSA input before applying it. This remains the
			// source of truth for retries and pruning, including after a partial
			// apply failure.
			resource.Targets[targetIndex] = runtime.RawExtension{Object: obj.DeepCopy()}

			// ResourceLeases are namespaced but may manage cluster-scoped objects,
			// so their lifecycle cannot rely on Kubernetes owner references. The
			// ResourceLease finalizer and recorded target identities provide the
			// explicit cascade during expiration or deletion.
			item, statusErr := managedResourceStatus(manager, obj)
			if statusErr != nil {
				syncErr = errors.Join(syncErr, statusErr)

				continue
			}

			current := currentItems.GetItem(item.ResourceID)
			result, applyErr := manager.Apply(ctx, resourceClient, obj, ssa.ApplyOptions{
				FieldOwner:        fieldOwner,
				Force:             resource.Policy.Force,
				Adopt:             resource.Policy.AllowsAdoption(),
				Protect:           resource.Policy.IsProtected(),
				PreviouslyCreated: current != nil && current.Created,
			})
			item.Created = result.Created

			if result.LastApply != nil {
				item.LastApply = *result.LastApply
			}

			if applyErr != nil {
				item.Status = metav1.ConditionFalse
				item.Message = "apply failed: " + applyErr.Error()
				syncErr = errors.Join(syncErr, applyErr)
			} else {
				item.Status = metav1.ConditionTrue
			}

			processedItems.UpdateItem(item)
		}
	}

	processedItems.SortDeterministic()
	br.Status.ProcessedItems = processedItems
	br.Status.UpdateStats()

	return syncErr
}

// pruneItems relinquishes the ResourceLease field manager's resources.
func (r *ResourceLeaseReconciler) pruneItems(
	ctx context.Context,
	br *capsulev1beta2.ResourceLease,
	resourceClient client.Client,
) (err error) {
	var syncErr error

	if br.Status.Request == nil {
		return errors.New("request status is nil")
	}

	manager := r.managedResourceManager(resourceClient, br)
	fieldOwner := meta.ResourceLeaseFieldOwner(br)

	for _, resource := range br.Status.Request.Resources {
		for _, target := range resource.Targets {
			obj, err := object(target)
			if err != nil {
				syncErr = errors.Join(syncErr, err)

				continue
			}

			item, statusErr := managedResourceStatus(manager, obj)
			if statusErr != nil {
				syncErr = errors.Join(syncErr, statusErr)

				continue
			}

			current := br.Status.ProcessedItems.GetItem(item.ResourceID)

			if resource.Policy.ShouldOrphan() {
				if orphanErr := manager.Orphan(ctx, resourceClient, obj, nil); orphanErr != nil {
					item.Status = metav1.ConditionFalse
					item.Message = "orphan failed: " + orphanErr.Error()
					br.Status.ProcessedItems.UpdateItem(item)

					syncErr = errors.Join(syncErr, orphanErr)

					continue
				}

				br.Status.ProcessedItems.RemoveItem(item)

				continue
			}

			deleted, pruneErr := manager.Prune(ctx, resourceClient, obj, ssa.PruneOptions{
				FieldOwner:        fieldOwner,
				PreviouslyCreated: current != nil && current.Created,
			})
			if pruneErr != nil {
				item.Status = metav1.ConditionFalse
				item.Message = "prune failed: " + pruneErr.Error()
				br.Status.ProcessedItems.UpdateItem(item)

				syncErr = errors.Join(syncErr, pruneErr)

				continue
			}

			if !deleted {
				if disownErr := manager.Disown(ctx, resourceClient, obj, nil); disownErr != nil {
					item.Status = metav1.ConditionFalse
					item.Message = "disown failed: " + disownErr.Error()
					br.Status.ProcessedItems.UpdateItem(item)

					syncErr = errors.Join(syncErr, disownErr)

					continue
				}
			}

			br.Status.ProcessedItems.RemoveItem(item)
		}
	}

	br.Status.ProcessedItems.SortDeterministic()
	br.Status.UpdateStats()

	return syncErr
}

func managedResourceStatus(
	manager ssa.Manager,
	obj *unstructured.Unstructured,
) (meta.ObjectReferenceStatus, error) {
	id, clusterScoped, err := manager.ResolveResourceID(obj, "", "")
	if err != nil {
		return meta.ObjectReferenceStatus{}, fmt.Errorf("resolving managed resource identity: %w", err)
	}

	return meta.ObjectReferenceStatus{
		ResourceID: id,
		ObjectReferenceStatusCondition: meta.ObjectReferenceStatusCondition{
			Type:          meta.ReadyCondition,
			ClusterScoped: clusterScoped,
		},
	}, nil
}

func object(re runtime.RawExtension) (*unstructured.Unstructured, error) {
	// Prefer decoded object when present.
	if re.Object != nil {
		if obj, ok := re.Object.(*unstructured.Unstructured); ok {
			return obj.DeepCopy(), nil
		}

		us, err := runtime.DefaultUnstructuredConverter.ToUnstructured(re.Object)
		if err != nil {
			return nil, err
		}

		return &unstructured.Unstructured{Object: us}, nil
	}

	// Fall back to Raw for objects coming back from the API server.
	if len(re.Raw) == 0 {
		return nil, errors.New("object is nil")
	}

	obj := &unstructured.Unstructured{}
	if _, _, err := unstructured.UnstructuredJSONScheme.Decode(re.Raw, nil, obj); err != nil {
		return nil, err
	}

	return obj, nil
}

func defaultTargetNamespace(obj *unstructured.Unstructured, namespace string) {
	if obj.GetNamespace() == "" {
		obj.SetNamespace(namespace)
	}
}

func (r *ResourceLeaseReconciler) validateResolvedServiceAccount(
	ctx context.Context,
	br *capsulev1beta2.ResourceLease,
) error {
	if br.Status.Request == nil || br.Status.Request.Impersonation == nil {
		return errors.New("resolved ServiceAccount is nil")
	}

	controllerName, controllerNamespace := configuration.ControllerServiceAccount()
	if br.Status.Request.Impersonation.Name.String() == controllerName &&
		br.Status.Request.Impersonation.Namespace.String() == controllerNamespace {
		return nil
	}

	reader := r.resources.Reader
	if reader == nil {
		reader = r.Client
	}

	key := client.ObjectKey{
		Name:      br.Status.Request.Impersonation.Name.String(),
		Namespace: br.Status.Request.Impersonation.Namespace.String(),
	}
	serviceAccount := &corev1.ServiceAccount{}

	if err := reader.Get(ctx, key, serviceAccount); err != nil {
		return fmt.Errorf(
			"resolved ServiceAccount %s/%s is unavailable: %w",
			key.Namespace,
			key.Name,
			err,
		)
	}

	return nil
}

func (r *ResourceLeaseReconciler) resourceClient(
	ctx context.Context,
	log logr.Logger,
	br *capsulev1beta2.ResourceLease,
	brt capsulev1beta2.ResourceLeaseTemplateSource,
) (client.Client, error) {
	if br.Status.Request == nil {
		br.Status.Request = &capsulev1beta2.ResourceLeaseStatusRequest{}
	}

	serviceAccount := br.Status.Request.Impersonation
	if serviceAccount == nil {
		if brt != nil {
			serviceAccount = r.resolveTemplateServiceAccount(log, brt)
		}

		if serviceAccount == nil {
			controllerName, controllerNamespace := configuration.ControllerServiceAccount()
			serviceAccount = &meta.NamespacedRFC1123ObjectReferenceWithNamespace{
				Name:      meta.RFC1123Name(controllerName),
				Namespace: meta.RFC1123SubdomainName(controllerNamespace),
			}
		}

		resolved := *serviceAccount
		br.Status.Request.Impersonation = &resolved
	}

	controllerName, controllerNamespace := configuration.ControllerServiceAccount()
	if serviceAccount.Name.String() == controllerName &&
		serviceAccount.Namespace.String() == controllerNamespace {
		return r.Client, nil
	}

	if r.ImpersonationCache == nil {
		return nil, errors.New("impersonation client cache is not configured")
	}

	if cached, ok := r.ImpersonationCache.Get(
		serviceAccount.Namespace.String(),
		serviceAccount.Name.String(),
	); ok {
		return cached, nil
	}

	if r.Configuration == nil {
		return nil, errors.New("capsule configuration is required for service account impersonation")
	}

	restConfig, err := r.Configuration.ServiceAccountClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("load impersonation REST configuration: %w", err)
	}

	log.V(5).Info(
		"using impersonation client for ResourceLease resources",
		"serviceaccount", serviceAccount.Name,
		"namespace", serviceAccount.Namespace,
	)

	resourceClient, err := r.ImpersonationCache.LoadOrCreate(
		ctx,
		log,
		restConfig,
		r.Scheme(),
		*serviceAccount,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"load client for ServiceAccount %s/%s: %w",
			serviceAccount.Namespace,
			serviceAccount.Name,
			err,
		)
	}

	return resourceClient, nil
}

func (r *ResourceLeaseReconciler) resolveTemplateServiceAccount(
	log logr.Logger,
	brt capsulev1beta2.ResourceLeaseTemplateSource,
) *meta.NamespacedRFC1123ObjectReferenceWithNamespace {
	switch template := brt.(type) {
	case *capsulev1beta2.ResourceLeaseTemplate:
		if template == nil {
			return nil
		}

		if template.Spec.Impersonation != nil {
			return &meta.NamespacedRFC1123ObjectReferenceWithNamespace{
				Name:      template.Spec.Impersonation.Name,
				Namespace: meta.RFC1123SubdomainName(template.Namespace),
			}
		}

		if r.Configuration == nil {
			return nil
		}

		name := r.Configuration.ServiceAccountClientProperties().TenantDefaultServiceAccount
		if name == "" {
			return nil
		}

		return &meta.NamespacedRFC1123ObjectReferenceWithNamespace{
			Name:      name,
			Namespace: meta.RFC1123SubdomainName(template.Namespace),
		}
	case *capsulev1beta2.GlobalResourceLeaseTemplate:
		if template == nil {
			return nil
		}

		return r.resolveGlobalTemplateServiceAccount(log, template)
	default:
		return nil
	}
}

func (r *ResourceLeaseReconciler) resolveGlobalTemplateServiceAccount(
	log logr.Logger,
	brt *capsulev1beta2.GlobalResourceLeaseTemplate,
) *meta.NamespacedRFC1123ObjectReferenceWithNamespace {
	if brt.Spec.Impersonation != nil {
		return brt.Spec.Impersonation.DeepCopy()
	}

	if r.Configuration == nil {
		return nil
	}

	properties := r.Configuration.ServiceAccountClientProperties()
	name := properties.GlobalDefaultServiceAccount.String()
	namespace := properties.GlobalDefaultServiceAccountNamespace.String()

	if (name == "") != (namespace == "") {
		log.V(2).Info(
			"global default impersonation ServiceAccount requires both name and namespace",
			"name", name,
			"namespace", namespace,
		)

		return nil
	}

	if name == "" {
		return nil
	}

	return &meta.NamespacedRFC1123ObjectReferenceWithNamespace{
		Name:      properties.GlobalDefaultServiceAccount,
		Namespace: properties.GlobalDefaultServiceAccountNamespace,
	}
}

func (r *ResourceLeaseReconciler) managedResourceManager(
	resourceClient client.Client,
	br *capsulev1beta2.ResourceLease,
) ssa.Manager {
	manager := r.resources

	if resourceClient == nil {
		resourceClient = r.Client
	}

	manager.Reader = resourceClient

	if manager.Metadata.CreatedByValue == "" {
		manager.Metadata.CreatedByValue = meta.ValueControllerResourceLease
	}

	if manager.Metadata.ManagedByValue == "" {
		manager.Metadata.ManagedByValue = meta.ValueControllerResourceLease
	}

	if manager.Metadata.ProtectedByValue == "" {
		manager.Metadata.ProtectedByValue = meta.ValueControllerResourceLease
	}

	if manager.Metadata.ProtectedByServiceAccountAnnotation == "" {
		manager.Metadata.ProtectedByServiceAccountAnnotation = meta.ResourceLeaseServiceAccountAnnotation
	}

	if br != nil && br.Status.Request != nil && br.Status.Request.Impersonation != nil {
		manager.Metadata.ProtectedByServiceAccount = users.GetServiceAccountFullName(*br.Status.Request.Impersonation)
	}

	if manager.Metadata.AppManagedByValue == "" {
		manager.Metadata.AppManagedByValue = meta.ValueAppResourceLeaseManager
	}

	return manager
}
