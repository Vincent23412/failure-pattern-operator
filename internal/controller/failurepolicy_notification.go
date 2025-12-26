package controller

import (
	"context"

	resiliencev1alpha1 "github.com/Vincent23412/failure-pattern-operator/api/v1alpha1"
)

func (r *FailurePolicyReconciler) sendNotification(
	ctx context.Context,
	policy *resiliencev1alpha1.FailurePolicy,
	result DetectionResult,
	execResult *ActionExecutionResult,
) error {
	return nil
}
