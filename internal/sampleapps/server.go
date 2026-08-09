package sampleapps

import (
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
)

type Server struct {
	appID    string
	port     int
	listener net.Listener
	stopOnce sync.Once
	done     chan struct{}
}

type handler func(net.Conn)

func StartServer(appID string, port int) (*Server, error) {
	return StartServerOn(appID, fmt.Sprintf("127.0.0.1:%d", port))
}

// StartServerOn runs a sample app on an explicit address.
//
// StartServer binds loopback, which is what a sample app should be: it is a
// listener this process opens on the operator's behalf, and it has no business
// being reachable from the network. This exists for tests that need to reach
// one the way a real host is reached, since the terminal refuses to be pointed
// at loopback at all.
func StartServerOn(appID, addr string) (*Server, error) {
	appHandler := handlerFor(appID)
	if appHandler == nil {
		return nil, fmt.Errorf("unknown sample app %q", appID)
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("sample app %s failed to listen on %s: %w", appID, addr, err)
	}
	server := &Server{
		appID:    appID,
		port:     listener.Addr().(*net.TCPAddr).Port,
		listener: listener,
		done:     make(chan struct{}),
	}
	go server.serve(appHandler)
	return server, nil
}

func (s *Server) serve(appHandler handler) {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.done:
				return
			default:
			}
			if errors.Is(err, net.ErrClosed) {
				return
			}
			log.Printf("sample app %s accept failed: %v", s.appID, err)
			return
		}
		go appHandler(conn)
	}
}

// Addr is where the server is listening.
//
// Useful when the port was left to the kernel, which is what a test does to
// avoid colliding with whatever else is on the machine.
func (s *Server) Addr() string { return s.listener.Addr().String() }

func (s *Server) Stop() error {
	var err error
	s.stopOnce.Do(func() {
		close(s.done)
		err = s.listener.Close()
	})
	return err
}

func handlerFor(appID string) handler {
	switch appID {
	case "app1":
		return handleApp1
	case "app2":
		return handleApp2
	case "app3":
		return handleApp3
	default:
		return nil
	}
}
