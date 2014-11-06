shuttle TCP Proxy
=======

Shuttle is a TCP proxy and load balancer, which can be updated live via an HTTP
interface.

## Usage

Shuttle's external dependencies consist of "github.com/gorilla/mux" and
"launchpad.net/gocheck", the latter required only for testing.


Shuttle can be started with a default configuration, as well as its last
configuration state. The -state configuration is updated on changes to the
internal config. If the state config file doesn't exist, the default is loaded.
The default config is never written to by shuttle.

    $ ./shuttle -config default_config.json -state state_config.json -http 127.0.0.1:9090


The current config can be queried via the `/_config` endpoint. This returns a
json list of Services and their Backends, which can be saved directly as a
config file. The configuration itself is a list of Services, each of which may
contain a list of backends.

A GET request to the path `/` returns an extended json config containing live
stats from all Services. Individual services can be queried by their name,
`/service_name`, returning just the json stats for that service. Backend stats
can be queried directly as well via the path `service_name/backend_name`.

Issuing a PUT with a json config to the service's endpoint will create, or
replace that service. If the service config is identical to the running config
OR the query parameter `backends_only` is supplied, only the backends will be
updated. Any changes to the running service require shutting down the listener,
and starting a new service, which will create a very small period where
connection may be rejected.

	Service Configuration JSON format.
	{
		"name":            Service name, also assigned by request path /service_name.
		"address":         Service address in host:port format.
		"backends":        [list of backend configurations].
		"balance":         Balance algorothm, "RR"(round robin) or "LC"(least connected). Default is RR.
		"check_interval":  Interval between health checks in seconds. Heath checks disabled if set to 0.
		"fall":            Number of failed checks before a backend is marked DOWN,
		"rise":            Number of successful checks before a service is marked UP.
		"client_timeout":  Timeout in seconds for Read or Write operations on the client connection.
		"server_timeout":  Timeout in seconds for a Read or Write opertaions on the server connection.
		"connect_timeout": Timeout in seconds gto connect to the backend. Also used for health checks.
	}


Issuing a PUT with a json config to the backend's endpoint will create or
replace that backend. Existing connections relying on the old config will
continue to run until the connection is closed.

	Backend Configuration JSON format
	{
		"name":          Backend name. Also assigned via /service_name/backend_name.
		"address":       Backend address in host:port format.
		"check_address": Address for health checks in host:port format. Currently only does a TCP connect.
		"weight":        Weight of this connection for round robin balancing. We send "weight" number of successive connection to each backend. 
	}



## TODO

- Connection limits (per service and/or per backend)
- Mark backend down after non-check connection failures (still requires checks to bring it back up)
- Health check via http, or tcp call/resp pattern
- Protocol bridging? e.g. `TCP<->unix`, `UDP->TCP`?!
- better logging
