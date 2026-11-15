package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"
)

// One run of a tool through keymoat: decide what the command is, ask first when it signs, and run it in the
// sandbox when it runs the project's code.

type runOptions struct {
	keys    bool // the user asked for keys to be visible
	netOpen bool // the user asked for no network restriction
}

func shimDir() string { return filepath.Join(stateDir(), "bin") }

// findReal finds the tool itself on the PATH: the first one that is not keymoat standing in for it.
func findReal(tool string) (string, error) {
	if strings.Contains(tool, "/") {
		return tool, nil
	}
	self, _ := os.Executable()
	self = realPath(self)
	shims := realPath(shimDir())
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" || realPath(dir) == shims {
			continue
		}
		p := filepath.Join(dir, tool)
		info, err := os.Stat(p)
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			continue
		}
		if realPath(p) == self {
			continue
		}
		return p, nil
	}
	return "", fmt.Errorf("%s is not installed (it is not on the PATH)", tool)
}

func newContext() *Context {
	home := realPath(homeDir())
	cwd := realPath(mustGetwd())
	c := &Context{Cwd: cwd, Home: home, Root: projectRoot(cwd, home), Env: os.Environ()}
	if c.Root != "" {
		c.EnvFiles = projectEnvFiles(c.Root)
	}
	return c
}

// execDirect replaces keymoat with the tool: nothing to clean up afterwards.
func execDirect(real string, args []string, env []string) int {
	err := syscall.Exec(real, append([]string{real}, args...), env)
	say("cannot run %s: %v", real, err)
	return 127
}

func runTool(tool string, args []string, opts runOptions) int {
	real, err := findReal(tool)
	if err != nil {
		say("%v", err)
		return 127
	}
	name := filepath.Base(tool)
	// Already inside a sandbox (a tool calling a tool): the outer run's rules hold, and a second sandbox cannot start.
	if os.Getenv("KEYMOAT_ACTIVE") == "1" {
		if insideSandbox() {
			return execDirect(real, args, os.Environ())
		}
		// The variable alone proves nothing: a project could set it to switch keymoat off.
		say("KEYMOAT_ACTIVE is set, but this is not a keymoat sandbox: ignoring it")
	}
	if strings.EqualFold(os.Getenv("KEYMOAT"), "off") {
		say("off for this command (KEYMOAT=off): %s runs unprotected", name)
		logEvent(Event{Kind: "bypass", Tool: name, Args: args})
		return execDirect(real, args, os.Environ())
	}
	cleanStaleKeys()
	opts.keys = opts.keys || os.Getenv("KEYMOAT_KEYS") == "1"
	opts.netOpen = opts.netOpen || strings.EqualFold(os.Getenv("KEYMOAT_NET"), "open")

	c := newContext()
	plan := Plan{Tool: name, Args: args, Sandbox: true}
	if _, known := tools[name]; known {
		plan = classify(name, args, c)
	}
	if opts.keys && plan.Mode == ModeNoKeys {
		plan.Mode = ModeSigning
		plan.Notes = append(plan.Notes, "keys are visible to this command, as you asked")
		logEvent(Event{Kind: "keys-visible", Tool: name, Project: c.Root, Args: args})
	}
	plan.NetOpen = plan.NetOpen || opts.netOpen

	if plan.Gate != nil && !plan.Gate.Local {
		if !confirmGate(plan.Gate, name) {
			logEvent(Event{Kind: "gate-declined", Tool: name, Project: c.Root, Detail: plan.Gate.Action + " on " + plan.Gate.Network, Args: args})
			say("not run.")
			return 1
		}
		logEvent(Event{Kind: "gate-confirmed", Tool: name, Project: c.Root, Detail: plan.Gate.Action + " on " + plan.Gate.Network, Args: args})
	}
	for _, n := range plan.Notes {
		say("%s", n)
	}
	if plan.WarmUp != "" {
		warmUp(plan.WarmUp)
	}
	if hasFlag(plan.Args, "--provider.wallet") && firstFlagValue(plan.Args, "--provider.wallet") == devWalletPath() {
		if _, err := devWallet(); err != nil {
			say("cannot make the throwaway wallet: %v", err)
			return 1
		}
	}

	// Keys from the vault become files for the length of this command.
	var cleanups []func()
	defer func() {
		for _, f := range cleanups {
			f()
		}
	}()
	for _, ref := range plan.VaultRefs {
		path, cleanup, err := materialize(ref.name)
		if err != nil {
			say("%v", err)
			return 1
		}
		cleanups = append(cleanups, cleanup)
		plan.Args = withKeyPath(plan.Args, ref, path)
		plan.KeyFiles = append(plan.KeyFiles, path)
	}

	if !plan.Sandbox {
		if len(cleanups) == 0 {
			return execDirect(real, plan.Args, os.Environ())
		}
		cmd := exec.Command(real, plan.Args...)
		return wait(cmd)
	}
	if err := sandboxAvailable(); err != nil {
		say("%v", err)
		say("to run without protection: KEYMOAT=off %s ...", name)
		return 126
	}
	return runSandboxed(real, plan, c, &cleanups)
}

