package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	v13 "k8s.io/client-go/listers/apps/v1"
	v14 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"

	"github.com/cilium/cilium/pkg/ipam/types"
	v2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"github.com/cilium/cilium/pkg/k8s/client/clientset/versioned"
	"github.com/cilium/cilium/pkg/k8s/client/informers/externalversions"
	v22 "github.com/cilium/cilium/pkg/k8s/client/listers/cilium.io/v2"
	"github.com/cilium/cilium/pkg/logging"
	"github.com/cilium/cilium/pkg/logging/logfields"
)

var mlog = logging.DefaultLogger.WithField(logfields.LogSubsys, "my-ipam")

var (
	certFile = pflag.String("cert_file", "", "https cert file")
	keyFile  = pflag.String("key_file", "", "https key file")

	address = pflag.String("port", ":443", "address to listen. ")
)


var (
	ciliumNodeLister v22.CiliumNodeLister
	ciliumClientset *versioned.Clientset

	stsLister v13.StatefulSetLister
	podLister v14.PodLister
)

func main()  {
	kf := flag.NewFlagSet("klog", flag.PanicOnError)
	klog.InitFlags(kf)

	pflag.CommandLine.AddGoFlagSet(kf)
	pflag.Parse()

	kubeConfig, err := clientcmd.BuildConfigFromFlags("", os.Getenv("KUBECONFIG"))
	if err != nil {
		klog.Fatalf("Failed to create config: %v", err)
		panic(err.Error())
	}

	http.HandleFunc("/inject", ServerInject)

	clientset := kubernetes.NewForConfigOrDie(kubeConfig)
	ciliumClientset = versioned.NewForConfigOrDie(kubeConfig)

	sharedInformer := informers.NewSharedInformerFactory(clientset, time.Minute)
	ciliumSharedInformer := externalversions.NewSharedInformerFactoryWithOptions(ciliumClientset, time.Minute)

	podQueue :=  workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "pod")
	ciliumNodeQueue := workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "ciliumNode")

	ciliumSharedInformer.Cilium().V2().CiliumNodes().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			if err != nil {
				mlog.Errorf("get add meta name failed %v. ", err)
				return
			}
			ciliumNodeQueue.Add(key)

		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(newObj)
			if err != nil {
				mlog.Errorf("get upd meta name failed %v. ", err)
				return
			}
			ciliumNodeQueue.Add(key)
		},
		DeleteFunc: func(obj interface{}) {
			// nil
		},
	})

	stsLister = sharedInformer.Apps().V1().StatefulSets().Lister()
	podLister = sharedInformer.Core().V1().Pods().Lister()
	sharedInformer.Core().V1().Pods().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			if err != nil {
				mlog.Errorf("get add pod meta name failed %v. ", err)
				return
			}
			podQueue.Add(key)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(newObj)
			if err != nil {
				mlog.Errorf("get upd pod meta name failed %v. ", err)
				return
			}
			podQueue.Add(key)
		},
		DeleteFunc: nil,
	})

	ciliumNodeLister = ciliumSharedInformer.Cilium().V2().CiliumNodes().Lister()

	ch := context.TODO().Done()
	sharedInformer.Start(ch)
	sharedInformer.WaitForCacheSync(ch)
	ciliumSharedInformer.Start(ch)
	ciliumSharedInformer.WaitForCacheSync(ch)

	go func() {
		lc := mlog.WithField("name", "ciliumNodeQueue")
		for {
			key, quit := ciliumNodeQueue.Get()
			if quit {
				lc.Infof("quit. ")
				return
			}
			cn, err := ciliumNodeLister.Get(key.(string))
			if err != nil {
				lc.Errorf("get key %s failed %v. ", key.(string), err)
				ciliumNodeQueue.Done(key)
				continue
			}
			err = SyncCiliumNode(cn)
			if err != nil {
				ciliumNodeQueue.AddRateLimited(key)
			} else {
				ciliumNodeQueue.Forget(key)
			}
			ciliumNodeQueue.Done(key)
		}
	}()
	go func() {
		lp := mlog.WithField("name", "podQueue")
		for {
			key, quit := podQueue.Get()
			if quit {
				lp.Infof("quit. ")
				return
			}
			podQueue.Forget(key)
			ns, name, err := cache.SplitMetaNamespaceKey(key.(string))
			if err != nil {
				lp.Infof("get %s meta name faild %v. ", key.(string), err)
				podQueue.Done(key)
				continue
			}
			pod, err := podLister.Pods(ns).Get(name)
			if err != nil {
				lp.Errorf("get pod %s/%s info failed %v. ", ns, name, err)
				podQueue.Done(key)
				continue
			}
			err = DoPodAddHandle(ciliumClientset, pod)
			if err != nil {
				podQueue.AddRateLimited(key)
			} else {
				podQueue.Forget(key)
			}
			podQueue.Done(key)
		}
	}()

	if *certFile != "" && *keyFile != "" {
		klog.Infoln("listen https: ")
		klog.Fatal(http.ListenAndServeTLS(*address, *certFile, *keyFile, nil))
	}
	klog.Fatal(http.ListenAndServe(*address, nil))
}

