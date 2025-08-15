package dbus

import (
	"fmt"
	"strings"

	"github.com/godbus/dbus/v5"
	"atomic-bridge-handover/internal/logging"
)

const (
	NetworkManagerService   = "org.freedesktop.NetworkManager"
	NetworkManagerPath      = "/org/freedesktop/NetworkManager"
	NetworkManagerInterface = "org.freedesktop.NetworkManager"
	DeviceInterface         = "org.freedesktop.NetworkManager.Device"
	ConnectionInterface     = "org.freedesktop.NetworkManager.Connection.Active"
	SettingsInterface       = "org.freedesktop.NetworkManager.Settings"
	ConnectionSettingsInterface = "org.freedesktop.NetworkManager.Settings.Connection"
)

type NetworkManagerClient struct {
	conn   *dbus.Conn
	logger *logging.Logger
}

type DeviceInfo struct {
	Path        dbus.ObjectPath
	Interface   string
	DeviceType  uint32
	State       uint32
	ActiveConn  dbus.ObjectPath
	IPConfig    *IPConfiguration
}

type IPConfiguration struct {
	Address   string
	Prefix    uint32
	Gateway   string
	DNS       []string
}

type ConnectionSettings struct {
	UUID       string
	ID         string
	Type       string
	Interface  string
	Settings   map[string]map[string]dbus.Variant
}

func NewNetworkManagerClient(logger *logging.Logger) (*NetworkManagerClient, error) {
	conn, err := dbus.SystemBus()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to system bus: %v", err)
	}

	client := &NetworkManagerClient{
		conn:   conn,
		logger: logger,
	}

	// Test connection
	if err := client.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to ping NetworkManager: %v", err)
	}

	return client, nil
}

func (c *NetworkManagerClient) Close() {
	if c.conn != nil {
		c.conn.Close()
	}
}

func (c *NetworkManagerClient) Ping() error {
	obj := c.conn.Object(NetworkManagerService, NetworkManagerPath)
	call := obj.Call("org.freedesktop.DBus.Peer.Ping", 0)
	return call.Err
}

func (c *NetworkManagerClient) GetDevices() ([]dbus.ObjectPath, error) {
	obj := c.conn.Object(NetworkManagerService, NetworkManagerPath)
	
	var devices []dbus.ObjectPath
	err := obj.Call(NetworkManagerInterface+".GetDevices", 0).Store(&devices)
	if err != nil {
		return nil, fmt.Errorf("failed to get devices: %v", err)
	}

	return devices, nil
}

func (c *NetworkManagerClient) GetDeviceInfo(devicePath dbus.ObjectPath) (*DeviceInfo, error) {
	obj := c.conn.Object(NetworkManagerService, devicePath)
	
	// Get device properties
	var deviceType uint32
	var state uint32
	var iface string
	var activeConn dbus.ObjectPath

	if err := obj.Call("org.freedesktop.DBus.Properties.Get", 0, DeviceInterface, "DeviceType").Store(&deviceType); err != nil {
		return nil, fmt.Errorf("failed to get device type: %v", err)
	}

	if err := obj.Call("org.freedesktop.DBus.Properties.Get", 0, DeviceInterface, "State").Store(&state); err != nil {
		return nil, fmt.Errorf("failed to get device state: %v", err)
	}

	if err := obj.Call("org.freedesktop.DBus.Properties.Get", 0, DeviceInterface, "Interface").Store(&iface); err != nil {
		return nil, fmt.Errorf("failed to get device interface: %v", err)
	}

	if err := obj.Call("org.freedesktop.DBus.Properties.Get", 0, DeviceInterface, "ActiveConnection").Store(&activeConn); err != nil {
		c.logger.Debug("No active connection for device %s", devicePath)
	}

	info := &DeviceInfo{
		Path:       devicePath,
		Interface:  iface,
		DeviceType: deviceType,
		State:      state,
		ActiveConn: activeConn,
	}

	// Get IP configuration if device is active
	if state == 100 { // NM_DEVICE_STATE_ACTIVATED
		ipConfig, err := c.GetIPConfiguration(devicePath)
		if err != nil {
			c.logger.Warn("Failed to get IP configuration for %s: %v", iface, err)
		} else {
			info.IPConfig = ipConfig
		}
	}

	return info, nil
}

