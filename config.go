package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Everything keymoat keeps lives in one folder in the home directory. A sandboxed command can read none of the
// secrets in it and can write none of it: the hosts a project may reach are decided outside the sandbox.

func stateDir() string {
	if d := os.Getenv("KEYMOAT_HOME"); d != "" {
		return d
	}
	return filepath.Join(homeDir(), ".keymoat")
}

func homeDir() string {
	h, err := os.UserHomeDir()
	if err != nil || h == "" {
		fatal("cannot find the home directory")
	}
	return h
}

// ProjectTrust is what the user decided about one project's hosts.
type ProjectTrust struct {
	Hosts  []string `json:"hosts,omitempty"`
	Denied []string `json:"denied,omitempty"`
}

// Config is the user's own settings. Every list adds to the built-in one; nothing here removes a built-in protection
// except AllowRead, which is there for the case a tool truly needs a protected file.
type Config struct {
	AllowHosts []string                 `json:"allowHosts,omitempty"`
	Protect    []string                 `json:"protect,omitempty"`
	AllowRead  []string                 `json:"allowRead,omitempty"`
	AllowWrite []string                 `json:"allowWrite,omitempty"`
	Projects   map[string]*ProjectTrust `json:"projects,omitempty"`
	// Vault maps the path a Solana keypair used to have to its name in the vault, so settings that still point at
	// the old path keep working.
	Vault map[string]string `json:"vault,omitempty"`
}

func configPath() string { return filepath.Join(stateDir(), "config.json") }

func loadConfig() *Config {
	c := &Config{}
	data, err := os.ReadFile(configPath())
	if err == nil {
		if err := json.Unmarshal(data, c); err != nil {
			fatal("%s is not valid JSON: %v", configPath(), err)
		}
	}
	if c.Projects == nil {
		c.Projects = map[string]*ProjectTrust{}
	}
	if c.Vault == nil {
		c.Vault = map[string]string{}
	}
	return c
}

func (c *Config) save() error {
	if err := os.MkdirAll(stateDir(), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := configPath() + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, configPath())
}

func (c *Config) project(root string) *ProjectTrust {
	p := c.Projects[root]
	if p == nil {
		p = &ProjectTrust{}
		c.Projects[root] = p
	}
	return p
}

func addUnique(list []string, items ...string) []string {
	seen := map[string]bool{}
	for _, x := range list {
		seen[x] = true
	}
	for _, x := range items {
		if x != "" && !seen[x] {
			seen[x] = true
			list = append(list, x)
		}
	}
	sort.Strings(list)
	return list
}

// expandHome turns "~/x" into an absolute path; other paths are returned cleaned.
func expandHome(p string) string {
	if p == "~" {
		return homeDir()
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(homeDir(), p[2:])
	}
	return filepath.Clean(p)
}

// Event is one line of the log: what keymoat stopped, asked or let through with keys.
type Event struct {
	Time    string   `json:"time"`
	Kind    string   `json:"kind"` // blocked-host, gate-confirmed, gate-declined, keys-visible, bypass
	Tool    string   `json:"tool,omitempty"`
	Project string   `json:"project,omitempty"`
	Detail  string   `json:"detail,omitempty"`
	Args    []string `json:"args,omitempty"`
}

func eventsPath() string { return filepath.Join(stateDir(), "events.jsonl") }

func logEvent(e Event) {
	e.Time = time.Now().UTC().Format(time.RFC3339)
	e.Args = redactArgs(e.Args)
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	if os.MkdirAll(stateDir(), 0o700) != nil {
		return
	}
	f, err := os.OpenFile(eventsPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	f.Write(append(data, '\n'))
}

// redactArgs keeps a command line useful in the log without keeping a secret that was typed into it.
func redactArgs(args []string) []string {
	out := make([]string, len(args))
	hide := false
	for i, a := range args {
		switch {
		case hide:
			out[i], hide = "<hidden>", false
		case secretFlags[a]:
			out[i], hide = a, true
		case walletSecretKind(a) != "":
			out[i] = "<hidden>"
		default:
			out[i] = a
			if k, v, ok := strings.Cut(a, "="); ok && (secretFlags[k] || walletSecretKind(v) != "") {
				out[i] = k + "=<hidden>"
			}
		}
	}
	return out
}

var secretFlags = map[string]bool{"--private-key": true, "--private-keys": true, "--mnemonic": true, "--mnemonics": true, "--password": true, "--unsafe-password": true}

func cmdLog(args []string) int {
	n := 20
	asJSON := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--json":
			asJSON = true
		case "-n":
			if i+1 < len(args) {
				fmt.Sscanf(args[i+1], "%d", &n)
				i++
			}
		default:
			return usageError("keymoat log [-n COUNT] [--json]")
		}
	}
	data, err := os.ReadFile(eventsPath())
	if err != nil {
		fmt.Println("Nothing logged yet.")
		return 0
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if n > 0 && len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	for _, line := range lines {
		if asJSON {
			fmt.Println(line)
			continue
		}
		var e Event
		if json.Unmarshal([]byte(line), &e) != nil {
			continue
		}
		when := e.Time
		if t, err := time.Parse(time.RFC3339, e.Time); err == nil {
			when = t.Local().Format("2006-01-02 15:04")
		}
		fmt.Printf("%s  %-14s %-7s %s\n", when, e.Kind, e.Tool, e.Detail)
	}
	return 0
}

func fatal(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "keymoat: "+format+"\n", a...)
	os.Exit(2)
}

func say(format string, a ...any) { fmt.Fprintf(os.Stderr, "keymoat: "+format+"\n", a...) }

func usageError(usage string) int {
	fmt.Fprintln(os.Stderr, "usage: "+usage)
	return 2
}
