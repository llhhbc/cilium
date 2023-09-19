package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/cilium/cilium/api/v1/models"
	"github.com/spf13/pflag"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"
)

var (
	endpointListYaml = pflag.String("epFile", "/Users/lilh/Downloads/ytw_cilium_ana/k8s-endpoints-20230912-100345.yaml", "ep yaml file ")
	serviceListYaml  = pflag.String("svcFile", "/Users/lilh/Downloads/ytw_cilium_ana/k8s-services-20230912-100345.yaml", "svc yaml file ")
	ciliumJson       = pflag.String("ciliumJson", "/Users/lilh/Downloads/ytw_cilium_ana/cilium-bugtool-cilium-p5d9l-20230912-100345/cmd/cilium-service-list--o-json.md", "cilium json file. ")
)

func main() {
	pflag.Parse()

	epList := v1.EndpointsList{}
	svcList := v1.ServiceList{}

	m, err := os.ReadFile(*endpointListYaml)
	if err != nil {
		log.Fatalln("read ep file failed. ", err)
	}
	err = yaml.Unmarshal(m, &epList)
	if err != nil {
		log.Fatalln("load ep file failed. ", err)
	}

	m, err = os.ReadFile(*serviceListYaml)
	if err != nil {
		log.Fatalln("read svc file failed. ", err)
	}
	err = yaml.Unmarshal(m, &svcList)
	if err != nil {
		log.Fatalln("load svc file failed. ", err)
	}

	ciliumService := make([]*models.Service, 0)
	m, err = os.ReadFile(*ciliumJson)
	if err != nil {
		log.Fatalln("read cilium json failed ", err)
	}
	err = json.Unmarshal(m, &ciliumService)
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
			log.Printf("%d backend diff: cilium: %v, ep: %v, %v. ", item.Spec.ID,
				string(m), svcEpInfo[item.Spec.FrontendAddress.IP], err)
		}
	}
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
