# Atomic OVS Bridge Handover

A Go program for atomically moving network interfaces from NetworkManager control to Open vSwitch (OVS) bridges while maintaining connectivity.

## Features

- **Atomic Operation**: Minimizes network downtime during bridge creation
- **NetworkManager Integration**: Uses D-Bus to introspect current network configuration
- **Automatic Rollback**: Comprehensive error handling with automatic rollback on failure
- **Connectivity Validation**: Tests network connectivity after handover
- **Comprehensive Logging**: Detailed logging of every operation
- **Dry-Run Mode**: Test what would be done without making changes

## Prerequisites

- Root privileges
- Open vSwitch installed and running
- NetworkManager running
- Go 1.23.5+ (for development)

## Installation

The program is pre-compiled in `/opt/git/atomic-bridge-handover/atomic-bridge-handover`

## Usage

### Basic Usage

```bash
# Dry run to see what would happen
sudo ./atomic-bridge-handover -dry-run -verbose

# Create OVS bridge 'ovsbr0' and move 'enp2s0' to it
sudo ./atomic-bridge-handover

# Use custom bridge and interface names
sudo ./atomic-bridge-handover -bridge mybr0 -interface eth1
```

### Command Line Options

- `-bridge string`: Name of the OVS bridge to create (default "ovsbr0")
- `-interface string`: Name of the interface to add to bridge (default "enp2s0") 
- `-verbose`: Enable verbose logging
- `-dry-run`: Show what would be done without executing

### Example: Server 1424 Current Setup

```bash
# Current config: enp2s0 with IP 10.88.88.100/24
# This will create ovsbr0 bridge with the same IP configuration
sudo ./atomic-bridge-handover -bridge ovsbr0 -interface enp2s0 -verbose
```

## How It Works

1. **Introspection**: Uses NetworkManager D-Bus API to read current IP configuration
2. **Bridge Creation**: Creates OVS bridge and adds physical interface as port
3. **NetworkManager Deactivation**: Removes interface from NetworkManager control
4. **IP Migration**: Applies original IP configuration to the bridge
5. **Validation**: Tests connectivity by pinging the gateway
6. **Rollback**: On any failure, automatically reverts all changes

## Network Configuration

### Before Handover
```
enp2s0: 10.88.88.100/24 (managed by NetworkManager)
Gateway: 10.88.88.1
```

### After Handover
```
ovsbr0: 10.88.88.100/24 (manual configuration)
  └── enp2s0 (OVS port)
Gateway: 10.88.88.1 via ovsbr0
```

## Safety Features

- **Automatic Rollback**: If any step fails, all previous changes are reverted
- **Connectivity Testing**: Validates network access after handover
- **Comprehensive Logging**: Every operation is logged for debugging
- **Dry-Run Mode**: Test without making actual changes

## Troubleshooting

### Connection Lost After Handover

If you lose connectivity, the program will attempt automatic rollback. If that fails:

```bash
# Manual rollback steps
sudo ovs-vsctl del-port ovsbr0 enp2s0
sudo ovs-vsctl del-br ovsbr0
sudo systemctl restart NetworkManager
```

### Permission Errors

Ensure you're running as root:
```bash
sudo ./atomic-bridge-handover [options]
```

### OVS Not Running

Start Open vSwitch:
```bash
sudo systemctl start openvswitch-switch
sudo systemctl enable openvswitch-switch
```

### NetworkManager D-Bus Errors

Restart NetworkManager:
```bash
sudo systemctl restart NetworkManager
```

## Development

### Building from Source

```bash
cd /opt/git/atomic-bridge-handover
export PATH=/usr/local/go/bin:$PATH
go build -o atomic-bridge-handover main.go
```

### Dependencies

- `github.com/godbus/dbus/v5` - D-Bus communication with NetworkManager

## Architecture

The program uses a structured approach with these key components:

- **BridgeHandover struct**: Main orchestration logic
- **D-Bus Integration**: NetworkManager introspection and control
- **OVS Management**: Bridge and port creation via ovs-vsctl
- **Rollback System**: LIFO stack of rollback operations
- **Logging**: Comprehensive operation logging

## Example Output

```
[BRIDGE-HANDOVER] 2025/08/15 09:28:33 Starting atomic bridge handover: enp2s0 -> ovsbr0
[BRIDGE-HANDOVER] 2025/08/15 09:28:33 Introspecting current connection on interface enp2s0
[BRIDGE-HANDOVER] 2025/08/15 09:28:33 Found target device at path: /org/freedesktop/NetworkManager/Devices/2
[BRIDGE-HANDOVER] 2025/08/15 09:28:33 Current IP config - IP: 10.88.88.100, Gateway: 10.88.88.1, Netmask: 255.255.255.0
[BRIDGE-HANDOVER] 2025/08/15 09:28:33 Creating OVS bridge: ovsbr0
[BRIDGE-HANDOVER] 2025/08/15 09:28:34 OVS bridge ovsbr0 created successfully
[BRIDGE-HANDOVER] 2025/08/15 09:28:34 Configuring IP on bridge ovsbr0
[BRIDGE-HANDOVER] 2025/08/15 09:28:34 Atomic handover completed successfully
[BRIDGE-HANDOVER] 2025/08/15 09:28:35 Connectivity test passed - gateway 10.88.88.1 is reachable
```

## Important Notes

- **NEVER use `ovs-vsctl` manually** after running this program - it may conflict with the bridge configuration
- Always test with `-dry-run` first in production environments
- The program requires exclusive access to the target interface
- Ensure OVS is properly configured before running