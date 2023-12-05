package types

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	v12 "k8s.io/client-go/listers/core/v1"
)

const VpcLabel = "vpc.id"
const VpcNumLabel = "vpc.num"
const VpcInternalIPAnnotation = "vpc.internal.ip"
const VpcExternalIPAnnotation = "vpc.external.ip"

const masterLabel = "node-role.kubernetes.io/master"
const clusterLabel = "squids/cluster"

/*
由于使用k8s包会导致cycle引入，所以这里简单实现一个k8s client go，只需要实现nodeLister
*/

var nodeLister v12.NodeLister
var PodLister v12.PodLister
var clientSet kubernetes.Interface

func InitVpc(k8sClient kubernetes.Interface) error {
	log.Debugf("Start init vpc mod.")

	clientSet = k8sClient

	err := waitCiliumNodeToLabel(k8sClient)
	if err != nil {
		return err
	}

	shardFactory := informers.NewSharedInformerFactoryWithOptions(k8sClient, time.Hour)

	nodeLister = shardFactory.Core().V1().Nodes().Lister()
	PodLister = shardFactory.Core().V1().Pods().Lister()

	//shardFactory.Core().V1().Nodes().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
	//	AddFunc: func(obj interface{}) {
	//		n, ok := obj.(*corev1.Node)
	//		if !ok {
	//			return
	//		}
	//		syncNodeTunnelCache(n)
	//	},
	//	UpdateFunc: func(oldObj, newObj interface{}) {
	//		n, ok := newObj.(*corev1.Node)
	//		if !ok {
	//			return
	//		}
	//		syncNodeTunnelCache(n)
	//	},
	//	DeleteFunc: func(obj interface{}) {
	//		// no need
	//	},
	//})

	go shardFactory.Start(context.Background().Done())

	for t, ok := range shardFactory.WaitForCacheSync(context.Background().Done()) {
		if !ok {
			log.Errorf("Init vpc sharedFactory failed to wait %v ready", t)
		}
	}

	log.Debugf("Init vpc mod done.")
	return nil
}

func IsMaster(nodeName string) bool {
	selfNode, err := nodeLister.Get(nodeName)
	if err != nil {
		log.WithError(err).Errorf("Get self node %s info failed. ", GetName())
		return false
	}

	return IsMaster2(selfNode)
}

func IsMaster2(node *corev1.Node) bool {
	if node.Labels == nil {
		return false
	}

	if _, ok := node.Labels[masterLabel]; ok {
		return true
	}

	return false
}

func IsSameVpc(label string) bool {
	if label == "" {
		return false
	}
	selfNode, err := nodeLister.Get(GetName())
	if err != nil {
		log.WithError(err).Errorf("Get self node %s info failed. ", GetName())
		return false
	}

	if selfNode.Labels == nil {
		return false
	}
	clusterLabel, ok := selfNode.Labels[clusterLabel]
	if !ok {
		return false
	}

	log.Infof("Got svc label[%s] to node label[%s]", label, clusterLabel)

	if strings.Split(clusterLabel, "-")[1] == strings.Split(label, "-")[1] {
		return true
	}

	return false
}

