package route

import (
	"context"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	cloudprovider "k8s.io/cloud-provider"
	v1alpha1 "k8s.io/cloud-provider-vsphere/pkg/cloudprovider/vsphereparavirtual/apis/nsxnetworking/v1alpha1"
	client "k8s.io/cloud-provider-vsphere/pkg/cloudprovider/vsphereparavirtual/client/v1alpha1/clientset/versioned"
	"k8s.io/cloud-provider-vsphere/pkg/cloudprovider/vsphereparavirtual/route/routeset"
	"k8s.io/cloud-provider-vsphere/pkg/cloudprovider/vsphereparavirtual/route/staticroute"
	klog "k8s.io/klog/v2"
)

type RouteManager interface {
	// ListRoutes lists all managed routes that belong to the specified clusterName
	ListRoutes(ctx context.Context, clusterName string) (interface{}, error)
	CreateCPRoutes(routes interface{}) ([]*cloudprovider.Route, error)
	CreateRouteSetCR(ctx context.Context, clusterName string, nameHint string, nodeName string, cidr string, nodeIP string) (interface{}, error)
	CheckRouteCRReady(crName string) error
	// CreateRoute creates the described managed route
	// route.Name will be ignored, although the cloud-provider may use nameHint
	// to create a more user-meaningful name.
	//CreateRoute(ctx context.Context, clusterName string, nameHint string, route *cloudprovider.Route, nodeIP string) error
	// DeleteRoute deletes the specified managed route
	// Route should be as returned by ListRoutes
	DeleteRoute(route string) error
}

// GetRouteSetClient returns a new RouteSet client that can be used to access SC
func GetRouteSetClient(config *rest.Config) (client.Interface, error) {
	v1alpha1.AddToScheme(scheme.Scheme)
	rClient, err := client.NewForConfig(config)
	if err != nil {
		klog.V(6).Infof("Failed to create RouteSet clientset")
		return nil, err
	}
	return rClient, nil
}

func GetRouteManager(vpcMode bool, config *rest.Config, clusterNS string, ownerRef metav1.OwnerReference) (RouteManager, error) {
	routeClient, err := GetRouteSetClient(config)
	if err != nil {
		return nil, err
	}

	ownerRefs := []metav1.OwnerReference{
		ownerRef,
	}

	if vpcMode {
		return &staticroute.Staticroute{
			routeClient,
			clusterNS,
			ownerRefs,
		}, nil
	}

	return &routeset.Routeset{
		routeClient,
		clusterNS,
		ownerRefs,
	}, nil
}
