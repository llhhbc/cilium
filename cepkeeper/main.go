package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	cilium_v2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	v13 "k8s.io/client-go/listers/apps/v1"
	v14 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"
	klog2 "k8s.io/klog/v2"

	"github.com/cilium/cilium/pkg/k8s/client/clientset/versioned"
	"github.com/cilium/cilium/pkg/k8s/client/informers/externalversions"
	v22 "github.com/cilium/cilium/pkg/k8s/client/listers/cilium.io/v2"
	"github.com/cilium/cilium/pkg/logging"
	"github.com/cilium/cilium/pkg/logging/logfields"
)

var mlog = logging.DefaultLogger.WithField(logfields.LogSubsys, "cep-keeper")

const (
	cepFinalizeKey   = "qfrds/cepkeep"
	cepAnnotationKey = cepFinalizeKey
)

var (
	ciliumClientset *versioned.Clientset

	slel labels.Selector

	stsLister v13.StatefulSetLister
	podLister v14.PodLister
	cepLister v22.CiliumEndpointLister
)

var (
	labelSector = pflag.String("labelSector", `{"matchExpressions":[{"key":"AppName","operator":"Exists"}]}`, "LabelSelectorRequirement json")
)

func main() {
	kf := flag.NewFlagSet("klog", flag.PanicOnError)
	klog.InitFlags(kf)

	klogFlags2 := flag.NewFlagSet("klog2", flag.ExitOnError)
	klog2.InitFlags(klogFlags2)

	pflag.CommandLine.AddGoFlagSet(kf)
	pflag.Parse()

	// Sync the glog and klog flags.
	kf.VisitAll(func(f1 *flag.Flag) {
		f2 := klogFlags2.Lookup(f1.Name)
		if f2 != nil {
			value := f1.Value.String()
			f2.Value.Set(value)
		}
	})

	sel := v1.LabelSelector{}
	err := json.Unmarshal([]byte(*labelSector), &sel)
	if err != nil {
		klog.Fatalf("parse label sector failed %v. ", err)
	}
	slel, err = v1.LabelSelectorAsSelector(&sel)
	if err != nil {
		klog.Fatalf("convert to selector failed %v. ", err)
	}

	kubeConfig, err := clientcmd.BuildConfigFromFlags("", os.Getenv("KUBECONFIG"))
	if err != nil {
		klog.Fatalf("Failed to create config: %v", err)
	}

	clientset := kubernetes.NewForConfigOrDie(kubeConfig)
	ciliumClientset = versioned.NewForConfigOrDie(kubeConfig)

	sharedInformer := informers.NewSharedInformerFactory(clientset, time.Minute)
	ciliumSharedInformer := externalversions.NewSharedInformerFactoryWithOptions(ciliumClientset, time.Minute)

	stsLister = sharedInformer.Apps().V1().StatefulSets().Lister()
	podLister = sharedInformer.Core().V1().Pods().Lister()
	cepLister = ciliumSharedInformer.Cilium().V2().CiliumEndpoints().Lister()

	ciliumSharedInformer.Cilium().V2().CiliumEndpoints().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: watchCep,
		UpdateFunc: func(oldObj, newObj interface{}) {
			watchCep(newObj)
		},
	})

	ch := context.TODO().Done()
	sharedInformer.Start(ch)
	sharedInformer.WaitForCacheSync(ch)
	ciliumSharedInformer.Start(ch)
	ciliumSharedInformer.WaitForCacheSync(ch)

	for {
		err = cleanUnusedCep(slel)
		if err != nil {
			mlog.Errorf("keep cep failed %v. ", err)
		}
		time.Sleep(time.Minute * 5)
	}
}

