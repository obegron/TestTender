package main

import (
	"strings"
	"testing"
	"time"
)

func TestValidateAppConfigRejectsUnsafeValues(t *testing.T) {
	tests := []struct {
		name string
		cfg  appConfig
		want string
	}{
		{name: "empty state", cfg: appConfig{}, want: "state directory"},
		{name: "root state", cfg: appConfig{stateDir: "/"}, want: "filesystem root"},
		{name: "negative concurrency", cfg: appConfig{stateDir: "/tmp/sidewhale", limits: runtimeLimits{maxConcurrent: -1}}, want: "max-concurrent"},
		{name: "negative duration", cfg: appConfig{stateDir: "/tmp/sidewhale", limits: runtimeLimits{maxRuntime: -time.Second}}, want: "max-runtime"},
		{name: "negative request limit", cfg: appConfig{stateDir: "/tmp/sidewhale", maxRequestBodyBytes: -1}, want: "max-request-body-bytes"},
		{name: "TLS certificate without key", cfg: appConfig{stateDir: "/tmp/sidewhale", tlsCertFile: "server.pem"}, want: "tls-cert and tls-key"},
		{name: "client CA without server TLS", cfg: appConfig{stateDir: "/tmp/sidewhale", tlsClientCAFile: "ca.pem"}, want: "tls-client-ca requires"},
		{name: "mTLS with Unix bypass", cfg: appConfig{stateDir: "/tmp/sidewhale", unixSocketPath: "/tmp/docker.sock", tlsCertFile: "server.pem", tlsKeyFile: "server-key.pem", tlsClientCAFile: "ca.pem"}, want: "Unix listener"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAppConfig(tt.cfg)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("validateAppConfig() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestValidateAppConfigAcceptsUnlimitedValues(t *testing.T) {
	cfg := appConfig{stateDir: t.TempDir()}
	if err := validateAppConfig(cfg); err != nil {
		t.Fatalf("validateAppConfig() unexpected error: %v", err)
	}
}

func TestValidateAppConfigAcceptsStrictMTLS(t *testing.T) {
	cfg := appConfig{
		stateDir:        t.TempDir(),
		tlsCertFile:     "server.pem",
		tlsKeyFile:      "server-key.pem",
		tlsClientCAFile: "ca.pem",
	}
	if err := validateAppConfig(cfg); err != nil {
		t.Fatalf("validateAppConfig() unexpected error: %v", err)
	}
}
