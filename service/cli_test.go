package service

import "testing"

func TestParseArgs(t *testing.T) {
	t.Run("requires config path", func(t *testing.T) {
		_, _, err := parseArgs(nil)
		if err == nil {
			t.Fatal("expected an error, got nil")
		}
	})

	t.Run("accepts optional runtime override", func(t *testing.T) {
		configPath, runtimeOverride, err := parseArgs([]string{"./config.json", "./runtime.js"})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if configPath != "./config.json" {
			t.Fatalf("unexpected config path: %q", configPath)
		}
		if runtimeOverride != "./runtime.js" {
			t.Fatalf("unexpected runtime override: %q", runtimeOverride)
		}
	})
}
