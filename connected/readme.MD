# connected

wireguard router and connection manager

- automatically connect to random server from configured connections at startup
- monitor connection and move to next server when current dies
- NAT connection for network
  - clients point default gateway to this router for route all traffic through VPN
- kill switch if VPN connection drops. No connectivity without active VPN.
- enable queries to VPN DNS server from network
- support ports forwarded from VPN server
  - static ports configured in conf file
  - dynamic ports configured by web api
- register VPN IP in dns
  - uses external dns registration tool to register in cloudflare hosted dns
###Config file
Possible locations:
- /run/secrets/connected.conf
- /etc/connected.conf
- folder specified in environment variable  CONFIG_FOLDER + connected.conf

###API endpoint
```
POST /cmd 
```
- Get current VPN IP
```json
// request
{
  "action"  : "ip"
}
// response
{
  "status" : "ok", 
  "ip" : "201.102.150.105"
}
```
- Drop connection to current VPN server and connect to next VPN server.
- Return new VPN IP
```json
// request
{
  "action"  : "next"
}
// response
{
  "status" : "ok", 
  "ip" : "201.102.150.106"
}
```
- add port forward
```json
// request
{
  "action"    : "enable",
  "host"      : "sabnzbd",
  "proto"     : "tcp",
  "ext_port"  : "54372",
  "int_port"  : "8080",
  "ip"        : "192.168.1.50"
} 
// response
{
  "status"  : "ok" // or "missing required field", "invalid host"
}
```
- remove port forward
```json
// request
{
  "action"    : "disable",
  "host"      : "sabnzbd",
  "proto"     : "tcp",
  "ext_port"  : "54372",
  "int_port"  : "8080",
  "ip"        : "192.168.1.50"
} 
// response
{
  "status"  : "ok" // or "no match found", 
}
```
- remove all dynamic port forwards
```json
// request
{
  "action"  : "cleanup"
}
// response
{
  "status" : "ok", 
}
```