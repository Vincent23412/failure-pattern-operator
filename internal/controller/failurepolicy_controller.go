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

	appsv1 "k8s.io/api/apps/v1"
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

	logger := log.FromContext(ctx)

	policy, ok, err := r.getPolicy(ctx, req.NamespacedName)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !ok {
		return ctrl.Result{}, nil
	}

	logger.Info("Reconciling FailurePolicy", "name", policy.Name)

	target := policy.Spec.Target
	if target.Kind != "Deployment" {
		logger.Info("Unsupported target kind, skipping", "kind", target.Kind)
		return ctrl.Result{}, nil
	}

	namespace := resolveTargetNamespace(policy)

	deploy, ok, err := r.getTargetDeployment(ctx, namespace, target, logger)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !ok {
		return ctrl.Result{}, nil
	}

	pods, ok, err := r.listPodsForDeployment(ctx, namespace, deploy, logger)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !ok {
		return ctrl.Result{}, nil
	}

	result := detectFailure(pods, policy, policy.Status.LastObservedTotalRestarts)
	if err := r.updateStatusFromDetection(ctx, policy, result); err != nil {
		return ctrl.Result{}, err
	}

	decision := decideAction(result, policy, time.Now())
	execResult, err := r.executeAction(ctx, decision, policy, deploy, result, logger)
	if err != nil {
		return ctrl.Result{}, err
	}
	if execResult != nil && execResult.Executed {
		notifyPayload := FailureNotification{
			Policy:       policy.Name,
			Namespace:    policy.Namespace,
			Target:       execResult.Target,
			Action:       string(execResult.Action),
			RestartDelta: result.RestartDelta,
			Message:      execResult.Message,
			Timestamp:    time.Now(),
		}
		if err := r.sendNotification(ctx, policy, notifyPayload); err != nil {
			logger.Error(err, "Failed to send notification")
		}
	}
	if decision.RequeueAfter != nil {
		return ctrl.Result{RequeueAfter: *decision.RequeueAfter}, nil
	}

	return ctrl.Result{
		RequeueAfter: time.Duration(policy.Spec.Detection.WindowSeconds) * time.Second,
	}, nil
}

func (r *FailurePolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&resiliencev1alpha1.FailurePolicy{}).
		Watches(
			&appsv1.Deployment{},
			handler.EnqueueRequestsFromMapFunc(func(
				ctx context.Context,
				obj client.Object,
			) []reconcile.Request {

				deploy, ok := obj.(*appsv1.Deployment)
				if !ok {
					return nil
				}

				var policies resiliencev1alpha1.FailurePolicyList
				if err := r.List(ctx, &policies); err != nil {
					return nil
				}

				reqs := []reconcile.Request{}
				for _, p := range policies.Items {
					target := p.Spec.Target

					if target.Kind != "Deployment" {
						continue
					}

					targetNs := p.Namespace
					if target.Namespace != "" {
						targetNs = target.Namespace
					}

					if deploy.Namespace == targetNs &&
						deploy.Name == target.Name {
						reqs = append(reqs, reconcile.Request{
							NamespacedName: client.ObjectKey{
								Namespace: p.Namespace,
								Name:      p.Name,
							},
						})
					}
				}

				return reqs
			}),
		).
		Named("failurepolicy").
		Complete(r)
}
