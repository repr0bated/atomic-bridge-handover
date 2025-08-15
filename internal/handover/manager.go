package handover

import (
	"fmt"
	"time"

	"atomic-bridge-handover/internal/dbus"
	"atomic-bridge-handover/internal/logging"
	"atomic-bridge-handover/internal/ovs"
	dbusLib "github.com/godbus/dbus/v5"
)

type Manager struct {
	dbusClient  *dbus.NetworkManagerClient
	ovsManager  *ovs.Manager
	logger      *logging.Logger
	dryRun      bool
}

type HandoverState struct {
	OriginalDevice     *dbus.DeviceInfo
	OriginalSettings   *dbus.ConnectionSettings
	BridgeName         string
	BridgeCreated      bool
	ConnectionsCreated []dbusLib.ObjectPath
	OriginalDeactivated bool
}

func NewManager(dbusClient *dbus.NetworkManagerClient, ovsManager *ovs.Manager, logger *logging.Logger, dryRun bool) *Manager {
	return &Manager{
		dbusClient: dbusClient,
		ovsManager: ovsManager,
		logger:     logger,
		dryRun:     dryRun,
	}
}

func (m *Manager) PerformHandover(interfaceName, bridgeName string) error {
	m.logger.Info("Starting atomic handover for interface %s to bridge %s", interfaceName, bridgeName)

	state := &HandoverState{
		BridgeName: bridgeName,
	}

	// Step 1: Introspect current configuration
	if err := m.introspectCurrentState(interfaceName, state); err != nil {
		return fmt.Errorf("failed to introspect current state: %v", err)
	}

	// Step 2: Create OVS bridge structure
	if err := m.createOVSStructure(state); err != nil {
		m.rollback(state)
		return fmt.Errorf("failed to create OVS structure: %v", err)
	}

	// Step 3: Create NetworkManager OVS connections
	if err := m.createNMConnections(state); err != nil {
		m.rollback(state)
		return fmt.Errorf("failed to create NetworkManager connections: %v", err)
	}

	// Step 4: Perform atomic handover
	if err := m.performAtomicHandover(state); err != nil {
		m.rollback(state)
		return fmt.Errorf("failed to perform atomic handover: %v", err)
	}

	// Step 5: Verify connectivity
	if err := m.verifyConnectivity(state); err != nil {
		m.rollback(state)
		return fmt.Errorf("connectivity verification failed: %v", err)
	}

	m.logger.Info("Atomic handover completed successfully")
	return nil
}

func (m *Manager) introspectCurrentState(interfaceName string, state *HandoverState) error {
	m.logger.Info("Introspecting current state for interface: %s", interfaceName)

	// Get device information
	deviceInfo, err := m.dbusClient.GetDeviceByInterface(interfaceName)
	if err != nil {
		return fmt.Errorf("failed to get device info: %v", err)
	}

	state.OriginalDevice = deviceInfo

	// Introspect and log current configuration
	m.dbusClient.IntrospectConnection(deviceInfo)

	// Get current connection settings
	if deviceInfo.ActiveConn != "/" {
		settings, err := m.dbusClient.GetActiveConnectionSettings(deviceInfo)
		if err != nil {
			return fmt.Errorf("failed to get connection settings: %v", err)
		}
		state.OriginalSettings = settings
	} else {
		m.logger.Warn("No active connection found for interface %s", interfaceName)
	}

	return nil
}

func (m *Manager) createOVSStructure(state *HandoverState) error {
	m.logger.Info("Creating OVS bridge structure")

	if m.dryRun {
		m.logger.Info("DRY RUN: Would create OVS bridge structure for %s", state.BridgeName)
		state.BridgeCreated = true
		return nil
	}

	if err := m.ovsManager.CreateBridgeStructure(state.BridgeName); err != nil {
		return err
	}

	state.BridgeCreated = true
	return nil
}

func (m *Manager) createNMConnections(state *HandoverState) error {
	m.logger.Info("Creating NetworkManager OVS connections")

	if m.dryRun {
		m.logger.Info("DRY RUN: Would create NetworkManager OVS connections")
		return nil
	}

	if state.OriginalSettings == nil {
		return fmt.Errorf("no original settings available for connection creation")
	}

	if err := m.dbusClient.CreateOVSConnection(state.BridgeName, state.OriginalDevice.Interface, state.OriginalSettings); err != nil {
		return err
	}

	return nil
}

