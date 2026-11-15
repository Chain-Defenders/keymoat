//go:build linux

package main

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareWritable(t *testing.T) {
	home := realPath(t.TempDir())
	write(t, filepath.Join(home, ".cargo", "config.toml"), "")
	s := Spec{Home: home, AllowWrite: []string{filepath.Join(home, ".cargo", "registry"), filepath.Join(home, ".cargo", ".package-cache"), filepath.Join(home, ".npm"), filepath.Join(home, ".bun", "install", "cache"), filepath.Join(home, ".svm"), filepath.Join(home, ".rustup", "toolchains", "solana")}}
	write(t, filepath.Join(home, ".rustup", "settings.toml"), "")
	prepareWritable(s)
	for path, want := range map[string]bool{".cargo/registry": true, ".cargo/.package-cache": true, ".npm": true, ".bun": false, ".svm": false, ".rustup/toolchains/solana": false} {
		if got := exists(filepath.Join(home, path)); got != want {
			t.Errorf("%s exists = %v, want %v", path, got, want)
		}
	}
	if isDir(filepath.Join(home, ".cargo", ".package-cache")) || !isDir(filepath.Join(home, ".cargo", "registry")) {
		t.Errorf("a lock file is a file and a registry is a folder")
	}
}

func TestBwrapArgs(t *testing.T) {
	home := realPath(t.TempDir())
	root := filepath.Join(home, "proj")
	write(t, filepath.Join(home, ".ssh", "id"), "secret")
	write(t, filepath.Join(home, ".netrc"), "secret")
	write(t, filepath.Join(home, ".foundry", "keystores", "deployer"), "key")
	write(t, filepath.Join(root, "foundry.toml"), "")
	write(t, filepath.Join(home, ".npm", "keep"), "")
	sock := filepath.Join(home, "proxy.sock")
	write(t, sock, "")
	s := Spec{Home: home, Root: root, DenyRead: []string{filepath.Join(home, ".ssh"), filepath.Join(home, ".netrc"), filepath.Join(home, ".foundry", "keystores"), filepath.Join(home, ".missing")}, AllowRead: []string{filepath.Join(home, ".foundry", "keystores", "deployer")}, AllowWrite: []string{filepath.Join(home, ".npm")}}
	line := " " + strings.Join(bwrapArgs(s, sock, "/usr/bin/keymoat", "/usr/bin/forge", []string{"test"}), " ") + " "
	for _, must := range []string{
		" --ro-bind / / ", " --unshare-net ", " --die-with-parent ",
		" --tmpfs " + filepath.Join(home, ".ssh") + " ",
		" --ro-bind /dev/null " + filepath.Join(home, ".netrc") + " ",
		" --bind " + root + " " + root + " ",
		" --bind " + filepath.Join(home, ".npm") + " ",
		" -- /usr/bin/keymoat __bridge 18080 " + sock + " /usr/bin/forge test ",
	} {
		if !strings.Contains(line, must) {
			t.Errorf("missing %q in:\n%s", must, line)
		}
	}
	if strings.Contains(line, ".missing") {
		t.Errorf("a path that does not exist cannot be covered")
	}
	// The named key is mounted back after its folder was covered.
	if strings.Index(line, "--tmpfs "+filepath.Join(home, ".foundry", "keystores")) > strings.Index(line, "--ro-bind "+filepath.Join(home, ".foundry", "keystores", "deployer")) {
		t.Errorf("the key must be mounted after the cover:\n%s", line)
	}
	open := strings.Join(bwrapArgs(Spec{Home: home, NetOpen: true}, "", "/usr/bin/keymoat", "/usr/bin/forge", nil), " ")
	if strings.Contains(open, "--unshare-net") || strings.Contains(open, "__bridge") {
		t.Errorf("an open network needs no bridge: %s", open)
	}
}

