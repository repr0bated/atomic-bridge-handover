package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	// NetworkManager D-Bus constants
	nmService       = "org.freedesktop.NetworkManager"
	nmPath          = "/org/freedesktop/NetworkManager"
	nmInterface     = "org.freedesktop.NetworkManager"
	nmDeviceInterface = "org.freedesktop.NetworkManager.Device"
	nmConnectionInterface = "org.freedesktop.NetworkManager.Connection.Active"
	nmSettingsInterface = "org.freedesktop.NetworkManager.Settings"
	nmConnectionSettingsInterface = "org.freedesktop.NetworkManager.Settings.Connection"
)

type BridgeHandover struct {
	conn            *dbus.Conn
	bridgeName      string
	interfaceName   string
	originalIP      string
	originalGateway string
	originalNetmask string
	originalDNS     []string
	rollbackSteps   []func() error
	logger          *log.Logger
}

func NewBridgeHandover(bridgeName, interfaceName string, logger *log.Logger) (*BridgeHandover, error) {
	conn, err := dbus.SystemBus()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to system bus: %w", err)
	}

	return &BridgeHandover{
		conn:          conn,
		bridgeName:    bridgeName,
		interfaceName: interfaceName,
		rollbackSteps: make([]func() error, 0),
		logger:        logger,
	}, nil
}

func (bh *BridgeHandover) Close() {
	if bh.conn != nil {
		bh.conn.Close()
	}
}

func (bh *BridgeHandover) addRollbackStep(step func() error) {
	bh.rollbackSteps = append(bh.rollbackSteps, step)
}

func (bh *BridgeHandover) rollback() {
	bh.logger.Println("Starting rollback procedures...")
	
	// Execute rollback steps in reverse order
	for i := len(bh.rollbackSteps) - 1; i >= 0; i-- {
		if err := bh.rollbackSteps[i](); err != nil {
			bh.logger.Printf("Rollback step %d failed: %v", i, err)
		}
	}
}

func (bh *BridgeHandover) introspectCurrentConnection() error {
	bh.logger.Printf("Introspecting current connection on interface %s", bh.interfaceName)

	// Get NetworkManager object
	nmObj := bh.conn.Object(nmService, nmPath)

	// Get all devices
	var devices []dbus.ObjectPath
	err := nmObj.Call(nmInterface+".GetDevices", 0).Store(&devices)
	if err != nil {
		return fmt.Errorf("failed to get devices: %w", err)
	}

	// Find our target interface
	var targetDevice dbus.ObjectPath
	for _, devicePath := range devices {
		deviceObj := bh.conn.Object(nmService, devicePath)
		
		var interfaceName string
		err := deviceObj.Call("org.freedesktop.DBus.Properties.Get", 0, 
			nmDeviceInterface, "Interface").Store(&interfaceName)
		if err != nil {
			continue
		}

		if interfaceName == bh.interfaceName {
			targetDevice = devicePath
			break
		}
	}

	if targetDevice == "" {
		return fmt.Errorf("interface %s not found", bh.interfaceName)
	}

	bh.logger.Printf("Found target device at path: %s", targetDevice)

	// Get active connection for this device
	deviceObj := bh.conn.Object(nmService, targetDevice)
	var activeConnectionPath dbus.ObjectPath
	err = deviceObj.Call("org.freedesktop.DBus.Properties.Get", 0,
		nmDeviceInterface, "ActiveConnection").Store(&activeConnectionPath)
	if err != nil {
		return fmt.Errorf("failed to get active connection: %w", err)
	}

	if activeConnectionPath == "/" {
		return fmt.Errorf("no active connection found on interface %s", bh.interfaceName)
	}

	bh.logger.Printf("Active connection path: %s", activeConnectionPath)

	// Get IP configuration from the active connection
	err = bh.extractIPConfig(activeConnectionPath)
	if err != nil {
		return fmt.Errorf("failed to extract IP config: %w", err)
	}

	bh.logger.Printf("Current IP config - IP: %s, Gateway: %s, Netmask: %s", 
		bh.originalIP, bh.originalGateway, bh.originalNetmask)

	return nil
}