func (c *NetworkManagerClient) GetIPConfiguration(devicePath dbus.ObjectPath) (*IPConfiguration, error) {
	obj := c.conn.Object(NetworkManagerService, devicePath)
	
	var ip4ConfigPath dbus.ObjectPath
	if err := obj.Call("org.freedesktop.DBus.Properties.Get", 0, DeviceInterface, "Ip4Config").Store(&ip4ConfigPath); err != nil {
		return nil, fmt.Errorf("failed to get IP4Config path: %v", err)
	}

	if ip4ConfigPath == "/" {
		return nil, fmt.Errorf("no IP configuration available")
	}

	ip4Obj := c.conn.Object(NetworkManagerService, ip4ConfigPath)
	
	// Get address data
	var addressData []map[string]dbus.Variant
	if err := ip4Obj.Call("org.freedesktop.DBus.Properties.Get", 0, "org.freedesktop.NetworkManager.IP4Config", "AddressData").Store(&addressData); err != nil {
		return nil, fmt.Errorf("failed to get address data: %v", err)
	}

	if len(addressData) == 0 {
		return nil, fmt.Errorf("no addresses configured")
	}

	config := &IPConfiguration{}
	
	// Extract first address
	if addr, ok := addressData[0]["address"]; ok {
		config.Address = addr.Value().(string)
	}
	if prefix, ok := addressData[0]["prefix"]; ok {
		config.Prefix = prefix.Value().(uint32)
	}

	// Get gateway
	var gateway string
	if err := ip4Obj.Call("org.freedesktop.DBus.Properties.Get", 0, "org.freedesktop.NetworkManager.IP4Config", "Gateway").Store(&gateway); err == nil {
		config.Gateway = gateway
	}

	// Get DNS
	var nameserverData []map[string]dbus.Variant
	if err := ip4Obj.Call("org.freedesktop.DBus.Properties.Get", 0, "org.freedesktop.NetworkManager.IP4Config", "NameserverData").Store(&nameserverData); err == nil {
		for _, ns := range nameserverData {
			if addr, ok := ns["address"]; ok {
				config.DNS = append(config.DNS, addr.Value().(string))
			}
		}
	}

	return config, nil
}

func (c *NetworkManagerClient) InterfaceExists(interfaceName string) (bool, error) {
	devices, err := c.GetDevices()
	if err != nil {
		return false, err
	}

	for _, devicePath := range devices {
		info, err := c.GetDeviceInfo(devicePath)
		if err != nil {
			c.logger.Debug("Failed to get info for device %s: %v", devicePath, err)
			continue
		}

		if info.Interface == interfaceName {
			return true, nil
		}
	}

	return false, nil
}

func (c *NetworkManagerClient) GetDeviceByInterface(interfaceName string) (*DeviceInfo, error) {
	devices, err := c.GetDevices()
	if err != nil {
		return nil, err
	}

	for _, devicePath := range devices {
		info, err := c.GetDeviceInfo(devicePath)
		if err != nil {
			c.logger.Debug("Failed to get info for device %s: %v", devicePath, err)
			continue
		}

		if info.Interface == interfaceName {
			return info, nil
		}
	}

	return nil, fmt.Errorf("device with interface %s not found", interfaceName)
}

func (c *NetworkManagerClient) GetActiveConnectionSettings(deviceInfo *DeviceInfo) (*ConnectionSettings, error) {
	if deviceInfo.ActiveConn == "/" {
		return nil, fmt.Errorf("no active connection")
	}

	// Get the connection path from active connection
	obj := c.conn.Object(NetworkManagerService, deviceInfo.ActiveConn)
	var connPath dbus.ObjectPath
	if err := obj.Call("org.freedesktop.DBus.Properties.Get", 0, ConnectionInterface, "Connection").Store(&connPath); err != nil {
		return nil, fmt.Errorf("failed to get connection path: %v", err)
	}

	// Get connection settings
	connObj := c.conn.Object(NetworkManagerService, connPath)
	var settings map[string]map[string]dbus.Variant
	if err := connObj.Call(ConnectionSettingsInterface+".GetSettings", 0).Store(&settings); err != nil {
		return nil, fmt.Errorf("failed to get connection settings: %v", err)
	}

	connSettings := &ConnectionSettings{
		Settings: settings,
	}

	// Extract basic info
	if conn, ok := settings["connection"]; ok {
		if uuid, ok := conn["uuid"]; ok {
			connSettings.UUID = uuid.Value().(string)
		}
		if id, ok := conn["id"]; ok {
			connSettings.ID = id.Value().(string)
		}
		if connType, ok := conn["type"]; ok {
			connSettings.Type = connType.Value().(string)
		}
		if iface, ok := conn["interface-name"]; ok {
			connSettings.Interface = iface.Value().(string)
		}
	}

	return connSettings, nil
}