// warmUp runs a program once and throws the result away. Anchor gives the test validator five seconds to come
// up; the first start of that large program after a while can take longer than that on macOS, and the test run
// then fails before it began. Starting it once beforehand takes that wait out of Anchor's five seconds.
func warmUp(program string) {
	path, err := exec.LookPath(program)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	exec.CommandContext(ctx, path, "--version").Run()
}

// withKeyPath puts the temporary key file where the tool expects a path.
func withKeyPath(args []string, ref vaultRef, path string) []string {
	if ref.explicit {
		out := append([]string{}, args...)
		for i, a := range out {
			if (a == ref.flag || a == "-k") && i+1 < len(out) {
				out[i+1] = path
			} else if strings.HasPrefix(a, ref.flag+"=") {
				out[i] = ref.flag + "=" + path
			}
		}
		return out
	}
	if ref.prepend {
		return append([]string{ref.flag, path}, args...)
	}
	return append(append([]string{}, args...), ref.flag, path)
}

var npmToken = regexp.MustCompile(`(?m)^[^#;\n]*(_authToken|_auth|_password)\s*=`)

func runSandboxed(real string, plan Plan, c *Context, cleanups *[]func()) int {
	cfg := loadConfig()
	spec := Spec{Home: c.Home, Root: c.Root, NetOpen: plan.NetOpen, AllowUSB: plan.USB}
	spec.DenyRead = protectedPaths(c.Home, cfg)
	spec.AllowWrite = append(writablePaths(c.Home, cfg), realPaths(plan.ExtraWrite)...)
	if c.Root != "" {
		spec.DenyWrite = append(spec.DenyWrite, filepath.Join(c.Root, ".git", "hooks"))
	}
	// keymoat's own folder, wherever it is: the tool links and the settings decide what the next run may do.
	spec.DenyWrite = append(spec.DenyWrite, realPath(stateDir()))
	// A registry token is a secret like any other, unless this command is the one that publishes.
	if npmrc := filepath.Join(c.Home, ".npmrc"); plan.Mode != ModeAuth && !allowedToRead(cfg, npmrc) {
		if data, err := os.ReadFile(npmrc); err == nil && npmToken.Match(data) {
			spec.DenyRead = append(spec.DenyRead, realPath(npmrc))
		}
	}
	for _, k := range realPaths(plan.KeyFiles) {
		switch keyFileVerdict(k, spec.DenyRead, c.Home, plan.Mode) {
		case keyOpen:
			spec.AllowRead = append(spec.AllowRead, k)
		case keyRefused:
			say("not opening %s for this command: it is in a protected folder and is not a key file", k)
		}
	}

	// Env files: every one on the machine is hidden, except this project's. In a run without keys, the project's
	// own are hidden too when they hold one, and what is harmless in them is handed over through the environment.
	spec.HideEnvFiles = true
	var hidden []EnvFile
	for _, f := range c.EnvFiles {
		if plan.Mode != ModeSigning && len(f.Secrets) > 0 {
			hidden = append(hidden, f)
			// Unwritable too: a file that can be renamed can be read under its new name.
			spec.DenyWrite = append(spec.DenyWrite, f.Path)
			if runtime.GOOS != "darwin" {
				spec.DenyRead = append(spec.DenyRead, f.Path)
			}
			continue
		}
		spec.AllowEnvFiles = append(spec.AllowEnvFiles, f.Path)
	}
	// Linux cannot hide files by a name pattern, so the env files of the projects next to this one are covered
	// one by one. Projects kept elsewhere are out of its reach: see the README.
	if runtime.GOOS != "darwin" && c.Root != "" {
		spec.DenyRead = append(spec.DenyRead, neighbourEnvFiles(c.Root)...)
	}
	// Wallet files kept in the project (a keypair, a `.secret`) are closed like the env files that hold a key.
	hiddenFiles := 0
	if plan.Mode != ModeSigning && c.Root != "" {
		for _, p := range projectSecretFiles(c.Root) {
			if !contains(spec.AllowRead, p) {
				spec.DenyRead = append(spec.DenyRead, p)
				spec.DenyWrite = append(spec.DenyWrite, p)
				hiddenFiles++
			}
		}
	}
	env, dropped := os.Environ(), []string(nil)
	if plan.Mode != ModeSigning {
		env, dropped = childEnv(os.Environ(), hidden, c.Cwd, c.Root)
	}

	// The network: the built-in hosts, the user's, the project's approved ones, and what was typed on the command line.
	var proxy *Proxy
	if !plan.NetOpen {
		trust := &ProjectTrust{}
		if c.Root != "" {
			trust = cfg.project(c.Root)
			found := projectHosts(c.Root, c.EnvFiles, os.Environ())
			if fresh := newHosts(found, cfg, trust); len(fresh) > 0 {
				askAboutHosts(fresh, found, cfg, trust)
			}
		}
		allowed := append(append(append([]string{}, defaultHosts...), cfg.AllowHosts...), trust.Hosts...)
		allowed = append(allowed, plan.CLIHosts...)
		if rpc, _ := solanaConfig(c.Home); hostOf(rpc) != "" {
			allowed = append(allowed, hostOf(rpc))
		}
		proxy = newProxy(func(host string) bool { return hostMatches(host, allowed) })
		network, addr := proxyListenAddr()
		ln, err := net.Listen(network, addr)
		if err != nil {
			say("cannot start the network filter: %v", err)
			return 1
		}
		proxy.start(ln)
		defer proxy.stop()
		spec.ProxyPort = proxy.port()
		if spec.ProxyPort == 0 {
			spec.ProxyPort = bridgePortForEnv
		}
		env = append(env, proxyEnv(spec.ProxyPort)...)
		// Public repositories named by their SSH address are fetched over HTTPS: no SSH key is in reach here.
		env = append(env, "GIT_CONFIG_COUNT=2",
			"GIT_CONFIG_KEY_0=url.https://github.com/.insteadOf", "GIT_CONFIG_VALUE_0=git@github.com:",
			"GIT_CONFIG_KEY_1=url.https://github.com/.insteadOf", "GIT_CONFIG_VALUE_1=ssh://git@github.com/")
	}
	env = append(env, "KEYMOAT_ACTIVE=1")
	if c.Root != "" {
		spec.SocketDirs = append(spec.SocketDirs, c.Root)
	}
	// On macOS the command gets a temporary folder of its own, where it may also keep sockets. The system's is
	// shared with every other program.
	if runtime.GOOS == "darwin" {
		if tmp, err := privateTemp(c.Home); err == nil {
			spec.AllowWrite = append(spec.AllowWrite, tmp)
			spec.SocketDirs = append(spec.SocketDirs, tmp)
			env = append(env, "TMPDIR="+tmp+"/")
		}
	}
	if err := writeCanary(); err != nil {
		say("cannot write to %s: %v", stateDir(), err)
		return 1
	}
	gitBefore := gitSettings(c.Root)

	cmd, cleanup, err := sandboxCommand(spec, real, plan.Args)
	if err != nil {
		say("cannot start the sandbox: %v", err)
		return 1
	}
	*cleanups = append(*cleanups, cleanup)
	cmd.Env = env
	code := wait(cmd)

	for _, line := range newGitPrograms(gitBefore, gitSettings(c.Root)) {
		say("this command changed the repository's git settings to run a program: %s", line)
		say("check %s before your next git command", filepath.Join(c.Root, ".git", "config"))
	}
	if proxy != nil {
		if blocked := proxy.blockedHosts(); len(blocked) > 0 {
			say("blocked %s to %s", plural(len(blocked), "connection attempt", "connection attempts"), strings.Join(blocked, ", "))
			say("if %s should reach a host: keymoat allow HOST", plan.Tool)
			for _, h := range blocked {
				logEvent(Event{Kind: "blocked-host", Tool: plan.Tool, Project: c.Root, Detail: h})
			}
		}
	}
	if code != 0 && hiddenFiles > 0 {
		say("%s in this project hidden from this command (`keymoat scan` lists them). If the command has to sign, run it with KEYMOAT_KEYS=1.", plural(hiddenFiles, "wallet file was", "wallet files were"))
	}
	if code != 0 && len(dropped) > 0 {
		say("%s was hidden from this command, like every key. If the command has to sign, run it with KEYMOAT_KEYS=1.", strings.Join(dropped, ", "))
	}
	return code
}

