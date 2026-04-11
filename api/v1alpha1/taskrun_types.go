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
)

// TaskRunPhase represents the current lifecycle phase of a TaskRun.
// +kubebuilder:validation:Enum=Pending;Running;Succeeded;Failed
type TaskRunPhase string

const (
	TaskRunPhasePending   TaskRunPhase = "Pending"
	TaskRunPhaseRunning   TaskRunPhase = "Running"
	TaskRunPhaseSucceeded TaskRunPhase = "Succeeded"
	TaskRunPhaseFailed    TaskRunPhase = "Failed"
)

// StepPhase represents the current lifecycle phase of an individual step.
// +kubebuilder:validation:Enum=Pending;Running;Succeeded;Failed;Skipped
type StepPhase string

const (
	StepPhasePending   StepPhase = "Pending"
	StepPhaseRunning   StepPhase = "Running"
	StepPhaseSucceeded StepPhase = "Succeeded"
	StepPhaseFailed    StepPhase = "Failed"
	StepPhaseSkipped   StepPhase = "Skipped"
)

// ConcurrencyPolicy describes how the TaskRun handles concurrent executions.
// +kubebuilder:validation:Enum=Allow;Forbid;Replace
type ConcurrencyPolicy string

const (
	ConcurrencyPolicyAllow   ConcurrencyPolicy = "Allow"
	ConcurrencyPolicyForbid  ConcurrencyPolicy = "Forbid"
	ConcurrencyPolicyReplace ConcurrencyPolicy = "Replace"
)

// AuthType describes the authentication mechanism used by the TaskRun.
// +kubebuilder:validation:Enum=oidc;basic;mtls;none
type AuthType string

const (
	AuthTypeOIDC  AuthType = "oidc"
	AuthTypeBasic AuthType = "basic"
	AuthTypeMTLS  AuthType = "mtls"
	AuthTypeNone  AuthType = "none"
)

// Backoff describes the retry backoff strategy.
// +kubebuilder:validation:Enum=linear;exponential
type Backoff string

const (
	BackoffLinear      Backoff = "linear"
	BackoffExponential Backoff = "exponential"
)

// TaskRunSpec defines the desired state of TaskRun.
type TaskRunSpec struct {
	// schedule is a cron expression defining when this TaskRun executes.
	// Omit for one-shot Jobs.
	// +optional
	Schedule string `json:"schedule,omitempty"`

	// concurrencyPolicy specifies how to treat concurrent executions of a scheduled TaskRun.
	// +optional
	// +kubebuilder:default=Forbid
	ConcurrencyPolicy ConcurrencyPolicy `json:"concurrencyPolicy,omitempty"`

	// auth defines the authentication context inherited by all authAware steps.
	// +optional
	Auth *AuthSpec `json:"auth,omitempty"`

	// steps is the ordered list of steps to execute.
	// +kubebuilder:validation:MinItems=1
	Steps []StepSpec `json:"steps"`

	// onFailure defines the retry and failure handling policy.
	// +optional
	OnFailure *FailurePolicy `json:"onFailure,omitempty"`
}

// AuthSpec defines how the TaskRun authenticates with external services.
type AuthSpec struct {
	// type is the authentication mechanism.
	Type AuthType `json:"type"`

	// tokenEndpoint is the OIDC token endpoint URL (required when type is oidc).
	// +optional
	TokenEndpoint string `json:"tokenEndpoint,omitempty"`

	// credentialsFrom references a Secret containing auth credentials.
	// +optional
	CredentialsFrom *CredentialsSource `json:"credentialsFrom,omitempty"`
}

// CredentialsSource references a Kubernetes Secret for auth credentials.
type CredentialsSource struct {
	// secretRef references the Secret containing credentials.
	SecretRef SecretKeySelector `json:"secretRef"`
}

// SecretKeySelector identifies specific keys within a Secret.
type SecretKeySelector struct {
	// name is the name of the Secret.
	Name string `json:"name"`

	// clientIdKey is the key within the Secret containing the client ID.
	// +optional
	ClientIDKey string `json:"clientIdKey,omitempty"`

	// clientSecretKey is the key within the Secret containing the client secret.
	// +optional
	ClientSecretKey string `json:"clientSecretKey,omitempty"`

	// usernameKey is the key within the Secret containing the username (for basic auth).
	// +optional
	UsernameKey string `json:"usernameKey,omitempty"`

	// passwordKey is the key within the Secret containing the password (for basic auth).
	// +optional
	PasswordKey string `json:"passwordKey,omitempty"`

	// certKey is the key within the Secret containing the TLS certificate (for mtls).
	// +optional
	CertKey string `json:"certKey,omitempty"`

	// keyKey is the key within the Secret containing the TLS private key (for mtls).
	// +optional
	KeyKey string `json:"keyKey,omitempty"`
}

// StepSpec defines a single step within a TaskRun.
type StepSpec struct {
	// name is a unique identifier for this step within the TaskRun.
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([a-z0-9\-]*[a-z0-9])?$`
	Name string `json:"name"`

	// action references a StepDefinition or ClusterStepDefinition by name.
	Action string `json:"action"`

	// params are the input parameters for this step, validated against the StepDefinition schema.
	// +optional
	Params map[string]string `json:"params,omitempty"`

	// outputs lists which step outputs to capture for downstream steps.
	// +optional
	Outputs []string `json:"outputs,omitempty"`
}

// FailurePolicy defines how failures are handled.
type FailurePolicy struct {
	// retries is the number of times to retry on failure.
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=0
	Retries int `json:"retries,omitempty"`

	// backoff is the retry backoff strategy.
	// +optional
	// +kubebuilder:default=exponential
	Backoff Backoff `json:"backoff,omitempty"`
}

// TaskRunStatus defines the observed state of TaskRun.
type TaskRunStatus struct {
	// phase is the current lifecycle phase of the TaskRun.
	// +optional
	Phase TaskRunPhase `json:"phase,omitempty"`

	// startTime is when the TaskRun began executing.
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// completionTime is when the TaskRun finished executing.
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// steps contains the status of each individual step.
	// +optional
	Steps []StepStatus `json:"steps,omitempty"`

	// conditions represent the current state of the TaskRun.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// StepStatus describes the observed state of a single step execution.
type StepStatus struct {
	// name is the step name matching StepSpec.Name.
	Name string `json:"name"`

	// phase is the current lifecycle phase of this step.
	Phase StepPhase `json:"phase"`

	// duration is the wall-clock time this step took to execute.
	// +optional
	Duration string `json:"duration,omitempty"`

	// outputs contains the captured output values from this step.
	// +optional
	Outputs map[string]string `json:"outputs,omitempty"`

	// logs contains the last N lines of runner container logs for this step.
	// +optional
	Logs string `json:"logs,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Schedule",type=string,JSONPath=`.spec.schedule`
// +kubebuilder:printcolumn:name="Last Run",type=date,JSONPath=`.status.startTime`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// TaskRun is the Schema for the taskruns API.
// It defines a declarative, typed alternative to raw Kubernetes Jobs and CronJobs.
type TaskRun struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of TaskRun
	// +required
	Spec TaskRunSpec `json:"spec"`

	// status defines the observed state of TaskRun
	// +optional
	Status TaskRunStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// TaskRunList contains a list of TaskRun
type TaskRunList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []TaskRun `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TaskRun{}, &TaskRunList{})
}
