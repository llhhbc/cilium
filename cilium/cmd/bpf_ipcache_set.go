// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of Cilium

package cmd

import (
	"fmt"
	"net"
	"os"
	"reflect"

	"github.com/cilium/cilium/pkg/bpf"
	"github.com/cilium/cilium/pkg/common"
	"github.com/cilium/cilium/pkg/maps/ipcache"
	"github.com/spf13/cobra"
)

var bpfIPCacheSetCmd = &cobra.Command{
	Use:   "set",
	Short: "Retrieve identity for an ip",
	Run: func(cmd *cobra.Command, args []string) {
		common.RequireRootPrivilege("cilium bpf ipcache set")

		if len(args) < 2 {
			Usagef(cmd, "No ip provided. cilium bpf ipcache set <key_ip> <dst_ip> ")
		}

		keyIp := args[0]
		dstIp := args[1]

		ip := net.ParseIP(keyIp).To4()
		if ip == nil {
			Usagef(cmd, "Invalid ip address. "+usage)
		}
		ipDest := net.ParseIP(dstIp).To4()
		if ipDest == nil {
			Usagef(cmd, "Invalid dest ip address. "+usage)
		}

		var foundKey *ipcache.Key
		var foundValue *ipcache.RemoteEndpointInfo
		hasFound := false

		err := ipcache.IPCache.DumpWithCallback(func(key bpf.MapKey, value bpf.MapValue) {
			if hasFound {
				return
			}
			var ok bool
			lkey, ok := key.(*ipcache.Key)
			if !ok {
				fmt.Printf("get invalid key %#v. ", key)
				os.Exit(1)
			}
			lvalue, ok := value.(*ipcache.RemoteEndpointInfo)
			if !ok {
				fmt.Printf("get invalid value %#v. ", value)
				os.Exit(1)
			}
			//fmt.Printf("get lkey %s, ip %s. \n", hex.EncodeToString(lkey.IP[:net.IPv4len]), hex.EncodeToString(ip[:net.IPv4len]))
			if reflect.DeepEqual([]byte(lkey.IP[:net.IPv4len]), []byte(ip[:net.IPv4len])) {
				hasFound = true
				foundKey = lkey.DeepCopy()
				foundValue = lvalue.DeepCopy()
				fmt.Printf("has found %s,%s. \n", foundKey.String(), foundValue.String())
				return
			}
		})
		if err != nil {
			fmt.Printf("dump failed %v. ", err)
			os.Exit(1)
		}

		if !hasFound || foundKey == nil || foundValue == nil {
			fmt.Fprintf(os.Stderr, "No entries found.\n")
			os.Exit(1)
		}

		newCp := foundValue.DeepCopy()
		copy(newCp.TunnelEndpoint[:], ipDest)

		fmt.Printf("do udpate [%s,%s] %s/%s. ", ip.String(), ipDest.String(), foundKey.String(), newCp.String())

		err = ipcache.IPCache.Update(foundKey, newCp)
		if err != nil {
			fmt.Printf("update failed %v. ", err)
		}
		fmt.Printf("update ok. ")
	},
}

func init() {
	bpfIPCacheCmd.AddCommand(bpfIPCacheSetCmd)
}