func cleanUnusedCep(slel labels.Selector) error {
	cepList, err := cepLister.List(slel)
	if err != nil {
		return fmt.Errorf("list cep failed %v. ", err)
	}
	for _, cep := range cepList {
		if cep.DeletionTimestamp == nil {
			continue
		}
		if cep.Annotations == nil || cep.Annotations[cepAnnotationKey] == "" {
			mlog.Warningf("get invalid cep without annotation. %s/%s. ", cep.Name, cep.Namespace)
			continue
		}
		stsNames := strings.Split(cep.Annotations[cepAnnotationKey], "/")
		if len(stsNames) != 2 {
			mlog.Warningf("get invalid cep annotation. %s/%s. ", cep.Name, cep.Namespace)
			continue
		}
		sts, err := stsLister.StatefulSets(cep.Namespace).Get(stsNames[1])
		if errors.IsNotFound(err) || (sts.Spec.Replicas != nil && *sts.Spec.Replicas == 0) {
			l := mlog.WithField("sts", stsNames[1]).
				WithField("cep", cep.Name).WithField("ns", cep.Namespace)
			l.Infof("sts is not found, clean cep. ")
			newCep := cep.DeepCopy()
			newCep.Finalizers = RemoveProtector(newCep.Finalizers)
			_, err = ciliumClientset.CiliumV2().CiliumEndpoints(cep.Namespace).
				Update(GetCtx(), newCep, v1.UpdateOptions{})
			if err != nil {
				l.Infof("update cep failed %v. ", err)
				continue
			}
			l.Infof("clean cep ok. ")
		} else if err != nil {
			mlog.Warningf("get sts %s failed %v. ", stsNames[1], err)
			continue
		}
	}
	return nil
}

func watchCep(object interface{}) {
	cep, ok := object.(*cilium_v2.CiliumEndpoint)
	if !ok {
		return
	}

	if !slel.Matches(labels.Set(cep.Labels)) {
		return
	}

	if HasSet(cep.Finalizers) {
		return
	}

	if cep.OwnerReferences == nil || len(cep.OwnerReferences) == 0 {
		return
	}
	l := mlog.WithField("cep", cep.Name).WithField("ns", cep.Namespace)
	po, err := podLister.Pods(cep.Namespace).Get(cep.OwnerReferences[0].Name)
	if err != nil {
		l.Warningf("skip get po failed %v. skip ", err)
		return
	}
	if po.OwnerReferences == nil {
		l.Warningf("skip pod without owner. ")
		return
	}
	if po.OwnerReferences[0].Kind != "StatefulSet" {
		return
	}
	l = l.WithField("sts", po.OwnerReferences[0].Name)
	sts, err := stsLister.StatefulSets(cep.Namespace).Get(po.OwnerReferences[0].Name)
	if err != nil {
		l.Warningf("skip get sts failed %v. ", err)
		return
	}

	// patch cep {"metadata":{"annotations":{"qfrds/cepkeep":"sts/aa"},"finalizers":["qfrds/cepkeep"]}}
	patchMsg := fmt.Sprintf(`{"metadata":{"annotations":{"%s":"%s"}, "finalizers":["%s"]}}`,
		cepAnnotationKey, fmt.Sprintf("StatefulSet/%s", sts.Name),
		strings.Join(append(cep.Finalizers, cepFinalizeKey), `","`))
	_, err = ciliumClientset.CiliumV2().CiliumEndpoints(cep.Namespace).Patch(GetCtx(),
		cep.Name, types.MergePatchType,
		[]byte(patchMsg), v1.PatchOptions{})
	if err != nil {
		l.Errorf("patch cep %s failed %v. ", patchMsg, err)
		return
	}
	l.Infof("patch cep ok. ")
}

func GetCtx() context.Context {
	ctx, _ := context.WithTimeout(context.Background(), time.Second*30)
	return ctx
}

func HasSet(src []string) bool {
	for _, s := range src {
		if s == cepFinalizeKey {
			return true
		}
	}
	return false
}

func RemoveProtector(src []string) []string {
	var res []string
	for _, s := range src {
		if s == cepFinalizeKey {
			continue
		}
		res = append(res, s)
	}
	return res
}
