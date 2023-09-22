package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path"
	"strings"
	"time"

	"github.com/cilium/cilium/api/v1/models"
	"github.com/spf13/pflag"
	v1 "k8s.io/api/core/v1"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
	klog2 "k8s.io/klog/v2"
)

var (
	config    *rest.Config
	clientset *kubernetes.Clientset
	timeout   = pflag.Duration("timeout", 20*time.Second, "request timeout. ")
	outDir    = pflag.String("outDir", "", "out dir. ")
)

func InitK8sClient() error {
	if clientset != nil {
		return nil
	}
	var err error
	config, err = clientcmd.BuildConfigFromFlags("", os.Getenv("KUBECONFIG"))
	if err != nil {
		return fmt.Errorf("init client config failed %v. ", err)
	}
	clientset, err = kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("get client failed %v. ", err)
	}
	return nil
}

func GetCtx() context.Context {
	ctx, _ := context.WithTimeout(context.Background(), *timeout)
	return ctx
}

var epList *v1.EndpointsList
var svcList *v1.ServiceList
var ciliumPodList *v1.PodList

func main() {
	//gf := flag.NewFlagSet("klog", flag.PanicOnError)
	//klog.InitFlags(gf)
	gf2 := flag.NewFlagSet("klog2", flag.PanicOnError)
	klog2.InitFlags(gf2)
	pflag.CommandLine.AddGoFlagSet(gf2)
	pflag.Parse()

	if *outDir == "" {
		*outDir = fmt.Sprintf("./out_%s", time.Now().Format("2006-01-0215:04:05"))
		err := os.MkdirAll(*outDir, 0755)
		if err != nil {
			log.Fatalf("create dir %s failed %v. ", *outDir, err)
		}
	}

	err := InitK8sClient()
	if err != nil {
		log.Fatalf("init k8s client failed. %v. ", err)
	}

	epList, err = clientset.CoreV1().Endpoints("").List(GetCtx(), v12.ListOptions{})
	if err != nil {
		log.Fatalf("get ep list failed %v. ", err)
	}
	svcList, err = clientset.CoreV1().Services("").List(GetCtx(), v12.ListOptions{})
	if err != nil {
		log.Fatalf("get svc list failed %v. ", err)
	}

	ciliumPodList, err = clientset.CoreV1().Pods("kube-system").List(GetCtx(), v12.ListOptions{
		LabelSelector: labels.FormatLabels(map[string]string{
			"k8s-app": "cilium",
		}),
	})
	if err != nil {
		log.Fatalf("get cilium pod list failed %v. ", err)
	}

	for _, ciliumPod := range ciliumPodList.Items {
		log.Printf("check pod %s. \n", ciliumPod.Name)
		ciliumSvcInfo, err := getCiliumServiceList(ciliumPod.Name, ciliumPod.Namespace)
		if err != nil {
			log.Printf("get cilium info failed %v. ", err)
			continue
		}
		res, ok := checkCiliumSvc(ciliumSvcInfo)
		if ok {
			log.Printf("check ok \n")
		} else {
			log.Printf("check failed, write to out file. ")
			err = os.WriteFile(path.Join(*outDir, fmt.Sprintf("%s.json", ciliumPod.Name)),
				[]byte(res), 0755)
			if err != nil {
				log.Fatalf("write file %s failed %v. ", ciliumPod.Name, err)
			}
		}
	}
}

func checkCiliumSvc(ciliumJson string) (string, bool) {
	ciliumService := make([]*models.Service, 0)
	err := json.Unmarshal([]byte(ciliumJson), &ciliumService)
	if err != nil {
		log.Fatalln("load cilium json failed ", err)
	}

	// get svc map   由于同svc对应的端口规则都相同，所以比较时，忽略端口
	svcEpInfo := make(map[string][]string, 0) // key: ip, value ip list
	svcInfo := make(map[string]string, 0)     // key: svcName.namespace, value: ip
	for _, svc := range svcList.Items {
		if svc.Spec.ClusterIP == "None" {
			continue
		}
		svcInfo[fmt.Sprintf("%s.%s", svc.Name, svc.Namespace)] = svc.Spec.ClusterIP
	}
	for _, ep := range epList.Items {
		name := fmt.Sprintf("%s.%s", ep.Name, ep.Namespace)
		if svcInfo[name] == "" {
			continue
		}
		if len(ep.Subsets) == 0 {
			continue
		}
		ipKey := svcInfo[name]
		for _, ss := range ep.Subsets[0].Addresses {
			svcEpInfo[ipKey] = append(svcEpInfo[ipKey], ss.IP)
		}
	}

	// do check
	okFlag := true
	var diffInfo strings.Builder
	for _, item := range ciliumService {
		if item.Spec.FrontendAddress == nil {
			log.Printf("skip item no frontend %d. ", item.Spec.ID)
		}
		if item.Spec.Flags.Type == "NodePort" || item.Spec.Flags.Type == "LoadBalancer" {
			continue
		}
		err = CheckBackendDiff(item.Spec.BackendAddresses, svcEpInfo[item.Spec.FrontendAddress.IP])
		if err != nil {
			m, _ := json.Marshal(item.Spec)
			diffInfo.WriteString(fmt.Sprintf("%d backend diff: cilium: %v, ep: %v, %v. ", item.Spec.ID,
				string(m), svcEpInfo[item.Spec.FrontendAddress.IP], err))
			okFlag = false
		}
	}
	return diffInfo.String(), okFlag
}

func CheckBackendDiff(ciliumBk []*models.BackendAddress, epIp []string) error {
	if len(ciliumBk) != len(epIp) {
		return fmt.Errorf("count diff. ")
	}
	for _, c := range ciliumBk {
		if c.IP == nil {
			return fmt.Errorf("bk ip empty. ")
		}
		hasFound := false
		for _, ip := range epIp {
			if *c.IP == ip {
				hasFound = true
				break
			}
		}
		if !hasFound {
			return fmt.Errorf("%s not found. ", *c.IP)
		}
	}
	return nil
}

func getCiliumServiceList(name, namespace string) (string, error) {
	req := clientset.CoreV1().RESTClient().Post().Resource("pods").
		Name(name).Namespace(namespace).
		SubResource("exec")
	cmd := strings.Split("cilium service list -o json", " ")
	option := &v1.PodExecOptions{Command: cmd, Stdin: true, Stdout: true, Stderr: true, TTY: false, Container: "cilium-agent"}
	req = req.VersionedParams(
		option,
		scheme.ParameterCodec,
	)
	//klog.V(3).Infof("run cmd with %s. ", req.URL().String())
	exec, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		return "", fmt.Errorf("error while creating executor: %v", err)
	}
	var res, resErr bytes.Buffer
	err = exec.Stream(remotecommand.StreamOptions{
		Stdin:             os.Stdin,
		Stdout:            &res,
		Stderr:            &resErr,
		Tty:               false,
		TerminalSizeQueue: nil,
	})
	if err != nil {
		return "", fmt.Errorf("exec failed %v %s,%s. ", err, res.String(), resErr.String())
	}
	if resErr.String() != "" {
		log.Printf("%s/%s run with err msg: %s. ", name, namespace, resErr.String())
	}
	return res.String(), nil
}
