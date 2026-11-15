//go:build linux

package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// On Linux the sandbox is bubblewrap: the whole filesystem is mounted read-only, the secrets are covered with
// empty folders, the project and the caches are mounted writable, and the command gets a network of its own with
// no way out. The proxy is reached through a socket file mounted into the sandbox; a small forwarder inside (this
// same program) gives the tools a local port to use as their proxy.

const bridgePort = 18080

func sandboxAvailable() error {
	if _, err := exec.LookPath("bwrap"); err != nil {
		return fmt.Errorf("bubblewrap (the `bwrap` command) is not installed; install it with your package manager")
	}
	return nil
}

func exists(p string) bool { _, err := os.Lstat(p); return err == nil }

func isDir(p string) bool { info, err := os.Stat(p); return err == nil && info.IsDir() }

// bwrapArgs builds the bubblewrap command line for a run. sock is the proxy's socket file ("" when the network is
// open), self is this program.
func bwrapArgs(s Spec, sock, self, real string, args []string) []string {
	a := []string{"--die-with-parent", "--ro-bind", "/", "/", "--dev", "/dev", "--proc", "/proc", "--tmpfs", "/tmp"}
	bind := func(flag, p string) {
		if exists(p) {
			a = append(a, flag, p, p)
		}
	}
	for _, p := range s.AllowWrite {
		bind("--bind", p)
	}
	if s.Root != "" {
		bind("--bind", s.Root)
	}
	for _, p := range s.DenyWrite {
		bind("--ro-bind", p)
	}
	// Covering a secret: an empty folder over a folder, an empty file over a file.
	for _, p := range s.DenyRead {
		switch {
		case !exists(p):
		case isDir(p):
			a = append(a, "--tmpfs", p)
		default:
			a = append(a, "--ro-bind", "/dev/null", p)
		}
	}
	for _, p := range s.AllowRead {
		bind("--ro-bind", p)
	}
	if s.NetOpen {
		return append(append(a, "--", real), args...)
	}
	// This program is mounted too: it may sit under /tmp, which the sandbox replaces.
	a = append(a, "--unshare-net", "--ro-bind", sock, sock, "--ro-bind", self, self)
	a = append(a, "--", self, "__bridge", strconv.Itoa(bridgePort), sock, real)
	return append(a, args...)
}

// Entries of the writable list that are files, not folders.
var writableFiles = map[string]bool{".package-cache": true, ".package-cache-mutate": true, ".global-cache": true, ".npmrc": true, "credentials.toml": true, ".crates.toml": true, ".crates2.json": true}

// The tool a folder in the home directory belongs to, for a tool that is installed and has not made its folder yet.
var toolOf = map[string]string{".cargo": "cargo", ".foundry": "forge", ".bun": "bun", ".yarn": "yarn"}

func onPath(tool string) bool {
	if tool == "" {
		return false
	}
	_, err := exec.LookPath(tool)
	return err == nil
}

// A folder can only be mounted writable if it exists, and a tool cannot create its cache in a read-only home. So
// the caches of the tools this machine has are created before the run: a cache whose tool is installed or whose
// tool folder exists (~/.cargo for ~/.cargo/registry), and the two every Node install uses.
func prepareWritable(s Spec) {
	always := map[string]bool{".cache": true, ".npm": true}
	for _, p := range s.AllowWrite {
		if exists(p) || !within(p, s.Home) {
			continue
		}
		rel, _ := filepath.Rel(s.Home, p)
		top := strings.Split(rel, string(filepath.Separator))[0]
		// The Solana tools make this one themselves, as a link.
		if strings.HasSuffix(rel, filepath.Join("toolchains", "solana")) {
			continue
		}
		if !always[top] && (top == rel || !(exists(filepath.Join(s.Home, top)) || onPath(toolOf[top]))) {
			continue
		}
		if writableFiles[filepath.Base(p)] {
			if os.MkdirAll(filepath.Dir(p), 0o755) == nil {
				if f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
					f.Close()
				}
			}
			continue
		}
		os.MkdirAll(p, 0o755)
	}
}

func sandboxCommand(s Spec, real string, args []string) (*exec.Cmd, func(), error) {
	bwrap, err := exec.LookPath("bwrap")
	if err != nil {
		return nil, nil, err
	}
	prepareWritable(s)
	self, err := os.Executable()
	if err != nil {
		return nil, nil, err
	}
	cmd := exec.Command(bwrap, bwrapArgs(s, proxySocket, self, real, args)...)
	return cmd, func() {
		if proxySocket != "" {
			os.RemoveAll(filepath.Dir(proxySocket))
		}
	}, nil
}

var proxySocket string

// On Linux the proxy listens on a socket file, which is what can be mounted into the sandbox.
func proxyListenAddr() (network, addr string) {
	dir, err := os.MkdirTemp("", "keymoat-")
	if err != nil {
		fatal("%v", err)
	}
	proxySocket = filepath.Join(dir, "proxy.sock")
	return "unix", proxySocket
}

// bridgeMain runs inside the sandbox: it forwards a local port to the proxy's socket and runs the real command.
func bridgeMain(args []string) int {
	if len(args) < 3 {
		return usageError("keymoat __bridge PORT SOCKET COMMAND...")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:"+args[0])
	if err != nil {
		say("cannot start the proxy bridge: %v", err)
		return 2
	}
	sock := args[1]
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				up, err := net.Dial("unix", sock)
				if err != nil {
					return
				}
				defer up.Close()
				go func() { io.Copy(up, c); closeWrite(up) }()
				io.Copy(c, up)
			}()
		}
	}()
	cmd := exec.Command(args[2], args[3:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		say("%v", err)
		return 127
	}
	return 0
}