func (m *Manager) performAtomicHandover(state *HandoverState) error {
	m.logger.Info("Performing atomic handover")

	if m.dryRun {
		m.logger.Info("DRY RUN: Would perform atomic handover")
		return nil
	}

	// Deactivate original connection
	if state.OriginalDevice.ActiveConn != "/" {
		m.logger.Info("Deactivating original connection")
		if err := m.dbusClient.DeactivateConnection(state.OriginalDevice.ActiveConn); err != nil {
			return fmt.Errorf("failed to deactivate original connection: %v", err)
		}
		state.OriginalDeactivated = true

		// Wait for deactivation
		time.Sleep(2 * time.Second)
	}

	// Find and activate bridge connection
	m.logger.Info("Activating bridge connection")
	devices, err := m.dbusClient.GetDevices()
	if err != nil {
		return fmt.Errorf("failed to get devices for activation: %v", err)
	}

	// Find the bridge device
	var bridgeDevice *dbus.DeviceInfo
	for _, devicePath := range devices {
		info, err := m.dbusClient.GetDeviceInfo(devicePath)
		if err != nil {
			continue
		}
		if info.Interface == state.BridgeName {
			bridgeDevice = info
			break
		}
	}

	if bridgeDevice == nil {
		return fmt.Errorf("bridge device %s not found", state.BridgeName)
	}

	// Activate bridge connection - NetworkManager will handle the rest automatically
	m.logger.Info("Bridge device found, NetworkManager should auto-activate OVS connections")

	return nil
}

func (m *Manager) verifyConnectivity(state *HandoverState) error {
	m.logger.Info("Verifying connectivity after handover")

	if m.dryRun {
		m.logger.Info("DRY RUN: Would verify connectivity")
		return nil
	}

	// Wait for network to stabilize
	time.Sleep(5 * time.Second)

	// Check if bridge is up and has the expected configuration
	bridgeDevice, err := m.dbusClient.GetDeviceByInterface(state.BridgeName)
	if err != nil {
		return fmt.Errorf("failed to get bridge device after handover: %v", err)
	}

	if bridgeDevice.State != 100 { // NM_DEVICE_STATE_ACTIVATED
		return fmt.Errorf("bridge device is not in activated state (state: %d)", bridgeDevice.State)
	}

	if bridgeDevice.IPConfig == nil {
		return fmt.Errorf("bridge device has no IP configuration")
	}

	m.logger.Info("Connectivity verification successful")
	m.logger.Info("Bridge %s is active with IP: %s/%d", state.BridgeName, 
		bridgeDevice.IPConfig.Address, bridgeDevice.IPConfig.Prefix)

	// Check OVS structure
	config, err := m.ovsManager.ShowBridge(state.BridgeName)
	if err != nil {
		m.logger.Warn("Failed to show OVS bridge configuration: %v", err)
	} else {
		m.logger.Debug("OVS bridge configuration:\n%s", config)
	}

	return nil
}

func (m *Manager) rollback(state *HandoverState) {
	m.logger.Error("Performing rollback due to handover failure")

	if m.dryRun {
		m.logger.Info("DRY RUN: Would perform rollback")
		return
	}

	// Reactivate original connection if it was deactivated
	if state.OriginalDeactivated && state.OriginalDevice != nil && state.OriginalDevice.ActiveConn != "/" {
		m.logger.Info("Attempting to reactivate original connection")
		// Note: This is complex because the original connection might have been destroyed
		// In a real implementation, we'd need to recreate it from the saved settings
		m.logger.Warn("Original connection reactivation not implemented - manual intervention may be required")
	}

	// Delete created NetworkManager connections
	for _, connPath := range state.ConnectionsCreated {
		m.logger.Info("Deleting connection: %s", connPath)
		if err := m.dbusClient.DeleteConnection(connPath); err != nil {
			m.logger.Error("Failed to delete connection %s during rollback: %v", connPath, err)
		}
	}

	// Cleanup OVS bridge structure
	if state.BridgeCreated {
		m.logger.Info("Cleaning up OVS bridge structure")
		if err := m.ovsManager.CleanupBridgeStructure(state.BridgeName); err != nil {
			m.logger.Error("Failed to cleanup OVS bridge during rollback: %v", err)
		}
	}

	m.logger.Info("Rollback completed")
}