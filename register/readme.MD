#register
- Dynamically register and unregister containers in dns as they are started and stopped.
  - Monitor docker engine for container start and stop events.
  - Register in dns on start, unregister on stop.
- Registered record depends on network type.
  - Bridge and macvlan containers register A and PTR record.
  - Host-network containers register CNAME alias for docker host.
  - Non-network containers are skipped.
- Linux utility `nsupdate` used for dns registration.
- Optionally unregister all records registered by this service on service shutdown.

###Configure with environment variables
```
DOCKER_HOST             -> url of docker server
                           default: unix:///var/run/docker.sock
DOCKER_API_VERSION      -> docker api version to target
                           default: latest
DOCKER_CERT_PATH        -> path to load docker TLS certificates from
DOCKER_TLS_VERIFY       -> enable or disable TLS verification
REGISTER_DNS_SERVER     -> dns server to register with, use system resolver if unset
REGISTER_DOMAIN_NAME    -> dns domain to register. zone should exist on server and set to allow updates
                           default: "home"
REGISTER_PTR            -> register PTR record in reverse lookup zone. true or false
                           default: true
REGISTER_TTL            -> TTL (in seconds) of records when registering
                           default: 3600
REGISTER_CLEANUP        -> unregister dns records registered by this service on shutdown of service. true or false
                           default: true
```