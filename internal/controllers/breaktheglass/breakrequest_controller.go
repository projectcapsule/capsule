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
	controllerName           = "breakrequest"
	annotationKeyManagedBy   = "app.kubernetes.io/managed-by"
	annotationValueManagedBy = "break-the-glass-controller"
	annotationActiveUntil    = "projectcapsule.dev/active-until"
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
			r.Metrics.DeleteRequestMetrics(br)
			log.V(5).
				Info("Request object not found, could have been deleted after reconcile request")

			return reconcile.Result{}, nil
		}

		r.Log.Error(err, "Error reading the object")

		return reconcile.Result{}, nil
	}

	defer func() {
		r.Metrics.DeleteRequestMetrics(br)
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

		if br.Status.Approved.StartTime.IsZero() ||
			time.Until(br.Status.Approved.StartTime.Time) <= 0 {
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
		}

		return ctrl.Result{}, nil

	case capsulev1beta2.RequestPhaseDenied:
		if err := r.addFinalizer(ctx, log, br); err != nil {
			return ctrl.Result{}, err
		}

		log.V(5).Info("BreakRequest is denied, handling denied state")

		// r.Recorder.Event(br, corev1.EventTypeWarning, "Denied", fmt.Sprintf("Request denied by %s %s", entity.Type, entity.Name))
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

				r.recorder.Eventf(
					br,
					nil,
					corev1.EventTypeNormal,
					evt.ReasonBreakRequestActivated,
					evt.ActionActivating,
					"Break request activated",
				)
				log.V(5).Info("Re-queueing when expiration is due")

				return ctrl.Result{
					RequeueAfter: br.Status.Active.ActiveUntil.Sub(ts.Time),
				}, nil
			}
		}

		return ctrl.Result{}, nil

	// When the BreakRequest has expired
	case capsulev1beta2.RequestPhaseExpired:
		if br.Status.KeepUntil.Time.IsZero() ||
			time.Until(br.Status.KeepUntil.Time) <= 0 {
			log.V(5).Info("AccessRequest is expired, deleting br")

			return ctrl.Result{}, r.Delete(ctx, br)
		}

		log.V(5).WithValues("keep-date", br.Status.KeepUntil.Time).
			Info("AccessRequest is expired, Holding expired state until keep date is reached")

		if err := r.deleteItems(ctx, br); err != nil {
			return ctrl.Result{}, err
		}

		return ctrl.Result{RequeueAfter: time.Until(br.Status.KeepUntil.Time)}, nil

	// The case when the AccessRequest is newly created
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

		log.V(5).Info("AccessRequest is newly created, moving to pending phase")

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
				return fmt.Errorf("failed to update instance before update: %w", err)
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
	if err := br.ActiveRequest(nil); err != nil {
		return err
	}

	// Reflect Binding
	if err := r.reconcileItems(ctx, br); err != nil {
		return fmt.Errorf("failed to create AccessRequest items %s: %w", br.Name, err)
	}

	return nil
}

// Creates the necessary items resources for the AccessRequest.
func (r *BreakRequestReconciler) reconcileItems(
	ctx context.Context,
	br *capsulev1beta2.BreakRequest,
) (err error) {
	var syncErr error

	tpl := br.Status.Template
	if tpl == nil {
		return errors.New("template is nil")
	}

	// reset the approved items, only the true approved items should be kept, including the modification done from the operator
	br.Status.Approved.Items = make(breaktheglass.Items)

	rendered, err := br.RenderItemsItems(tpl.Items)
	if err != nil {
		return err
	}

	codecFactory := serializer.NewCodecFactory(r.Scheme())

	for name, raw := range rendered {
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
		br.Status.Approved.Items[name] = &runtime.RawExtension{Object: obj.DeepCopy()}

		// Apply the object to the cluster
		_, err = controllerutil.CreateOrUpdate(ctx, r.Client, obj, func() error {
			labels := obj.GetLabels()
			if labels == nil {
				labels = map[string]string{}
			}

			labels[annotationKeyManagedBy] = annotationValueManagedBy
			obj.SetLabels(labels)

			return nil
		})
		if err != nil {
			syncErr = errors.Join(syncErr, err)
		}
	}

	return syncErr
}

// deletes items of the AccessRequest.
func (r *BreakRequestReconciler) deleteItems(
	ctx context.Context,
	br *capsulev1beta2.BreakRequest,
) (err error) {
	var syncErr error

	for _, item := range br.Status.Approved.Items {
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

func object(re *runtime.RawExtension) (client.Object, error) {
	if re.Object == nil {
		return nil, errors.New("object is nil")
	}

	if co, ok := re.Object.(client.Object); ok {
		return co, nil
	}

	us, err := runtime.DefaultUnstructuredConverter.ToUnstructured(re.Object)
	if err != nil {
		return nil, err
	}

	return &unstructured.Unstructured{Object: us}, nil
}