// privateTemp is the temporary folder sandboxed commands get on macOS. It is kept between runs, because tools
// cache there; what has not been touched for a week is removed.
func privateTemp(home string) (string, error) {
	dir := filepath.Join(home, "Library", "Caches", "keymoat", "tmp")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	dir = realPath(dir)
	if entries, err := os.ReadDir(dir); err == nil {
		old := time.Now().Add(-7 * 24 * time.Hour)
		for _, e := range entries {
			if info, err := e.Info(); err == nil && info.ModTime().Before(old) {
				os.RemoveAll(filepath.Join(dir, e.Name()))
			}
		}
	}
	return dir, nil
}

// The canary is a file every sandboxed run is forbidden to read. A process that can read it is not in a sandbox,
// whatever its environment says.
const canaryName = "canary"

func writeCanary() error {
	path := filepath.Join(stateDir(), canaryName)
	if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
		return nil
	}
	if err := os.MkdirAll(stateDir(), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte("keymoat\n"), 0o600)
}

// insideSandbox reports whether this process runs in a keymoat sandbox: the canary is there and cannot be read
// (macOS refuses, Linux shows an empty file in its place).
func insideSandbox() bool {
	data, err := os.ReadFile(filepath.Join(stateDir(), canaryName))
	if err != nil {
		return os.IsPermission(err)
	}
	return len(data) == 0
}

