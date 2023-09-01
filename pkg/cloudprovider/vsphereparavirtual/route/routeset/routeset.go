package routeset

import (
	"context"
	"fmt"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	cloudprovider "k8s.io/cloud-provider"
	v1alpha1 "k8s.io/cloud-provider-vsphere/pkg/cloudprovider/vsphereparavirtual/apis/nsxnetworking/v1alpha1"
	client "k8s.io/cloud-provider-vsphere/pkg/cloudprovider/vsphereparavirtual/client/v1alpha1/clientset/versioned"
	routecommon "k8s.io/cloud-provider-vsphere/pkg/cloudprovider/vsphereparavirtual/route"
	"k8s.io/klog/v2"
	"strings"
)

type Routeset struct {
	RouteClient client.Interface
	Namespace   string
	OwnerRefs   []metav1.OwnerReference
}

func (r *Routeset) ListRoutes(ctx context.Context, clusterName string) (interface{}, error) {
	// use labelSelector to filter RouteSet CRs that belong to this cluster
	labelSelector := metav1.LabelSelector{
		MatchLabels: map[string]string{routecommon.LabelKeyClusterName: clusterName},
	}

	routes, err := r.RouteClient.NsxV1alpha1().RouteSets(r.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labels.Set(labelSelector.MatchLabels).String(),
	})
	if err != nil {
		return nil, nil
	}
	return routes, nil
}

// createCPRoutes creates cloudprovider Routes based on RouteSet CR
func (r *Routeset) CreateCPRoutes(routeSets interface{}) ([]*cloudprovider.Route, error) {
	routeList, ok := routeSets.(*v1alpha1.RouteSetList)
	if !ok {
		return nil, fmt.Errorf("unknow static route list struct")
	}

	var routes []*cloudprovider.Route
	for _, routeSet := range routeList.Items {
		// only return cloudprovider.Route if RouteSet CR status 'Ready' is true
		condition := GetRouteSetCondition(&(routeSet.Status), v1alpha1.RouteSetConditionTypeReady)
		if condition != nil && condition.Status == v1.ConditionTrue {
			// one RouteSet per node, so we can use nodeName as the name of RouteSet CR
			nodeName := routeSet.Name
			for _, route := range routeSet.Spec.Routes {
				cpRoute := &cloudprovider.Route{
					Name:            route.Name,
					TargetNode:      types.NodeName(nodeName),
					DestinationCIDR: route.Destination,
				}
				routes = append(routes, cpRoute)
			}
		}
	}
	return routes, nil
}

// GetRouteSetCondition extracts the provided condition from the given RouteSetStatus and returns that.
// Returns nil if the condition is not present.
func GetRouteSetCondition(status *v1alpha1.RouteSetStatus, conditionType v1alpha1.RouteSetConditionType) *v1alpha1.RouteSetCondition {
	if status == nil {
		return nil
	}
	for i := range status.Conditions {
		if status.Conditions[i].Type == conditionType {
			return &status.Conditions[i]
		}
	}
	return nil
}

func (r *Routeset) CheckRouteCRReady(name string) error {
	routeSet, err := r.RouteClient.NsxV1alpha1().RouteSets(r.Namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		klog.ErrorS(routecommon.ErrListRouteSet, fmt.Sprintf("%v", err))
		return err
	}
	condition := GetRouteSetCondition(&(routeSet.Status), v1alpha1.RouteSetConditionTypeReady)
	if condition != nil && condition.Status == v1.ConditionTrue {
		return nil
	}

	return nil
}

// createRouteSetCR creates RouteSet CR through RouteSet client
func (r *Routeset) CreateRouteSetCR(ctx context.Context, clusterName string, nameHint string, nodeName string, cidr string, nodeIP string) (interface{}, error) {
	labels := map[string]string{
		routecommon.LabelKeyClusterName: clusterName,
	}
	nodeRef := metav1.OwnerReference{
		APIVersion: "v1",
		Kind:       "Node",
		Name:       nodeName,
		UID:        types.UID(nameHint),
	}
	owners := make([]metav1.OwnerReference, len(r.OwnerRefs))
	copy(owners, r.OwnerRefs)
	owners = append(owners, nodeRef)
	route := v1alpha1.Route{
		Name:        r.GetRouteName(nodeName, cidr, clusterName),
		Destination: cidr,
		Target:      nodeIP,
	}
	routeSetSpec := v1alpha1.RouteSetSpec{
		Routes: []v1alpha1.Route{
			route,
		},
	}
	routeSet := &v1alpha1.RouteSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            nodeName,
			OwnerReferences: owners,
			Namespace:       r.Namespace,
			Labels:          labels,
		},
		Spec: routeSetSpec,
	}

	_, err := r.RouteClient.NsxV1alpha1().RouteSets(r.Namespace).Create(ctx, routeSet, metav1.CreateOptions{})
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			return routeSet, nil
		}
		klog.ErrorS(routecommon.ErrCreateRouteSet, fmt.Sprintf("%v", err))
		return nil, err
	}

	klog.V(6).Infof("Successfully created RouteSet CR for node %s", nodeName)
	return routeSet, nil
}

// GetRouteName returns Route name as <nodeName>-<cidr>-<clusterName>
// e.g. nodeName-100.96.0.0-24-clusterName
func (r *Routeset) GetRouteName(nodeName string, cidr string, clusterName string) string {
	return strings.Replace(nodeName+"-"+cidr+"-"+clusterName, "/", "-", -1)
}

// DeleteRouteSetCR deletes corresponding RouteSet CR when there is a node deleted
func (r *Routeset) DeleteRoute(nodeName string) error {
	if err := r.RouteClient.NsxV1alpha1().RouteSets(r.Namespace).Delete(context.Background(), nodeName, metav1.DeleteOptions{}); err != nil {
		klog.ErrorS(routecommon.ErrDeleteRouteSet, fmt.Sprintf("%v", err))
		return err
	}
	klog.V(6).Infof("Successfully deleted RouteSet CR for node %s", nodeName)
	return nil
}
