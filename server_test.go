package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type testServer struct {
	addr     string
	sig      string
	listener net.Listener
	wg       *sync.WaitGroup
}

// Start a tcp server which responds with it's addr after every read.
func NewTestServer(addr string, c Tester) (*testServer, error) {
	s := &testServer{}
	s.wg = new(sync.WaitGroup)

	var err error

	// try really hard to bind this so we don't fail tests
	for i := 0; i < 3; i++ {
		s.listener, err = net.Listen("tcp", addr)
		if err == nil {
			break
		}
		c.Log("Listen error:", err)
		c.Log("Trying again in 1s...")
		time.Sleep(time.Second)
	}

	if err != nil {
		return nil, err
	}

	s.addr = s.listener.Addr().String()
	c.Log("listning on ", s.addr)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			conn, err := s.listener.Accept()
			if err != nil {
				return
			}

			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				defer conn.Close()
				buff := make([]byte, 1024)
				for {
					if _, err := conn.Read(buff); err != nil {
						if err != io.EOF {
							c.Logf("test server '%s' error: %s", s.addr, err)
						}
						return
					}
					if _, err := io.WriteString(conn, s.addr); err != nil {
						if err != io.EOF {
							c.Logf("test server '%s' error: %s", s.addr, err)
						}
						return
					}
				}
			}()
		}
	}()
	return s, nil
}

func (s *testServer) Stop() {
	s.listener.Close()
	// We may be imediately creating another identical server.
	// Wait until all goroutines return to ensure we can bind again.
	s.wg.Wait()
}

// Backend server for testing HTTP proxies
type testHTTPServer struct {
	addr     string
	name     string
	listener net.Listener
	server   *http.Server
	wg       *sync.WaitGroup
}

// Start a tcp server which responds with it's addr after every read.
func NewHTTPTestServer(addr string, c Tester) (*testHTTPServer, error) {
	s := &testHTTPServer{}
	s.wg = new(sync.WaitGroup)

	mux := http.NewServeMux()
	mux.HandleFunc("/addr", func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.WriteString(w, s.addr); err != nil {
			c.Logf("test server '%s' error: %s", s.addr, err)
		}
	})

	var err error

	// try really hard to bind this so we don't fail tests
	for i := 0; i < 3; i++ {
		s.listener, err = net.Listen("tcp", addr)
		if err == nil {
			break
		}
		c.Log("Listen error:", err)
		c.Log("Trying again in 1s...")
		time.Sleep(time.Second)
	}

	if err != nil {
		return nil, err
	}

	s.server = &http.Server{}
	s.server.Handler = mux
	s.addr = s.listener.Addr().String()

	if parts := strings.Split(s.addr, ":"); len(parts) == 2 {
		s.name = fmt.Sprintf("http-%s.server.test", parts[1])
	} else {
		c.Fatal("error naming http server")
	}

	c.Log("http listning on ", s.addr)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.server.Serve(s.listener)
	}()
	return s, nil
}

func (s *testHTTPServer) Stop() {
	s.listener.Close()
	// We may be imediately creating another identical server.
	// Wait until all goroutines return to ensure we can bind again.
	s.wg.Wait()
}
