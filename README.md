# Atomic OVS Bridge Handover

A Go program for atomically transferring network interfaces from standard NetworkManager connections to Open vSwitch bridges while maintaining connectivity.

## Features

- **Atomic Handover**: Seamless transition with minimal downtime
- **D-Bus Integration**: Uses NetworkManager D-Bus API for network management
- **OVS Integration**: Creates and manages Open vSwitch bridge structures
- **Rollback Support**: Automatic rollback on failure
- **Comprehensive Logging**: Detailed logging with verbose mode
- **Dry Run Mode**: Test operations without making changes

## Prerequisites

1. **Root Access**: Must be run as root
2. **NetworkManager**: Must be running and accessible via D-Bus
3. **Open vSwitch**: Must be installed and running
4. **Go 1.23+**: For compilation

### Installing Open vSwitch

On Debian/Ubuntu:
```bash
apt update
apt install openvswitch-switch openvswitch-common
systemctl enable openvswitch-switch
systemctl start openvswitch-switch
```

## Usage

### Basic Usage

```bash
# Dry run to see what would happen
./atomic-handover --dry-run --verbose --interface enp2s0 --bridge ovsbr0

# Perform actual handover
./atomic-handover --interface enp2s0 --bridge ovsbr0
```

### Command Line Options

- `--interface string`: Network interface to move to bridge (default "enp2s0")
- `--bridge string`: Name of the OVS bridge to create (default "ovsbr0")  
- `--verbose`: Enable verbose debug logging
- `--dry-run`: Show what would be done without executing

### Example

```bash
# Move enp2s0 to a new OVS bridge called 'br0'
./atomic-handover --interface enp2s0 --bridge br0 --verbose
```

## How It Works

1. **Introspection**: Analyzes current NetworkManager configuration
2. **OVS Setup**: Creates OVS bridge, port, and interface structure
3. **NM Integration**: Creates NetworkManager OVS connection profiles
4. **Atomic Switch**: Deactivates original connection and activates OVS bridge
5. **Verification**: Confirms connectivity and proper configuration
6. **Rollback**: Automatic cleanup on any failure

## Architecture

```
atomic-bridge-handover/
├── main.go                    # Main application with CLI
├── internal/
│   ├── logging/
│   │   └── logger.go         # Structured logging
│   ├── dbus/
│   │   └── client.go         # NetworkManager D-Bus client
│   ├── ovs/
│   │   └── manager.go        # OVS management
│   └── handover/
│       └── manager.go        # Orchestration logic
└── go.mod
```

## Network Flow

```
Before:
enp2s0 → NetworkManager → IP Configuration

After:
enp2s0 → OVS Port → OVS Bridge → NetworkManager → IP Configuration
```

## Safety Features

- **Prerequisites Validation**: Checks all requirements before starting
- **Atomic Operations**: All-or-nothing approach
- **Automatic Rollback**: Reverts changes on failure
- **Signal Handling**: Graceful shutdown on interruption
- **Connectivity Verification**: Confirms network is working after handover

## Troubleshooting

### OVS Not Available
Ensure Open vSwitch is installed and running:
```bash
systemctl status openvswitch-switch
ovs-vsctl show
```

### Permission Denied
Run as root:
```bash
sudo ./atomic-handover ...
```

### NetworkManager Issues
Check NetworkManager status:
```bash
systemctl status NetworkManager
```

## Development

### Building
```bash
go mod tidy
go build -o atomic-handover
```

### Testing
```bash
# Run with dry-run first
./atomic-handover --dry-run --verbose

# Check compilation
go build -v ./...
```

## Important Notes

- **NEVER use ovs-vsctl directly** in environments where this tool is deployed
- Always test with `--dry-run` first
- Ensure you have backup network access before running
- Monitor logs carefully during operation
- Have a rollback plan in case of issues

## License

This project is developed for server 1424 OVS bridge management.