package types

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cilium/cilium/pkg/bpf"
	"github.com/cilium/cilium/pkg/maps/ipcache"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	v12 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
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

	shardFactory.Core().V1().Nodes().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			n, ok := obj.(*corev1.Node)
			if !ok {
				log.Warningf("get invalid node %v. ", obj)
				return
			}
			syncNodeTunnelCache(n)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			n, ok := newObj.(*corev1.Node)
			if !ok {
				log.Warningf("get invalid node %v. ", newObj)
				return
			}
			syncNodeTunnelCache(n)
		},
		DeleteFunc: func(obj interface{}) {
			// no need
		},
	})

	go shardFactory.Start(context.Background().Done())

	for t, ok := range shardFactory.WaitForCacheSync(context.Background().Done()) {
		if !ok {
			log.Errorf("Init vpc sharedFactory failed to wait %v ready", t)
		}
	}

	go syncIpCacheInfo()

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

func GetNodeVpcAddrDebug(nodeName string) net.IP {
	cacheIp := GetNodeVpcAddrByCache(nodeName)
	labelIp := GetNodeVpcAddr(nodeName)

	if !cacheIp.Equal(labelIp) {
		log.Warningf("GetNodeVpcAddrDebug: cache ip %s not match label ip %s. ",
			cacheIp.String(), labelIp.String())
	}
	return labelIp
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

var NodeCidrCache sync.Map   // key: nodeName, value: node cidr
var NodeCidrConvert sync.Map // key: node cidr, value: dest ip

func syncNodeTunnelCache(node *corev1.Node) {
	log.Debugf("sync node %s. ", node.Name)
	l := log.WithField("nodeName", node.Name)
	if node.Labels == nil || node.Annotations == nil {
		l.Debugf("skip node without label. ")
		return
	}

	if selfVpcId == "" { // self is master
		l.Debugf("skip node for master. ")
		return
	}

	if node.Name == GetName() {
		l.Debugf("skip self node. ")
		return
	}

	isMaster := IsMaster2(node)
	if node.Labels[VpcLabel] != selfVpcId && !isMaster {
		l.Debugf("skip node vpc id not same. %s. ", node.Labels[VpcLabel])
		return // skip
	}
	NodeCidrCache.Store(node.Name, node.Spec.PodCIDR)
	newIp := GetNodeVpcAddr(node.Name)
	NodeCidrConvert.Store(node.Spec.PodCIDR, newIp)
}

func GetNodeVpcAddrByCache(nodeName string) net.IP {
	cur, ok := NodeCidrCache.Load(nodeName)
	if ok {
		res, ok := NodeCidrConvert.Load(cur)
		if ok {
			return res.(net.IP)
		}
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

var selfVpcId = ""

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
			selfVpcId = node.Labels[VpcLabel]
			log.Infof("load self vpc id ok: %s. ", selfVpcId)
			return true, nil
		}
	}
	return false, nil
}

func NodeCidrHandlerFunc(writer http.ResponseWriter, request *http.Request) {
	writer.Write([]byte(getNodeVpcInfo()))
}

func getNodeVpcInfo() string {
	var res strings.Builder
	res.WriteString(fmt.Sprintf("node info:\n"))
	NodeCidrCache.Range(func(key, value any) bool {
		res.WriteString(fmt.Sprintf("%s: %s\n", key, value))
		return true
	})
	res.WriteString("node cidr convert:\n")
	NodeCidrConvert.Range(func(key, value any) bool {
		res.WriteString(fmt.Sprintf("%s: %s\n", key, value))
		return true
	})

	return res.String()
}

func syncIpCacheInfo() {
	var err error
	var ipMask *net.IPNet
	l := log.WithField("mode", "syncIpCacheInfo")
	for {
		time.Sleep(time.Second * 30)
		l.Debugf("check ip cache. ")

		NodeCidrConvert.Range(func(cidr, destIpStr any) bool {
			_, ipMask, err = net.ParseCIDR(cidr.(string))
			if err != nil {
				l.Errorf("skip invalid cidr %v. ", cidr)
				return true
			}
			l.Debugf("check %v, %s, %v. ", cidr, ipMask.String(), destIpStr)
			err = ipcache.IPCache.DumpWithCallback(func(key bpf.MapKey, value bpf.MapValue) {
				lkey, ok := key.(*ipcache.Key)
				if !ok {
					l.Errorf("skip invliad key %v. ", key)
					return
				}
				l.Debugf("check cache %v, %v. ", key.String(), value.String())
				if !ipMask.Contains(lkey.IP.IP().To4()) {
					l.Debugf("skip mask not match. ")
					return
				}
				lvalue, ok := value.(*ipcache.RemoteEndpointInfo)
				destIp := net.ParseIP(destIpStr.(string))
				l.Debugf("check destIP %s,%v, %v. ", destIpStr, destIp.String(), lvalue.String())
				if lvalue.TunnelEndpoint.IP().Equal(destIp) {
					l.Debugf("skip dest ip equal. ")
					return
				}
				l.Warningf("%s get dest ip not match: need: %s, actual: %s, do update. ",
					lkey.String(), destIp.String(), lvalue.TunnelEndpoint.String())
				newCp := lvalue.DeepCopy()
				copy(newCp.TunnelEndpoint[:], destIp.To4())

				err = ipcache.IPCache.Update(key, newCp)
				if err != nil {
					l.Errorf("update %s from %s to %s failed %v. ",
						lkey.String(), lvalue.TunnelEndpoint.String(), newCp.TunnelEndpoint.String(), err)
					return
				}
				l.Infof("update %s from %s to %s ok. ",
					lkey.String(), lvalue.TunnelEndpoint.String(), newCp.TunnelEndpoint.String())
			})
			if err != nil {
				l.Errorf("check ip cache failed %v. ", err)
			}
			return true
		})

	}
}