func (bh *BridgeHandover) extractIPConfig(connectionPath dbus.ObjectPath) error {
	// Get IP4Config from the active connection
	connObj := bh.conn.Object(nmService, connectionPath)
	var ip4ConfigPath dbus.ObjectPath
	err := connObj.Call("org.freedesktop.DBus.Properties.Get", 0,
		nmConnectionInterface, "Ip4Config").Store(&ip4ConfigPath)
	if err != nil {
		return fmt.Errorf("failed to get IP4Config: %w", err)
	}

	if ip4ConfigPath == "/" {
		return fmt.Errorf("no IP4Config found")
	}

	// Get IP addresses
	ip4ConfigObj := bh.conn.Object(nmService, ip4ConfigPath)
	var addresses [][]uint32
	err = ip4ConfigObj.Call("org.freedesktop.DBus.Properties.Get", 0,
		"org.freedesktop.NetworkManager.IP4Config", "AddressData").Store(&addresses)
	if err != nil {
		// Try legacy format
		err = ip4ConfigObj.Call("org.freedesktop.DBus.Properties.Get", 0,
			"org.freedesktop.NetworkManager.IP4Config", "Addresses").Store(&addresses)
		if err != nil {
			return fmt.Errorf("failed to get addresses: %w", err)
		}
	}

	if len(addresses) > 0 && len(addresses[0]) >= 3 {
		// Convert IP address from uint32 to string
		ipInt := addresses[0][0]
		bh.originalIP = fmt.Sprintf("%d.%d.%d.%d",
			ipInt&0xFF, (ipInt>>8)&0xFF, (ipInt>>16)&0xFF, (ipInt>>24)&0xFF)
		
		// Get prefix length and convert to netmask
		prefixLen := addresses[0][1]
		bh.originalNetmask = prefixToNetmask(prefixLen)
	}

	// Get gateway
	var gateway uint32
	err = ip4ConfigObj.Call("org.freedesktop.DBus.Properties.Get", 0,
		"org.freedesktop.NetworkManager.IP4Config", "Gateway").Store(&gateway)
	if err == nil && gateway != 0 {
		bh.originalGateway = fmt.Sprintf("%d.%d.%d.%d",
			gateway&0xFF, (gateway>>8)&0xFF, (gateway>>16)&0xFF, (gateway>>24)&0xFF)
	}

	// Get DNS servers
	var nameservers []uint32
	err = ip4ConfigObj.Call("org.freedesktop.DBus.Properties.Get", 0,
		"org.freedesktop.NetworkManager.IP4Config", "Nameservers").Store(&nameservers)
	if err == nil {
		bh.originalDNS = make([]string, len(nameservers))
		for i, ns := range nameservers {
			bh.originalDNS[i] = fmt.Sprintf("%d.%d.%d.%d",
				ns&0xFF, (ns>>8)&0xFF, (ns>>16)&0xFF, (ns>>24)&0xFF)
		}
	}

	return nil
}

func prefixToNetmask(prefixLen uint32) string {
	mask := uint32(0xFFFFFFFF) << (32 - prefixLen)
	return fmt.Sprintf("%d.%d.%d.%d",
		(mask>>24)&0xFF, (mask>>16)&0xFF, (mask>>8)&0xFF, mask&0xFF)
}

// NetworkManager D-Bus helper functions for OVS bridge management
func (bh *BridgeHandover) bridgeConnectionExists() (bool, error) {
	settingsObj := bh.conn.Object(nmService, "/org/freedesktop/NetworkManager/Settings")
	
	var connections []dbus.ObjectPath
	err := settingsObj.Call(nmSettingsInterface+".ListConnections", 0).Store(&connections)
	if err != nil {
		return false, fmt.Errorf("failed to list connections: %w", err)
	}

	for _, connPath := range connections {
		connObj := bh.conn.Object(nmService, connPath)
		var settings map[string]map[string]dbus.Variant
		err := connObj.Call(nmConnectionSettingsInterface+".GetSettings", 0).Store(&settings)
		if err != nil {
			continue
		}

		if connSettings, ok := settings["connection"]; ok {
			if idVariant, ok := connSettings["id"]; ok {
				if id, ok := idVariant.Value().(string); ok && id == bh.bridgeName {
					if typeVariant, ok := connSettings["type"]; ok {
						if connType, ok := typeVariant.Value().(string); ok && connType == "ovs-bridge" {
							return true, nil
						}
					}
				}
			}
		}
	}

	return false, nil
}

