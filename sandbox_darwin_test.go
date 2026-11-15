//go:build darwin

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestProfileRules(t *testing.T) {
	s := Spec{Home: "/Users/x", Root: "/Users/x/proj", DenyRead: []string{"/Users/x/.ssh"}, AllowRead: []string{"/Users/x/.foundry/keystores/deployer"}, HideEnvFiles: true, AllowEnvFiles: []string{"/Users/x/proj/.env"}, AllowWrite: []string{"/Users/x/.npm"}, DenyWrite: []string{"/Users/x/proj/.git/hooks"}, ProxyPort: 4000}
	p := sandboxProfile(s)
	if !strings.HasPrefix(p, "(version 1)\n(deny default)\n") {
		t.Fatalf("the profile must start from nothing allowed")
	}
	// A rule on the local address lets every outbound connection through; so does a blanket allow.
	for _, bad := range []string{"(allow network*", "(allow default)", "(local ip \"localhost", "com.apple.dnssd", "securityd.xpc", "launchservicesd", "(allow iokit-open)", "(allow appleevent"} {
		if strings.Contains(p, bad) {
			t.Errorf("the profile must not contain %s", bad)
		}
	}
	if strings.Contains(p, "(allow file-write*)\n") {
		t.Errorf("writing must be allowed place by place, never everywhere")
	}
	order := []string{`(deny file-read* file-write* (subpath "/Users/x/.ssh"))`, `(allow file-read* (literal "/Users/x/proj/.env"))`, `(allow file-read* (subpath "/Users/x/.foundry/keystores/deployer"))`, `(allow file-write* (subpath "/Users/x/proj"))`, `(deny file-write* (subpath "/Users/x/proj/.git/hooks"))`, `(allow network-outbound (remote ip "localhost:*"))`}
	at := 0
	for _, rule := range order {
		i := strings.Index(p[at:], rule)
		if i < 0 {
			t.Fatalf("missing or out of order: %s\n%s", rule, p)
		}
		at += i
	}
	// The keychain folder is closed after every rule that opens something for reading.
	if strings.LastIndex(p, `(deny file-read* file-write* (subpath "/Users/x/Library/Keychains"))`) < strings.LastIndex(p, "(allow file-read*") {
		t.Errorf("the keychain folder must be closed last")
	}
	open := sandboxProfile(Spec{Home: "/Users/x", NetOpen: true, AllowUSB: true})
	if !strings.Contains(open, "(allow network*)") || !strings.Contains(open, "(allow iokit-open)") {
		t.Errorf("an open network and a hardware wallet are allowed when the run says so")
	}
}

func TestQuoting(t *testing.T) {
	if got := sbString(`/a "b"\c`); got != `"/a \"b\"\\c"` {
		t.Errorf("got %s", got)
	}
}

