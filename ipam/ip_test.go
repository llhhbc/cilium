package main

import (
	"net"
	"testing"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

func TestIp(t *testing.T)  {
	a := net.ParseIP("10.244.1.0")
	b := net.ParseIP("10.244.1.9")

	ip, cidr, _ := net.ParseCIDR("10.244.1.0/24")
	ip = ip.Mask(cidr.Mask)

	t.Log(string(a), string(b), a.Equal(b), ip.Equal(a), ip.Equal(b))
}

var info =`
matchExpressions:
- key: cilium-ipam-injector
  operator: NotIn
  values:
  - disabled
- key: DBType
  operator: In
  values:
  - postgres
`

var denyAll=`
matchLabels:
  nostatic: true
`

func TestYaml(t *testing.T)  {
	m, _ := yaml.YAMLToJSON([]byte(denyAll))
	t.Log(string(m))

	sel := v1.LabelSelector{}
	err := yaml.Unmarshal([]byte(denyAll), &sel)
	if err != nil {
		t.Fatalf("parse label sector failed %v. ", err)
	}
	t.Log("ok", sel.String())
}
