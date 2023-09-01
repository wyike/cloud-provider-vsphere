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

package vsphereparavirtual

import (
	"context"
	"fmt"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/cloud-provider-vsphere/pkg/cloudprovider/vsphereparavirtual/route"
	"net"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	rest "k8s.io/client-go/rest"
	cloudprovider "k8s.io/cloud-provider"
	routecommon "k8s.io/cloud-provider-vsphere/pkg/cloudprovider/vsphereparavirtual/route"
	"k8s.io/cloud-provider-vsphere/pkg/util"
	klog "k8s.io/klog/v2"
)

// RoutesProvider is the interface definition for Routes functionality
type RoutesProvider interface {
	cloudprovider.Routes
	AddNode(*v1.Node)
	DeleteNode(*v1.Node)
}

type routesProvider struct {
	routeManager route.RouteManager
	nodeMap      map[string]*v1.Node
	nodeMapLock  sync.RWMutex
}

var _ RoutesProvider = &routesProvider{}

// NewRoutes returns an implementation of RoutesProvider
func NewRoutes(clusterNS string, kcfg *rest.Config, ownerRef metav1.OwnerReference) (RoutesProvider, error) {
	routeManager, err := route.GetRouteManager(true, kcfg, clusterNS, ownerRef)
	if err != nil {
		return nil, err
	}

	return &routesProvider{
		routeManager: routeManager,
		nodeMap:      make(map[string]*v1.Node),
	}, nil
}

// ListRoutes implements Routes.ListRoutes
// Get RouteSet CR from SC namespace and then filters routes that belong to the specified clusterName
// Only return cloudprovider.Route if RouteSet CR status 'Ready' is true
func (r *routesProvider) ListRoutes(ctx context.Context, clusterName string) ([]*cloudprovider.Route, error) {
	klog.V(6).Infof("Attempting to list Routes for cluster %s", clusterName)

	routes, err := r.routeManager.ListRoutes(ctx, clusterName)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return []*cloudprovider.Route{}, nil
		}
		klog.ErrorS(routecommon.ErrListRouteSet, fmt.Sprintf("%v", err))
		return nil, err
	}

	return r.routeManager.CreateCPRoutes(routes)
}

// CreateRoute implements Routes.CreateRoute
// Create a RouteSet custom resource for a Node
func (r *routesProvider) CreateRoute(ctx context.Context, clusterName string, nameHint string, route *cloudprovider.Route) error {
	nodeName := string(route.TargetNode)
	klog.V(6).Infof("Creating Route for node %s with hint %s in cluster %s", nodeName, nameHint, clusterName)

	nodeIP, err := r.getNodeIPAddress(nodeName, util.IsIPv4(route.DestinationCIDR))
	if err != nil {
		klog.Errorf("getting node %s IP address failed: %v", nodeName, err)
		return err
	}

	_, err = r.routeManager.CreateRouteSetCR(ctx, clusterName, nameHint, nodeName, route.DestinationCIDR, nodeIP)
	if err != nil {
		klog.Errorf("creating RouteSet CR for node %s failed: %s", nodeName, err)
		return err
	}
	// nodeName equals routecr  name
	return r.checkStaticRouteRealizedState(nodeName)
}

// checkStaticRouteRealizedState checks static route realized state
// The check happens every 1 second and the default timeout is 10 seconds
func (r *routesProvider) checkStaticRouteRealizedState(routeSetName string) error {
	timeout := time.After(routecommon.RealizedStateTimeout)
	ticker := time.NewTicker(routecommon.RealizedStateSleepTime)
	defer ticker.Stop()
	for {
		select {
		case <-timeout:
			return fmt.Errorf("timed out waiting for static route %s", routeSetName)
		case <-ticker.C:
			r.routeManager.CheckRouteCRReady(routeSetName)
		}
	}
}

// DeleteRoute implements Routes.DeleteRoute
// Delete node's corresponding RouteSet CR
func (r *routesProvider) DeleteRoute(ctx context.Context, clusterName string, route *cloudprovider.Route) error {
	routeSetName := string(route.TargetNode)
	klog.V(6).Infof("Deleting RouteSet CR %s in cluster %s", routeSetName, clusterName)
	return r.routeManager.DeleteRoute(routeSetName)
}

// getNodeIPAddress gets node IP address
// IP family of node address and podCIDR should be the same
// The order is to choose node internal IP first, then external IP
// Return the first IP address as node IP
func (r *routesProvider) getNodeIPAddress(nodeName string, isIPv4 bool) (string, error) {
	node, err := r.getNode(nodeName)
	if err != nil {
		klog.Errorf("getting node %s failed: %v", nodeName, err)
		return "", err
	}

	allIPs := make([]net.IP, 0, len(node.Status.Addresses))
	for _, addr := range node.Status.Addresses {
		if addr.Type == v1.NodeInternalIP {
			ip := net.ParseIP(addr.Address)
			if ip != nil {
				allIPs = append(allIPs, ip)
			}
		}
	}
	for _, addr := range node.Status.Addresses {
		if addr.Type == v1.NodeExternalIP {
			ip := net.ParseIP(addr.Address)
			if ip != nil {
				allIPs = append(allIPs, ip)
			}
		}
	}
	if len(allIPs) == 0 {
		return "", fmt.Errorf("node %s has neither InternalIP nor ExternalIP", nodeName)
	}
	for _, ip := range allIPs {
		if (ip.To4() != nil) == isIPv4 {
			klog.V(4).Info("successfully fetching node %s IP address", node.Name)
			return ip.String(), nil
		}
	}

	return "", fmt.Errorf("node %s does not have the same IP family with podCIDR", nodeName)
}

// AddNode adds v1.Node in nodeMap
func (r *routesProvider) AddNode(node *v1.Node) {
	r.nodeMapLock.Lock()
	r.nodeMap[node.Name] = node
	klog.V(6).Infof("Added node %s into nodeMap", node.Name)
	r.nodeMapLock.Unlock()
}

// DeleteNode deletes v1.Node from nodeMap and removes corresponding RouteSet CR
func (r *routesProvider) DeleteNode(node *v1.Node) {
	r.nodeMapLock.Lock()
	delete(r.nodeMap, node.Name)
	klog.V(6).Infof("Deleted node %s from nodeMap", node.Name)
	r.nodeMapLock.Unlock()

	err := r.routeManager.DeleteRoute(node.Name)
	if err != nil {
		klog.Errorf("failed to delete RouteSet CR for node %s: %v", node.Name, err)
	}
}

// getNode returns v1.Node from nodeMap
func (r *routesProvider) getNode(nodeName string) (*v1.Node, error) {
	r.nodeMapLock.Lock()
	defer r.nodeMapLock.Unlock()
	if r.nodeMap[nodeName] != nil {
		return r.nodeMap[nodeName], nil
	}
	return nil, fmt.Errorf("node %s not found", nodeName)
}
