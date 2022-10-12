package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/cilium/cilium/pkg/ipam/types"
	v2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"github.com/cilium/cilium/pkg/k8s/client/clientset/versioned"
	"github.com/cilium/cilium/pkg/logging"
	"github.com/cilium/cilium/pkg/logging/logfields"
)

const CiliumIPAMPodAnnotation = "io.cilium.cni/IPAM.crd"

var plog = logging.DefaultLogger.WithField(logfields.LogSubsys, "pod-ipam")

var podAllocateCache sync.Map

func DoPodAddHandle(ciliumClientset *versioned.Clientset, pod *v1.Pod) error {
	if pod.Annotations == nil || pod.Annotations[CiliumIPAMPodAnnotation] == "" {
		return nil
	}

	nodeName := pod.Spec.NodeName

	if nodeName == "" {
		return nil
	}
	key := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)

	if pod.Status.PodIP != "" {
		podAllocateCache.Delete(key) // clean cache
		return nil
	}
	_, ok := podAllocateCache.Load(key) // has allocate before
	if ok {
		return nil
	}

	l := plog.WithField("nodeName", nodeName)

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

	newCn, err := AllocateIpForPod(cn, key, owner)
	if err != nil {
		l.Errorf("allocate ip for pod %s failed %v. ", key, err)
		return nil
	}

	if newCn == nil {
		return nil // no need allocate
	}
	l.Infof("allocate ip for pod. ")

	ctx, can := context.WithTimeout(context.TODO(), time.Second*20)
	defer can()
	_, err = ciliumClientset.CiliumV2().CiliumNodes().Update(ctx, newCn, metav1.UpdateOptions{})
	if err != nil {
		l.Errorf("Update cilium node %s failed : %v", nodeName, err)
		return err
	}
	l.Infof("allocate ip ok. ")
	podAllocateCache.Store(key, "true")
	return nil
}


func AllocateIpForPod(cn *v2.CiliumNode, owner, resource string) (*v2.CiliumNode, error)  {
	avaIp := ""

	for k, v := range cn.Spec.IPAM.Pool {
		if v.Owner == owner {
			return nil, nil // has already allocate
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
	return newCn, nil
}