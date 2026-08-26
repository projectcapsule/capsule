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
	"k8s.io/client-go/tools/events"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/controllers/utils"
	"github.com/projectcapsule/capsule/internal/metrics"
	"github.com/projectcapsule/capsule/pkg/api/breaktheglass"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/conditions"
	evt "github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/ssa"
)

const controllerName = "breakrequest"

type BreakRequestReconciler struct {
	client.Client

	Metrics   metrics.BreakRequestsRecorder
	recorder  events.EventRecorder
	Log       logr.Logger
	resources ssa.Manager
}

// SetupWithManager sets up the controller with the Manager.
func (r *BreakRequestReconciler) SetupWithManager(mgr ctrl.Manager, _ utils.ControllerOptions) error {
	r.Client = mgr.GetClient()
	r.recorder = mgr.GetEventRecorder(controllerName)
	r.resources = ssa.Manager{
		Reader: mgr.GetAPIReader(),
		Mapper: mgr.GetRESTMapper(),
		Metadata: ssa.Metadata{
			CreatedByValue:   meta.ValueControllerBreakTheGlass,
			ManagedByValue:   meta.ValueControllerBreakTheGlass,
			ProtectedByValue: meta.ValueControllerBreakTheGlass,
		},
	}

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
//
//nolint:cyclop
func (r *BreakRequestReconciler) reconcile(
	ctx context.Context,
	log logr.Logger,
	br *capsulev1beta2.BreakRequest,
) (res ctrl.Result, err error) {
	defer r.updateStatus(ctx, log, br)()

	if !br.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, br)
	}

	switch br.Status.Phase {
	case capsulev1beta2.RequestPhasePending:
		log.V(5).Info("BreakRequest is pending, waiting for TTL")

		return ctrl.Result{}, nil

	case capsulev1beta2.RequestPhaseApproved:
		log.V(5).Info("BreakRequest is approved, checking if duration can be started")

		if br.Status.Approved == nil {
			return ctrl.Result{}, fmt.Errorf("BreakRequest is in Approved phase but status.approved is nil")
		}

		if err := r.addFinalizer(ctx, log, br); err != nil {
			return ctrl.Result{}, err
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
		if err := r.pruneItems(ctx, br); err != nil {
			return ctrl.Result{}, err
		}

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

		return ctrl.Result{RequeueAfter: time.Until(br.Status.KeepUntil.Time)}, nil

	// The case when the BreakRequest is newly created
	case "":
		if br.Spec.Template.Kind != capsulev1beta2.BreakRequestTemplateKind {
			return ctrl.Result{}, fmt.Errorf(
				"unsupported BreakRequest template kind %q",
				br.Spec.Template.Kind,
			)
		}

		brt := &capsulev1beta2.BreakRequestTemplate{}
		if err := r.Get(ctx, client.ObjectKey{Name: br.Spec.Template.Name}, brt); err != nil {
			return ctrl.Result{}, fmt.Errorf(
				"failed to get BreakRequest Template %s: %w",
				br.Spec.Template.Name,
				err,
			)
		}
		// initialize br with all requirements from brt
		br.InitializeFromTemplate(brt)

		if ok, err := conditions.IsApproved(brt, br); ok {
			loadedContext, err := br.LoadTemplateContext(ctx, r.Client, r.managedResourceManager().Mapper)
			if err != nil {
				return ctrl.Result{}, err
			}

			props, err := br.GenerateApprovedProperties(loadedContext)
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

// Add a finalizer so managed resources are pruned before deletion and the
// BreakRequest can be retained for its configured audit period.
func (r *BreakRequestReconciler) addFinalizer(
	ctx context.Context,
	log logr.Logger,
	br *capsulev1beta2.BreakRequest,
) error {
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

func (r *BreakRequestReconciler) reconcileDelete(
	ctx context.Context,
	br *capsulev1beta2.BreakRequest,
) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(br, meta.ControllerFinalizer) {
		return ctrl.Result{}, nil
	}

	if err := r.pruneItems(ctx, br); err != nil {
		return ctrl.Result{}, err
	}

	if !br.Status.KeepUntil.IsZero() {
		if wait := time.Until(br.Status.KeepUntil.Time); wait > 0 {
			return ctrl.Result{RequeueAfter: wait}, nil
		}
	}

	controllerutil.RemoveFinalizer(br, meta.ControllerFinalizer)

	return ctrl.Result{}, r.Update(ctx, br)
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
		// Persist the rendered identities so partially applied resources can be
		// pruned if activation is cancelled or the request is deleted.
		br.Status.Approved.Resources = brCopy.Status.Approved.Resources
		br.Status.ManagedResourcesStatus = brCopy.Status.ManagedResourcesStatus

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
	br.Status.Approved.Resources = nil
	currentItems := br.Status.ProcessedItems
	processedItems := make(meta.ProcessedItems, 0)

	loadedContext, err := br.LoadTemplateContext(ctx, r.Client, r.managedResourceManager().Mapper)
	if err != nil {
		return err
	}

	rendered, err := br.RenderResources(tpl.ParamSchema, tpl.Resources, loadedContext)
	if err != nil {
		return err
	}

	manager := r.managedResourceManager()
	fieldOwner := meta.BreakRequestFieldOwner(br)

	for _, resource := range rendered {
		effective := apiruntime.ResourceTemplate{Policy: resource.Policy}

		for _, raw := range resource.Targets {
			obj, decodeErr := object(raw)
			if decodeErr != nil {
				syncErr = errors.Join(syncErr, decodeErr)

				continue
			}

			obj.SetNamespace(br.Namespace)

			if !br.Status.Active.ActiveUntil.IsZero() {
				ann := obj.GetAnnotations()
				if ann == nil {
					ann = map[string]string{}
				}

				ann[meta.BreakRequestActiveUntilAnnotation] = br.Status.Active.ActiveUntil.Format(time.RFC3339)
				obj.SetAnnotations(ann)
			}

			labels := obj.GetLabels()
			if labels == nil {
				labels = map[string]string{}
			}

			labels[meta.AppManagedByLabel] = meta.ValueAppBreakTheGlassManager
			obj.SetLabels(labels)

			// Keep the rendered identity even when apply fails so it can be pruned.
			effective.Targets = append(effective.Targets, runtime.RawExtension{Object: obj.DeepCopy()})

			// BreakRequests are namespaced but may manage cluster-scoped objects,
			// so their lifecycle cannot rely on Kubernetes owner references. The
			// BreakRequest finalizer and recorded target identities provide the
			// explicit cascade during expiration or deletion.
			item, statusErr := managedResourceStatus(manager, obj)
			if statusErr != nil {
				syncErr = errors.Join(syncErr, statusErr)

				continue
			}

			current := currentItems.GetItem(item.ResourceID)
			result, applyErr := manager.Apply(ctx, r.Client, obj, ssa.ApplyOptions{
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

		if len(effective.Targets) > 0 {
			br.Status.Approved.Resources = append(br.Status.Approved.Resources, effective)
		}
	}

	processedItems.SortDeterministic()
	br.Status.ProcessedItems = processedItems
	br.Status.UpdateStats()

	return syncErr
}

// pruneItems relinquishes the BreakRequest field manager's resources.
func (r *BreakRequestReconciler) pruneItems(
	ctx context.Context,
	br *capsulev1beta2.BreakRequest,
) (err error) {
	var syncErr error

	if br.Status.Approved == nil {
		return errors.New("approved status is nil")
	}

	manager := r.managedResourceManager()
	fieldOwner := meta.BreakRequestFieldOwner(br)

	for _, resource := range br.Status.Approved.Resources {
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

			deleted, pruneErr := manager.Prune(ctx, r.Client, obj, ssa.PruneOptions{
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
				if disownErr := manager.Disown(ctx, r.Client, obj, nil); disownErr != nil {
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

func (r *BreakRequestReconciler) managedResourceManager() ssa.Manager {
	manager := r.resources
	if manager.Reader == nil {
		manager.Reader = r.Client
	}

	if manager.Metadata.CreatedByValue == "" {
		manager.Metadata.CreatedByValue = meta.ValueControllerBreakTheGlass
	}

	if manager.Metadata.ManagedByValue == "" {
		manager.Metadata.ManagedByValue = meta.ValueControllerBreakTheGlass
	}

	if manager.Metadata.ProtectedByValue == "" {
		manager.Metadata.ProtectedByValue = meta.ValueControllerBreakTheGlass
	}

	return manager
}
