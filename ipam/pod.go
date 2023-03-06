package main

import (
	"context"
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/cilium/cilium/pkg/ipam/types"
	v2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"github.com/cilium/cilium/pkg/k8s/client/clientset/versioned"
	"github.com/cilium/cilium/pkg/logging"
	"github.com/cilium/cilium/pkg/logging/logfields"
)


var plog = logging.DefaultLogger.WithField(logfields.LogSubsys, "pod-ipam")

func DoPodAddHandle(ciliumClientset *versioned.Clientset, pod *v1.Pod, selector labels.Selector) error {
	nodeName := pod.Spec.NodeName

	if nodeName == "" || pod.Status.PodIP == "" {
		return nil
	}
	key := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
	l := plog.WithField("key", key)

	if !selector.Matches(labels.Set(pod.Labels)) {
		return nil
	}

	l.Infof("do pod. ")

	l = l.WithField("nodeName", nodeName)

	cn, err := ciliumNodeLister.Get(nodeName)
	if err != nil {
		l.Errorf("Could not find cilium node %s info : %v", nodeName, err)
		return nil
	}

	owner := ""
	if pod.OwnerReferences != nil && len(pod.OwnerReferences) > 0 {
		owner = fmt.Sprintf("%s/%s/%s", pod.Namespace, pod.OwnerReferences[0].Kind, pod.OwnerReferences[0].Name)
	}

	l = l.WithField("key", key).WithField("owner", owner)

	newCn := FlagIpForPod(cn, key, owner, pod.Status.PodIP)

	if newCn == nil {
		l.Infof("no need update. ")
		return nil // no need allocate
	}
	l = l.WithField("newIp", pod.Status.PodIP)
	l.Infof("flag ip for pod. ")

	ctx, can := context.WithTimeout(context.TODO(), time.Second*20)
	defer can()
	_, err = ciliumClientset.CiliumV2().CiliumNodes().Update(ctx, newCn, metav1.UpdateOptions{})
	if err != nil {
		l.Errorf("Update cilium node %s failed : %v", nodeName, err)
		return err
	}
	l.Infof("allocate ip ok. ")
	return nil
}

func FlagIpForPod(cn *v2.CiliumNode, owner, resource, ipInfo string) *v2.CiliumNode  {
	if cn.Spec.IPAM.Pool[ipInfo].Owner == owner && cn.Spec.IPAM.Pool[ipInfo].Resource == resource {
		return nil
	}
	newCn := cn.DeepCopy()
	if newCn.Spec.IPAM.Pool == nil {
		newCn.Spec.IPAM.Pool = make(map[string]types.AllocationIP, 0)
	}
	newCn.Spec.IPAM.Pool[ipInfo] = types.AllocationIP{
		Owner:    owner,
		Resource: resource,
	}
	return newCn
}


func AllocateIpForPod(cn *v2.CiliumNode, owner, resource string) (*v2.CiliumNode, string)  {
	avaIp := ""

	for k, v := range cn.Spec.IPAM.Pool {
		if v.Owner == owner {
			return nil, "" // has already allocate
		}
		if avaIp == "" {
			_, ok := cn.Status.IPAM.Used[k]
			if ok {
				continue
			}
			avaIp = k
		}
	}
	newCn := cn.DeepCopy()
	newCn.Spec.IPAM.Pool[avaIp] = types.AllocationIP{
		Owner:    owner,
		Resource: resource,
	}
	return newCn, avaIp
}