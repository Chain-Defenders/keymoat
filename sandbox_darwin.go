//go:build darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// On macOS the sandbox is the system's own (the one behind sandbox-exec). The profile starts from "nothing is
// allowed" and adds what a build tool needs, so the ways out of a sandbox that go through other apps (opening a
// file with another program, scripting the terminal, asking the keychain) stay closed without being listed.

const sandboxExec = "/usr/bin/sandbox-exec"

func sandboxAvailable() error {
	if _, err := os.Stat(sandboxExec); err != nil {
		return fmt.Errorf("%s is missing", sandboxExec)
	}
	return nil
}

func sbString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// sbPath matches a file exactly or a folder with everything under it.
func sbPath(p string) string {
	if info, err := os.Stat(p); err == nil && !info.IsDir() {
		return "(literal " + sbString(p) + ")"
	}
	return "(subpath " + sbString(p) + ")"
}

func sbRegexEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune(`\.+*?()[]{}|^$`, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func sandboxProfile(s Spec) string {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }

	w("(version 1)")
	w("(deny default)")
	w("(allow process-fork)")
	w("(allow process-exec)")
	w("(allow signal (target same-sandbox))")
	w("(allow process-info* (target same-sandbox))")
	w("(allow sysctl-read)")
	w("(allow file-ioctl)")
	w("(allow pseudo-tty)")
	w("(allow ipc-posix-shm*)")
	w("(allow ipc-posix-sem*)")
	w("(allow user-preference-read)")
	// Services a program needs to start, look up the user, check a TLS certificate and watch files. Not the launcher
	// of other apps, and not the name resolver: a name is resolved by the proxy or not at all. The security server
	// is needed by programs that check certificates the system's way; it is also what serves the login keychain,
	// which is why the keychain folder is made unreadable further down, whatever the settings say: without the
	// file, the keychain cannot be opened and its items cannot be asked for.
	w("(allow mach-lookup")
	for _, name := range []string{"com.apple.system.opendirectoryd.libinfo", "com.apple.system.notification_center", "com.apple.system.logger", "com.apple.logd", "com.apple.trustd.agent", "com.apple.trustd", "com.apple.FSEvents", "com.apple.SystemConfiguration.configd", "com.apple.diagnosticd", "com.apple.analyticsd", "com.apple.cfprefsd.daemon", "com.apple.cfprefsd.agent", "com.apple.SecurityServer"} {
		w("  (global-name %s)", sbString(name))
	}
	w(")")

	// Reading: everything, then not the secrets, then the exceptions.
	w("(allow file-read*)")
	for _, p := range s.DenyRead {
		w("(deny file-read* file-write* %s)", sbPath(p))
	}
	if s.HideEnvFiles {
		w(`(deny file-read* (regex #"/\.env(\.[^/]*)?$"))`)
		w(`(allow file-read* (regex #"/\.env\.(example|sample|template|dist|defaults)$"))`)
		for _, p := range s.AllowEnvFiles {
			w("(allow file-read* (literal %s))", sbString(p))
		}
	}
	for _, p := range s.AllowRead {
		w("(allow file-read* %s)", sbPath(p))
	}
	w("(deny file-read* file-write* (subpath %s))", sbString(filepath.Join(s.Home, "Library", "Keychains")))

	// Writing: only devices, temporary folders, the caches and the project. Nothing else, so a program that runs
	// with more rights than the user (a setuid one) gains nothing by it either.
	w(`(allow file-write* (subpath "/dev"))`)
	for _, p := range darwinTempDirs() {
		w("(allow file-write* (subpath %s))", sbString(p))
	}
	for _, p := range s.AllowWrite {
		w("(allow file-write* (subpath %s))", sbString(p))
	}
	if s.Root != "" {
		w("(allow file-write* (subpath %s))", sbString(s.Root))
	}
	for _, p := range s.DenyWrite {
		w("(deny file-write* %s)", sbPath(p))
	}
	// A secret stays unwritable too, wherever it sits.
	for _, p := range s.DenyRead {
		if !contains(s.AllowRead, p) {
			w("(deny file-write* %s)", sbPath(p))
		}
	}

	if s.NetOpen {
		w("(allow network*)")
		w("(allow system-socket)")
		w(`(allow mach-lookup (global-name "com.apple.dnssd.service") (global-name "com.apple.mDNSResponder"))`)
	} else {
		// Local services (a local node, a test validator) and the proxy, which is local too. Outbound is allowed by
		// where it goes and nothing else: a rule on the local address would let every connection out.
		w(`(allow network-outbound (remote ip "localhost:*"))`)
		w(`(allow network-bind (local ip "*:*"))`)
		for _, p := range s.SocketDirs {
			w("(allow network-bind network-outbound network-inbound (subpath %s))", sbString(p))
		}
		w(`(allow network-inbound (local ip "*:*"))`)
		w("(allow system-socket)")
	}
	if s.AllowUSB {
		w("(allow iokit-open)")
		w(`(allow mach-lookup (global-name "com.apple.hidd") (global-name "com.apple.iohideventsystem"))`)
	}
	return b.String()
}

// darwinTempDirs are the system's temporary folders: the shared ones and this user's own (which holds both the
// temporary and the cache folder the system hands out).
func darwinTempDirs() []string {
	dirs := []string{"/private/tmp", "/private/var/tmp"}
	if t := realPath(os.TempDir()); strings.HasPrefix(t, "/private/var/folders/") {
		dirs = append(dirs, filepath.Dir(t))
	} else {
		dirs = append(dirs, t)
	}
	return dirs
}

// sandboxCommand wraps a command so it runs under the profile. The cleanup removes what the run needed.
func sandboxCommand(s Spec, real string, args []string) (*exec.Cmd, func(), error) {
	dir, err := os.MkdirTemp("", "keymoat-")
	if err != nil {
		return nil, nil, err
	}
	profile := filepath.Join(dir, "profile.sb")
	if err := os.WriteFile(profile, []byte(sandboxProfile(s)), 0o600); err != nil {
		os.RemoveAll(dir)
		return nil, nil, err
	}
	cmd := exec.Command(sandboxExec, append([]string{"-f", profile, real}, args...)...)
	if os.Getenv("KEYMOAT_DEBUG") == "1" {
		say("the sandbox's rules for this run are kept in %s", profile)
		return cmd, func() {}, nil
	}
	return cmd, func() { os.RemoveAll(dir) }, nil
}

// On macOS the proxy listens on a local port; nothing else is needed inside the sandbox.
func proxyListenAddr() (network, addr string) { return "tcp", "127.0.0.1:0" }

func bridgeMain(args []string) int {
	say("the bridge is only used on Linux")
	return 2
}
