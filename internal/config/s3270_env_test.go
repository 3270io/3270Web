package config

import (
	"testing"
)

func TestS3270EnvOverridesFromEnv_Parsing(t *testing.T) {
	tests := []struct {
		name     string
		envKey   string
		envValue string
		check    func(*testing.T, S3270EnvOverrides)
	}{
		{
			name:     "Model override",
			envKey:   "S3270_MODEL",
			envValue: "3279-2-E",
			check: func(t *testing.T, o S3270EnvOverrides) {
				if !o.HasModel || o.Model != "3279-2-E" {
					t.Errorf("expected model 3279-2-E, got %v", o.Model)
				}
			},
		},
		{
			name:     "SplitArgs quoted values",
			envKey:   "S3270_SET", // Uses s3270OptionArgs
			envValue: "toggle 'some value' \"another value\"",
			check: func(t *testing.T, o S3270EnvOverrides) {
				// S3270_SET flag is -set. splitArgs returns ["toggle", "some value", "another value"]
				// S3270EnvOverridesFromEnv appends flag then args.
				// So overrides.Args should contain "-set", "toggle", "some value", "another value"
				found := false
				for i, arg := range o.Args {
					if arg == "-set" && i+3 < len(o.Args) {
						if o.Args[i+1] == "toggle" && o.Args[i+2] == "some value" && o.Args[i+3] == "another value" {
							found = true
							break
						}
					}
				}
				if !found {
					t.Errorf("expected parsed args for -set, got %v", o.Args)
				}
			},
		},
		{
			name:     "SplitArgs escape sequences",
			envKey:   "S3270_XRM",
			envValue: "wc3270.foo: \\\"bar\\\"",
			check: func(t *testing.T, o S3270EnvOverrides) {
				// expecting ["wc3270.foo:", "\"bar\""]
				// overrides.Args: ..., "-xrm", "wc3270.foo:", "\"bar\"", ...
				found := false
				for i, arg := range o.Args {
					if arg == "-xrm" && i+2 < len(o.Args) {
						if o.Args[i+1] == "wc3270.foo:" && o.Args[i+2] == "\"bar\"" {
							found = true
							break
						}
					}
				}
				if !found {
					t.Errorf("expected parsed escaped args, got %v", o.Args)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.envKey, tt.envValue)
			overrides, err := S3270EnvOverridesFromEnv()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			tt.check(t, overrides)
		})
	}
}
