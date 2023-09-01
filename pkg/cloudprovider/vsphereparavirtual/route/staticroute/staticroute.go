package staticroute

import (
	"context"
	"fmt"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/cloud-provider-vsphere/pkg/cloudprovider/vsphereparavirtual/apis/nsxnetworking/v1alpha1"
	client "k8s.io/cloud-provider-vsphere/pkg/cloudprovider/vsphereparavirtual/client/v1alpha1/clientset/versioned"
	routecommon "k8s.io/cloud-provider-vsphere/pkg/cloudprovider/vsphereparavirtual/route"
	"k8s.io/klog/v2"
)

type Staticroute struct {
	RouteClient client.Interface
	Namespace   string
	OwnerRefs   []metav1.OwnerReference
}

func (sr *Staticroute) ListRoutes(ctx context.Context, clusterName string) (interface{}, error) {
	// use labelSelector to filter RouteSet CRs that belong to this cluster
	labelSelector := metav1.LabelSelector{
		MatchLabels: map[string]string{routecommon.LabelKeyClusterName: clusterName},
	}

	routes, err := sr.RouteClient.NsxV1alpha1().StaticRoutes(sr.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labels.Set(labelSelector.MatchLabels).String(),
	})
	if err != nil {
		return nil, nil
	}
	return routes, nil
}

// createCPRoutes creates cloudprovider Routes based on RouteSet CR
func (r *Staticroute) CreateCPRoutes(staticroutes interface{}) ([]*cloudprovider.Route, error) {
	routeList, ok := staticroutes.(*v1alpha1.StaticRouteList)
	if !ok {
		return nil, fmt.Errorf("unknow static route list struct")
	}

	var routes []*cloudprovider.Route
	for _, staticroute := range routeList.Items {
		// only return cloudprovider.Route if RouteSet CR status 'Ready' is true
		condition := GetRouteSetCondition(&(staticroute.Status), v1alpha1.Ready)
		if condition != nil && condition.Status == v1.ConditionTrue {
			// one RouteSet per node, so we can use nodeName as the name of RouteSet CR
			// verfy the accrurity below
			nodeName := staticroute.Name
			cpRoute := &cloudprovider.Route{
				Name:            staticroute.Name,
				TargetNode:      types.NodeName(nodeName),
				DestinationCIDR: staticroute.Spec.Network,
			}
			routes = append(routes, cpRoute)
		}
	}
	return routes, nil
}

// GetRouteSetCondition extracts the provided condition from the given RouteSetStatus and returns that.
// Returns nil if the condition is not present.
func GetRouteSetCondition(status *v1alpha1.StaticRouteStatus, conditionType v1alpha1.ConditionType) *v1alpha1.StaticRouteCondition {
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

func (sr *Staticroute) CheckRouteCRReady(name string) error {
	routeSet, err := sr.RouteClient.NsxV1alpha1().StaticRoutes(sr.Namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		klog.ErrorS(routecommon.ErrListRouteSet, fmt.Sprintf("%v", err))
		return err
	}
	condition := GetRouteSetCondition(&(routeSet.Status), v1alpha1.Ready)
	if condition != nil && condition.Status == v1.ConditionTrue {
		return nil
	}

	return nil
}

// createRouteSetCR creates RouteSet CR through RouteSet client
func (sr *Staticroute) CreateRouteSetCR(ctx context.Context, clusterName string, nameHint string, nodeName string, cidr string, nodeIP string) (interface{}, error) {
	labels := map[string]string{
		routecommon.LabelKeyClusterName: clusterName,
	}
	nodeRef := metav1.OwnerReference{
		APIVersion: "v1",
		Kind:       "Node",
		Name:       nodeName,
		UID:        types.UID(nameHint),
	}
	owners := make([]metav1.OwnerReference, len(sr.OwnerRefs))
	copy(owners, sr.OwnerRefs)
	owners = append(owners, nodeRef)
	//verify its accuracy
	staticrouteSpec := v1alpha1.StaticRouteSpec{
		Network:  cidr,
		NextHops: []v1alpha1.NextHop{{nodeIP}},
	}
	staticRoute := &v1alpha1.StaticRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:            nodeName,
			OwnerReferences: owners,
			Namespace:       sr.Namespace,
			Labels:          labels,
		},
		Spec: staticrouteSpec,
	}

	_, err := sr.RouteClient.NsxV1alpha1().StaticRoutes(sr.Namespace).Create(ctx, staticRoute, metav1.CreateOptions{})
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			return staticRoute, nil
		}
		klog.ErrorS(routecommon.ErrCreateRouteSet, fmt.Sprintf("%v", err))
		return nil, err
	}

	klog.V(6).Infof("Successfully created RouteSet CR for node %s", nodeName)
	return staticRoute, nil
}

func (sr *Staticroute) DeleteRoute(nodeName string) error {
	if err := sr.RouteClient.NsxV1alpha1().StaticRoutes(sr.Namespace).Delete(context.Background(), nodeName, metav1.DeleteOptions{}); err != nil {
		klog.ErrorS(routecommon.ErrDeleteRouteSet, fmt.Sprintf("%v", err))
		return err
	}
	klog.V(6).Infof("Successfully deleted RouteSet CR for node %s", nodeName)
	return nil
}
