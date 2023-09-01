/*
Copyright 2023 The Kubernetes Authors.

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

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// StaticRoute is the Schema for the staticroutes API.
type StaticRoute struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   StaticRouteSpec   `json:"spec,omitempty"`
	Status StaticRouteStatus `json:"status,omitempty"`
}

type StaticRouteStatusCondition string

// StaticRouteCondition defines condition of StaticRoute.
type StaticRouteCondition Condition

// StaticRouteSpec defines static routes configuration on VPC.
type StaticRouteSpec struct {
	// Specify network address in CIDR format.
	Network string `json:"network"`
	// Next hop gateway
	NextHops []NextHop `json:"nextHops"`
}

// NextHop defines next hop configuration for network.
type NextHop struct {
	// Next hop gateway IP address.
	IPAddress string `json:"ipAddress"`
}

// StaticRouteStatus defines the observed state of StaticRoute.
type StaticRouteStatus struct {
	Conditions []StaticRouteCondition `json:"conditions"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// StaticRouteList contains a list of StaticRoute.
type StaticRouteList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []StaticRoute `json:"items"`
}
