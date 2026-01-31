package config

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode"
)

type s3270OptionKind int

const (
	s3270OptionBool s3270OptionKind = iota
	s3270OptionValue
	s3270OptionArgs
)

type s3270OptionSpec struct {
	EnvVar      string
	Flag        string
	Description string
	Default     string
	DefaultVal  string
	Kind        s3270OptionKind
}

var s3270OptionSpecs = []s3270OptionSpec{
	{
		EnvVar:      "S3270_HELP",
		Flag:        "--help",
		Description: "Display basic command-line help and then exit",
		Default:     "false",
		DefaultVal:  "false",
		Kind:        s3270OptionBool,
	},
	{
		EnvVar:      "S3270_PREFER_IPV4",
		Flag:        "-4",
		Description: "Prefer IPv4 addresses",
		Default:     "false",
		DefaultVal:  "false",
		Kind:        s3270OptionBool,
	},
	{
		EnvVar:      "S3270_PREFER_IPV6",
		Flag:        "-6",
		Description: "Prefer IPv6 addresses",
		Default:     "false",
		DefaultVal:  "false",
		Kind:        s3270OptionBool,
	},
	{
		EnvVar:      "S3270_ACCEPT_HOSTNAME",
		Flag:        "-accepthostname",
		Description: "Name to match in the host's TLS certificate",
		Default:     "(none)",
		DefaultVal:  "",
		Kind:        s3270OptionValue,
	},
	{
		EnvVar:      "S3270_CA_DIR",
		Flag:        "-cadir",
		Description: "Directory containing CA root certificates for TLS",
		Default:     "(none)",
		DefaultVal:  "",
		Kind:        s3270OptionValue,
	},
	{
		EnvVar:      "S3270_CA_FILE",
		Flag:        "-cafile",
		Description: "File containing CA root certificate for TLS",
		Default:     "(none)",
		DefaultVal:  "",
		Kind:        s3270OptionValue,
	},
	{
		EnvVar:      "S3270_CALLBACK",
		Flag:        "-callback",
		Description: "Connect to the specified port for an s3270 protocol session",
		Default:     "(none)",
		DefaultVal:  "",
		Kind:        s3270OptionValue,
	},
	{
		EnvVar:      "S3270_CERT_FILE",
		Flag:        "-certfile",
		Description: "File containing client certificate for TLS",
		Default:     "(none)",
		DefaultVal:  "",
		Kind:        s3270OptionValue,
	},
	{
		EnvVar:      "S3270_CERT_FILE_TYPE",
		Flag:        "-certfiletype",
		Description: "Type of client certificate file (pem or asn1) for TLS",
		Default:     "pem",
		DefaultVal:  "pem",
		Kind:        s3270OptionValue,
	},
	{
		EnvVar:      "S3270_CHAIN_FILE",
		Flag:        "-chainfile",
		Description: "File containing chain of CA certificates for TLS",
		Default:     "(none)",
		DefaultVal:  "",
		Kind:        s3270OptionValue,
	},
	{
		EnvVar:      "S3270_CLEAR",
		Flag:        "-clear",
		Description: "Set a Boolean resource to false",
		Default:     "(none)",
		DefaultVal:  "",
		Kind:        s3270OptionArgs,
	},
	{
		EnvVar:      "S3270_CLIENT_CERT",
		Flag:        "-clientcert",
		Description: "Name of client certificate for TLS",
		Default:     "(none)",
		DefaultVal:  "",
		Kind:        s3270OptionValue,
	},
	{
		EnvVar:      "S3270_CODE_PAGE",
		Flag:        "-codepage",
		Description: "Host EBCDIC code page",
		Default:     "bracket",
		DefaultVal:  "bracket",
		Kind:        s3270OptionValue,
	},
	{
		EnvVar:      "S3270_CONNECT_TIMEOUT",
		Flag:        "-connecttimeout",
		Description: "Timeout before giving up on a host connection",
		Default:     "(none)",
		DefaultVal:  "",
		Kind:        s3270OptionValue,
	},
	{
		EnvVar:      "S3270_COOKIE_FILE",
		Flag:        "-cookiefile",
		Description: "Specifies the security cookie file",
		Default:     "(none)",
		DefaultVal:  "",
		Kind:        s3270OptionValue,
	},
	{
		EnvVar:      "S3270_DEV_NAME",
		Flag:        "-devname",
		Description: "Device name (workstation ID) response to TELNET NEW-ENVIRON request",
		Default:     "(none)",
		DefaultVal:  "",
		Kind:        s3270OptionValue,
	},
	{
		EnvVar:      "S3270_EXEC_COMMAND",
		Flag:        "-e",
		Description: "Command to run instead of connecting to a host",
		Default:     "(none)",
		DefaultVal:  "",
		Kind:        s3270OptionArgs,
	},
	{
		EnvVar:      "S3270_HTTPD",
		Flag:        "-httpd",
		Description: "Start HTTP server",
		Default:     "(none)",
		DefaultVal:  "",
		Kind:        s3270OptionValue,
	},
	{
		EnvVar:      "S3270_KEY_FILE",
		Flag:        "-keyfile",
		Description: "Key file for TLS client certificate",
		Default:     "(none)",
		DefaultVal:  "",
		Kind:        s3270OptionValue,
	},
	{
		EnvVar:      "S3270_KEY_FILE_TYPE",
		Flag:        "-keyfiletype",
		Description: "Type of client certificate key file (pem or asn1) for TLS",
		Default:     "pem",
		DefaultVal:  "pem",
		Kind:        s3270OptionValue,
	},
	{
		EnvVar:      "S3270_KEY_PASSWORD",
		Flag:        "-keypasswd",
		Description: "Password for the key file (OpenSSL) or certificate file (macOS) for TLS",
		Default:     "(none)",
		DefaultVal:  "",
		Kind:        s3270OptionValue,
	},
	{
		EnvVar:      "S3270_LOGIN_MACRO",
		Flag:        "-loginmacro",
		Description: "Actions to run when host connection is established",
		Default:     "(none)",
		DefaultVal:  "",
		Kind:        s3270OptionValue,
	},
	{
		EnvVar:      "S3270_MIN_VERSION",
		Flag:        "-minversion",
		Description: "Minimum version number required (major.minor type iteration)",
		Default:     "(none)",
		DefaultVal:  "",
		Kind:        s3270OptionValue,
	},
	{
		EnvVar:      "S3270_MODEL",
		Flag:        "-model",
		Description: "Model of 3270 to emulate",
		Default:     "3279-4-E",
		DefaultVal:  "3279-4-E",
		Kind:        s3270OptionValue,
	},
	{
		EnvVar:      "S3270_NO_VERIFY_CERT",
		Flag:        "-noverifycert",
		Description: "Do not verify the TLS host certificate",
		Default:     "false",
		DefaultVal:  "false",
		Kind:        s3270OptionBool,
	},
	{
		EnvVar:      "S3270_NVT",
		Flag:        "-nvt",
		Description: "Force NVT mode -- do not negotiate 3270 mode",
		Default:     "false",
		DefaultVal:  "false",
		Kind:        s3270OptionBool,
	},
	{
		EnvVar:      "S3270_OVERSIZE",
		Flag:        "-oversize",
		Description: "Make the display larger than the default for the 3270 model",
		Default:     "(none)",
		DefaultVal:  "",
		Kind:        s3270OptionArgs,
	},
	{
		EnvVar:      "S3270_PORT",
		Flag:        "-port",
		Description: "TCP port to connect to",
		Default:     "23",
		DefaultVal:  "23",
		Kind:        s3270OptionValue,
	},
	{
		EnvVar:      "S3270_PROXY",
		Flag:        "-proxy",
		Description: "Type of proxy and proxy server to use",
		Default:     "(none)",
		DefaultVal:  "",
		Kind:        s3270OptionValue,
	},
	{
		EnvVar:      "S3270_SCRIPT_PORT",
		Flag:        "-scriptport",
		Description: "Accept TCP connections for s3270 protocol sessions",
		Default:     "(none)",
		DefaultVal:  "",
		Kind:        s3270OptionValue,
	},
	{
		EnvVar:      "S3270_SCRIPT_PORT_ONCE",
		Flag:        "-scriptportonce",
		Description: "Accept only one -scriptport session",
		Default:     "false",
		DefaultVal:  "false",
		Kind:        s3270OptionBool,
	},
	{
		EnvVar:      "S3270_SET",
		Flag:        "-set",
		Description: "Set a Boolean resource to true or set a resource to some value",
		Default:     "(none)",
		DefaultVal:  "",
		Kind:        s3270OptionArgs,
	},
	{
		EnvVar:      "S3270_SOCKET",
		Flag:        "-socket",
		Description: "Accept s3270 protocol sessions on the Unix-domain socket /tmp/x3sck.PID",
		Default:     "false",
		DefaultVal:  "false",
		Kind:        s3270OptionBool,
	},
	{
		EnvVar:      "S3270_TLS_MAX_PROTOCOL",
		Flag:        "-tlsmaxprotocol",
		Description: "Set the maximum TLS protocol",
		Default:     "varies",
		DefaultVal:  "",
		Kind:        s3270OptionValue,
	},
	{
		EnvVar:      "S3270_TLS_MIN_PROTOCOL",
		Flag:        "-tlsminprotocol",
		Description: "Set the minimum TLS protocol",
		Default:     "varies",
		DefaultVal:  "",
		Kind:        s3270OptionValue,
	},
	{
		EnvVar:      "S3270_TERMINAL_NAME",
		Flag:        "-tn",
		Description: "Use terminal-name as the terminal name reported to the host",
		Default:     "varies",
		DefaultVal:  "",
		Kind:        s3270OptionValue,
	},
	{
		EnvVar:      "S3270_TRACE",
		Flag:        "-trace",
		Description: "Turn on data stream and action tracing",
		Default:     "false",
		DefaultVal:  "false",
		Kind:        s3270OptionBool,
	},
	{
		EnvVar:      "S3270_TRACE_FILE",
		Flag:        "-tracefile",
		Description: "Use file-name as the file for data stream and action tracing",
		Default:     "POSIX: /tmp/x3trc.PID; Windows: x3trc.PID.txt",
		DefaultVal:  "",
		Kind:        s3270OptionValue,
	},
	{
		EnvVar:      "S3270_TRACE_FILE_SIZE",
		Flag:        "-tracefilesize",
		Description: "Limit trace files to bytes bytes in size",
		Default:     "(none)",
		DefaultVal:  "",
		Kind:        s3270OptionValue,
	},
	{
		EnvVar:      "S3270_USER",
		Flag:        "-user",
		Description: "Use user-name in the reply to TELNET NEW-ENVIRON sub-negotiation",
		Default:     "varies",
		DefaultVal:  "",
		Kind:        s3270OptionValue,
	},
	{
		EnvVar:      "S3270_UTENV",
		Flag:        "-utenv",
		Description: "Allow unit-test-specific environment variables to have effect",
		Default:     "false",
		DefaultVal:  "false",
		Kind:        s3270OptionBool,
	},
	{
		EnvVar:      "S3270_UTF8",
		Flag:        "-utf8",
		Description: "Use UTF-8 encoding for all I/O on the workstation",
		Default:     "false",
		DefaultVal:  "false",
		Kind:        s3270OptionBool,
	},
	{
		EnvVar:      "S3270_VERSION",
		Flag:        "-v",
		Description: "Display version information on standard output and then exit",
		Default:     "false",
		DefaultVal:  "false",
		Kind:        s3270OptionBool,
	},
	{
		EnvVar:      "S3270_XRM",
		Flag:        "-xrm",
		Description: "Set the value of a resource",
		Default:     "(none)",
		DefaultVal:  "",
		Kind:        s3270OptionArgs,
	},
}

