package main

import (
	"encoding/json"
	"log"
	"net"
	"sync"
	"time"
)

var (
	Registry = ServiceRegistry{
		svcs: make(map[string]*Service, 0),
	}
)

type Service struct {
	sync.Mutex
	Name          string
	Addr          string
	Backends      []*Backend
	Balance       string
	Inter         uint64
	ErrLim        uint64
	Fall          uint64
	Rise          uint64
	ClientTimeout time.Duration
	ServerTimeout time.Duration
	DialTimeout   time.Duration
	Sent          uint64
	Rcvd          uint64
	Errors        uint64

	// Next returns the backend to be used for a new connection according our
	// load balancing algorithm
	next func() *Backend
	// the last backend we used and the number of times we used it
	lastBackend int
	lastCount   int

	// Each Service owns it's own netowrk listener
	listener net.Listener
}

// Stats returned about a service
type ServiceStat struct {
	Name          string        `json:"name"`
	Addr          string        `json:"address"`
	Backends      []BackendStat `json:"backends"`
	Balance       string        `json:"balance"`
	Inter         uint64        `json:"check_interval"`
	ErrLim        uint64        `json:"error_limit"`
	Fall          uint64        `json:"fall"`
	Rise          uint64        `json:"rise"`
	ClientTimeout uint64        `json:"client_timeout"`
	ServerTimeout uint64        `json:"server_timeout"`
	DialTimeout   uint64        `json:"connect_timeout"`
	Sent          uint64        `json:"sent"`
	Rcvd          uint64        `json:"received"`
	Errors        uint64        `json:"errors"`
}

// Subset of service fields needed for configuration.
type ServiceConfig struct {
	Name          string          `json:"name"`
	Addr          string          `json:"address"`
	Backends      []BackendConfig `json:"backends"`
	Balance       string          `json:"balance"`
	Inter         uint64          `json:"check_interval"`
	ErrLim        uint64          `json:"error_limit"`
	Fall          uint64          `json:"fall"`
	Rise          uint64          `json:"rise"`
	ClientTimeout uint64          `json:"client_timeout"`
	ServerTimeout uint64          `json:"server_timeout"`
	DialTimeout   uint64          `json:"connect_timeout"`
}

// Create a Service from a config struct
func NewService(cfg ServiceConfig) *Service {
	s := &Service{
		Name:          cfg.Name,
		Addr:          cfg.Addr,
		Inter:         cfg.Inter,
		ErrLim:        cfg.ErrLim,
		Fall:          cfg.Fall,
		Rise:          cfg.Rise,
		ClientTimeout: time.Duration(cfg.ClientTimeout) * time.Second,
		ServerTimeout: time.Duration(cfg.ServerTimeout) * time.Second,
		DialTimeout:   time.Duration(cfg.DialTimeout) * time.Second,
	}

	if s.Inter == 0 {
		s.Inter = 2
	}
	if s.Rise == 0 {
		s.Rise = 2
	}
	if s.Fall == 0 {
		s.Fall = 2
	}

	for _, b := range cfg.Backends {
		s.Add(NewBackend(b))
	}

	// set balance here so the correct balance func
	// gets assigned to Service.next
	s.SetBalance(cfg.Balance)

	return s
}

func (s *Service) Stats() ServiceStat {
	s.Lock()
	defer s.Unlock()

	stats := ServiceStat{
		Name:          s.Name,
		Addr:          s.Addr,
		Balance:       s.Balance,
		Inter:         s.Inter,
		ErrLim:        s.ErrLim,
		Fall:          s.Fall,
		Rise:          s.Rise,
		ClientTimeout: uint64(s.ClientTimeout / time.Second),
		ServerTimeout: uint64(s.ServerTimeout / time.Second),
		DialTimeout:   uint64(s.DialTimeout / time.Second),
	}

	for _, b := range s.Backends {
		stats.Backends = append(stats.Backends, b.Stats())
		stats.Sent += b.Sent
		stats.Rcvd += b.Rcvd
		stats.Errors += b.Errors
	}

	return stats
}

func (s *Service) Config() ServiceConfig {
	s.Lock()
	defer s.Unlock()

	config := ServiceConfig{
		Name:          s.Name,
		Addr:          s.Addr,
		Balance:       s.Balance,
		Inter:         s.Inter,
		ErrLim:        s.ErrLim,
		Fall:          s.Fall,
		Rise:          s.Rise,
		ClientTimeout: uint64(s.ClientTimeout / time.Second),
		ServerTimeout: uint64(s.ServerTimeout / time.Second),
		DialTimeout:   uint64(s.DialTimeout / time.Second),
	}
	for _, b := range s.Backends {
		config.Backends = append(config.Backends, b.Config())
	}

	return config
}

// Change the service's balancing algorithm
func (s *Service) SetBalance(balance string) {
	s.Lock()
	defer s.Unlock()

	switch balance {
	case "RR", "":
		s.next = s.roundRobin
	case "LC":
		s.next = s.leastConn
	default:
		log.Printf("invalid balancing algorithm '%s'", balance)
	}
}

// Fill out and verify service
func (s *Service) Start() (err error) {
	s.Lock()
	defer s.Unlock()

	s.listener, err = newTimeoutListener(s.Addr, s.ClientTimeout)
	if err != nil {
		return err
	}

	if s.Backends == nil {
		s.Backends = make([]*Backend, 0)
	}

	s.run()
	return nil
}

func (s Service) String() string {
	j, err := json.MarshalIndent(s.Stats(), "", "  ")
	if err != nil {
		log.Println("Service JSON error:", err)
		return ""
	}
	return string(j)
}

func (s *Service) Get(name string) *Backend {
	s.Lock()
	defer s.Unlock()

	for _, b := range s.Backends {
		if b.Name == name {
			return b
		}
	}
	return nil
}

// Add a backend to this service
func (s *Service) Add(backend *Backend) {
	s.Lock()
	defer s.Unlock()

	backend.Up = true
	backend.rwTimeout = s.ServerTimeout
	backend.dialTimeout = s.DialTimeout
	backend.checkInterval = time.Duration(s.Inter) * time.Second

	// replace an exiting backend if we have it.
	for i, b := range s.Backends {
		if b.Name == backend.Name {
			b.Stop()
			s.Backends[i] = backend
			backend.Start()
			return
		}
	}

	s.Backends = append(s.Backends, backend)
	backend.Start()
}

// Remove a Backend by name
func (s *Service) Remove(name string) *Backend {
	s.Lock()
	defer s.Unlock()

	for i, b := range s.Backends {
		if b.Name == name {
			last := len(s.Backends) - 1
			deleted := b
			s.Backends[i], s.Backends[last] = s.Backends[last], nil
			s.Backends = s.Backends[:last]
			deleted.Stop()
			return deleted
		}
	}
	return nil
}

// Start the Service's Accept loop
func (s *Service) run() {
	go func() {
		for {
			conn, err := s.listener.Accept()
			if err != nil {
				if err := err.(*net.OpError); err.Temporary() {
					continue
				}
				// we must be getting shut down
				return
			}

			backend := s.next()
			if backend == nil {
				log.Println("error: no backend for", s.Name)
				conn.Close()
				continue
			}

			go backend.Proxy(conn)
		}
	}()
}

// Stop the Service's Accept loop by closing the Listener,
// and stop all backends for this service.
func (s *Service) Stop() {
	s.Lock()
	defer s.Unlock()

	for _, backend := range s.Backends {
		backend.Stop()
	}

	err := s.listener.Close()
	if err != nil {
		log.Println(err)
	}
}
