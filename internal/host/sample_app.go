package host

import (
	"fmt"

	"github.com/jnnngs/3270Web/internal/sampleapps"
)

// GoSampleAppHost runs a Go-based sample application and connects using s3270.
type GoSampleAppHost struct {
	AppID    string
	Port     int
	ExecPath string
	Args     []string
	Target   string

	server *sampleapps.Server
	client *S3270
}

const sampleAppClientNotStarted = "sample app client not started"

func NewGoSampleAppHost(appID string, port int, execPath string, args []string, target string) (*GoSampleAppHost, error) {
	if appID == "" {
		return nil, fmt.Errorf("missing sample app id")
	}
	if port <= 0 {
		return nil, fmt.Errorf("invalid sample app port %d", port)
	}
	if execPath == "" {
		return nil, fmt.Errorf("missing s3270 executable path")
	}
	if target == "" {
		return nil, fmt.Errorf("missing sample app target host")
	}
	return &GoSampleAppHost{
		AppID:    appID,
		Port:     port,
		ExecPath: execPath,
		Args:     args,
		Target:   target,
	}, nil
}

func (h *GoSampleAppHost) Start() error {
	if h.server == nil {
		server, err := sampleapps.StartServer(h.AppID, h.Port)
		if err != nil {
			return err
		}
		h.server = server
	}
	h.client = NewS3270(h.ExecPath, h.Args...)
	h.client.TargetHost = h.Target
	if err := h.client.Start(); err != nil {
		h.server.Stop()
		h.server = nil
		return err
	}
	if err := h.client.UpdateScreen(); err != nil {
		_ = h.client.Stop()
		h.client = nil
		_ = h.server.Stop()
		h.server = nil
		return err
	}
	return nil
}

func (h *GoSampleAppHost) Stop() error {
	if h.client != nil {
		_ = h.client.Stop()
		h.client = nil
	}
	if h.server != nil {
		_ = h.server.Stop()
		h.server = nil
	}
	return nil
}

func (h *GoSampleAppHost) IsConnected() bool {
	if h.client == nil {
		return false
	}
	return h.client.IsConnected()
}

func (h *GoSampleAppHost) UpdateScreen() error {
	if h.client == nil {
		return fmt.Errorf(sampleAppClientNotStarted)
	}
	return h.client.UpdateScreen()
}

func (h *GoSampleAppHost) GetScreen() *Screen {
	if h.client == nil {
		return &Screen{}
	}
	return h.client.GetScreen()
}

func (h *GoSampleAppHost) SendKey(key string) error {
	if h.client == nil {
		return fmt.Errorf(sampleAppClientNotStarted)
	}
	return h.client.SendKey(key)
}

func (h *GoSampleAppHost) SubmitScreen() error {
	if h.client == nil {
		return fmt.Errorf(sampleAppClientNotStarted)
	}
	return h.client.SubmitScreen()
}

func (h *GoSampleAppHost) SubmitUnformatted(data string) error {
	if h.client == nil {
		return fmt.Errorf(sampleAppClientNotStarted)
	}
	return h.client.SubmitUnformatted(data)
}
