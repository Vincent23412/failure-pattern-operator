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
	"strconv"
	"time"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	resiliencev1alpha1 "github.com/Vincent23412/failure-pattern-operator/api/v1alpha1"
)

type FailurePolicyReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=resilience.example.com,resources=failurepolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=resilience.example.com,resources=failurepolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=resilience.example.com,resources=failurepolicies/finalizers,verbs=update

func (r *FailurePolicyReconciler) Reconcile(
	ctx context.Context,
	req ctrl.Request,
) (ctrl.Result, error) {

	log := log.FromContext(ctx)

	policy, ok, err := r.getPolicy(ctx, req.NamespacedName)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !ok {
		return ctrl.Result{}, nil
	}

	log.Info("Reconciling FailurePolicy", "name", policy.Name)

	target := policy.Spec.Target
	if target.Kind != "Deployment" {
		log.Info("Unsupported target kind, skipping", "kind", target.Kind)
		return ctrl.Result{}, nil
	}

	namespace := resolveTargetNamespace(policy)

	deploy, ok, err := r.getTargetDeployment(ctx, namespace, target, log)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !ok {
		return ctrl.Result{}, nil
	}

	pods, ok, err := r.listPodsForDeployment(ctx, namespace, deploy, log)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !ok {
		return ctrl.Result{}, nil
	}

	currentTotalRestarts, _ := collectRestartMetrics(pods)

	delta := computeDelta(currentTotalRestarts, policy.Status.LastObservedTotalRestarts)
	if err := r.updateStatus(ctx, policy, currentTotalRestarts, delta); err != nil {
		return ctrl.Result{}, err
	}

	remaining, err := r.applyActionIfNeeded(ctx, policy, deploy, delta, log)
	if err != nil {
		return ctrl.Result{}, err
	}
	if remaining != nil {
		return ctrl.Result{RequeueAfter: *remaining}, nil
	}

	return ctrl.Result{
		RequeueAfter: time.Duration(policy.Spec.Detection.WindowSeconds) * time.Second,
	}, nil
}

func (r *FailurePolicyReconciler) getPolicy(
	ctx context.Context,
	key client.ObjectKey,
) (*resiliencev1alpha1.FailurePolicy, bool, error) {
	var policy resiliencev1alpha1.FailurePolicy
	if err := r.Get(ctx, key, &policy); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return &policy, true, nil
}

func resolveTargetNamespace(policy *resiliencev1alpha1.FailurePolicy) string {
	if policy.Spec.Target.Namespace != "" {
		return policy.Spec.Target.Namespace
	}
	return policy.Namespace
}

func (r *FailurePolicyReconciler) getTargetDeployment(
	ctx context.Context,
	namespace string,
	target resiliencev1alpha1.TargetRef,
	log logr.Logger,
) (*appsv1.Deployment, bool, error) {
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
			return nil, false, nil
		}
		log.Error(err, "Failed to get target Deployment")
		return nil, false, err
	}
	return &deploy, true, nil
}

func (r *FailurePolicyReconciler) listPodsForDeployment(
	ctx context.Context,
	namespace string,
	deploy *appsv1.Deployment,
	log logr.Logger,
) ([]corev1.Pod, bool, error) {
	selector := deploy.Spec.Selector
	if selector == nil {
		log.Info("Deployment has no selector, skipping", "deployment", deploy.Name)
		return nil, false, nil
	}

	labelSelector, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		log.Error(err, "Failed to parse Deployment selector")
		return nil, false, err
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
		return nil, false, err
	}

	return podList.Items, true, nil
}

func collectRestartMetrics(pods []corev1.Pod) (int, int) {
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

	return currentTotalRestarts, affectedPods
}

func computeDelta(currentTotal, lastTotal int) int {
	delta := currentTotal - lastTotal
	if delta < 0 {
		return currentTotal
	}
	return delta
}