// Git settings that make git run a program. The project is writable, so a command can set one; git would then
// run it outside the sandbox the next time the user uses git. keymoat cannot forbid writing the settings (a
// dependency install needs to), so it says when one of these appears.
var gitProgramSetting = regexp.MustCompile(`(?i)^\s*(fsmonitor|sshcommand|hookspath|pager|editor|gitproxy|askpass|helper|command|textconv|clean|smudge|process|program|external|sequenceeditor)\s*=|^\s*\[\s*alias\s*\]`)

func gitSettings(root string) map[string]bool {
	out := map[string]bool{}
	if root == "" {
		return out
	}
	data, err := os.ReadFile(filepath.Join(root, ".git", "config"))
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(data), "\n") {
		if gitProgramSetting.MatchString(line) {
			out[strings.TrimSpace(line)] = true
		}
	}
	return out
}

func newGitPrograms(before, after map[string]bool) []string {
	var out []string
	for line := range after {
		if !before[line] {
			out = append(out, line)
		}
	}
	sort.Strings(out)
	return out
}

const (
	keyNotProtected = iota // nothing to open: the file is readable anyway
	keyOpen
	keyRefused
)

// Names of the registry credential files a publish may read.
var registryFiles = map[string]bool{".npmrc": true, ".yarnrc.yml": true, ".bunfig.toml": true, "credentials.toml": true, "credentials": true}

// keyFileVerdict decides whether a file a command names as its key may be taken off the protected list for the
// run. The name of a key file can come from a project file or from a script the user did not read, so it is not
// trusted: only one file is ever opened, never a folder, and only a file that is what it claims to be (a keystore
// in Foundry's folder, a Solana keypair, a key keymoat itself handed out, or the registry's token for a publish).
func keyFileVerdict(path string, protected []string, home string, mode Mode) int {
	inProtected := false
	for _, d := range protected {
		if within(path, d) {
			inProtected = true
		}
	}
	if !inProtected {
		return keyNotProtected
	}
	info, err := os.Lstat(path)
	if err != nil {
		return keyNotProtected
	}
	if !info.Mode().IsRegular() {
		return keyRefused
	}
	switch {
	case filepath.Dir(path) == realPath(filepath.Join(home, ".foundry", "keystores")):
	case isSolanaKeypairFile(path):
	case within(path, realPath(filepath.Join(stateDir(), "run"))):
	case mode == ModeAuth && registryFiles[filepath.Base(path)]:
	default:
		return keyRefused
	}
	return keyOpen
}

