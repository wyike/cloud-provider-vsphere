package ippool

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/cloud-provider-vsphere/pkg/cloudprovider/vsphereparavirtual/controllers/routablepod/ippool/v1alpha1"
	"k8s.io/cloud-provider-vsphere/pkg/cloudprovider/vsphereparavirtual/controllers/routablepod/ippool/v1alpha2"
	"time"
)

const (
	DefaultResyncTime time.Duration = time.Minute * 1
)

type IPPoolManager interface {
	GetIPPool(clusterNS, clusterName string) (interface{}, error)
	GetIPPoolFromIndexer(key string) (interface{}, error)
	CreateIPPool(clusterNS, clusterName string, ownerRef *metav1.OwnerReference) (interface{}, error)
	CheckIPPoolSubnets(old, cur interface{}) bool

	GetIPPoolSubnet(ippool interface{}) (map[string]string, error)
	AddSubnetToIPPool(node *corev1.Node, ippool interface{}, ownerRef *metav1.OwnerReference) (interface{}, error)
	DeleteSubnetFromIPPool(subnetName string, ippool interface{}) (interface{}, error)

	GetippoolListerSynced() cache.InformerSynced
	StartIppoolInformer()
	GetIppoolinformer() cache.SharedIndexInformer
}

func GetIPPoolManager(vpcMode bool, scCfg *rest.Config, clusterNS string) (IPPoolManager, error) {
	if vpcMode {
		return v1alpha2.NewIPPoolV2Manager(scCfg, clusterNS)
	}

	return v1alpha1.NewIPPoolV1Manager(scCfg, clusterNS)
}
