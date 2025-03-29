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

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// PodWatchSpec defines the desired state of PodWatch.
type PodWatchSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Foo is an example field of PodWatch. Edit podwatch_types.go to remove/update
	//Status  []WatchStatus `json:"status"`
	Channel string `json:"channel"`
}

type PodReport struct {
	Hash        string `json:"podHash"`
	Name        string `json:"name"`
	OwnerRef    string `json:"ownerRef"`
	LastUpdated string `json:"lastUpdated"`
	Reason      string `json:"reason"`
	LastStatus  string `json:"lastStatus"`
	ThreadID    string `json:"threadID"`
}

// PodWatchStatus defines the observed state of PodWatch.
type PodWatchStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Reports []PodReport `json:"reports"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// PodWatch is the Schema for the podwatches API.
type PodWatch struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PodWatchSpec   `json:"spec,omitempty"`
	Status PodWatchStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PodWatchList contains a list of PodWatch.
type PodWatchList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PodWatch `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PodWatch{}, &PodWatchList{})
}
