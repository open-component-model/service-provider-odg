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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// DefaultReleaseName provides the default value for the helm release of the odg controller.
const DefaultReleaseName = "odg"

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ODGSpec defines the desired state of ODG
type ODGSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// The following markers will use OpenAPI v3 schema to validate the value
	// More info: https://book.kubebuilder.io/reference/markers/crd-validation.html

	// foo is an example field of ODG. Edit odg_types.go to remove/update
	// +optional
	Foo *string `json:"foo,omitempty"`
}

// InstancePhase is a custom type representing the phase of a service instance.
type InstancePhase string

// ResourceLocation is a custom type representing the location of a resource.
type ResourceLocation string

// Constants representing the phases of an instance lifecycle and the
// locations where managed resources can live.
const (
	Pending     InstancePhase = "Pending"
	Progressing InstancePhase = "Progressing"
	Ready       InstancePhase = "Ready"
	Failed      InstancePhase = "Failed"
	Terminating InstancePhase = "Terminating"
	Unknown     InstancePhase = "Unknown"

	ManagedControlPlane ResourceLocation = "ManagedControlPlane"
	PlatformCluster     ResourceLocation = "PlatformCluster"
)

// ManagedResource defines a kubernetes object with its lifecycle phase.
type ManagedResource struct {
	corev1.TypedObjectReference `json:",inline"`

	// +required
	Phase InstancePhase `json:"phase"`
	// +optional
	Message string `json:"message,omitempty"`
	// +optional
	Location ResourceLocation `json:"location,omitempty"`
}

// ODGStatus defines the observed state of ODG.
type ODGStatus struct {
	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// conditions represent the current state of the OCM resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// ObservedGeneration is the generation of this resource that was last reconciled by the controller.
	ObservedGeneration int64 `json:"observedGeneration"`
	// Phase is the current phase of the resource.
	Phase string `json:"phase"`
	// Resources managed by this OCM instance.
	// +optional
	Resources []ManagedResource `json:"resources,omitempty"`
}

// ODG is the Schema for the odgs API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:JSONPath=`.status.phase`,name="Phase",type=string
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:metadata:labels="openmcp.cloud/cluster=onboarding"
type ODG struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of ODG
	// +required
	Spec ODGSpec `json:"spec"`

	// status defines the observed state of ODG
	// +optional
	Status ODGStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// ODGList contains a list of ODG
type ODGList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ODG `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(GroupVersion, &ODG{}, &ODGList{})
		return nil
	})
}

// Finalizer returns the finalizer string for the ODG resource
func (o *ODG) Finalizer() string {
	return GroupVersion.Group + "/finalizer"
}

// GetStatus returns the status of the ODG resource
func (o *ODG) GetStatus() any {
	return o.Status
}

// GetConditions returns the conditions of the ODG resource
func (o *ODG) GetConditions() *[]metav1.Condition {
	return &o.Status.Conditions
}

// SetPhase sets the phase of the ODG resource status
func (o *ODG) SetPhase(phase string) {
	o.Status.Phase = phase
}

// SetObservedGeneration sets the observed generation of the ODG resource
func (o *ODG) SetObservedGeneration(gen int64) {
	o.Status.ObservedGeneration = gen
}