func (r *FailurePolicyReconciler) updateStatus(
	ctx context.Context,
	policy *resiliencev1alpha1.FailurePolicy,
	currentTotalRestarts int,
	delta int,
) error {
	policy.Status.RecentRestartDelta = delta
	policy.Status.LastObservedTotalRestarts = currentTotalRestarts
	policy.Status.FailureDetected = delta >= policy.Spec.Detection.MaxRestarts
	policy.Status.LastCheckedTime = metav1.Now()

	return r.Status().Update(ctx, policy)
}

func (r *FailurePolicyReconciler) applyActionIfNeeded(
	ctx context.Context,
	policy *resiliencev1alpha1.FailurePolicy,
	deploy *appsv1.Deployment,
	delta int,
	log logr.Logger,
) (*time.Duration, error) {
	// log.Info("ftifnowgw", "policy.Status.FailureDetected ", policy.Status.FailureDetected)
	cooldown := time.Duration(policy.Spec.Action.CooldownSeconds) * time.Second

	if policy.Status.LastActionTime != nil {
		elapsed := time.Since(policy.Status.LastActionTime.Time)
		if elapsed < cooldown {
			remaining := cooldown - elapsed
			log.Info("Cooldown active, skipping action", "remaining", remaining)
			return &remaining, nil
		}
	}

	if policy.Status.FailureDetected {
		switch policy.Spec.Action.Type {
		case resiliencev1alpha1.AnnotateAction:
			if err := r.annotateDeploymentForFailure(ctx, policy, deploy, delta); err != nil {
				return nil, err
			}
		case resiliencev1alpha1.ScaleDownAction:
			scaled, err := r.scaleDownDeployment(ctx, deploy)
			if err != nil {
				return nil, err
			}
			if scaled {
				now := metav1.Now()
				policy.Status.LastActionTime = &now
				if err := r.Status().Update(ctx, policy); err != nil {
					return nil, err
				}
			}
		default:
			log.Info("Unknown action type, skipping", "action", policy.Spec.Action.Type)
		}
	}

	return nil, nil
}

func (r *FailurePolicyReconciler) annotateDeploymentForFailure(
	ctx context.Context,
	policy *resiliencev1alpha1.FailurePolicy,
	deploy *appsv1.Deployment,
	delta int,
) error {
	if deploy.Annotations == nil {
		deploy.Annotations = map[string]string{}
	}

	if deploy.Annotations["resilience.example.com/failure-pattern"] != "RestartStorm" {
		deploy.Annotations["resilience.example.com/failure-pattern"] = "RestartStorm"
		deploy.Annotations["resilience.example.com/recent-restarts"] = strconv.Itoa(delta)
		deploy.Annotations["resilience.example.com/last-detected"] = time.Now().Format(time.RFC3339)

		now := metav1.Now()
		policy.Status.LastActionTime = &now

		if err := r.Status().Update(ctx, policy); err != nil {
			return err
		}
	}

	return nil
}

func (r *FailurePolicyReconciler) scaleDownDeployment(
	ctx context.Context,
	deploy *appsv1.Deployment,
) (bool, error) {
	if deploy.Spec.Replicas == nil {
		return false, nil
	}

	if *deploy.Spec.Replicas <= 1 {
		// 下限保護
		return false, nil
	}

	newReplicas := *deploy.Spec.Replicas - 1
	deploy.Spec.Replicas = &newReplicas

	if err := r.Update(ctx, deploy); err != nil {
		return false, err
	}

	return true, nil
}

func (r *FailurePolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&resiliencev1alpha1.FailurePolicy{}).
		Watches(
			&appsv1.Deployment{},
			handler.EnqueueRequestsFromMapFunc(
				func(ctx context.Context, obj client.Object) []reconcile.Request {
					var policies resiliencev1alpha1.FailurePolicyList
					if err := r.List(ctx, &policies); err != nil {
						return nil
					}

					reqs := make([]reconcile.Request, 0, len(policies.Items))
					for _, p := range policies.Items {
						reqs = append(reqs, reconcile.Request{
							NamespacedName: client.ObjectKey{
								Namespace: p.Namespace,
								Name:      p.Name,
							},
						})
					}
					return reqs
				},
			),
		).
		Named("failurepolicy").
		Complete(r)
}
