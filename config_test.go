package main

import (
	"os"
	"testing"
)

func TestSecretFromEnv_PlainString(t *testing.T) {
	result, err := secretFromEnv("plainpassword")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "plainpassword" {
		t.Errorf("expected 'plainpassword', got %q", result)
	}
}

func TestSecretFromEnv_EnvVarSet(t *testing.T) {
	t.Setenv("GOSTATS_TEST_SECRET", "supersecret")
	result, err := secretFromEnv("$env:GOSTATS_TEST_SECRET")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "supersecret" {
		t.Errorf("expected 'supersecret', got %q", result)
	}
}

func TestSecretFromEnv_EnvVarUnset(t *testing.T) {
	os.Unsetenv("GOSTATS_TEST_MISSING")
	_, err := secretFromEnv("$env:GOSTATS_TEST_MISSING")
	if err == nil {
		t.Errorf("expected error for unset env var, got none")
	}
}

func TestSecretFromEnv_EmptyPrefix(t *testing.T) {
	// A string that is just the prefix with no variable name is still looked up
	os.Unsetenv("")
	_, err := secretFromEnv("$env:")
	if err == nil {
		t.Errorf("expected error for empty env var name, got none")
	}
}
