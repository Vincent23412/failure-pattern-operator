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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type TargetRef struct {
	// Kind of the target resource (e.g., Deployment)
	Kind string `json:"kind"`

	// Name of the target resource
	Name string `json:"name"`

	// Namespace of the target resource (optional, default: same namespace)
	Namespace string `json:"namespace,omitempty"`
}

type DetectionRule struct {
	// WindowSeconds defines the time window for failure detection
	WindowSeconds int `json:"windowSeconds"`

	// MaxRestarts defines the restart count threshold within the window
	MaxRestarts int `json:"maxRestarts"`
}

type ActionRule struct {
	// Type defines the action to take (e.g., Annotate, Scale)
	Type ActionType `json:"type"`

	// CooldownSeconds defines how long to wait before taking the same action again
	CooldownSeconds int `json:"cooldownSeconds"`
}
type ActionType string

const (
	// AnnotateAction adds an annotation to the target workload
	AnnotateAction ActionType = "Annotate"

	// ScaleDownAction scales down the target workload by one replica
	ScaleDownAction ActionType = "ScaleDown"
)

type FailurePolicySpec struct {
	// Target defines which workload this policy applies to
	Target TargetRef `json:"target"`

	// Detection defines how to detect failure patterns
	Detection DetectionRule `json:"detection"`

	// Action defines what to do when a failure pattern is detected
	Action ActionRule `json:"action"`
}

// type FailurePolicyStatus struct {
// 	// LastCheckedTime records the last time the policy was evaluated
// 	LastCheckedTime metav1.Time `json:"lastCheckedTime,omitempty"`

// 	// FailureDetected indicates whether a failure pattern is currently detected
// 	FailureDetected bool `json:"failureDetected,omitempty"`

// 	// Pattern describes the detected failure pattern
// 	Pattern string `json:"pattern,omitempty"`

// 	// AffectedPods indicates how many pods are involved in the failure pattern
// 	AffectedPods int `json:"affectedPods,omitempty"`

// 	// LastActionTime records when the last action was executed
// 	LastActionTime metav1.Time `json:"lastActionTime,omitempty"`
// }

type FailurePolicyStatus struct {
	// 上一次觀測到的累積 restart 次數（checkpoint）
	LastObservedTotalRestarts int `json:"lastObservedTotalRestarts,omitempty"`

	// 自上次 reconcile 以來新增的 restart 次數
	RecentRestartDelta int `json:"recentRestartDelta,omitempty"`

	// 是否判定為 failure pattern
	FailureDetected bool `json:"failureDetected,omitempty"`

	LastCheckedTime metav1.Time `json:"lastCheckedTime,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// FailurePolicy is the Schema for the failurepolicies API
type FailurePolicy struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of FailurePolicy
	// +required
	Spec FailurePolicySpec `json:"spec"`

	// status defines the observed state of FailurePolicy
	// +optional
	Status FailurePolicyStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// FailurePolicyList contains a list of FailurePolicy
type FailurePolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []FailurePolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&FailurePolicy{}, &FailurePolicyList{})
}