type S3270EnvOverrides struct {
	Model       string
	CodePage    string
	Args        []string
	ExecArgs    []string
	HasModel    bool
	HasCodePage bool
}

// EnsureDotEnv writes a default .env file if none exists.
func EnsureDotEnv(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}

	content := buildDotEnvContent()
	return os.WriteFile(path, []byte(content), 0644)
}

// LoadDotEnv loads environment variables from a .env file without overriding existing variables.
func LoadDotEnv(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		key, value, ok := parseEnvLine(line)
		if !ok {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		_ = os.Setenv(key, value)
	}
	return scanner.Err()
}

// S3270EnvOverridesFromEnv builds s3270 command-line arguments from environment variables.
func S3270EnvOverridesFromEnv() (S3270EnvOverrides, error) {
	var overrides S3270EnvOverrides
	var parseErr error
	certFile := strings.TrimSpace(os.Getenv("S3270_CERT_FILE"))
	keyFile := strings.TrimSpace(os.Getenv("S3270_KEY_FILE"))

	for _, spec := range s3270OptionSpecs {
		value := strings.TrimSpace(os.Getenv(spec.EnvVar))
		if value == "" {
			continue
		}

		switch spec.EnvVar {
		case "S3270_MODEL":
			overrides.Model = value
			overrides.HasModel = true
			continue
		case "S3270_CODE_PAGE":
			overrides.CodePage = value
			overrides.HasCodePage = true
			continue
		case "S3270_CERT_FILE_TYPE":
			if certFile == "" {
				continue
			}
		case "S3270_KEY_FILE_TYPE":
			if keyFile == "" {
				continue
			}
		}

		switch spec.Kind {
		case s3270OptionBool:
			if parseBool(value) {
				overrides.Args = append(overrides.Args, spec.Flag)
			}
		case s3270OptionValue:
			overrides.Args = append(overrides.Args, spec.Flag, value)
		case s3270OptionArgs:
			args, err := splitArgs(value)
			if err != nil && parseErr == nil {
				parseErr = err
			}
			if len(args) > 0 {
				if spec.EnvVar == "S3270_EXEC_COMMAND" {
					overrides.ExecArgs = append([]string{spec.Flag}, args...)
				} else {
					overrides.Args = append(overrides.Args, spec.Flag)
					overrides.Args = append(overrides.Args, args...)
				}
			}
		}
	}

	return overrides, parseErr
}

