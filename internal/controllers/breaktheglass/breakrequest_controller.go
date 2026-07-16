// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package breaktheglass

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/tools/events"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/breaktheglass/conditions"
	"github.com/projectcapsule/capsule/internal/controllers/utils"
	"github.com/projectcapsule/capsule/internal/metrics"
	"github.com/projectcapsule/capsule/pkg/api/breaktheglass"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	evt "github.com/projectcapsule/capsule/pkg/runtime/events"
)

const (
	controllerName        = "breakrequest"
	labelKeyManagedBy     = "app.kubernetes.io/managed-by"
	labelValueManagedBy   = "break-the-glass-controller"
	annotationActiveUntil = "projectcapsule.dev/active-until"
)

type BreakRequestReconciler struct {
	client.Client

	scheme   *runtime.Scheme
	Metrics  metrics.BreakRequestsRecorder
	recorder events.EventRecorder
	Log      logr.Logger
}

// SetupWithManager sets up the controller with the Manager.
func (r *BreakRequestReconciler) SetupWithManager(mgr ctrl.Manager, _ utils.ControllerOptions) error {
	r.scheme = mgr.GetScheme()
	r.Client = mgr.GetClient()
	r.recorder = mgr.GetEventRecorder(controllerName)

	return ctrl.NewControllerManagedBy(mgr).
		For(&capsulev1beta2.BreakRequest{}).
		Named(controllerName).
		Complete(r)
}

// Reconcile the request.
func (r *BreakRequestReconciler) Reconcile(
	ctx context.Context,
	req ctrl.Request,
) (ctrl.Result, error) {
	log := r.Log.WithValues("Request.Name", req.Name).WithValues("Request.Namespace", req.Namespace)

	br := &capsulev1beta2.BreakRequest{}
	if err := r.Get(ctx, req.NamespacedName, br); err != nil {
		if apierrors.IsNotFound(err) {
			// ensure metrics for this object are removed
			r.Metrics.DeleteRequestMetrics(&capsulev1beta2.BreakRequest{ObjectMeta: metav1.ObjectMeta{Name: req.Name, Namespace: req.Namespace}})
			log.V(5).
				Info("Request object not found, could have been deleted after reconcile request")

			return reconcile.Result{}, nil
		}

		r.Log.Error(err, "Error reading the object")

		return reconcile.Result{}, err
	}

	defer func() {
		r.Metrics.RecordRequestCondition(br)
	}()

	return r.reconcile(ctx, log, br)
}

// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.4/pkg/reconcile
func (r *BreakRequestReconciler) reconcile(
	ctx context.Context,
	log logr.Logger,
	br *capsulev1beta2.BreakRequest,
) (res ctrl.Result, err error) {
	defer r.updateStatus(ctx, log, br)()

	switch br.Status.Phase {
	case capsulev1beta2.RequestPhasePending:
		log.V(5).Info("BreakRequest is pending, waiting for TTL")

		return ctrl.Result{}, nil

	case capsulev1beta2.RequestPhaseApproved:
		log.V(5).Info("BreakRequest is approved, checking if duration can be started")

		if br.Status.Approved == nil {
			return ctrl.Result{}, fmt.Errorf("BreakRequest is in Approved phase but status.approved is nil")
		}

		if !br.Status.Approved.StartTime.IsZero() {
			if wait := time.Until(br.Status.Approved.StartTime.Time); wait > 0 {
				log.V(5).Info("BreakRequest is approved, waiting for startTime", "startTime", br.Status.Approved.StartTime.Time)

				return ctrl.Result{RequeueAfter: wait}, nil
			}
		}

		log.V(5).Info("BreakRequest is approved, activating br")

		// Transition to Active Phase
		if err := r.transitionRequestActivation(ctx, br); err != nil {
			return ctrl.Result{}, fmt.Errorf(
				"failed to activate BreakRequest %s: %w",
				br.Name,
				err,
			)
		}

		log.V(5).Info("BreakRequest activated successfully")

		r.recorder.Eventf(
			br,
			nil,
			corev1.EventTypeNormal,
			evt.ReasonBreakRequestActivated,
			evt.ActionActivated,
			"Break request activated",
		)

		return ctrl.Result{}, nil

	case capsulev1beta2.RequestPhaseDenied:
		if err := r.addFinalizer(ctx, log, br); err != nil {
			return ctrl.Result{}, err
		}

		log.V(5).Info("BreakRequest is denied, handling denied state")

		return ctrl.Result{}, nil

	case capsulev1beta2.RequestPhaseActive:
		if err := r.addFinalizer(ctx, log, br); err != nil {
			return ctrl.Result{}, err
		}

		if br.Status.Active != nil {
			if !br.Status.Active.ActiveUntil.IsZero() {
				ts := metav1.Now()
				if ts.After(br.Status.Active.ActiveUntil.Time) {
					r.recorder.Eventf(
						br,
						nil,
						corev1.EventTypeNormal,
						evt.ReasonBreakRequestExpired,
						evt.ActionExpired,
						"Break request expired",
					)

					return ctrl.Result{}, br.ExpireRequest(nil)
				}

				log.V(5).Info("Re-queueing when expiration is due")

				return ctrl.Result{
					RequeueAfter: time.Until(br.Status.Active.ActiveUntil.Time),
				}, nil
			}
		}

		return ctrl.Result{}, nil

	// When the BreakRequest has expired
	case capsulev1beta2.RequestPhaseExpired:
		if br.Status.KeepUntil.Time.IsZero() ||
			time.Until(br.Status.KeepUntil.Time) <= 0 {
			log.V(5).Info("BreakRequest is expired, deleting br")
			br.DeleteRequest()

			if err := r.Update(ctx, br); err != nil {
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, r.Delete(ctx, br)
		}

		log.V(5).WithValues("keep-date", br.Status.KeepUntil.Time).
			Info("BreakRequest is expired, Holding expired state until keep date is reached")

		if err := r.deleteItems(ctx, br); err != nil {
			return ctrl.Result{}, err
		}

		return ctrl.Result{RequeueAfter: time.Until(br.Status.KeepUntil.Time)}, nil

	// The case when the BreakRequest is newly created
	case "":
		brt := &capsulev1beta2.BreakRequestTemplate{}
		if err := r.Get(ctx, client.ObjectKey{Name: br.Spec.TemplateName}, brt); err != nil {
			return ctrl.Result{}, fmt.Errorf(
				"failed to get BreakRequest Template %s: %w",
				br.Spec.TemplateName,
				err,
			)
		}
		// initialize br with all requirements from brt
		br.InitializeFromTemplate(brt)

		if ok, err := conditions.IsApproved(brt, br); ok {
			props, err := br.GenerateApprovedProperties()
			if err != nil {
				return ctrl.Result{}, err
			}

			err = br.ApproveRequest(&breaktheglass.AccessEntity{
				Type: breaktheglass.AccessEntityTypeSystem,
			}, props, "Auto Approved")

			return ctrl.Result{}, err
		} else if err != nil {
			return ctrl.Result{}, fmt.Errorf(
				"auto approval could not be evaluated for BreakRequest %s: %w",
				br.Name,
				err,
			)
		}

		log.V(5).Info("BreakRequest is newly created, moving to pending phase")

		if err := br.SetRequested(); err != nil {
			return ctrl.Result{}, err
		}

		r.recorder.Eventf(
			br,
			nil,
			corev1.EventTypeNormal,
			evt.ReasonBreakRequestReviewNeeded,
			evt.ActionPendingReview,
			"Break request review pending",
		)

		return ctrl.Result{}, nil

	case capsulev1beta2.RequestPhaseRequested:
		return ctrl.Result{}, nil
	default:
		log.WithValues("phase", br.Status.Phase).Info("Unhandled phase")

		return ctrl.Result{}, nil
	}
}

func (r *BreakRequestReconciler) updateStatus(
	ctx context.Context,
	log logr.Logger,
	br *capsulev1beta2.BreakRequest,
) func() {
	return func() {
		err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			current := &capsulev1beta2.BreakRequest{}
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

// We are adding a finalizer to the BreakRequest to ensure it's not deleted before the request is processed (KeepFor period).
func (r *BreakRequestReconciler) addFinalizer(
	ctx context.Context,
	log logr.Logger,
	br *capsulev1beta2.BreakRequest,
) error {
	if br.Status.KeepUntil.Time.IsZero() || time.Until(br.Status.KeepUntil.Time) <= 0 {
		return nil
	}

	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, br, func() error {
		finalizerName := meta.ControllerFinalizer
		if controllerutil.ContainsFinalizer(br, finalizerName) {
			log.V(5).Info("Finalizer already exists", "name", br.Name)

			return nil
		}

		log.V(5).Info("Adding finalizer to BreakRequest", "name", br.Name)
		controllerutil.AddFinalizer(br, finalizerName)

		return nil
	}); err != nil {
		return fmt.Errorf("failed to add finalizer to BreakRequest %s: %w", br.Name, err)
	}

	return r.Get(ctx, client.ObjectKeyFromObject(br), br)
}

// When a request is approved, it can be activated immediately or after a certain duration.
func (r *BreakRequestReconciler) transitionRequestActivation(
	ctx context.Context,
	br *capsulev1beta2.BreakRequest,
) error {
	// Avoid persisting the Active phase when item reconciliation fails.
	brCopy := br.DeepCopy()

	if err := brCopy.ActiveRequest(nil); err != nil {
		return err
	}

	// Reflect Binding
	if err := r.reconcileItems(ctx, brCopy); err != nil {
		return fmt.Errorf("failed to create BreakRequest items %s: %w", brCopy.Name, err)
	}

	br.Status = brCopy.Status
	br.Finalizers = brCopy.Finalizers

	return nil
}

// Creates the necessary items resources for the BreakRequest.
func (r *BreakRequestReconciler) reconcileItems(
	ctx context.Context,
	br *capsulev1beta2.BreakRequest,
) (err error) {
	var syncErr error

	tpl := br.Status.Template
	if tpl == nil {
		return errors.New("template is nil")
	}

	if br.Status.Approved == nil {
		return errors.New("approved status is nil")
	}

	if br.Status.Active == nil {
		return errors.New("active status is nil")
	}

	// reset the approved items; only the effective items should be kept
	br.Status.Approved.Templates = nil

	rendered, err := br.RenderItems(tpl.ParamSchema, tpl.Templates)
	if err != nil {
		return err
	}

	codecFactory := serializer.NewCodecFactory(r.Scheme())

	for _, raw := range rendered {
		obj := &unstructured.Unstructured{}
		if _, _, decodeErr := codecFactory.UniversalDeserializer().
			Decode(raw.Raw, nil, obj); decodeErr != nil {
			syncErr = errors.Join(syncErr, decodeErr)

			continue
		}

		obj.SetNamespace(br.Namespace)

		if !br.Status.Active.ActiveUntil.IsZero() {
			ann := obj.GetAnnotations()
			if ann == nil {
				ann = map[string]string{}
			}

			ann[annotationActiveUntil] = br.Status.Active.ActiveUntil.Format(time.RFC3339)
			obj.SetAnnotations(ann)
		}

		if orerr := controllerutil.SetOwnerReference(br, obj, r.scheme); orerr != nil {
			syncErr = errors.Join(syncErr, orerr)

			continue
		}

		// append the item to the approved items (use deep copy to avoid using the cluster object)
		br.Status.Approved.Templates = append(br.Status.Approved.Templates, runtime.RawExtension{Object: obj.DeepCopy()})

		// Apply the object to the cluster
		_, err = controllerutil.CreateOrUpdate(ctx, r.Client, obj, func() error {
			// CreateOrUpdate re-fetches the live object before running this function, so any
			// metadata that must be enforced needs to be applied here as well.
			if !br.Status.Active.ActiveUntil.IsZero() {
				ann := obj.GetAnnotations()
				if ann == nil {
					ann = map[string]string{}
				}

				ann[annotationActiveUntil] = br.Status.Active.ActiveUntil.Format(time.RFC3339)
				obj.SetAnnotations(ann)
			}

			if err := controllerutil.SetOwnerReference(br, obj, r.scheme); err != nil {
				return err
			}

			labels := obj.GetLabels()
			if labels == nil {
				labels = map[string]string{}
			}

			labels[labelKeyManagedBy] = labelValueManagedBy
			obj.SetLabels(labels)

			return nil
		})
		if err != nil {
			syncErr = errors.Join(syncErr, err)
		}
	}

	return syncErr
}

// deletes items of the BreakRequest.
func (r *BreakRequestReconciler) deleteItems(
	ctx context.Context,
	br *capsulev1beta2.BreakRequest,
) (err error) {
	var syncErr error

	if br.Status.Approved == nil {
		return errors.New("approved status is nil")
	}

	for _, item := range br.Status.Approved.Templates {
		obj, err := object(item)
		if err != nil {
			syncErr = errors.Join(syncErr, err)

			continue
		}

		if derr := r.Delete(ctx, obj); derr != nil {
			if !apierrors.IsNotFound(derr) {
				syncErr = errors.Join(syncErr, derr)

				continue
			}
		}
	}

	return syncErr
}

func object(re runtime.RawExtension) (client.Object, error) {
	// Prefer decoded object when present.
	if re.Object != nil {
		if co, ok := re.Object.(client.Object); ok {
			return co, nil
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
