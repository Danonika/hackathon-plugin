package objectstore

import (
	"context"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

func clearMinioEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"MINIO_ENDPOINT", "MINIO_ACCESS_KEY", "MINIO_SECRET_KEY", "MINIO_BUCKET", "MINIO_USE_SSL"} {
		t.Setenv(key, "")
	}
}

func TestSettingsFromEnvDefaults(t *testing.T) {
	clearMinioEnv(t)
	got := SettingsFromEnv()
	if got.Endpoint != "" || got.AccessKey != "" || got.SecretKey != "" {
		t.Fatalf("expected empty endpoint and credentials, got %+v", got)
	}
	if got.Bucket != "hackathonkit" {
		t.Fatalf("Bucket = %q, want hackathonkit", got.Bucket)
	}
	if got.UseSSL {
		t.Fatalf("UseSSL = true, want false by default")
	}
}

func TestSettingsFromEnvOverrides(t *testing.T) {
	t.Setenv("MINIO_ENDPOINT", "minio.local:9000")
	t.Setenv("MINIO_ACCESS_KEY", "access")
	t.Setenv("MINIO_SECRET_KEY", "secret")
	t.Setenv("MINIO_BUCKET", "custom-bucket")
	t.Setenv("MINIO_USE_SSL", "true")
	got := SettingsFromEnv()
	if got.Endpoint != "minio.local:9000" || got.AccessKey != "access" || got.SecretKey != "secret" {
		t.Fatalf("env values not read: %+v", got)
	}
	if got.Bucket != "custom-bucket" {
		t.Fatalf("Bucket = %q", got.Bucket)
	}
	if !got.UseSSL {
		t.Fatalf("UseSSL = false, want true")
	}
}

func TestSettingsFromEnvUseSSLBoolSpellings(t *testing.T) {
	for _, value := range []string{"1", "TRUE", "true", "yes", "on"} {
		clearMinioEnv(t)
		t.Setenv("MINIO_USE_SSL", value)
		if !SettingsFromEnv().UseSSL {
			t.Fatalf("MINIO_USE_SSL=%q should enable TLS", value)
		}
	}
	for _, value := range []string{"0", "false", "no", ""} {
		clearMinioEnv(t)
		t.Setenv("MINIO_USE_SSL", value)
		if SettingsFromEnv().UseSSL {
			t.Fatalf("MINIO_USE_SSL=%q must not enable TLS", value)
		}
	}
}

func TestOpenEmptyEndpoint(t *testing.T) {
	clearMinioEnv(t)
	client, err := Open(context.Background(), Settings{Bucket: "hackathonkit"})
	if err == nil {
		client.client = nil
		t.Fatal("expected an error for an empty endpoint")
	}
	if !strings.Contains(err.Error(), "MINIO_ENDPOINT") {
		t.Fatalf("err = %v", err)
	}
}

func TestOpenInvalidEndpoint(t *testing.T) {
	client, err := Open(context.Background(), Settings{Endpoint: "http://minio.local:9000/path", Bucket: "hackathonkit"})
	if err == nil {
		t.Fatal("expected an error for a fully-qualified endpoint")
	}
	if client != nil {
		t.Fatalf("client = %+v, want nil", client)
	}
}

// cancelledContext makes minio-go fail before any dial, so these tests stay
// offline while still exercising the error returns.
func cancelledContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func TestOpenBucketProbeError(t *testing.T) {
	client, err := Open(cancelledContext(t), Settings{
		Endpoint:  "127.0.0.1:1",
		AccessKey: "access",
		SecretKey: "secret",
		Bucket:    "hackathonkit",
	})
	if err == nil {
		t.Fatal("expected the bucket probe to fail")
	}
	if client != nil {
		t.Fatalf("client = %+v, want nil", client)
	}
}

func deadClient(t *testing.T) *Client {
	t.Helper()
	cli, err := minio.New("127.0.0.1:1", &minio.Options{
		Creds: credentials.NewStaticV4("access", "secret", ""),
	})
	if err != nil {
		t.Fatal(err)
	}
	return &Client{client: cli, bucket: "hackathonkit"}
}

func TestPutWithCancelledContext(t *testing.T) {
	if err := deadClient(t).Put(cancelledContext(t), "name", []byte("data")); err == nil {
		t.Fatal("expected Put to fail")
	}
}

func TestGetWithCancelledContext(t *testing.T) {
	if _, err := deadClient(t).Get(cancelledContext(t), "name"); err == nil {
		t.Fatal("expected Get to fail")
	}
}
