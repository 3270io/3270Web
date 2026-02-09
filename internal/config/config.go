package config

import (
	"encoding/xml"
	"io"
	"os"
	"strings"
)

type Config struct {
	ExecPath     string             `xml:"exec-path"`
	Style        string             `xml:"style"`
	S3270Options S3270Options       `xml:"s3270-options"`
	TargetHost   TargetHost         `xml:"target-host"`
	Fonts        FontsConfig        `xml:"fonts"`
	ColorSchemes ColorSchemesConfig `xml:"colorschemes"`
}

type S3270Options struct {
	Charset    string `xml:"charset"`
	Model      string `xml:"model"`
	Additional string `xml:"additional"`
}

type TargetHost struct {
	AutoConnect bool   `xml:"autoconnect,attr"`
	Value       string `xml:",chardata"`
}

type Font struct {
	Name        string `xml:"name,attr"`
	Description string `xml:"description,attr"`
}

type FontsConfig struct {
	Default string `xml:"default,attr"`
	Fonts   []Font `xml:"font"`
}

type ColorScheme struct {
	Name string `xml:"name,attr"`
	PNFg string `xml:"pnfg,attr"`
	PNBg string `xml:"pnbg,attr"`
	PIFg string `xml:"pifg,attr"`
	PIBg string `xml:"pibg,attr"`
	PHFg string `xml:"phfg,attr"`
	PHBg string `xml:"phbg,attr"`
	UNFg string `xml:"unfg,attr"`
	UNBg string `xml:"unbg,attr"`
	UIFg string `xml:"uifg,attr"`
	UIBg string `xml:"uibg,attr"`
	UHFg string `xml:"uhfg,attr"`
	UHBg string `xml:"uhbg,attr"`
}

type ColorSchemesConfig struct {
	Default string        `xml:"default,attr"`
	Schemes []ColorScheme `xml:"scheme"`
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
	if cfg.S3270Options.Charset == "" {
		cfg.S3270Options.Charset = "bracket"
	}
	if cfg.S3270Options.Model == "" {
		cfg.S3270Options.Model = "3"
	}
	cfg.TargetHost.Value = strings.TrimSpace(cfg.TargetHost.Value)
	if cfg.Fonts.Default == "" && len(cfg.Fonts.Fonts) > 0 {
		cfg.Fonts.Default = cfg.Fonts.Fonts[0].Name
	}
	if cfg.ColorSchemes.Default == "" && len(cfg.ColorSchemes.Schemes) > 0 {
		cfg.ColorSchemes.Default = cfg.ColorSchemes.Schemes[0].Name
	}

	return &cfg, nil
}
