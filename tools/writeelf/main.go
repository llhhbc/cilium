package main

import (
	"context"
	"log"
	"path"

	"github.com/cilium/cilium/pkg/datapath"
	"github.com/cilium/cilium/pkg/datapath/loader"
	"github.com/cilium/cilium/pkg/elf"
	"github.com/cilium/cilium/pkg/endpoint"
	"github.com/cilium/cilium/pkg/fqdn/restore"
	"github.com/cilium/cilium/pkg/lock"
	"github.com/cilium/cilium/pkg/maps/ctmap"
	monitorAPI "github.com/cilium/cilium/pkg/monitor/api"
	"github.com/cilium/cilium/pkg/option"
	"github.com/cilium/cilium/pkg/policy"
	"github.com/spf13/pflag"
)

type MyTest struct {
	repo *policy.Repository
}

func (m *MyTest) Init() {
	m.repo = policy.NewPolicyRepository(nil, nil, nil)
	ctmap.InitMapInfo(option.CTMapEntriesGlobalTCPDefault, option.CTMapEntriesGlobalAnyDefault, true, true, true)
}

func (m MyTest) GetNamedPorts() (npm policy.NamedPortMultiMap) {
	//TODO implement me
	panic("implement me")
}

func (m MyTest) GetPolicyRepository() *policy.Repository {
	//TODO implement me
	return m.repo
}

func (m MyTest) QueueEndpointBuild(ctx context.Context, epID uint64) (func(), error) {
	//TODO implement me
	panic("implement me")
}

func (m MyTest) GetCompilationLock() *lock.RWMutex {
	//TODO implement me
	panic("implement me")
}

func (m MyTest) GetCIDRPrefixLengths() (s6, s4 []int) {
	return nil, nil
}

func (m MyTest) SendNotification(msg monitorAPI.AgentNotifyMessage) error {
	//TODO implement me
	panic("implement me")
}

func (m MyTest) Datapath() datapath.Datapath {
	//TODO implement me
	panic("implement me")
}

func (m MyTest) GetDNSRules(epID uint16) restore.DNSRules {
	//TODO implement me
	panic("implement me")
}

func (m MyTest) RemoveRestoredDNSRules(epID uint16) {
	//TODO implement me
	panic("implement me")
}

var (
	baseDir = pflag.String("baseDir", ".", "")
	epDir   = pflag.String("epDir", "", "")
)

func main() {
	pflag.Parse()
	ctx := context.Background()

	t := &MyTest{}

	t.Init()

	res := endpoint.ReadEPsFromDirNames(ctx, t, t, t, *baseDir, []string{
		*epDir,
	})

	template, err := elf.Open(path.Join(*baseDir, *epDir, "template.o"))
	if err != nil {
		log.Fatalf("load template.o failed. ")
	}
	defer template.Close()

	for k, ep := range res {
		log.Printf("do key %d. ", k)
		epc := ep.CreateEpInfoCache(*epDir)
		dstPath := path.Join(epc.StateDir(), "bpf_lxc.o")
		opts, strings := loader.ELFSubstitutions(epc)
		if err = template.Write(dstPath, opts, strings); err != nil {
			log.Fatalf("write elf failed %v. ", err)
		}
	}

}
