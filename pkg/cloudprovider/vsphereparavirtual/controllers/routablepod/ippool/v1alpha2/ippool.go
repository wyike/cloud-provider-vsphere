package v1alpha2

import (
	"context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	ippoolv1alpha2 "k8s.io/cloud-provider-vsphere/pkg/cloudprovider/vsphereparavirtual/apis/nsxnetworking/v1alpha2"
	ippoolclientset2 "k8s.io/cloud-provider-vsphere/pkg/cloudprovider/vsphereparavirtual/client/v1alpha2/clientset/versioned"
	ippoolscheme2 "k8s.io/cloud-provider-vsphere/pkg/cloudprovider/vsphereparavirtual/client/v1alpha2/clientset/versioned/scheme"
	ippoolfactory2 "k8s.io/cloud-provider-vsphere/pkg/cloudprovider/vsphereparavirtual/client/v1alpha2/informers/externalversions"
	ippoolinformers2 "k8s.io/cloud-provider-vsphere/pkg/cloudprovider/vsphereparavirtual/client/v1alpha2/informers/externalversions"
	ippoolinformers12 "k8s.io/cloud-provider-vsphere/pkg/cloudprovider/vsphereparavirtual/client/v1alpha2/informers/externalversions/nsxnetworking/v1alpha2"
	"k8s.io/cloud-provider-vsphere/pkg/cloudprovider/vsphereparavirtual/controllers/routablepod/helper"
	"k8s.io/cloud-provider-vsphere/pkg/cloudprovider/vsphereparavirtual/controllers/routablepod/ippool"
	"reflect"
)

type IPPoolV2Manager struct {
	Ippoolclientset       ippoolclientset2.Interface
	Ippoolinformer        ippoolinformers12.IPPoolInformer
	IppoolInformerFactory ippoolfactory2.SharedInformerFactory

	ippoolListerSynced cache.InformerSynced
}

func NewIPPoolV2Manager(scCfg *rest.Config, clusterNS string) (*IPPoolV2Manager, error) {
	ipcs, err := ippoolclientset2.NewForConfig(scCfg)
	if err != nil {
		return nil, fmt.Errorf("error building ippool clientset: %w", err)
	}

	s := scheme.Scheme
	if err := ippoolscheme2.AddToScheme(s); err != nil {
		return nil, fmt.Errorf("failed to register ippoolSchemes")
	}

	ippoolInformerFactory := ippoolinformers2.NewSharedInformerFactoryWithOptions(ipcs, ippool.DefaultResyncTime, ippoolinformers2.WithNamespace(clusterNS))
	ippoolInformer := ippoolInformerFactory.Nsx().V1alpha2().IPPools()

	return &IPPoolV2Manager{
		ipcs,
		ippoolInformer,
		ippoolInformerFactory,
		ippoolInformer.Informer().HasSynced,
	}, nil
}

func (p *IPPoolV2Manager) GetIPPool(clusterNS, clusterName string) (interface{}, error) {
	ctx := context.Background()
	ippool, err := p.Ippoolclientset.NsxV1alpha2().IPPools(clusterNS).Get(ctx, helper.IppoolNameFromClusterName(clusterName), metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return ippool, nil
}

func (p *IPPoolV2Manager) GetIPPoolFromIndexer(key string) (interface{}, error) {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return nil, err
	}

	ippool, err := p.Ippoolinformer.Lister().IPPools(namespace).Get(name)
	if err != nil {
		return nil, fmt.Errorf("fail to get ippool with key %s", key)
	}

	return ippool, nil
}

func (p *IPPoolV2Manager) UpdateIPPool(ippool *ippoolv1alpha2.IPPool) (*ippoolv1alpha2.IPPool, error) {
	ippool, err := p.Ippoolclientset.NsxV1alpha2().IPPools(ippool.Namespace).Update(context.Background(), ippool, metav1.UpdateOptions{})
	if err != nil {
		return nil, fmt.Errorf("fail to get ippool %s in namespace %s", ippool.Name, ippool.Namespace)
	}

	return ippool, nil
}