// The real thing: commands run under bubblewrap, in a home folder made for the test.
func TestSandboxHolds(t *testing.T) {
	if err := sandboxAvailable(); err != nil {
		t.Skip(err)
	}
	if err := exec.Command("bwrap", "--ro-bind", "/", "/", "true").Run(); err != nil {
		t.Skip("bubblewrap cannot start here (unprivileged user namespaces are off)")
	}
	home := realPath(t.TempDir())
	root := filepath.Join(home, "proj")
	secret := filepath.Join(home, ".ssh", "id")
	keyfile := filepath.Join(home, ".foundry", "keystores", "deployer")
	other := filepath.Join(home, ".foundry", "keystores", "other")
	write(t, secret, "secret")
	write(t, filepath.Join(home, ".netrc"), "secret")
	write(t, keyfile, "the named key")
	write(t, other, "another key")
	write(t, filepath.Join(root, ".env"), "KEY=1")
	write(t, filepath.Join(root, ".git", "hooks", "keep"), "")
	write(t, filepath.Join(home, ".npm", "keep"), "")

	// The proxy's socket, as a run would have it: nothing is allowed through.
	network, addr := proxyListenAddr()
	ln, err := net.Listen(network, addr)
	if err != nil {
		t.Fatal(err)
	}
	p := newProxy(func(string) bool { return false })
	p.start(ln)
	defer p.stop()

	spec := Spec{Home: home, Root: root, DenyRead: []string{filepath.Join(home, ".ssh"), filepath.Join(home, ".netrc"), filepath.Join(home, ".foundry", "keystores"), filepath.Join(root, ".env")}, AllowRead: []string{keyfile}, AllowWrite: []string{filepath.Join(home, ".npm")}, DenyWrite: []string{filepath.Join(root, ".git", "hooks")}}
	run := func(script string) error {
		cmd, _, err := sandboxCommand(spec, "/bin/sh", []string{"-c", script})
		if err != nil {
			t.Fatal(err)
		}
		cmd.Dir = root
		return cmd.Run()
	}
	// A covered file reads as empty and a covered folder lists as empty: `test -s` and a non-empty listing must fail.
	mustFail := map[string]string{
		"read a secret":                 "test -s " + secret,
		"list a secret folder":          "test -n \"$(ls -A " + filepath.Join(home, ".ssh") + ")\"",
		"read a secret file":            "test -s " + filepath.Join(home, ".netrc"),
		"read a key that was not named": "test -s " + other,
		"read the project's env file":   "test -s " + filepath.Join(root, ".env"),
		"write a git hook":              "touch " + filepath.Join(root, ".git", "hooks", "pre-commit"),
		"overwrite the named key":       "echo x > " + keyfile,
		"connect out directly":          "exec 3<>/dev/tcp/1.1.1.1/80",
	}
	for what, script := range mustFail {
		if what == "connect out directly" {
			script = "bash -c '" + script + "'"
		}
		if err := run(script); err == nil {
			t.Errorf("the sandbox let a command %s", what)
		}
	}
	// The test's home folder is under /tmp, which the sandbox replaces with a scratch one: a write there may
	// succeed inside and must still not reach the real folder.
	run("touch " + filepath.Join(home, "evil"))
	if exists(filepath.Join(home, "evil")) {
		t.Errorf("the sandbox let a command write in the home folder")
	}
	mustWork := map[string]string{
		"read the named key":   "test -s " + keyfile,
		"write in the project": "touch " + filepath.Join(root, "out.txt"),
		"write in a cache":     "touch " + filepath.Join(home, ".npm", "x"),
		"reach the proxy":      "bash -c 'exec 3<>/dev/tcp/127.0.0.1/18080'",
	}
	for what, script := range mustWork {
		if err := run(script); err != nil {
			t.Errorf("the sandbox stopped a command that should %s: %v", what, err)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".npm", "x")); err != nil {
		t.Errorf("a write in a cache did not reach the real folder")
	}
}

// Inside the sandbox the program runs itself as the bridge; in a test, "itself" is the test binary.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "__bridge" {
		os.Exit(bridgeMain(os.Args[2:]))
	}
	os.Exit(m.Run())
}
