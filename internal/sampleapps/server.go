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
	appHandler := handlerFor(appID)
	if appHandler == nil {
		return nil, fmt.Errorf("unknown sample app %q", appID)
	}
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return nil, fmt.Errorf("sample app %s failed to listen on 127.0.0.1:%d: %w", appID, port, err)
	}
	server := &Server{
		appID:    appID,
		port:     port,
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
	default:
		return nil
	}
}
