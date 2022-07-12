package types

import (
	"context"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"net"
	"strings"
	"time"

	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	v12 "k8s.io/client-go/listers/core/v1"
)

const VpcLabel = "vpc.id"
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

	shardFactory := informers.NewSharedInformerFactoryWithOptions(k8sClient, time.Hour)

	nodeLister = shardFactory.Core().V1().Nodes().Lister()
	PodLister = shardFactory.Core().V1().Pods().Lister()

	go shardFactory.Start(context.Background().Done())

	for t, ok := range shardFactory.WaitForCacheSync(context.Background().Done()) {
		if !ok {
			log.Errorf("Init vpc sharedFactory failed to wait %v ready", t)
		}
	}

	log.Debugf("Init vpc mod done.")
	return nil
}

func GetNodeVpcConvert(srcIP string) net.IP {
	log.Debugf("Get node list for svc. %#v", srcIP)

	nodeList, err := nodeLister.List(labels.Everything())
	if err != nil {
		log.WithError(err).Errorln("Get node list failed. ")
		return nil
	}
	log.Debugf("Get current node list %d. ", len(nodeList))
	for _, n := range nodeList {
		for _, ip := range n.Status.Addresses {
			if ip.Address == srcIP {
				return GetNodeVpcAddr(n.Name)
			}
		}
	}
	return nil
}

func IsMaster(nodeName string) bool {
	selfNode, err := nodeLister.Get(nodeName)
	if err != nil {
		log.WithError(err).Errorf("Get self node %s info failed. ", GetName())
		return false
	}

	if selfNode.Labels == nil {
		return false
	}

	if _, ok := selfNode.Labels[masterLabel]; ok {
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
	var nextNodeIP net.IP
	selfNode, err := clientSet.CoreV1().Nodes().Get(context.Background(), GetName(), v1.GetOptions{})
	if err != nil {
		log.WithError(err).Errorf("Get self node %s info failed. ", GetName())
		return nil
	}
	nextNode, err := clientSet.CoreV1().Nodes().Get(context.Background(), nodeName, v1.GetOptions{})
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
		nextNodeIP = net.ParseIP(nextNode.Annotations[VpcInternalIPAnnotation]).To4()
		log.Infof("Got same vpc to next node[%s], use internal ip %s. ", nextNode.Name, nextNodeIP.String())
		return nextNodeIP
	}
	if selfVpc != nextVpc && nextNode.Annotations[VpcExternalIPAnnotation] != "" {
		nextNodeIP = net.ParseIP(nextNode.Annotations[VpcExternalIPAnnotation]).To4()
		log.Infof("Got diff vpc to next node[%s], use external ip %s. ", nextNode.Name, nextNodeIP.String())
		return nextNodeIP
	}
	return nil
}
