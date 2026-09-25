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

// Test runs the Go tests with the race detector, then the web UI unit tests.
func Test() error {
	if err := sh.RunV("go", "test", "-race", "-count=1", "./..."); err != nil {
		return err
	}
	return WebTest()
}

// WebTest runs the dependency-free Node unit tests for the web UI modules.
func WebTest() error {
	files, err := filepath.Glob(filepath.Join("web", "js", "tests", "*.test.js"))
	if err != nil {
		return err
	}
	for _, file := range files {
		if err := sh.RunV("node", file); err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
	}
	return nil
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
