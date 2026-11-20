//go:build !darwin && !linux

package main

import (
	"fmt"
	"os/exec"
	"runtime"
)

func sandboxAvailable() error {
	return fmt.Errorf("KeyMoat has no sandbox for %s; on Windows, use it inside WSL", runtime.GOOS)
}

func sandboxCommand(s Spec, real string, args []string) (*exec.Cmd, func(), error) {
	return nil, nil, sandboxAvailable()
}

func proxyListenAddr() (network, addr string) { return "tcp", "127.0.0.1:0" }

func bridgeMain(args []string) int { return 2 }
