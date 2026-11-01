#!/usr/bin/env bash

# Advanced users can make use of wireguard to hop into the STACKIT
# internal network straight from their workstation.
# Others just use SSH.

DEBIAN_FRONTEND=noninteractive apt-get install -y wireguard # just to make sure tooling is there

set -euo pipefail

WG_ENDPOINT=$(sed -r 's/\s+//g' /etc/fqdn)
WG_IFACE=wg0
WG_NET=192.168.123.0/24
WG_SERVER_IP=192.168.123.1/24
WG_CLIENT_IP=192.168.123.2/32
WG_PORT=51820
OUT_IFACE=enp3s0
SERVER_CONF=/etc/wireguard/wg0.conf
CLIENT_CONF=/home/ubuntu/jumphost.conf

wg_server_private_key=$(wg genkey)
wg_server_public_key=$(echo "$wg_server_private_key" | wg pubkey)
wg_client_private_key=$(wg genkey)
wg_client_public_key=$(echo "$wg_client_private_key" | wg pubkey)

sysctl -w net.ipv4.ip_forward=1
mkdir -p /etc/wireguard

cat > "$SERVER_CONF" <<SERVER
[Interface]
PrivateKey = $wg_server_private_key
Address = $WG_SERVER_IP
ListenPort = $WG_PORT

PostUp   = iptables -t nat -A POSTROUTING -s $WG_NET -o $OUT_IFACE -j MASQUERADE
PostDown = iptables -t nat -D POSTROUTING -s $WG_NET -o $OUT_IFACE -j MASQUERADE

PostUp   = iptables -A FORWARD -i $WG_IFACE -j ACCEPT
PostDown = iptables -D FORWARD -i $WG_IFACE -j ACCEPT

PostUp   = iptables -A FORWARD -o $WG_IFACE -m state --state RELATED,ESTABLISHED -j ACCEPT
PostDown = iptables -D FORWARD -o $WG_IFACE -m state --state RELATED,ESTABLISHED -j ACCEPT

PostUp = iptables -A FORWARD -p tcp --dport 22 -j ACCEPT
PostUp = iptables -A FORWARD -p udp --dport 51820 -j ACCEPT

[Peer]
PublicKey = $wg_client_public_key
AllowedIPs = $WG_CLIENT_IP
SERVER

chmod 0600 "$SERVER_CONF"

cat > "$CLIENT_CONF" <<CLIENT
# This wireguard config file allows you to access your STACKIT internal network
# via your jumphost. It was generated during cloud-init in the VM.
# See $SERVER_CONF for the endpoint configuration.
#
# To use it, basically run this on your computer:
#  > scp $WG_ENDPOINT:$CLIENT_CONF .
#  > sudo wg-quick up jumphost.conf

[Interface]
PrivateKey = $wg_client_private_key
Address = $WG_CLIENT_IP

[Peer]
PublicKey = $wg_server_public_key
Endpoint = $WG_ENDPOINT:$WG_PORT
AllowedIPs = 0.0.0.0/0
PersistentKeepalive = 25
CLIENT


systemctl enable wg-quick@$WG_IFACE
systemctl restart wg-quick@$WG_IFACE

