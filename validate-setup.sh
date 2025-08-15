#!/bin/bash

# Validation script for atomic bridge handover
# Run this before executing the handover to ensure system is ready

echo "=== Atomic Bridge Handover - System Validation ==="
echo

# Check if running as root
if [[ $EUID -ne 0 ]]; then
   echo "❌ ERROR: This script must be run as root"
   exit 1
fi

echo "✅ Running as root"

# Check if OVS is installed and running
if ! command -v ovs-vsctl &> /dev/null; then
    echo "❌ ERROR: Open vSwitch not installed"
    echo "   Install with: apt install openvswitch-switch"
    exit 1
fi

echo "✅ Open vSwitch installed"

# Check if OVS is running
if ! systemctl is-active --quiet openvswitch-switch; then
    echo "❌ ERROR: Open vSwitch service not running"
    echo "   Start with: systemctl start openvswitch-switch"
    exit 1
fi

echo "✅ Open vSwitch service running"

# Check if NetworkManager is running
if ! systemctl is-active --quiet NetworkManager; then
    echo "❌ ERROR: NetworkManager service not running"
    echo "   Start with: systemctl start NetworkManager"
    exit 1
fi

echo "✅ NetworkManager service running"

# Check target interface
INTERFACE=${1:-enp2s0}
if ! ip link show "$INTERFACE" &> /dev/null; then
    echo "❌ ERROR: Interface $INTERFACE not found"
    echo "   Available interfaces:"
    ip link show | grep -E '^[0-9]+:' | cut -d: -f2 | tr -d ' '
    exit 1
fi

echo "✅ Interface $INTERFACE exists"

# Check if interface has IP
IP_INFO=$(ip addr show "$INTERFACE" | grep 'inet ' | head -1)
if [[ -z "$IP_INFO" ]]; then
    echo "❌ ERROR: Interface $INTERFACE has no IP address"
    echo "   Configure networking first"
    exit 1
fi

IP=$(echo "$IP_INFO" | awk '{print $2}')
echo "✅ Interface $INTERFACE has IP: $IP"

# Check if interface is managed by NetworkManager
NM_DEVICES=$(nmcli device show "$INTERFACE" 2>/dev/null)
if [[ $? -ne 0 ]]; then
    echo "⚠️  WARNING: Interface $INTERFACE not managed by NetworkManager"
    echo "   The program may not be able to introspect current configuration"
else
    echo "✅ Interface $INTERFACE managed by NetworkManager"
    
    # Show current connection info
    CONNECTION=$(nmcli device show "$INTERFACE" | grep 'GENERAL.CONNECTION:' | awk '{print $2}')
    if [[ -n "$CONNECTION" && "$CONNECTION" != "--" ]]; then
        echo "   Active connection: $CONNECTION"
    fi
fi

# Check gateway connectivity
GATEWAY=$(ip route | grep default | head -1 | awk '{print $3}')
if [[ -n "$GATEWAY" ]]; then
    echo "✅ Default gateway: $GATEWAY"
    
    # Test connectivity
    if ping -c 1 -W 3 "$GATEWAY" &> /dev/null; then
        echo "✅ Gateway connectivity test passed"
    else
        echo "⚠️  WARNING: Cannot ping gateway $GATEWAY"
        echo "   Network connectivity may be limited"
    fi
else
    echo "⚠️  WARNING: No default gateway found"
fi

# Check if target bridge already exists
BRIDGE=${2:-ovsbr0}
if ovs-vsctl br-exists "$BRIDGE" 2>/dev/null; then
    echo "⚠️  WARNING: Bridge $BRIDGE already exists"
    echo "   Ports on bridge:"
    ovs-vsctl list-ports "$BRIDGE" | sed 's/^/     /'
else
    echo "✅ Bridge $BRIDGE does not exist (ready to create)"
fi

echo
echo "=== Validation Summary ==="
echo "Target Interface: $INTERFACE ($IP)"
echo "Target Bridge: $BRIDGE"
echo "Gateway: ${GATEWAY:-none}"
echo

echo "✅ System is ready for atomic bridge handover"
echo
echo "Next steps:"
echo "1. Test with dry-run: ./atomic-bridge-handover -dry-run -verbose"
echo "2. Execute handover: ./atomic-bridge-handover -interface $INTERFACE -bridge $BRIDGE -verbose"