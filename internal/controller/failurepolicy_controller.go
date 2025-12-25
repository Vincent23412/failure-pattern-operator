/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	resiliencev1alpha1 "github.com/Vincent23412/failure-pattern-operator/api/v1alpha1"
)

// FailurePolicyReconciler reconciles a FailurePolicy object
type FailurePolicyReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=resilience.example.com,resources=failurepolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=resilience.example.com,resources=failurepolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=resilience.example.com,resources=failurepolicies/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the FailurePolicy object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.22.4/pkg/reconcile
func (r *FailurePolicyReconciler) Reconcile(
	ctx context.Context,
	req ctrl.Request,
) (ctrl.Result, error) {

	log := log.FromContext(ctx)

	// =========================================================
	// 1. Load FailurePolicy (Primary Resource)
	// =========================================================
	var policy resiliencev1alpha1.FailurePolicy
	if err := r.Get(ctx, req.NamespacedName, &policy); err != nil {
		// CR 被刪掉時會進這裡，直接結束
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	log.Info("Reconciling FailurePolicy", "name", policy.Name)

	// =========================================================
	// 2. Resolve Target (what workload we are monitoring)
	// =========================================================
	target := policy.Spec.Target

	// 目前只支援 Deployment（先鎖 scope）
	if target.Kind != "Deployment" {
		log.Info("Unsupported target kind, skipping", "kind", target.Kind)
		return ctrl.Result{}, nil
	}

	namespace := policy.Namespace
	if target.Namespace != "" {
		namespace = target.Namespace
	}

	// =========================================================
	// 3. List Pods belonging to the target Deployment
	// =========================================================
	var deploy appsv1.Deployment
	if err := r.Get(
		ctx,
		client.ObjectKey{
			Namespace: namespace,
			Name:      target.Name,
		},
		&deploy,
	); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Target Deployment not found, skipping", "deployment", target.Name)
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get target Deployment")
		return ctrl.Result{}, err
	}

	selector := deploy.Spec.Selector
	if selector == nil {
		log.Info("Deployment has no selector, skipping", "deployment", deploy.Name)
		return ctrl.Result{}, nil
	}

	labelSelector, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		log.Error(err, "Failed to parse Deployment selector")
		return ctrl.Result{}, err
	}

	var podList corev1.PodList
	if err := r.List(
		ctx,
		&podList,
		client.InNamespace(namespace),
		client.MatchingLabelsSelector{
			Selector: labelSelector,
		},
	); err != nil {
		return ctrl.Result{}, err
	}

	pods := podList.Items

	// =========================================================
	// 4. Monitor: collect restart information from Pod status
	// =========================================================

	currentTotalRestarts := 0
	affectedPods := 0

	for _, pod := range pods {
		podRestarts := 0
		for _, cs := range pod.Status.ContainerStatuses {
			podRestarts += int(cs.RestartCount)
		}

		if podRestarts > 0 {
			affectedPods++
			currentTotalRestarts += podRestarts
		}
	}

	lastTotal := policy.Status.LastObservedTotalRestarts
	delta := currentTotalRestarts - lastTotal
	if delta < 0 {
		delta = currentTotalRestarts
	}

	policy.Status.RecentRestartDelta = delta
	policy.Status.LastObservedTotalRestarts = currentTotalRestarts
	policy.Status.FailureDetected = delta >= policy.Spec.Detection.MaxRestarts
	policy.Status.LastCheckedTime = metav1.Now()

	if err := r.Status().Update(ctx, &policy); err != nil {
		return ctrl.Result{}, err
	}

	// =========================================================
	// 7. Requeue: periodic re-evaluation
	// =========================================================
	return ctrl.Result{
		RequeueAfter: time.Duration(policy.Spec.Detection.WindowSeconds) * time.Second,
	}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *FailurePolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&resiliencev1alpha1.FailurePolicy{}).
		Named("failurepolicy").
		Complete(r)
}
