//go:build mage

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"
)

const binaryName = "player"

// Default target runs Build.
func Default() error {
	mg.Deps(Build)
	return nil
}

// Build compiles the application binary.
func Build() error {
	return sh.RunV("go", "build", "-o", binaryName, "./cmd/player")
}

// Test runs all tests in the project with the race detector enabled.
func Test() error {
	return sh.RunV("go", "test", "-race", "-count=1", "./...")
}

// Install builds and copies the binary to GOPATH/bin.
func Install() error {
	mg.Deps(Build)

	goPath := os.Getenv("GOPATH")
	if goPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("resolving home directory: %w", err)
		}
		goPath = filepath.Join(home, "go")
	}

	binDir := filepath.Join(goPath, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", binDir, err)
	}

	src := filepath.Join(".", binaryName)
	dst := filepath.Join(binDir, binaryName)

	if runtime.GOOS == "windows" {
		return sh.Copy(dst, src)
	}
	return sh.RunV("cp", "-v", src, dst)
}

// Clean removes build artifacts.
func Clean() error {
	if err := sh.Rm(binaryName); err != nil {
		return fmt.Errorf("removing %s: %w", binaryName, err)
	}
	return nil
}

// DockerBuild builds the container image.
func DockerBuild() error {
	return sh.RunV("docker", "build", "-t", "player:latest", ".")
}

// DockerPush pushes the container image to the registry.
func DockerPush() error {
	return sh.RunV("docker", "push", "player:latest")
}
