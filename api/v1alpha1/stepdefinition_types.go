/*
Copyright 2026.

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
	"k8s.io/apimachinery/pkg/runtime"
)

// StepDefinitionSpec defines the schema, runner, and outputs for a step type.
// Shared between StepDefinition (namespaced) and ClusterStepDefinition (cluster-scoped).
type StepDefinitionSpec struct {
	// schema is a JSON Schema (as raw JSON) that validates step parameters.
	// The controller and webhook parse this at runtime for validation.
	// +kubebuilder:validation:Required
	// +kubebuilder:pruning:PreserveUnknownFields
	Schema *runtime.RawExtension `json:"schema"`

	// runner defines the container image that executes this step.
	// Omit for API-native steps that the controller executes directly.
	// +optional
	Runner *RunnerSpec `json:"runner,omitempty"`

	// outputs declares what this step produces for downstream steps.
	// +optional
	Outputs []OutputSpec `json:"outputs,omitempty"`

	// authAware indicates whether this step type can receive auth context.
	// +optional
	AuthAware bool `json:"authAware,omitempty"`
}

// RunnerSpec defines the container image used to execute a step.
type RunnerSpec struct {
	// image is the container image reference for the runner.
	Image string `json:"image"`
}

// OutputSpec declares a named output that a step produces.
type OutputSpec struct {
	// name is the output identifier, referenced via {{ steps.<step>.outputs.<name> }}.
	Name string `json:"name"`

	// description is a human-readable explanation of this output.
	// +optional
	Description string `json:"description,omitempty"`
}

// StepDefinitionStatus defines the observed state of StepDefinition.
type StepDefinitionStatus struct {
	// conditions represent the current state of the StepDefinition.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// StepDefinition is the namespaced Schema for step type definitions.
// Namespace-scoped StepDefinitions take precedence over ClusterStepDefinitions with the same name.
type StepDefinition struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the step type schema, runner, and outputs
	// +required
	Spec StepDefinitionSpec `json:"spec"`

	// status defines the observed state of StepDefinition
	// +optional
	Status StepDefinitionStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// StepDefinitionList contains a list of StepDefinition
type StepDefinitionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []StepDefinition `json:"items"`
}

func init() {
	SchemeBuilder.Register(&StepDefinition{}, &StepDefinitionList{})
}