func (p *IPPoolV2Manager) CreateIPPool(clusterNS, clusterName string, ownerRef *metav1.OwnerReference) (interface{}, error) {
	ippool := &ippoolv1alpha2.IPPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      helper.IppoolNameFromClusterName(clusterName),
			Namespace: clusterNS,
			OwnerReferences: []metav1.OwnerReference{
				*ownerRef,
			},
		},
		Spec: ippoolv1alpha2.IPPoolSpec{
			Subnets: []ippoolv1alpha2.SubnetRequest{},
		},
	}

	return p.Ippoolclientset.NsxV1alpha2().IPPools(clusterNS).Create(context.Background(), ippool, metav1.CreateOptions{})
}

func (p *IPPoolV2Manager) GetIPPoolSubnet(pool interface{}) (map[string]string, error) {
	ippool, ok := pool.(*ippoolv1alpha2.IPPool)
	if !ok {
		return nil, fmt.Errorf("unknow ippool struct")
	}

	subs := make(map[string]string)
	for _, sub := range ippool.Status.Subnets {
		subs[sub.Name] = sub.CIDR
	}

	return subs, nil
}

func (p *IPPoolV2Manager) DeleteSubnetFromIPPool(subnetName string, pool interface{}) (interface{}, error) {
	ippool, ok := pool.(*ippoolv1alpha2.IPPool)
	if !ok {
		return nil, fmt.Errorf("unknow ippool struct")
	}

	newSubnets := []ippoolv1alpha2.SubnetRequest{}
	for _, sub := range ippool.Spec.Subnets {
		if sub.Name == subnetName {
			continue
		}
		newSubnets = append(newSubnets, sub)
	}
	ippool.Spec.Subnets = newSubnets

	ippool, err := p.UpdateIPPool(ippool)
	if err != nil {
		return nil, fmt.Errorf("fail to update ippool %s in namespace %s", ippool.Name, ippool.Namespace)
	}

	return ippool, nil
}

func (p *IPPoolV2Manager) AddSubnetToIPPool(node *corev1.Node, pool interface{}, ownerRef *metav1.OwnerReference) (interface{}, error) {
	ippool, ok := pool.(*ippoolv1alpha2.IPPool)
	if !ok {
		return nil, fmt.Errorf("unknow ippool struct")
	}

	// skip if the request already added
	for _, sub := range ippool.Spec.Subnets {
		if sub.Name == node.Name {
			//klog.V(4).Info("node %s already requested the ip", node.Name)
			return ippool, nil
		}
	}

	newIPPool := ippool.DeepCopy()
	// add node cidr allocation req to the ippool spec only when node doesn't contain pod cidr
	if node.Spec.PodCIDR == "" || len(node.Spec.PodCIDRs) == 0 {
		newIPPool.Spec.Subnets = append(newIPPool.Spec.Subnets, ippoolv1alpha2.SubnetRequest{
			Name:         node.Name,
			IPFamily:     helper.IPFamilyDefault,
			PrefixLength: helper.PrefixLengthDefault,
		})
	}

	if newIPPool.OwnerReferences == nil {
		newIPPool.OwnerReferences = []metav1.OwnerReference{*ownerRef}
	}

	ippool, err := p.UpdateIPPool(ippool)
	if err != nil {
		return nil, fmt.Errorf("fail to update ippool %s in namespace %s", ippool.Name, ippool.Namespace)
	}

	return ippool, nil
}

func (p *IPPoolV2Manager) StartIppoolInformer() {
	p.IppoolInformerFactory.Start(wait.NeverStop)
}

func (p *IPPoolV2Manager) GetippoolListerSynced() cache.InformerSynced {
	return p.ippoolListerSynced
}

func (p *IPPoolV2Manager) GetIppoolinformer() cache.SharedIndexInformer {
	return p.Ippoolinformer.Informer()
}

func (p *IPPoolV2Manager) CheckIPPoolSubnets(old, cur interface{}) bool {
	oldIPPool, ok := old.(*ippoolv1alpha2.IPPool)
	if !ok {
		return false
	}
	curIPPool, ok := cur.(*ippoolv1alpha2.IPPool)
	if !ok {
		return false
	}
	if reflect.DeepEqual(oldIPPool.Status.Subnets, curIPPool.Status.Subnets) {
		return false
	}

	return true
}
