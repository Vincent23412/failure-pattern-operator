package controller

import (
	"time"

	resiliencev1alpha1 "github.com/Vincent23412/failure-pattern-operator/api/v1alpha1"
)

type ActionDecision struct {
	ShouldAct    bool
	Action       resiliencev1alpha1.ActionType
	Reason       string
	RequeueAfter *time.Duration
}

func decideAction(
	result DetectionResult,
	policy *resiliencev1alpha1.FailurePolicy,
	now time.Time,
) ActionDecision {
	cooldown := time.Duration(policy.Spec.Action.CooldownSeconds) * time.Second

	if !result.FailureDetected {
		return ActionDecision{
			ShouldAct: false,
			Reason:    "failure threshold not reached",
		}
	}

	if policy.Status.LastActionTime != nil {
		elapsed := now.Sub(policy.Status.LastActionTime.Time)
		if elapsed < cooldown {
			remaining := cooldown - elapsed
			return ActionDecision{
				ShouldAct:    false,
				Reason:       "cooldown active",
				RequeueAfter: &remaining,
			}
		}
	}

	return ActionDecision{
		ShouldAct: true,
		Action:    policy.Spec.Action.Type,
		Reason:    "failure detected and cooldown passed",
	}
}