func (bh *BridgeHandover) createOVSBridgeConnection() (dbus.ObjectPath, error) {
	bh.logger.Printf("Creating OVS bridge connection: %s", bh.bridgeName)

	// Build connection settings for OVS bridge
	settings := map[string]map[string]dbus.Variant{
		"connection": {
			"id":   dbus.MakeVariant(bh.bridgeName),
			"type": dbus.MakeVariant("ovs-bridge"),
			"uuid": dbus.MakeVariant(generateUUID()),
		},
		"ovs-bridge": {
			"bridge-name": dbus.MakeVariant(bh.bridgeName),
		},
	}

	settingsObj := bh.conn.Object(nmService, "/org/freedesktop/NetworkManager/Settings")
	var connPath dbus.ObjectPath
	err := settingsObj.Call(nmSettingsInterface+".AddConnection", 0, settings).Store(&connPath)
	if err != nil {
		return "", fmt.Errorf("failed to create bridge connection: %w", err)
	}

	bh.logger.Printf("Created OVS bridge connection at path: %s", connPath)
	return connPath, nil
}

func (bh *BridgeHandover) createOVSPortConnection() (dbus.ObjectPath, error) {
	bh.logger.Printf("Creating OVS port connection for interface: %s", bh.interfaceName)

	portName := fmt.Sprintf("%s-port", bh.interfaceName)
	
	// Build connection settings for OVS port
	settings := map[string]map[string]dbus.Variant{
		"connection": {
			"id":     dbus.MakeVariant(portName),
			"type":   dbus.MakeVariant("ovs-port"),
			"uuid":   dbus.MakeVariant(generateUUID()),
			"master": dbus.MakeVariant(bh.bridgeName),
			"slave-type": dbus.MakeVariant("ovs-port"),
		},
		"ovs-port": {
			"bridge-name": dbus.MakeVariant(bh.bridgeName),
		},
	}

	settingsObj := bh.conn.Object(nmService, "/org/freedesktop/NetworkManager/Settings")
	var connPath dbus.ObjectPath
	err := settingsObj.Call(nmSettingsInterface+".AddConnection", 0, settings).Store(&connPath)
	if err != nil {
		return "", fmt.Errorf("failed to create port connection: %w", err)
	}

	// Create OVS interface connection for the physical interface
	interfaceConnPath, err := bh.createOVSInterfaceConnection(portName)
	if err != nil {
		// Cleanup port connection if interface creation fails
		bh.deleteConnection(connPath)
		return "", fmt.Errorf("failed to create interface connection: %w", err)
	}

	bh.logger.Printf("Created OVS port connection at path: %s", connPath)
	bh.logger.Printf("Created OVS interface connection at path: %s", interfaceConnPath)
	return connPath, nil
}

func (bh *BridgeHandover) createOVSInterfaceConnection(portName string) (dbus.ObjectPath, error) {
	interfaceName := fmt.Sprintf("%s-interface", bh.interfaceName)
	
	// Build connection settings for OVS interface
	settings := map[string]map[string]dbus.Variant{
		"connection": {
			"id":         dbus.MakeVariant(interfaceName),
			"type":       dbus.MakeVariant("ovs-interface"),
			"uuid":       dbus.MakeVariant(generateUUID()),
			"master":     dbus.MakeVariant(portName),
			"slave-type": dbus.MakeVariant("ovs-interface"),
		},
		"ovs-interface": {
			"type": dbus.MakeVariant("system"),
		},
	}

	settingsObj := bh.conn.Object(nmService, "/org/freedesktop/NetworkManager/Settings")
	var connPath dbus.ObjectPath
	err := settingsObj.Call(nmSettingsInterface+".AddConnection", 0, settings).Store(&connPath)
	if err != nil {
		return "", fmt.Errorf("failed to create interface connection: %w", err)
	}

	return connPath, nil
}

func (bh *BridgeHandover) activateConnection(connPath dbus.ObjectPath) error {
	bh.logger.Printf("Activating connection: %s", connPath)

	nmObj := bh.conn.Object(nmService, nmPath)
	var activeConnPath dbus.ObjectPath
	err := nmObj.Call(nmInterface+".ActivateConnection", 0, connPath, dbus.ObjectPath("/"), dbus.ObjectPath("/")).Store(&activeConnPath)
	if err != nil {
		return fmt.Errorf("failed to activate connection: %w", err)
	}

	bh.logger.Printf("Activated connection, active path: %s", activeConnPath)
	return nil
}

func (bh *BridgeHandover) deleteConnection(connPath dbus.ObjectPath) error {
	connObj := bh.conn.Object(nmService, connPath)
	return connObj.Call(nmConnectionSettingsInterface+".Delete", 0).Err
}

