package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"reflect"
	"time"
)

var (
	// Status400s is a set of response codes to set an Error page for all 4xx responses.
	Status400s = []int{400, 401, 402, 403, 404, 405, 406, 407, 408, 409, 410, 411, 412, 413, 414, 415, 416, 417, 418}
	// Status500s is a set of response codes to set an Error page for all 5xx responses.
	Status500s = []int{500, 501, 502, 503, 504, 505}
)

// Client is an http client for communicating with the shuttle server api
type Client struct {
	httpClient  *http.Client
	shuttleAddr string
}

// Config is the global configuration for all Services.
// Defaults set here can be overridden by individual services.
type Config struct {
	// Balance method
	// Valid values are "RR" for RoundRobin, the default, and "LC" for
	// LeastConnected.
	Balance string `json:"balance,omitempty"`

	// CheckInterval is in time in milliseconds between service health checks.
	CheckInterval int `json:"check_interval"`

	// Fall is the number of failed health checks before a service is marked.
	Fall int `json:"fall"`

	// Rise is the number of successful health checks before a down service is
	// marked up.
	Rise int `json:"rise"`

	// ClientTimeout is the maximum inactivity time, in milliseconds, for a
	// connection to the client before it is closed.
	ClientTimeout int `json:"client_timeout"`

	// ServerTimeout is the maximum inactivity time, in milliseconds, for a
	// connection to the backend before it is closed.
	ServerTimeout int `json:"server_timeout"`

	// DialTimeout is the timeout in milliseconds for connections to the
	// backend service, including name resolution.
	DialTimeout int `json:"connect_timeout"`

	// Services is a slice of ServiceConfig for each service. A service
	// corresponds to one listening connection, and a number of backends to
	// proxy.
	Services []ServiceConfig `json:"services"`
}

// Marshal returns an entire config as a json []byte.
func (c *Config) Marshal() []byte {
	js, _ := json.Marshal(c)
	return js
}

// The string representation of a config is in json.
func (c *Config) String() string {
	return string(c.Marshal())
}

// BackendConfig defines the parameters unique for individual backends.
type BackendConfig struct {
	// Name must be unique for this service.
	// Used for reference and for the HTTP API.
	Name string `json:"name"`

	// Addr must in the form ip:port
	Addr string `json:"address"`

	// CheckAddr must be in the form ip:port.
	// A TCP connect is performed against this address to determine server
	// availability. If this is empty, no checks will be performed.
	CheckAddr string `json:"check_address"`

	// Weight is always used for RoundRobin balancing. Default is 1
	Weight int `json:"weight"`

	// Network must be "tcp" or "udp".
	// Default is "tcp"
	Network string `json:"network,omitempty"`
}

func (b BackendConfig) Equal(other BackendConfig) bool {
	if other.Weight == 0 {
		other.Weight = 1
	}

	if b.Weight == 0 {
		b.Weight = 1
	}

	if b.Network == "" {
		b.Network = "tcp"
	}

	if other.Network == "" {
		other.Network = "tcp"
	}

	return b == other
}

func (b *BackendConfig) Marshal() []byte {
	js, _ := json.Marshal(b)
	return js
}

func (b *BackendConfig) String() string {
	return string(b.Marshal())
}

// Subset of service fields needed for configuration.
type ServiceConfig struct {
	// Name is the unique name of the service. This is used only for reference
	// and in the HTTP API.
	Name string `json:"name"`

	// Addr is the listening address for this service. Must be in the form
	// "ip:addr"
	Addr string `json:"address"`

	// Virtualhosts is a set of virtual hostnames for which this service should
	// handle HTTP requests.
	VirtualHosts []string `json:"virtual_hosts,omitempty"`

	// Balance method
	// Valid values are "RR" for RoundRobin, the default, and "LC" for
	// LeastConnected.
	Balance string `json:"balance,omitempty"`

	// CheckInterval is in time in milliseconds between service health checks.
	CheckInterval int `json:"check_interval"`

	// Fall is the number of failed health checks before a service is marked.
	Fall int `json:"fall"`

	// Rise is the number of successful health checks before a down service is
	// marked up.
	Rise int `json:"rise"`

	// ClientTimeout is the maximum inactivity time, in milliseconds, for a
	// connection to the client before it is closed.
	ClientTimeout int `json:"client_timeout"`

	// ServerTimeout is the maximum inactivity time, in milliseconds, for a
	// connection to the backend before it is closed.
	ServerTimeout int `json:"server_timeout"`

	// DialTimeout is the timeout in milliseconds for connections to the
	// backend service, including name resolution.
	DialTimeout int `json:"connect_timeout"`

	// ErrorPages are responses to be returned for HTTP error codes. Each page
	// is defined by a URL mapped and is mapped to a list of error codes that
	// should return the content at the URL. Error pages are retrieved ahead of
	// time if possible, and cached.
	ErrorPages map[string][]int `json:"error_pages,omitempty"`

	// Network must be "tcp" or "udp".
	// Default is "tcp"
	Network string `json:"network,omitempty"`

	// Backends is a list of all servers handling connections for this service.
	Backends []BackendConfig `json:"backends,omitempty"`
}