// The real thing: commands run under the profile, in a home folder made for the test.
func TestSandboxHolds(t *testing.T) {
	if err := sandboxAvailable(); err != nil {
		t.Skip(err)
	}
	// The stand-in home folder cannot be in the system's temporary folder, which the sandbox lets commands write.
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Skip(err)
	}
	home, err := os.MkdirTemp(cache, "keymoat-test-")
	if err != nil {
		t.Skip(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	home = realPath(home)
	root := filepath.Join(home, "proj")
	secret := filepath.Join(home, ".ssh", "id")
	keyfile := filepath.Join(home, ".foundry", "keystores", "deployer")
	other := filepath.Join(home, ".foundry", "keystores", "other")
	write(t, secret, "secret")
	write(t, keyfile, "the named key")
	write(t, other, "another key")
	write(t, filepath.Join(root, ".env"), "KEY=1")
	write(t, filepath.Join(root, ".env.safe"), "A=1")
	write(t, filepath.Join(root, ".env.example"), "KEY=")
	write(t, filepath.Join(home, "elsewhere", ".env"), "KEY=2")
	write(t, filepath.Join(root, ".git", "hooks", "keep"), "")
	write(t, filepath.Join(home, ".npm", "keep"), "")

	spec := Spec{Home: home, Root: root, DenyRead: []string{filepath.Join(home, ".ssh"), filepath.Join(home, ".foundry", "keystores")}, AllowRead: []string{keyfile}, HideEnvFiles: true, AllowEnvFiles: []string{filepath.Join(root, ".env.safe")}, AllowWrite: []string{filepath.Join(home, ".npm")}, DenyWrite: []string{filepath.Join(root, ".git", "hooks"), filepath.Join(root, ".env")}, SocketDirs: []string{root}, ProxyPort: 1}
	run := func(name string, args ...string) error {
		cmd, cleanup, err := sandboxCommand(spec, name, args)
		if err != nil {
			t.Fatal(err)
		}
		defer cleanup()
		cmd.Dir = root
		return cmd.Run()
	}
	mustFail := map[string][]string{
		"read a secret":                 {"/bin/cat", secret},
		"list a secret folder":          {"/bin/ls", filepath.Join(home, ".ssh")},
		"read a key that was not named": {"/bin/cat", other},
		"read the project's env file":   {"/bin/cat", filepath.Join(root, ".env")},
		"read another project's env":    {"/bin/cat", filepath.Join(home, "elsewhere", ".env")},
		"write in the home folder":      {"/usr/bin/touch", filepath.Join(home, "evil")},
		"write a shell start-up file":   {"/usr/bin/touch", filepath.Join(home, ".zshrc")},
		"write a git hook":              {"/usr/bin/touch", filepath.Join(root, ".git", "hooks", "pre-commit")},
		"overwrite the named key":       {"/usr/bin/touch", keyfile},
		"start another app":             {"/usr/bin/open", "-g", "-a", "Calculator"},
		"rename the hidden env file":    {"/bin/mv", filepath.Join(root, ".env"), filepath.Join(root, "renamed.txt")},
		"delete the hidden env file":    {"/bin/rm", filepath.Join(root, ".env")},
		"hard-link the hidden env file": {"/bin/ln", filepath.Join(root, ".env"), filepath.Join(root, "linked.txt")},
		"hard-link a secret":            {"/bin/ln", secret, filepath.Join(root, "linked2.txt")},
		"write outside home and temp":   {"/usr/bin/touch", "/Users/Shared/keymoat-test-probe"},
		"run a setuid program":          {"/bin/ps", "-p", "1"},
		"use a socket outside":          {"/usr/bin/python3", "-c", "import socket; s=socket.socket(socket.AF_UNIX); s.bind('/private/tmp/keymoat-test.sock')"},
		"open the login keychain":       {"/usr/bin/security", "show-keychain-info", filepath.Join(home, "Library", "Keychains", "login.keychain-db")},
	}
	for what, c := range mustFail {
		if err := run(c[0], c[1:]...); err == nil {
			t.Errorf("the sandbox let a command %s", what)
		}
	}
	mustWork := map[string][]string{
		"read the named key":          {"/bin/cat", keyfile},
		"read a harmless env file":    {"/bin/cat", filepath.Join(root, ".env.safe")},
		"read an example env file":    {"/bin/cat", filepath.Join(root, ".env.example")},
		"write in the project":        {"/usr/bin/touch", filepath.Join(root, "out.txt")},
		"write in a cache":            {"/usr/bin/touch", filepath.Join(home, ".npm", "x")},
		"write a temporary file":      {"/usr/bin/touch", filepath.Join(os.TempDir(), "keymoat-test-tmp")},
		"run a shell":                 {"/bin/sh", "-c", "echo ok | /usr/bin/grep -q ok"},
		"use a socket in the project": {"/usr/bin/python3", "-c", "import socket,os; os.chdir('" + root + "'); s=socket.socket(socket.AF_UNIX); s.bind('t.sock'); s.listen(1); socket.socket(socket.AF_UNIX).connect('t.sock')"},
	}
	for what, c := range mustWork {
		if err := run(c[0], c[1:]...); err != nil {
			t.Errorf("the sandbox stopped a command that should %s: %v", what, err)
		}
	}
	os.Remove(filepath.Join(os.TempDir(), "keymoat-test-tmp"))
	os.Remove("/Users/Shared/keymoat-test-probe")
	os.Remove("/private/tmp/keymoat-test.sock")
	if data, err := os.ReadFile(filepath.Join(root, ".env")); err != nil || string(data) != "KEY=1" {
		t.Errorf("the hidden env file was changed or removed: %q %v", data, err)
	}

	// A listener on this machine's own outward address stands in for the internet: it must be out of reach,
	// while a local one is not.
	if _, err := exec.LookPath("nc"); err == nil {
		if err := run("/bin/sh", "-c", "nc -z -w 2 1.1.1.1 443"); err == nil {
			t.Errorf("the sandbox let a command connect out directly")
		}
	}
}