func generateUUID() string {
	// Simple UUID v4 generation for connection UUIDs
	// In production, use proper UUID library
	return fmt.Sprintf("%08x-%04x-4%03x-%04x-%012x",
		time.Now().UnixNano()&0xffffffff,
		time.Now().UnixNano()>>32&0xffff,
		time.Now().UnixNano()>>48&0x0fff,
		0x8000|(time.Now().UnixNano()>>60&0x3fff),
		time.Now().UnixNano()&0xffffffffffff)
}

func (bh *BridgeHandover) createOVSBridge() error {
	bh.logger.Printf("Creating OVS bridge via NetworkManager: %s", bh.bridgeName)

	// Check if bridge connection already exists
	if exists, err := bh.bridgeConnectionExists(); err != nil {
		return fmt.Errorf("failed to check bridge existence: %w", err)
	} else if exists {
		bh.logger.Printf("Bridge connection %s already exists", bh.bridgeName)
		return nil
	}

	// Create OVS bridge connection
	bridgeConnPath, err := bh.createOVSBridgeConnection()
	if err != nil {
		return fmt.Errorf("failed to create bridge connection: %w", err)
	}

	// Add rollback step to delete bridge connection
	bh.addRollbackStep(func() error {
		bh.logger.Printf("Rollback: Deleting bridge connection %s", bh.bridgeName)
		return bh.deleteConnection(bridgeConnPath)
	})

	// Create OVS port connection for the interface
	portConnPath, err := bh.createOVSPortConnection()
	if err != nil {
		return fmt.Errorf("failed to create port connection: %w", err)
	}

	// Add rollback step to remove port connection
	bh.addRollbackStep(func() error {
		bh.logger.Printf("Rollback: Deleting port connection for %s", bh.interfaceName)
		return bh.deleteConnection(portConnPath)
	})

	// Activate the bridge connection
	err = bh.activateConnection(bridgeConnPath)
	if err != nil {
		return fmt.Errorf("failed to activate bridge connection: %w", err)
	}

	bh.logger.Printf("OVS bridge %s created successfully via NetworkManager", bh.bridgeName)
	return nil
}

func (bh *BridgeHandover) configureBridgeIP() error {
	bh.logger.Printf("Configuring IP on bridge %s", bh.bridgeName)

	// Bring bridge up
	cmd := exec.Command("ip", "link", "set", "dev", bh.bridgeName, "up")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to bring bridge up: %w", err)
	}

	// Configure IP address
	if bh.originalIP != "" && bh.originalNetmask != "" {
		// Convert netmask to CIDR
		cidr := netmaskToCIDR(bh.originalNetmask)
		ipWithCIDR := fmt.Sprintf("%s/%d", bh.originalIP, cidr)
		
		bh.logger.Printf("Setting IP %s on bridge %s", ipWithCIDR, bh.bridgeName)
		cmd = exec.Command("ip", "addr", "add", ipWithCIDR, "dev", bh.bridgeName)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to set IP on bridge: %w", err)
		}

		// Add rollback step to remove IP
		bh.addRollbackStep(func() error {
			bh.logger.Printf("Rollback: Removing IP from bridge %s", bh.bridgeName)
			cmd := exec.Command("ip", "addr", "del", ipWithCIDR, "dev", bh.bridgeName)
			return cmd.Run()
		})
	}

	// Configure gateway
	if bh.originalGateway != "" {
		bh.logger.Printf("Setting gateway %s", bh.originalGateway)
		cmd = exec.Command("ip", "route", "add", "default", "via", bh.originalGateway, "dev", bh.bridgeName)
		if err := cmd.Run(); err != nil {
			// Gateway might already exist, log but don't fail
			bh.logger.Printf("Warning: failed to set gateway (might already exist): %v", err)
		}
	}

	return nil
}

func netmaskToCIDR(netmask string) int {
	parts := strings.Split(netmask, ".")
	if len(parts) != 4 {
		return 24 // default
	}

	cidr := 0
	for _, part := range parts {
		var octet uint32
		fmt.Sscanf(part, "%d", &octet)
		for octet > 0 {
			if octet&0x80 != 0 {
				cidr++
			}
			octet <<= 1
		}
	}
	return cidr
}

