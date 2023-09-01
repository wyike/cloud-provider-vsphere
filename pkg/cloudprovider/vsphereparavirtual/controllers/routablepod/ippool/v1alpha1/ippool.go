package v1alpha1

import (
	"context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	ippoolv1alpha1 "k8s.io/cloud-provider-vsphere/pkg/cloudprovider/vsphereparavirtual/apis/nsxnetworking/v1alpha1"
	ippoolclientset "k8s.io/cloud-provider-vsphere/pkg/cloudprovider/vsphereparavirtual/client/v1alpha1/clientset/versioned"
	ippoolscheme "k8s.io/cloud-provider-vsphere/pkg/cloudprovider/vsphereparavirtual/client/v1alpha1/clientset/versioned/scheme"
	ippoolfactory "k8s.io/cloud-provider-vsphere/pkg/cloudprovider/vsphereparavirtual/client/v1alpha1/informers/externalversions"
	ippoolinformers "k8s.io/cloud-provider-vsphere/pkg/cloudprovider/vsphereparavirtual/client/v1alpha1/informers/externalversions"
	ippoolinformers1 "k8s.io/cloud-provider-vsphere/pkg/cloudprovider/vsphereparavirtual/client/v1alpha1/informers/externalversions/nsxnetworking/v1alpha1"
	"k8s.io/cloud-provider-vsphere/pkg/cloudprovider/vsphereparavirtual/controllers/routablepod/helper"
	"k8s.io/cloud-provider-vsphere/pkg/cloudprovider/vsphereparavirtual/controllers/routablepod/ippool"
	"reflect"
)

type IPPoolV1Manager struct {
	Ippoolclientset       ippoolclientset.Interface
	Ippoolinformer        ippoolinformers1.IPPoolInformer
	IppoolInformerFactory ippoolfactory.SharedInformerFactory

	ippoolListerSynced cache.InformerSynced
}

func NewIPPoolV1Manager(scCfg *rest.Config, clusterNS string) (*IPPoolV1Manager, error) {
	ipcs, err := ippoolclientset.NewForConfig(scCfg)
	if err != nil {
		return nil, fmt.Errorf("error building ippool clientset: %w", err)
	}

	s := scheme.Scheme
	if err := ippoolscheme.AddToScheme(s); err != nil {
		return nil, fmt.Errorf("failed to register ippoolSchemes")
	}

	ippoolInformerFactory := ippoolinformers.NewSharedInformerFactoryWithOptions(ipcs, ippool.DefaultResyncTime, ippoolinformers.WithNamespace(clusterNS))
	ippoolInformer := ippoolInformerFactory.Nsx().V1alpha1().IPPools()

	return &IPPoolV1Manager{
		ipcs,
		ippoolInformer,
		ippoolInformerFactory,
		ippoolInformer.Informer().HasSynced,
	}, nil
}

func (p *IPPoolV1Manager) GetIPPool(clusterNS, clusterName string) (interface{}, error) {
	ctx := context.Background()
	ippool, err := p.Ippoolclientset.NsxV1alpha1().IPPools(clusterNS).Get(ctx, helper.IppoolNameFromClusterName(clusterName), metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return ippool, nil
}

// why we need get from indexer
func (p *IPPoolV1Manager) GetIPPoolFromIndexer(key string) (interface{}, error) {
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

func (p *IPPoolV1Manager) UpdateIPPool(ippool *ippoolv1alpha1.IPPool) (*ippoolv1alpha1.IPPool, error) {
	ippool, err := p.Ippoolclientset.NsxV1alpha1().IPPools(ippool.Namespace).Update(context.Background(), ippool, metav1.UpdateOptions{})
	if err != nil {
		return nil, fmt.Errorf("fail to get ippool %s in namespace %s", ippool.Name, ippool.Namespace)
	}

	return ippool, nil
}

func (p *IPPoolV1Manager) CreateIPPool(clusterNS, clusterName string, ownerRef *metav1.OwnerReference) (interface{}, error) {
	ippool := &ippoolv1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      helper.IppoolNameFromClusterName(clusterName),
			Namespace: clusterNS,
			OwnerReferences: []metav1.OwnerReference{
				*ownerRef,
			},
		},
		Spec: ippoolv1alpha1.IPPoolSpec{
			Subnets: []ippoolv1alpha1.SubnetRequest{},
		},
	}

	return p.Ippoolclientset.NsxV1alpha1().IPPools(clusterNS).Create(context.Background(), ippool, metav1.CreateOptions{})
}

func (p *IPPoolV1Manager) GetIPPoolSubnet(pool interface{}) (map[string]string, error) {
	ippool, ok := pool.(*ippoolv1alpha1.IPPool)
	if !ok {
		return nil, fmt.Errorf("unknow ippool struct")
	}

	subs := make(map[string]string)
	for _, sub := range ippool.Status.Subnets {
		subs[sub.Name] = sub.CIDR
	}

	return subs, nil
}

func (p *IPPoolV1Manager) DeleteSubnetFromIPPool(subnetName string, pool interface{}) (interface{}, error) {
	ippool, ok := pool.(*ippoolv1alpha1.IPPool)
	if !ok {
		return nil, fmt.Errorf("unknow ippool struct")
	}

	newSubnets := []ippoolv1alpha1.SubnetRequest{}
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

func (p *IPPoolV1Manager) AddSubnetToIPPool(node *corev1.Node, pool interface{}, ownerRef *metav1.OwnerReference) (interface{}, error) {
	ippool, ok := pool.(*ippoolv1alpha1.IPPool)
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
		newIPPool.Spec.Subnets = append(newIPPool.Spec.Subnets, ippoolv1alpha1.SubnetRequest{
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

func (p *IPPoolV1Manager) StartIppoolInformer() {
	p.IppoolInformerFactory.Start(wait.NeverStop)
}

func (p *IPPoolV1Manager) GetippoolListerSynced() cache.InformerSynced {
	return p.ippoolListerSynced
}

func (p *IPPoolV1Manager) GetIppoolinformer() cache.SharedIndexInformer {
	return p.Ippoolinformer.Informer()
}

func (p *IPPoolV1Manager) CheckIPPoolSubnets(old, cur interface{}) bool {
	oldIPPool, ok := old.(*ippoolv1alpha1.IPPool)
	if !ok {
		return false
	}
	curIPPool, ok := cur.(*ippoolv1alpha1.IPPool)
	if !ok {
		return false
	}
	if reflect.DeepEqual(oldIPPool.Status.Subnets, curIPPool.Status.Subnets) {
		return false
	}

	return true
}
