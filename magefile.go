//go:build mage

// Package main provides Mage targets for the Player project.
// Run `mage -l` to list available targets.
// Prerequisites: Node.js 18+, npm, and a running Player server for E2E tests.
package main

import (
	"os"
	"os/exec"
)

// runnerDir is the path to the LLM e2e runner, relative to the project root.
const runnerDir = "player-server/test/e2e-llm/runner"

// E2E builds the LLM e2e runner and runs all scenarios against a live server.
//
// The Player server must already be running before invoking this target.
// Start it from player-server/ with:
//
//	MEDIA_ROOT=./testdata/media SECURE_COOKIES=false DB_PATH=/tmp/player-e2e-llm.db ./player
//
// Override the target URL with PLAYER_URL (default: http://localhost:8080).
// Set ANTHROPIC_API_KEY to enable the Haiku screenshot oracle (Layer 5).
func E2E() error {
	if err := npmRun("build"); err != nil {
		return err
	}
	return nodeRun("dist/index.js")
}

// npmRun runs an npm script in runnerDir, inheriting stdout/stderr and env.
func npmRun(script string) error {
	cmd := exec.Command("npm", "run", script)
	cmd.Dir = runnerDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// nodeRun executes a Node.js script in runnerDir, inheriting stdout/stderr and env.
func nodeRun(script string) error {
	cmd := exec.Command("node", script)
	cmd.Dir = runnerDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	return cmd.Run()
}
