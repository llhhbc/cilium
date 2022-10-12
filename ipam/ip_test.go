package main

import (
	"net"
	"testing"
)

func TestIp(t *testing.T)  {
	a := net.ParseIP("10.244.1.0")
	b := net.ParseIP("10.244.1.9")

	ip, cidr, _ := net.ParseCIDR("10.244.1.0/24")
	ip = ip.Mask(cidr.Mask)

	t.Log(string(a), string(b), a.Equal(b), ip.Equal(a), ip.Equal(b))
}