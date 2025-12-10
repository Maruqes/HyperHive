package wireguard

import (
	"512SvMan/db"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"

	"github.com/Maruqes/512SvMan/logger"
	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

/*
gpt explanation :D

WireGuard – how keys work (server vs clients)

- Keys are not "per connection", they are "per identity" (interface/device).
- Each WireGuard interface on the SERVER (e.g., wg0) has ONE key pair:
	- ServerPrivateKey  -> stays only on the server
	- ServerPublicKey   -> is distributed to clients
  This key is stable: it doesn't change each time someone connects.

- Each CLIENT also has its own key pair:
	- ClientPrivateKey  -> stays only on that client
	- ClientPublicKey   -> is added to the server config as a Peer

- In the SERVER file:
	- We use ONLY the ServerPrivateKey in the [Interface] section.
	- For each client we add a [Peer] with:
		- PublicKey = ClientPublicKey
		- AllowedIPs = internal IP of that client (e.g., 10.10.0.2/32)

- In the CLIENT file:
	- We use the ClientPrivateKey in the [Interface] section.
	- In the [Peer] section, we set:
		- PublicKey = ServerPublicKey
		- Endpoint  = server IP:port
		- AllowedIPs = traffic we want to send through the tunnel (e.g., 0.0.0.0/0)

Summary:
- The server always reuses the SAME private key from the WireGuard interface.
- Each client has its own private key.
- The server knows the public key of each client.
- Each client knows the public key of the server.
*/

const iface = "wg0-hh512"
const ServerCIDR = "10.128.0.1/24" // server address
const listenPort = 51512

func ListenPort() int {
	return listenPort
}

func ServerCIDRValue() string {
	return ServerCIDR
}

func runCommand(desc string, args ...string) error {
	if len(args) == 0 {
		return fmt.Errorf("%s: no command provided", desc)
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin = os.Stdin
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = io.MultiWriter(os.Stdout, &stdoutBuf)
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)
	if err := cmd.Run(); err != nil {
		stdoutStr := strings.TrimSpace(stdoutBuf.String())
		stderrStr := strings.TrimSpace(stderrBuf.String())
		logger.Error(desc + " failed: " + err.Error())
		if stderrStr != "" {
			logger.Error(desc + " stderr: " + stderrStr)
		}
		if stdoutStr != "" {
			logger.Error(desc + " stdout: " + stdoutStr)
		}

		var details []string
		if stderrStr != "" {
			details = append(details, "stderr: "+stderrStr)
		}
		if stdoutStr != "" {
			details = append(details, "stdout: "+stdoutStr)
		}
		if len(details) > 0 {
			return fmt.Errorf("%s: %s: %w", desc, strings.Join(details, "; "), err)
		}
		return fmt.Errorf("%s: %w", desc, err)
	}
	logger.Info(desc + " succeeded")
	return nil
}

func saveServerKeys(privateKey, publicKey wgtypes.Key) error {
	keysDir := "wireguard/keys"
	if err := os.MkdirAll(keysDir, 0700); err != nil {
		return fmt.Errorf("create keys directory: %w", err)
	}

	privKeyPath := keysDir + "/server_private.key"
	if err := os.WriteFile(privKeyPath, []byte(privateKey.String()), 0600); err != nil {
		return fmt.Errorf("write private key: %w", err)
	}

	pubKeyPath := keysDir + "/server_public.key"
	if err := os.WriteFile(pubKeyPath, []byte(publicKey.String()), 0644); err != nil {
		return fmt.Errorf("write public key: %w", err)
	}

	logger.Info("Server keys saved to " + keysDir)
	return nil
}

func getServerKeys() (privateKey, publicKey wgtypes.Key, err error) {
	keysDir := "wireguard/keys"
	privKeyPath := keysDir + "/server_private.key"
	pubKeyPath := keysDir + "/server_public.key"

	privKeyData, err := os.ReadFile(privKeyPath)
	if err != nil {
		return wgtypes.Key{}, wgtypes.Key{}, fmt.Errorf("read private key: %w", err)
	}

	privateKey, err = wgtypes.ParseKey(strings.TrimSpace(string(privKeyData)))
	if err != nil {
		return wgtypes.Key{}, wgtypes.Key{}, fmt.Errorf("parse private key: %w", err)
	}

	pubKeyData, err := os.ReadFile(pubKeyPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			publicKey = privateKey.PublicKey()
			if err := os.WriteFile(pubKeyPath, []byte(publicKey.String()), 0644); err != nil {
				return wgtypes.Key{}, wgtypes.Key{}, fmt.Errorf("write public key: %w", err)
			}
			return privateKey, publicKey, nil
		}
		return wgtypes.Key{}, wgtypes.Key{}, fmt.Errorf("read public key: %w", err)
	}

	publicKey, err = wgtypes.ParseKey(strings.TrimSpace(string(pubKeyData)))
	if err != nil {
		return wgtypes.Key{}, wgtypes.Key{}, fmt.Errorf("parse public key: %w", err)
	}

	return privateKey, publicKey, nil
}

