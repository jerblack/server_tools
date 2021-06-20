# register
- Dynamically register and unregister containers in dns as they are started and stopped.
  - Monitor docker engine for container start and stop events.
  - Register in dns on start, unregister on stop.
- Registered record depends on network type.
  - Bridge and macvlan containers register A and PTR record.
  - Host-network containers register CNAME alias for docker host.
  - Non-network containers are skipped.
- Linux utility `nsupdate` used for dns registration.
- Optionally unregister all records registered by this service on service shutdown.

### environment variables
```
DOCKER_HOST             -> url of docker server
                           default: unix:///var/run/docker.sock
DOCKER_API_VERSION      -> docker api version to target
                           default: latest
DOCKER_CERT_PATH        -> path to load docker TLS certificates from
DOCKER_TLS_VERIFY       -> enable or disable TLS verification
REGISTER_DNS_SERVER     -> dns server to register with, use system resolver if unset
                           dns server should be able to resolve SOA and NS record for configured domain name.
REGISTER_DOMAIN_NAME    -> domain name will be appended to host name from docker engine and used to register dns records.
                           zone should exist on server and set to allow updates
                           default: "home"
REGISTER_PTR            -> register PTR record in reverse lookup zone. true or false
                           default: true
REGISTER_TTL            -> TTL (in seconds) of records when registering
                           default: 3600
REGISTER_CLEANUP        -> unregister dns records registered by this service on shutdown of service. true or false
                           default: true
REGISTER_CONFIG		    -> path to config file including file name
                           default: /etc/register.conf
```
Environment variables are prioritized over config file entries if they conflict.

### config file example
All lines optional. 
``` 
all docker-options are defined by docker sdk, including allowed values
docker-host = unix:///var/run/docker.sock
# unset for latest
docker-api-version = v1.41
# unused if unset
docker-cert-path = /var/lib/docker/certs/cert.cer
docker-tls-verify = false
# use system resolver if unset.  
register-dns-server = 192.168.0.35
register-domain-name = home
register-ptr = true
register-ttl = 3600
register-cleanup = true

# enable port forwarding by sending web requests to "connected" server api
# forward-server must be set if any forward rules are set 
# forward-server is "connected" server ip and http port
forward-server = 192.168.0.99:3099
# forward rules in format: forward = <hostname> <protocol> <ext port> <int port>
# hostname should match --hostname flag in docker run or hostname directive in docker-compose.yml
# can be set multiple times. set one line for each rule. 
# forward rules are enabled when container starts and disabled when container stops
forward = server1 tcp 54871 8080
forward = server1 udp 54871 8080
forward = server2 tcp 59663 9090
```