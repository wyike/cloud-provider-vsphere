/*
Copyright 2021 The Kubernetes Authors.

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

package v1alpha2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/cloud-provider-vsphere/pkg/cloudprovider/vsphereparavirtual/apis/nsxnetworking/v1alpha1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// IPPool is the Schema for the ippools API.
type IPPool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   IPPoolSpec   `json:"spec"`
	Status IPPoolStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// IPPoolList contains a list of IPPool.
type IPPoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []IPPool `json:"items"`
}

// IPPoolSpec defines the desired state of IPPool.
type IPPoolSpec struct {
	// Type defines the type of this IPPool, public or private.
	// +kubebuilder:validation:Enum=public;private
	// +kubebuilder:default=private
	Type string `json:"type"`
	// Subnets defines set of subnets need to be allocated.
	// +optional
	Subnets []SubnetRequest `json:"subnets"`
}

// IPPoolStatus defines the observed state of IPPool.
type IPPoolStatus struct {
	// Subnets defines subnets allocation result.
	Subnets []SubnetResult `json:"subnets"`
	// Conditions defines current state of the IPPool.
	Conditions []v1alpha1.Condition `json:"conditions"`
}

// SubnetRequest defines the subnet allocation request.
type SubnetRequest struct {
	// PrefixLength defines prefix length for this subnet.
	PrefixLength int `json:"prefixLength,omitempty"`

	// IPFamily defines the IP family type for this subnet, could be IPv4 or IPv6.
	// This is optional, the default is IPv4.
	// +kubebuilder:validation:Enum=IPv4;IPv6
	// +kubebuilder:default=IPv4
	IPFamily string `json:"ipFamily,omitempty"`

	// Name defines the name of this subnet.
	Name string `json:"name"`
}

// SubnetResult defines the subnet allocation result.
type SubnetResult struct {
	// CIDR defines the allocated CIDR.
	CIDR string `json:"cidr"`

	// Name defines the name of this subnet.
	Name string `json:"name"`
}
