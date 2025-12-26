package controller

import (
	"context"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	resiliencev1alpha1 "github.com/Vincent23412/failure-pattern-operator/api/v1alpha1"
)

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
	logger logr.Logger,
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
			logger.Info("Target Deployment not found, skipping", "deployment", target.Name)
			return nil, false, nil
		}
		logger.Error(err, "Failed to get target Deployment")
		return nil, false, err
	}
	return &deploy, true, nil
}

func (r *FailurePolicyReconciler) listPodsForDeployment(
	ctx context.Context,
	namespace string,
	deploy *appsv1.Deployment,
	logger logr.Logger,
) ([]corev1.Pod, bool, error) {
	selector := deploy.Spec.Selector
	if selector == nil {
		logger.Info("Deployment has no selector, skipping", "deployment", deploy.Name)
		return nil, false, nil
	}

	labelSelector, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		logger.Error(err, "Failed to parse Deployment selector")
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

func (r *FailurePolicyReconciler) updateStatusFromDetection(
	ctx context.Context,
	policy *resiliencev1alpha1.FailurePolicy,
	result DetectionResult,
) error {
	policy.Status.RecentRestartDelta = result.RestartDelta
	policy.Status.LastObservedTotalRestarts = result.CurrentTotalRestarts
	policy.Status.FailureDetected = result.FailureDetected
	policy.Status.LastCheckedTime = metav1.NewTime(result.ObservedAt)

	return r.Status().Update(ctx, policy)
}
