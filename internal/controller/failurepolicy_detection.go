package controller

import (
	"time"

	corev1 "k8s.io/api/core/v1"

	resiliencev1alpha1 "github.com/Vincent23412/failure-pattern-operator/api/v1alpha1"
)

type DetectionResult struct {
	FailureDetected      bool
	RestartDelta         int
	AffectedPods         int
	CurrentTotalRestarts int
	ObservedAt           time.Time
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

func detectFailure(
	pods []corev1.Pod,
	policy *resiliencev1alpha1.FailurePolicy,
	lastObserved int,
) DetectionResult {
	currentTotal, affected := collectRestartMetrics(pods)
	delta := computeDelta(currentTotal, lastObserved)

	return DetectionResult{
		FailureDetected:      delta >= policy.Spec.Detection.MaxRestarts,
		RestartDelta:         delta,
		AffectedPods:         affected,
		CurrentTotalRestarts: currentTotal,
		ObservedAt:           time.Now(),
	}
}