func ensureServerKeys() (wgtypes.Key, wgtypes.Key, error) {
	privateKey, publicKey, err := getServerKeys()
	if err == nil {
		return privateKey, publicKey, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return wgtypes.Key{}, wgtypes.Key{}, err
	}

	privateKey, err = wgtypes.GeneratePrivateKey()
	if err != nil {
		return wgtypes.Key{}, wgtypes.Key{}, fmt.Errorf("generate server private key: %w", err)
	}
	publicKey = privateKey.PublicKey()
	if err := saveServerKeys(privateKey, publicKey); err != nil {
		return wgtypes.Key{}, wgtypes.Key{}, err
	}
	return privateKey, publicKey, nil
}

func createInterface() error {
	err := runCommand("ip link wireguard", "ip", "link", "add", "dev", iface, "type", "wireguard")
	if err != nil {
		return err
	}
	err = runCommand("ip adress wireguard", "ip", "address", "add", ServerCIDR, "dev", iface)
	if err != nil {
		return err
	}
	err = runCommand("ip set dev wireguard", "ip", "link", "set", "up", "dev", iface)
	if err != nil {
		return err
	}
	return nil
}

func doesInterfaceExists() (bool, error) {
	cmd := exec.Command("ip", "link", "show", iface)
	err := cmd.Run()
	if err != nil {
		// If the command fails, the interface doesn't exist
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() != 0 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func buildClientConfig(clientPriv wgtypes.Key, clientIPCIDR, endpoint string, keepaliveSec int) (string, error) {
	if keepaliveSec <= 0 {
		keepaliveSec = 25
	}
	_, serverPublic, err := getServerKeys()
	if err != nil {
		return "", fmt.Errorf("load server keys: %w", err)
	}
	cfg := fmt.Sprintf(`[Interface]
PrivateKey = %s
Address = %s
DNS = 1.1.1.1

[Peer]
PublicKey = %s
Endpoint = %s
AllowedIPs = 0.0.0.0/0, ::/0
PersistentKeepalive = %d
`,
		clientPriv.String(),
		clientIPCIDR,
		serverPublic.String(),
		endpoint,
		keepaliveSec,
	)

	return cfg, nil
}

func GeneratePeerAndGenerateConfig(clientIPCIDR, endpoint string, keepaliveSec int) (string, string, error) {
	clientPriv, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return "", "", fmt.Errorf("generate client private key: %w", err)
	}
	cfg, err := buildClientConfig(clientPriv, clientIPCIDR, endpoint, keepaliveSec)
	if err != nil {
		return "", "", err
	}

	clientPub := clientPriv.PublicKey()
	if err := addPeerToDevice(clientPub, clientIPCIDR); err != nil {
		return "", "", err
	}

	return cfg, clientPub.String(), nil
}

func addPeerToDevice(clientPublic wgtypes.Key, clientIPCIDR string) error {
	_, network, err := net.ParseCIDR(clientIPCIDR)
	if err != nil {
		return fmt.Errorf("parse client cidr %q: %w", clientIPCIDR, err)
	}

	client, err := wgctrl.New()
	if err != nil {
		return fmt.Errorf("wgctrl.New: %w", err)
	}
	defer client.Close()

	peerCfg := wgtypes.PeerConfig{
		PublicKey:         clientPublic,
		ReplaceAllowedIPs: true,
		AllowedIPs:        []net.IPNet{*network},
	}

	if err := client.ConfigureDevice(iface, wgtypes.Config{
		Peers: []wgtypes.PeerConfig{peerCfg},
	}); err != nil {
		return fmt.Errorf("ConfigureDevice(%s): %w", iface, err)
	}
	return nil
}

func RemovePeerByIP(clientIPCIDR string) error {
	clientIPCIDR = strings.TrimSpace(clientIPCIDR)
	if clientIPCIDR == "" {
		return fmt.Errorf("client ip cidr is required")
	}

	client, err := wgctrl.New()
	if err != nil {
		return fmt.Errorf("wgctrl.New: %w", err)
	}
	defer client.Close()

	dev, err := client.Device(iface)
	if err != nil {
		return fmt.Errorf("Device(%s): %w", iface, err)
	}

	var (
		targetKey wgtypes.Key
		found     bool
	)
	for i := range dev.Peers {
		for _, allowed := range dev.Peers[i].AllowedIPs {
			if allowed.String() == clientIPCIDR {
				targetKey = dev.Peers[i].PublicKey
				found = true
				break
			}
		}
		if found {
			break
		}
	}

	if !found {
		return nil
	}

	if err := client.ConfigureDevice(iface, wgtypes.Config{
		Peers: []wgtypes.PeerConfig{
			{
				PublicKey: targetKey,
				Remove:    true,
			},
		},
	}); err != nil {
		return fmt.Errorf("ConfigureDevice(%s): %w", iface, err)
	}

	return nil
}

func RemoveAllPeers() error {
	exists, err := doesInterfaceExists()
	if err != nil {
		return fmt.Errorf("check interface %s: %w", iface, err)
	}
	if !exists {
		return nil
	}

	client, err := wgctrl.New()
	if err != nil {
		return fmt.Errorf("wgctrl.New: %w", err)
	}
	defer client.Close()

	if err := client.ConfigureDevice(iface, wgtypes.Config{
		ReplacePeers: true,
		Peers:        nil,
	}); err != nil {
		return fmt.Errorf("ConfigureDevice(%s): %w", iface, err)
	}

	return nil
}

func GetPeers() ([]wgtypes.Peer, error) {
	client, err := wgctrl.New()
	if err != nil {
		return nil, fmt.Errorf("wgctrl.New: %w", err)
	}
	defer client.Close()

	dev, err := client.Device(iface)
	if err != nil {
		return nil, fmt.Errorf("Device(%s): %w", iface, err)
	}

	return dev.Peers, nil
}

func SetupInterface() error {
	exists, err := doesInterfaceExists()
	if err != nil {
		return fmt.Errorf("check interface %s: %w", iface, err)
	}

	if !exists {
		if err := createInterface(); err != nil {
			return fmt.Errorf("create interface %s: %w", iface, err)
		}
	}

	serverPriv, _, err := ensureServerKeys()
	if err != nil {
		return err
	}

	port := listenPort

	deviceCfg := wgtypes.Config{
		PrivateKey: &serverPriv,
		ListenPort: &port,
	}

	client, err := wgctrl.New()
	if err != nil {
		return fmt.Errorf("wgctrl.New: %w", err)
	}
	defer client.Close()

	if err := client.ConfigureDevice(iface, deviceCfg); err != nil {
		return fmt.Errorf("ConfigureDevice(%s): %w", iface, err)
	}

	return nil
}

func AutoStartVPN(ctx context.Context) error {
	if err := SetupInterface(); err != nil {
		return err
	}

	peers, err := db.GetAllWireguardPeers(ctx)
	if err != nil {
		return fmt.Errorf("load wireguard peers: %w", err)
	}

	for _, peer := range peers {
		publicKey := strings.TrimSpace(peer.PublicKey)
		if publicKey == "" {
			logger.Errorf("wireguard peer %d (%s) is missing a public key; skipping restore", peer.Id, peer.Name)
			continue
		}

		key, err := wgtypes.ParseKey(publicKey)
		if err != nil {
			logger.Errorf("wireguard peer %d (%s) has invalid public key: %v", peer.Id, peer.Name, err)
			continue
		}

		if err := addPeerToDevice(key, peer.ClientIP); err != nil {
			return fmt.Errorf("restore wireguard peer %s (%s): %w", peer.Name, peer.ClientIP, err)
		}
	}

	return nil
}

func NextAvailableClientIP(ctx context.Context) (string, error) {
	serverIP, network, err := net.ParseCIDR(ServerCIDR)
	if err != nil {
		return "", fmt.Errorf("parse server cidr %q: %w", ServerCIDR, err)
	}
	serverIPv4 := serverIP.To4()
	if serverIPv4 == nil {
		return "", fmt.Errorf("server cidr must be IPv4: %s", ServerCIDR)
	}

	prefix := [3]byte{serverIPv4[0], serverIPv4[1], serverIPv4[2]}
	startHost := int(serverIPv4[3]) + 1
	if startHost < 1 {
		startHost = 1
	}

	peers, err := db.GetAllWireguardPeers(ctx)
	if err != nil {
		return "", fmt.Errorf("list wireguard peers: %w", err)
	}

	used := make(map[int]struct{}, len(peers))
	for _, peer := range peers {
		if host, ok := clientHostOctet(peer.ClientIP, prefix); ok {
			used[host] = struct{}{}
		}
	}

	for host := startHost; host < 255; host++ {
		if _, exists := used[host]; exists {
			continue
		}
		return fmt.Sprintf("%d.%d.%d.%d", prefix[0], prefix[1], prefix[2], byte(host)), nil
	}
	return "", fmt.Errorf("no available client ip left in %s", network.String())
}

func clientHostOctet(ip string, prefix [3]byte) (int, bool) {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return 0, false
	}
	if idx := strings.Index(ip, "/"); idx != -1 {
		ip = ip[:idx]
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return 0, false
	}
	ipv4 := parsed.To4()
	if ipv4 == nil {
		return 0, false
	}
	if ipv4[0] != prefix[0] || ipv4[1] != prefix[1] || ipv4[2] != prefix[2] {
		return 0, false
	}
	return int(ipv4[3]), true
}