func (c *NetworkManagerClient) CreateOVSConnection(bridgeName, interfaceName string, originalSettings *ConnectionSettings) error {
	c.logger.Info("Creating OVS bridge connection for %s", bridgeName)

	// Create bridge connection settings
	bridgeSettings := map[string]map[string]dbus.Variant{
		"connection": {
			"id":              dbus.MakeVariant(bridgeName + "-bridge"),
			"type":            dbus.MakeVariant("ovs-bridge"),
			"interface-name":  dbus.MakeVariant(bridgeName),
			"autoconnect":     dbus.MakeVariant(true),
		},
		"ovs-bridge": {
			"fail-mode": dbus.MakeVariant("standalone"),
		},
	}

	// Copy IP settings from original connection
	if ipv4, ok := originalSettings.Settings["ipv4"]; ok {
		bridgeSettings["ipv4"] = ipv4
	}
	if ipv6, ok := originalSettings.Settings["ipv6"]; ok {
		bridgeSettings["ipv6"] = ipv6
	}

	settingsObj := c.conn.Object(NetworkManagerService, "/org/freedesktop/NetworkManager/Settings")
	var bridgeConnPath dbus.ObjectPath
	if err := settingsObj.Call(SettingsInterface+".AddConnection", 0, bridgeSettings).Store(&bridgeConnPath); err != nil {
		return fmt.Errorf("failed to create bridge connection: %v", err)
	}

	c.logger.Info("Created bridge connection at %s", bridgeConnPath)

	// Create port connection
	portSettings := map[string]map[string]dbus.Variant{
		"connection": {
			"id":              dbus.MakeVariant(bridgeName + "-port"),
			"type":            dbus.MakeVariant("ovs-port"),
			"interface-name":  dbus.MakeVariant(bridgeName + "-port"),
			"master":          dbus.MakeVariant(bridgeName),
			"slave-type":      dbus.MakeVariant("ovs-bridge"),
			"autoconnect":     dbus.MakeVariant(true),
		},
		"ovs-port": {
			"tag": dbus.MakeVariant(uint32(0)),
		},
	}

	var portConnPath dbus.ObjectPath
	if err := settingsObj.Call(SettingsInterface+".AddConnection", 0, portSettings).Store(&portConnPath); err != nil {
		return fmt.Errorf("failed to create port connection: %v", err)
	}

	c.logger.Info("Created port connection at %s", portConnPath)

	// Create interface connection
	interfaceSettings := map[string]map[string]dbus.Variant{
		"connection": {
			"id":              dbus.MakeVariant(interfaceName + "-ovs"),
			"type":            dbus.MakeVariant("ovs-interface"),
			"interface-name":  dbus.MakeVariant(interfaceName),
			"master":          dbus.MakeVariant(bridgeName + "-port"),
			"slave-type":      dbus.MakeVariant("ovs-port"),
			"autoconnect":     dbus.MakeVariant(true),
		},
		"ovs-interface": {
			"type": dbus.MakeVariant("system"),
		},
	}

	var interfaceConnPath dbus.ObjectPath
	if err := settingsObj.Call(SettingsInterface+".AddConnection", 0, interfaceSettings).Store(&interfaceConnPath); err != nil {
		return fmt.Errorf("failed to create interface connection: %v", err)
	}

	c.logger.Info("Created interface connection at %s", interfaceConnPath)

	return nil
}

func (c *NetworkManagerClient) ActivateConnection(connPath dbus.ObjectPath, devicePath dbus.ObjectPath) error {
	obj := c.conn.Object(NetworkManagerService, NetworkManagerPath)
	
	var activeConnPath dbus.ObjectPath
	err := obj.Call(NetworkManagerInterface+".ActivateConnection", 0, connPath, devicePath, "/").Store(&activeConnPath)
	if err != nil {
		return fmt.Errorf("failed to activate connection: %v", err)
	}

	c.logger.Info("Activated connection %s", activeConnPath)
	return nil
}

func (c *NetworkManagerClient) DeactivateConnection(activeConnPath dbus.ObjectPath) error {
	obj := c.conn.Object(NetworkManagerService, NetworkManagerPath)
	
	err := obj.Call(NetworkManagerInterface+".DeactivateConnection", 0, activeConnPath).Err
	if err != nil {
		return fmt.Errorf("failed to deactivate connection: %v", err)
	}

	c.logger.Info("Deactivated connection %s", activeConnPath)
	return nil
}

func (c *NetworkManagerClient) DeleteConnection(connPath dbus.ObjectPath) error {
	obj := c.conn.Object(NetworkManagerService, connPath)
	
	err := obj.Call(ConnectionSettingsInterface+".Delete", 0).Err
	if err != nil {
		return fmt.Errorf("failed to delete connection: %v", err)
	}

	c.logger.Info("Deleted connection %s", connPath)
	return nil
}

func (c *NetworkManagerClient) IntrospectConnection(deviceInfo *DeviceInfo) {
	c.logger.Info("=== Device Information ===")
	c.logger.Info("Interface: %s", deviceInfo.Interface)
	c.logger.Info("Type: %d", deviceInfo.DeviceType)
	c.logger.Info("State: %d", deviceInfo.State)
	c.logger.Info("Path: %s", deviceInfo.Path)

	if deviceInfo.IPConfig != nil {
		c.logger.Info("=== IP Configuration ===")
		c.logger.Info("Address: %s/%d", deviceInfo.IPConfig.Address, deviceInfo.IPConfig.Prefix)
		c.logger.Info("Gateway: %s", deviceInfo.IPConfig.Gateway)
		c.logger.Info("DNS: %s", strings.Join(deviceInfo.IPConfig.DNS, ", "))
	}

	if deviceInfo.ActiveConn != "/" {
		c.logger.Info("=== Active Connection ===")
		c.logger.Info("Path: %s", deviceInfo.ActiveConn)
		
		settings, err := c.GetActiveConnectionSettings(deviceInfo)
		if err != nil {
			c.logger.Error("Failed to get connection settings: %v", err)
			return
		}

		c.logger.Info("ID: %s", settings.ID)
		c.logger.Info("UUID: %s", settings.UUID)
		c.logger.Info("Type: %s", settings.Type)
		c.logger.Info("Interface: %s", settings.Interface)
	}
}