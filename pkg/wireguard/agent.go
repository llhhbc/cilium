package wireguard

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"os"

	"github.com/cilium/cilium/pkg/node"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// TODO items:
// - 2. read local peers and avoid overriding them
// - 3. implement DeletePeer method
// - 4. patch routes

const (
	wgIfaceName      = "wg0"                          // TODO make config param
	PubKeyAnnotation = "io.cilium.network.wg-pub-key" // TODO use consts from other pkg
)

type Agent struct {
	wgClient *wgctrl.Client
	privKey  wgtypes.Key

	wireguardV4CIDR *net.IPNet
	wireguardIPv4   net.IP

	isInit bool

	listenPort int
}

func NewAgent(privKey string, wgV4Net *net.IPNet) (*Agent, error) {
	key, err := loadOrGeneratePrivKey(privKey)
	if err != nil {
		return nil, err
	}

	node.SetWireguardPubKey(key.PublicKey().String())

	// TODO

	wgClient, err := wgctrl.New()
	if err != nil {
		return nil, err
	}

	return &Agent{
		wgClient: wgClient,
		privKey:  key,

		wireguardIPv4:   nil, // set by node manager
		wireguardV4CIDR: wgV4Net,

		listenPort: 51871, // TODO make configurable
	}, nil
}

// TODO call this
func (a *Agent) Close() error {
	return a.wgClient.Close()
}

func (a *Agent) UpdatePeer(wgIPv4 net.IP, pubKey string) error {
	// Check self first due to annoying optimizations by @aanm

	if node.GetWireguardIPv4() == nil {
		// TODO maybe queue updates
		return nil
	}

	if !a.isInit && node.GetWireguardIPv4() != nil {
		a.wireguardIPv4 = node.GetWireguardIPv4()

		link := &netlink.Wireguard{LinkAttrs: netlink.LinkAttrs{Name: wgIfaceName}}
		err := netlink.LinkAdd(link)
		if err != nil && !errors.Is(err, unix.EEXIST) {
			return err
		}

		ip := &net.IPNet{
			IP:   a.wireguardIPv4,
			Mask: a.wireguardV4CIDR.Mask,
		}

		err = netlink.AddrAdd(link, &netlink.Addr{IPNet: ip})
		if err != nil && !errors.Is(err, unix.EEXIST) {
			return err
		}

		cfg := &wgtypes.Config{
			PrivateKey:   &a.privKey,
			ListenPort:   &a.listenPort,
			ReplacePeers: false,
		}
		if err := a.wgClient.ConfigureDevice(wgIfaceName, *cfg); err != nil {
			return err
		}

		if err := netlink.LinkSetUp(link); err != nil {
			return err
		}

		a.isInit = true
	}

	// TODO continue from here with remote peer update <---

	return nil
}

func loadOrGeneratePrivKey(filePath string) (key wgtypes.Key, err error) {
	bytes, err := ioutil.ReadFile(filePath)
	if os.IsNotExist(err) {
		key, err = wgtypes.GeneratePrivateKey()
		if err != nil {
			return wgtypes.Key{}, fmt.Errorf("failed to generate wg private key: %w", err)
		}

		err = ioutil.WriteFile(filePath, key[:], os.ModePerm) // TODO fix do not use 777 for priv key
		if err != nil {
			return wgtypes.Key{}, fmt.Errorf("failed to save wg private key: %w", err)
		}

		return key, nil
	} else if err != nil {
		return wgtypes.Key{}, fmt.Errorf("failed to load wg private key: %w", err)
	}

	return wgtypes.NewKey(bytes)
}
