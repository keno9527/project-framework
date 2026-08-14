package main

import (
	"testing"

	"github.com/gin-gonic/gin"
)

func TestConfigureGinModeDefaultsToRelease(t *testing.T) {
	originalMode := gin.Mode()
	t.Cleanup(func() { gin.SetMode(originalMode) })
	t.Setenv(gin.EnvGinMode, "")
	gin.SetMode(gin.DebugMode)

	configureGinMode()

	if got := gin.Mode(); got != gin.ReleaseMode {
		t.Fatalf("Gin mode = %q, want %q", got, gin.ReleaseMode)
	}
}

func TestConfigureGinModePreservesExplicitMode(t *testing.T) {
	originalMode := gin.Mode()
	t.Cleanup(func() { gin.SetMode(originalMode) })
	t.Setenv(gin.EnvGinMode, gin.DebugMode)
	gin.SetMode(gin.DebugMode)

	configureGinMode()

	if got := gin.Mode(); got != gin.DebugMode {
		t.Fatalf("Gin mode = %q, want %q", got, gin.DebugMode)
	}
}