// cleanStaleKeys removes vault keys that a run left behind because it was killed before it could clean up.
func cleanStaleKeys() {
	dir := filepath.Join(stateDir(), "run")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	old := time.Now().Add(-12 * time.Hour)
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || info.ModTime().After(old) {
			continue
		}
		sub := filepath.Join(dir, e.Name())
		if files, err := os.ReadDir(sub); err == nil {
			for _, f := range files {
				wipe(filepath.Join(sub, f.Name()))
			}
		}
		os.Remove(sub)
	}
}

// allowedToRead reports whether the user's settings open a path that keymoat would otherwise protect.
func allowedToRead(cfg *Config, path string) bool {
	for _, p := range cfg.AllowRead {
		if realPath(expandHome(p)) == realPath(path) {
			return true
		}
	}
	return false
}

// neighbourEnvFiles lists the env files of the folders that sit beside a project.
func neighbourEnvFiles(root string) []string {
	var out []string
	walkProject(filepath.Dir(root), 2, func(path, name string) {
		if isEnvFileName(name) && !within(path, root) {
			out = append(out, path)
		}
	})
	return out
}

// bridgePortForEnv is the port tools use as their proxy when the proxy itself listens on a socket file (Linux).
const bridgePortForEnv = 18080

func realPaths(list []string) []string {
	var out []string
	for _, p := range list {
		out = append(out, realPath(p))
	}
	return out
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// askAboutHosts asks once about hosts the project's files point at. Without a terminal nothing is allowed and
// nothing is recorded, so the question comes up the next time there is someone to answer it.
func askAboutHosts(fresh []string, found map[string]string, cfg *Config, trust *ProjectTrust) {
	if !interactive() {
		return
	}
	fmt.Fprintf(os.Stderr, "\nkeymoat: this project's files point at %s keymoat has not seen before:\n\n", plural(len(fresh), "host", "hosts"))
	for _, h := range fresh {
		fmt.Fprintf(os.Stderr, "  %-40s (%s)\n", h, found[h])
	}
	answer, err := readLine("\nLet this project's commands connect to them? [y/N] ")
	if err != nil {
		return
	}
	if strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes") {
		trust.Hosts = addUnique(trust.Hosts, fresh...)
	} else {
		trust.Denied = addUnique(trust.Denied, fresh...)
		fmt.Fprintln(os.Stderr, "keymoat: not allowed. `keymoat allow HOST` changes that later.")
	}
	if err := cfg.save(); err != nil {
		say("could not save the answer: %v", err)
	}
}

// wait runs a command with the terminal attached and returns its exit code. An interrupt goes to the command, which
// shares the terminal; keymoat stays to clean up after it.
func wait(cmd *exec.Cmd) int {
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)
	defer signal.Stop(sigs)
	if err := cmd.Start(); err != nil {
		say("cannot run %s: %v", filepath.Base(cmd.Path), err)
		return 127
	}
	done := make(chan struct{})
	go func() {
		for {
			select {
			case s := <-sigs:
				// The terminal already delivered an interrupt to the whole group; the others are passed on.
				if s != syscall.SIGINT && s != syscall.SIGQUIT {
					cmd.Process.Signal(s)
				}
			case <-done:
				return
			}
		}
	}()
	err := cmd.Wait()
	close(done)
	if err == nil {
		return 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return 128 + int(ws.Signal())
		}
		return ee.ExitCode()
	}
	say("%v", err)
	return 1
}

func cmdRun(args []string) int {
	var opts runOptions
	i := 0
	for ; i < len(args); i++ {
		switch args[i] {
		case "--keys":
			opts.keys = true
			continue
		case "--open-net":
			opts.netOpen = true
			continue
		case "--":
			i++
		}
		break
	}
	if i >= len(args) {
		return usageError("keymoat run [--keys] [--open-net] -- COMMAND [ARGS...]")
	}
	return runTool(args[i], args[i+1:], opts)
}