func GetNodeVpcAddr(nodeName string) net.IP {
	if nodeLister == nil {
		log.Warningf("Node vpc lister is not init, skip. ")
		return nil
	}

	// TODO add vpc lan
	selfNode, err := nodeLister.Get(GetName())
	if err != nil {
		log.WithError(err).Errorf("Get self node %s info failed. ", GetName())
		return nil
	}
	nextNode, err := nodeLister.Get(nodeName)
	if err != nil {
		log.WithError(err).Errorf("Get next node %s info failed. ", nodeName)
		return nil
	}

	if nextNode.Annotations == nil {
		return nil
	}

	selfVpc := ""
	if selfNode.Labels != nil {
		selfVpc = selfNode.Labels[VpcLabel]
	}
	nextVpc := ""
	if nextNode.Labels != nil {
		nextVpc = nextNode.Labels[VpcLabel]
	}

	if selfVpc == nextVpc && nextNode.Annotations[VpcInternalIPAnnotation] != "" {
		return GetIp(fmt.Sprintf("Got same vpc to next node[%s], use internal", nextNode.Name),
			nextNode.Annotations[VpcInternalIPAnnotation])
	}

	// for vpc.num > 1
	if nextNode.Annotations[VpcNumLabel] != "" && nextNode.Annotations[VpcNumLabel] > "1" {
		num, err := strconv.Atoi(nextNode.Annotations[VpcNumLabel])
		if err != nil {
			log.Warningf("Get invalid %s config %s. skip. ", VpcNumLabel, nextNode.Annotations[VpcNumLabel])
			return nil
		}
		for i := 1; i < num; i++ {
			// 由于有多个vpc配置，所以，vpcId必须要匹配。
			if selfNode.Annotations[VpcLabel] == nextNode.Annotations[GetKey(VpcLabel, i)] {
				return GetIp(fmt.Sprintf("Got same vpc %d to next node[%s], use internal", i, nextNode.Name),
					nextNode.Annotations[GetKey(VpcInternalIPAnnotation, i)])
			}
		}
	}

	if selfVpc != nextVpc && nextNode.Annotations[VpcExternalIPAnnotation] != "" {
		return GetIp(fmt.Sprintf("Got diff vpc to next node[%s], use external", nextNode.Name),
			nextNode.Annotations[VpcExternalIPAnnotation])
	}

	return nil
}

func GetIp(desc, src string) net.IP {
	res := net.ParseIP(src).To4()
	log.Debugf("%s ip %s. ", desc, res.String())
	return res
}

func GetKey(prefix string, idx int) string {
	return fmt.Sprintf("%s_%d", prefix, idx)
}

/*
-- node节点缓存方案
同vpc下，只缓存内部地址
mater节点，不同vpc时，缓存外部地址
其它节点不缓存

-- master节点缓存方案
不需要
*/

var nodeTunnelCache sync.Map // key: nodeName, value: ip

var localVpcId = ""

var selfIsMaster *bool

func syncNodeTunnelCache(node *corev1.Node) {
	if node.Labels == nil || node.Annotations == nil {
		return
	}

	if selfIsMaster != nil && *selfIsMaster {
		return
	}

	if node.Name == GetName() {
		f := IsMaster(node.Name)
		selfIsMaster = &f
		if localVpcId == "" {
			localVpcId = node.Labels[VpcLabel]
		}
	}

	isMaster := IsMaster2(node)
	if node.Labels[VpcLabel] != localVpcId && !isMaster {
		return // skip
	}
	newIp := GetNodeVpcAddr(node.Name)
	cur, ok := nodeTunnelCache.Load(node.Name)
	if !ok {
		nodeTunnelCache.Store(node.Name, newIp)
		return
	}
	oldIp, ok := cur.(net.IP)
	if ok && oldIp.Equal(newIp) {
		return
	}
	nodeTunnelCache.Store(node.Name, newIp)
}

func GetNodeVpcAddrByCache(nodeName string) net.IP {
	cur, ok := nodeTunnelCache.Load(nodeName)
	if ok {
		return cur.(net.IP)
	}
	return nil
}

func waitCiliumNodeToLabel(k8sClient kubernetes.Interface) error {
	logger := log.WithFields(logrus.Fields{
		"component": "waitCiliumNodeToLabel",
	})

	ok, err := isNodeOk(k8sClient)
	if ok {
		return nil
	}
	for i := 0; i < 10; i++ {
		// wait to init
		time.Sleep(time.Second * 20)
		ok, err = isNodeOk(k8sClient)
		if err != nil {
			logger.Errorf("check node failed %v. ", err)
			continue
		}
		if ok {
			return nil
		}
	}
	return fmt.Errorf("get ciliumnodes timeout. ")
}

func isNodeOk(k8sClient kubernetes.Interface) (bool, error) {
	node, err := k8sClient.CoreV1().Nodes().Get(context.TODO(), nodeName, v1.GetOptions{})
	if err != nil {
		return false, fmt.Errorf("get node info failed %v. ", err)
	}

	if IsMaster2(node) {
		return true, nil
	}

	if node.Labels != nil && node.Annotations != nil {
		if node.Labels[VpcLabel] != "" {
			return true, nil
		}
	}
	return false, nil
}