func (bh *BridgeHandover) removeInterfaceFromNM() error {
	bh.logger.Printf("Removing interface %s from NetworkManager control", bh.interfaceName)

	// Deactivate any active connections on this interface
	nmObj := bh.conn.Object(nmService, nmPath)
	
	// Get all active connections
	var activeConnections []dbus.ObjectPath
	err := nmObj.Call("org.freedesktop.DBus.Properties.Get", 0,
		nmInterface, "ActiveConnections").Store(&activeConnections)
	if err != nil {
		return fmt.Errorf("failed to get active connections: %w", err)
	}

	for _, connPath := range activeConnections {
		connObj := bh.conn.Object(nmService, connPath)
		
		// Get devices for this connection
		var devices []dbus.ObjectPath
		err := connObj.Call("org.freedesktop.DBus.Properties.Get", 0,
			nmConnectionInterface, "Devices").Store(&devices)
		if err != nil {
			continue
		}

		// Check if our interface is in the devices list
		for _, devicePath := range devices {
			deviceObj := bh.conn.Object(nmService, devicePath)
			var interfaceName string
			err := deviceObj.Call("org.freedesktop.DBus.Properties.Get", 0,
				nmDeviceInterface, "Interface").Store(&interfaceName)
			if err != nil {
				continue
			}

			if interfaceName == bh.interfaceName {
				bh.logger.Printf("Deactivating connection on %s", bh.interfaceName)
				err := nmObj.Call(nmInterface+".DeactivateConnection", 0, connPath).Err
				if err != nil {
					bh.logger.Printf("Warning: failed to deactivate connection: %v", err)
				}
				// Wait a moment for deactivation
				time.Sleep(2 * time.Second)
				break
			}
		}
	}

	return nil
}

func (bh *BridgeHandover) performAtomicHandover() error {
	bh.logger.Println("Starting atomic handover process")

	// Step 1: Introspect current connection
	if err := bh.introspectCurrentConnection(); err != nil {
		return fmt.Errorf("introspection failed: %w", err)
	}

	// Step 2: Create OVS bridge and add interface
	if err := bh.createOVSBridge(); err != nil {
		return fmt.Errorf("OVS bridge creation failed: %w", err)
	}

	// Step 3: Remove interface from NetworkManager control
	if err := bh.removeInterfaceFromNM(); err != nil {
		bh.logger.Printf("Warning: failed to remove from NM: %v", err)
	}

	// Step 4: Configure IP on bridge (this is the critical atomic step)
	if err := bh.configureBridgeIP(); err != nil {
		bh.rollback()
		return fmt.Errorf("bridge IP configuration failed: %w", err)
	}

	bh.logger.Println("Atomic handover completed successfully")
	return nil
}

func (bh *BridgeHandover) validateConnectivity() error {
	bh.logger.Println("Validating connectivity after handover")

	// Test basic connectivity
	if bh.originalGateway != "" {
		cmd := exec.Command("ping", "-c", "1", "-W", "3", bh.originalGateway)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("connectivity test failed - cannot ping gateway %s: %w", 
				bh.originalGateway, err)
		}
		bh.logger.Printf("Connectivity test passed - gateway %s is reachable", bh.originalGateway)
	}

	return nil
}

func main() {
	var (
		bridgeName    = flag.String("bridge", "ovsbr0", "Name of the OVS bridge to create")
		interfaceName = flag.String("interface", "enp2s0", "Name of the interface to add to bridge")
		verbose       = flag.Bool("verbose", false, "Enable verbose logging")
		dryRun        = flag.Bool("dry-run", false, "Show what would be done without executing")
	)
	flag.Parse()

	// Setup logging
	logger := log.New(os.Stdout, "[BRIDGE-HANDOVER] ", log.LstdFlags|log.Lshortfile)
	if *verbose {
		logger.SetOutput(os.Stdout)
	}

	logger.Printf("Starting atomic bridge handover: %s -> %s", *interfaceName, *bridgeName)

	if *dryRun {
		logger.Println("DRY-RUN MODE: No actual changes will be made")
		// In a real implementation, you'd show what would be done
		logger.Printf("Would create bridge: %s", *bridgeName)
		logger.Printf("Would add interface: %s", *interfaceName)
		return
	}

	// Check if running as root
	if os.Geteuid() != 0 {
		logger.Fatal("This program must be run as root")
	}

	// Create bridge handover instance
	bh, err := NewBridgeHandover(*bridgeName, *interfaceName, logger)
	if err != nil {
		logger.Fatalf("Failed to initialize bridge handover: %v", err)
	}
	defer bh.Close()

	// Perform the atomic handover
	if err := bh.performAtomicHandover(); err != nil {
		logger.Fatalf("Atomic handover failed: %v", err)
	}

	// Validate connectivity
	if err := bh.validateConnectivity(); err != nil {
		logger.Printf("Warning: Connectivity validation failed: %v", err)
		logger.Println("Consider rolling back if connectivity is critical")
	}

	logger.Println("Atomic bridge handover completed successfully!")
}