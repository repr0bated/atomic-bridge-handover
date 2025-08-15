package ovs

import (
	"fmt"
	"os/exec"
	"strings"

	"atomic-bridge-handover/internal/logging"
)

type Manager struct {
	logger *logging.Logger
	dryRun bool
}

func NewManager(logger *logging.Logger, dryRun bool) *Manager {
	return &Manager{
		logger: logger,
		dryRun: dryRun,
	}
}

func (m *Manager) CheckOVSAvailable() error {
	m.logger.Debug("Checking OVS availability")
	
	// Check if ovs-vsctl is available
	if _, err := exec.LookPath("ovs-vsctl"); err != nil {
		return fmt.Errorf("ovs-vsctl not found in PATH: %v", err)
	}

	// Check if OVS database is running
	cmd := exec.Command("ovs-vsctl", "show")
	if m.dryRun {
		m.logger.Info("DRY RUN: Would execute: %s", cmd.String())
		return nil
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("OVS is not running or accessible: %v (output: %s)", err, string(output))
	}

	m.logger.Debug("OVS is available and running")
	return nil
}

func (m *Manager) BridgeExists(bridgeName string) (bool, error) {
	m.logger.Debug("Checking if bridge %s exists", bridgeName)
	
	cmd := exec.Command("ovs-vsctl", "br-exists", bridgeName)
	if m.dryRun {
		m.logger.Info("DRY RUN: Would execute: %s", cmd.String())
		return false, nil
	}

	err := cmd.Run()
	if err != nil {
		// ovs-vsctl br-exists returns non-zero if bridge doesn't exist
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 2 {
				return false, nil
			}
		}
		return false, fmt.Errorf("failed to check bridge existence: %v", err)
	}

	return true, nil
}

func (m *Manager) CreateBridge(bridgeName string) error {
	m.logger.Info("Creating OVS bridge: %s", bridgeName)
	
	cmd := exec.Command("ovs-vsctl", "add-br", bridgeName)
	if m.dryRun {
		m.logger.Info("DRY RUN: Would execute: %s", cmd.String())
		return nil
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create bridge %s: %v (output: %s)", bridgeName, err, string(output))
	}

	m.logger.Info("Successfully created bridge: %s", bridgeName)
	return nil
}

func (m *Manager) AddPort(bridgeName, portName string) error {
	m.logger.Info("Adding port %s to bridge %s", portName, bridgeName)
	
	cmd := exec.Command("ovs-vsctl", "add-port", bridgeName, portName)
	if m.dryRun {
		m.logger.Info("DRY RUN: Would execute: %s", cmd.String())
		return nil
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to add port %s to bridge %s: %v (output: %s)", portName, bridgeName, err, string(output))
	}

	m.logger.Info("Successfully added port %s to bridge %s", portName, bridgeName)
	return nil
}

func (m *Manager) RemovePort(bridgeName, portName string) error {
	m.logger.Info("Removing port %s from bridge %s", portName, bridgeName)
	
	cmd := exec.Command("ovs-vsctl", "del-port", bridgeName, portName)
	if m.dryRun {
		m.logger.Info("DRY RUN: Would execute: %s", cmd.String())
		return nil
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to remove port %s from bridge %s: %v (output: %s)", portName, bridgeName, err, string(output))
	}

	m.logger.Info("Successfully removed port %s from bridge %s", portName, bridgeName)
	return nil
}

func (m *Manager) DeleteBridge(bridgeName string) error {
	m.logger.Info("Deleting OVS bridge: %s", bridgeName)
	
	cmd := exec.Command("ovs-vsctl", "del-br", bridgeName)
	if m.dryRun {
		m.logger.Info("DRY RUN: Would execute: %s", cmd.String())
		return nil
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to delete bridge %s: %v (output: %s)", bridgeName, err, string(output))
	}

	m.logger.Info("Successfully deleted bridge: %s", bridgeName)
	return nil
}

func (m *Manager) SetBridgeController(bridgeName, controller string) error {
	m.logger.Info("Setting controller for bridge %s: %s", bridgeName, controller)
	
	cmd := exec.Command("ovs-vsctl", "set-controller", bridgeName, controller)
	if m.dryRun {
		m.logger.Info("DRY RUN: Would execute: %s", cmd.String())
		return nil
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to set controller for bridge %s: %v (output: %s)", bridgeName, err, string(output))
	}

	m.logger.Info("Successfully set controller for bridge %s", bridgeName)
	return nil
}

func (m *Manager) SetFailMode(bridgeName, failMode string) error {
	m.logger.Info("Setting fail mode for bridge %s: %s", bridgeName, failMode)
	
	cmd := exec.Command("ovs-vsctl", "set-fail-mode", bridgeName, failMode)
	if m.dryRun {
		m.logger.Info("DRY RUN: Would execute: %s", cmd.String())
		return nil
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to set fail mode for bridge %s: %v (output: %s)", bridgeName, err, string(output))
	}

	m.logger.Info("Successfully set fail mode for bridge %s", bridgeName)
	return nil
}

func (m *Manager) ListPorts(bridgeName string) ([]string, error) {
	m.logger.Debug("Listing ports for bridge: %s", bridgeName)
	
	cmd := exec.Command("ovs-vsctl", "list-ports", bridgeName)
	if m.dryRun {
		m.logger.Info("DRY RUN: Would execute: %s", cmd.String())
		return []string{}, nil
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to list ports for bridge %s: %v (output: %s)", bridgeName, err, string(output))
	}

	ports := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(ports) == 1 && ports[0] == "" {
		return []string{}, nil
	}

	return ports, nil
}

func (m *Manager) ShowBridge(bridgeName string) (string, error) {
	m.logger.Debug("Showing bridge configuration: %s", bridgeName)
	
	cmd := exec.Command("ovs-vsctl", "show")
	if m.dryRun {
		m.logger.Info("DRY RUN: Would execute: %s", cmd.String())
		return "DRY RUN MODE", nil
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to show bridge configuration: %v", err)
	}

	return string(output), nil
}

func (m *Manager) PortExists(bridgeName, portName string) (bool, error) {
	ports, err := m.ListPorts(bridgeName)
	if err != nil {
		return false, err
	}

	for _, port := range ports {
		if port == portName {
			return true, nil
		}
	}

	return false, nil
}

// Atomic operation to create bridge structure
func (m *Manager) CreateBridgeStructure(bridgeName string) error {
	m.logger.Info("Creating OVS bridge structure for: %s", bridgeName)

	// Create bridge
	if err := m.CreateBridge(bridgeName); err != nil {
		return err
	}

	// Set fail mode to standalone for safety
	if err := m.SetFailMode(bridgeName, "standalone"); err != nil {
		// Cleanup on failure
		m.DeleteBridge(bridgeName)
		return err
	}

	m.logger.Info("Successfully created bridge structure for: %s", bridgeName)
	return nil
}

// Cleanup bridge structure
func (m *Manager) CleanupBridgeStructure(bridgeName string) error {
	m.logger.Info("Cleaning up OVS bridge structure for: %s", bridgeName)

	// List and remove all ports first
	ports, err := m.ListPorts(bridgeName)
	if err != nil {
		m.logger.Warn("Failed to list ports for cleanup: %v", err)
	} else {
		for _, port := range ports {
			if err := m.RemovePort(bridgeName, port); err != nil {
				m.logger.Warn("Failed to remove port %s during cleanup: %v", port, err)
			}
		}
	}

	// Delete the bridge
	return m.DeleteBridge(bridgeName)
}