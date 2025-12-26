package controller

import (
	"context"
	"strconv"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	resiliencev1alpha1 "github.com/Vincent23412/failure-pattern-operator/api/v1alpha1"
)

func (r *FailurePolicyReconciler) executeAction(
	ctx context.Context,
	decision ActionDecision,
	policy *resiliencev1alpha1.FailurePolicy,
	deploy *appsv1.Deployment,
	result DetectionResult,
	logger logr.Logger,
) error {
	if !decision.ShouldAct {
		logger.Info("No action executed", "reason", decision.Reason)
		return nil
	}

	switch decision.Action {
	case resiliencev1alpha1.AnnotateAction:
		return r.annotateDeploymentForFailure(ctx, policy, deploy, result.RestartDelta)
	case resiliencev1alpha1.ScaleDownAction:
		scaled, err := r.scaleDownDeployment(ctx, deploy)
		if err != nil {
			return err
		}
		if scaled {
			now := metav1.Now()
			policy.Status.LastActionTime = &now
			return r.Status().Update(ctx, policy)
		}
		return nil
	default:
		logger.Info("Unknown action type, skipping", "action", decision.Action)
		return nil
	}
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