// Compare a service's settings, ignoring individual backends.
func (s ServiceConfig) Equal(other ServiceConfig) bool {
	// just remove the backends and compare the rest
	s.Backends = nil
	other.Backends = nil

	// FIXME: Normalize default in one place!

	if s.Balance != other.Balance {
		if s.Balance == "" && other.Balance == "RR" {
			other.Balance = ""
		} else if s.Balance == "RR" && other.Balance == "" {
			other.Balance = "RR"
		}
	}

	if s.CheckInterval == 0 {
		s.CheckInterval = 2000
	}
	if other.CheckInterval == 0 {
		other.CheckInterval = 2000
	}
	if s.Rise == 0 {
		s.Rise = 2
	}
	if other.Rise == 0 {
		other.Rise = 2
	}
	if s.Fall == 0 {
		s.Fall = 2
	}
	if other.Fall == 0 {
		other.Fall = 2
	}

	if s.Network == "" {
		s.Network = "tcp"
	}

	// We handle backends separately
	s.Backends = nil
	other.Backends = nil

	return reflect.DeepEqual(s, other)
}

// Check for equality including backends
func (s ServiceConfig) DeepEqual(other ServiceConfig) bool {
	if len(s.Backends) != len(other.Backends) {
		return false
	}

	if !s.Equal(other) {
		return false
	}

	for _, a := range s.Backends {
	NEXT:
		for _, b := range other.Backends {
			if a.Name == b.Name {
				if a.Equal(b) {
					break NEXT
				} else {
					return false
				}
			}
		}
	}

	return true
}

func (b *ServiceConfig) Marshal() []byte {
	js, _ := json.Marshal(b)
	return js
}

func (b *ServiceConfig) String() string {
	return string(b.Marshal())
}

// An http client for communicating with the shuttle server.
func NewClient(addr string) *Client {
	return &Client{
		httpClient:  &http.Client{Timeout: 2 * time.Second},
		shuttleAddr: addr,
	}
}

// GetConfig retrieves the configuration for a running shuttle server.
func (c *Client) GetConfig() (*Config, error) {

	req, err := http.NewRequest("GET", fmt.Sprintf("http://%s/_config", c.shuttleAddr), nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	config := &Config{}
	err = json.Unmarshal(body, config)
	if err != nil {
		return nil, err
	}

	return config, nil
}

// UpdateService updates a service on a running shuttle server.
func (c *Client) UpdateService(name string, service *ServiceConfig) error {

	js, err := json.Marshal(service)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Post(fmt.Sprintf("http://%s/%s", c.shuttleAddr, name), "application/json",
		bytes.NewBuffer(js))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to register service with shuttle: %s", resp.Status)
	}
	return nil
}

// UnregisterService removes a service from a running shuttle server.
func (c *Client) UnregisterService(service *ServiceConfig) error {
	js, err := json.Marshal(service)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("DELETE", fmt.Sprintf("http://%s/%s", c.shuttleAddr, service.Name), bytes.NewBuffer(js))
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.New(fmt.Sprintf("failed to unregister service: %s", resp.Status))
	}
	return nil
}

// UnregisterBackend removes a backend from its service on a running shuttle server.
func (c *Client) UnregisterBackend(service, backend string) error {
	req, err := http.NewRequest("DELETE", fmt.Sprintf("http://%s/%s/%s", c.shuttleAddr, service, backend), nil)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.New(fmt.Sprintf("failed to unregister backend: %s", resp.Status))
	}
	return nil
}