func buildDotEnvContent() string {
	var buf bytes.Buffer
	buf.WriteString("# S3270 command-line options (.env overrides).\n")
	buf.WriteString("# Values are passed to the s3270 CLI unless empty/false.\n\n")
	for _, spec := range s3270OptionSpecs {
		buf.WriteString(fmt.Sprintf("# %s: %s (default: %s)\n", spec.Flag, spec.Description, spec.Default))
		buf.WriteString(fmt.Sprintf("%s=%s\n\n", spec.EnvVar, spec.DefaultVal))
	}
	return buf.String()
}

func parseEnvLine(line string) (string, string, bool) {
	idx := strings.IndexRune(line, '=')
	if idx == -1 {
		return "", "", false
	}
	key := strings.TrimSpace(line[:idx])
	if key == "" {
		return "", "", false
	}
	value := strings.TrimSpace(line[idx+1:])
	if value == "" {
		return key, "", true
	}
	unquoted, err := strconv.Unquote(value)
	if err == nil {
		return key, unquoted, true
	}
	return key, value, true
}

func parseBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func splitArgs(input string) ([]string, error) {
	var args []string
	var current bytes.Buffer
	var inSingle, inDouble, escaped, inToken bool

	flush := func() {
		if !inToken {
			return
		}
		args = append(args, current.String())
		current.Reset()
		inToken = false
	}

	for _, r := range input {
		switch {
		case escaped:
			current.WriteRune(r)
			escaped = false
		case r == '\\' && !inSingle:
			escaped = true
			inToken = true
		case r == '\'' && !inDouble:
			inSingle = !inSingle
			inToken = true
		case r == '"' && !inSingle:
			inDouble = !inDouble
			inToken = true
		case unicode.IsSpace(r) && !inSingle && !inDouble:
			flush()
		default:
			current.WriteRune(r)
			inToken = true
		}
	}
	if escaped || inSingle || inDouble {
		return args, fmt.Errorf("unterminated quote or escape in %q", input)
	}
	flush()
	return args, nil
}