func SyncCiliumNode(cn *v2.CiliumNode) error  {
	return DoCiliumNodeIpAlloc(ciliumClientset, cn)
}

func DoCiliumNodeIpAlloc(ciliumClientset *versioned.Clientset, cn *v2.CiliumNode) error {
	l := mlog.WithField("ciliumNode_key", fmt.Sprintf("%s/%s", cn.Namespace, cn.Name))

	updateFlag := false
	if len(cn.Spec.IPAM.Pool) - len(cn.Status.IPAM.Used) <= 5 {
		updateFlag = DoAllocate(l, cn)
	}

	updateFlag = DoOwnerIpRecycle(l, cn) || updateFlag

	if !updateFlag {
		return nil
	}

	_, err := ciliumClientset.CiliumV2().CiliumNodes().Update(context.TODO(), cn, v1.UpdateOptions{})
	if err != nil {
		l.Errorf("update cilium node failed %v. ", err)
		return err
	}
	return nil
}

func DoAllocate(l *logrus.Entry, cn *v2.CiliumNode) bool {
	max := ""
	for k, _ := range cn.Spec.IPAM.Pool {
		if max < k {
			max = k
		}
	}

	l = l.WithField("cidr", cn.Spec.IPAM.PodCIDRs).WithField("old_pool_len", len(cn.Spec.IPAM.Pool))
	ip, ipnet, err := net.ParseCIDR(cn.Spec.IPAM.PodCIDRs[0])
	if err != nil {
		l.Errorf("parse pod cidr failed %v. ", err)
		return false
	}

	var maxIp net.IP
	start := true
	if max != "" {
		start = false
		maxIp = net.ParseIP(max)
		l.Infof("get max ip %s. ", maxIp.String())
	}
	addStep := 0
	for ip := ip.Mask(ipnet.Mask); ipnet.Contains(ip); inc(ip) {
		if !start && maxIp != nil && maxIp.Equal(ip) {
			start = true
			continue
		}
		if !start {
			continue
		}
		if addStep >= 10 {
			break
		}
		cn.Spec.IPAM.Pool[ip.String()] = types.AllocationIP{}

		addStep++
	}
	l.Infof("will expand ip to %d. ", len(cn.Spec.IPAM.Pool))
	return true
}

func inc(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
}

func DoOwnerIpRecycle(l *logrus.Entry, cn *v2.CiliumNode) (needUpdate bool) {
	for k, v := range cn.Spec.IPAM.Pool {
		if v.Owner == "" {
			continue
		}
		l = l.WithField("ip", k).WithField("owner", v.Owner).WithField("resource", v.Resource)
		rs := strings.Split(v.Resource, "/")
		if len(rs) != 3 {
			l.Warningf("skip invalid resource ip recycle check. ")
			continue
		}
		needRecycle := false
		switch rs[1] {
		case "StatefulSet":
			_, err := stsLister.StatefulSets(rs[0]).Get(rs[2])
			if errors.IsNotFound(err) {
				needRecycle = true
			} else if err != nil {
				l.Errorf("get sts info failed %v. ", err)
				continue
			}
		default:
			l.Warningf("skip unsupported resource. ")
		}
		if !needRecycle {
			continue
		}
		l.Infof("owner is not exists, do recycle. ")
		cn.Spec.IPAM.Pool[k] = types.AllocationIP{}
		needUpdate = true
	}
	return
}
