package config

import (
	"encoding/xml"
	"io"
	"os"
)

type Config struct {
	ExecPath     string       `xml:"exec-path"`
	Style        string       `xml:"style"`
	S3270Options S3270Options `xml:"s3270-options"`
	TargetHost   TargetHost   `xml:"target-host"`
}

type S3270Options struct {
	Charset string `xml:"charset"`
	Model   string `xml:"model"`
}

type TargetHost struct {
	AutoConnect bool   `xml:"autoconnect,attr"`
	Value       string `xml:",chardata"`
}

// Load reads configuration from an XML file.
func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var cfg Config
	decoder := xml.NewDecoder(f)
	// Handle ISO-8859-1 as simple pass-through (valid for ASCII subset or if we don't care about special chars in config)
	decoder.CharsetReader = func(charset string, input io.Reader) (io.Reader, error) {
		return input, nil
	}

	if err := decoder.Decode(&cfg); err != nil {
		return nil, err
	}

	// Set defaults
	if cfg.ExecPath == "" {
		cfg.ExecPath = "/usr/local/bin"
	}
	if cfg.S3270Options.Model == "" {
		cfg.S3270Options.Model = "3"
	}

	return &cfg, nil
}
