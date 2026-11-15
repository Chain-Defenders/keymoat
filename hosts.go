package main

import (
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Where a sandboxed command may connect. The built-in list is what the toolchains themselves need: package
// registries, compiler downloads, source hosts and contract verification. A project's own RPC endpoints are added
// only after the user has said yes to them once, because a project file is something a dependency can rewrite.

var defaultHosts = []string{
	// packages
	"registry.npmjs.org", "registry.yarnpkg.com", "repo.yarnpkg.com", "registry.npmmirror.com", "nodejs.org",
	"crates.io", "*.crates.io", "static.rust-lang.org",
	"api.soldeer.xyz", "*.soldeer.xyz", "soldeer-revisions.s3.amazonaws.com",
	// source
	"github.com", "api.github.com", "codeload.github.com", "*.githubusercontent.com", "gitlab.com", "bitbucket.org",
	// compilers and toolchains
	"binaries.soliditylang.org", "solc-bin.ethereum.org", "release.anza.xyz", "release.solana.com", "*.vyperlang.org",
	// verification
	"*.etherscan.io", "*.basescan.org", "*.arbiscan.io", "*.polygonscan.com", "*.bscscan.com", "*.snowtrace.io", "*.blockscout.com", "sourcify.dev", "*.sourcify.dev", "*.routescan.io",
	// the public Solana clusters
	"api.mainnet-beta.solana.com", "api.devnet.solana.com", "api.testnet.solana.com",
	// signature lookups
	"api.openchain.xyz", "*.4byte.directory", "sig.eth.samczsun.com",
}

// The public Solana clusters, which the Solana tools reach by name.
var solanaClusters = map[string]string{
	"mainnet-beta": "https://api.mainnet-beta.solana.com", "mainnet": "https://api.mainnet-beta.solana.com", "m": "https://api.mainnet-beta.solana.com",
	"devnet": "https://api.devnet.solana.com", "d": "https://api.devnet.solana.com",
	"testnet": "https://api.testnet.solana.com", "t": "https://api.testnet.solana.com",
	"localnet": "http://127.0.0.1:8899", "localhost": "http://127.0.0.1:8899", "l": "http://127.0.0.1:8899",
}

// hostMatches reports whether a host is covered by a list of names and "*.suffix" patterns.
func hostMatches(host string, list []string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, p := range list {
		p = strings.ToLower(p)
		if p == host {
			return true
		}
		if strings.HasPrefix(p, "*.") && (strings.HasSuffix(host, p[1:]) || host == p[2:]) {
			return true
		}
	}
	return false
}

func isLocalHost(host string) bool {
	host = strings.ToLower(strings.Trim(host, "[]"))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsUnspecified())
}

// hostOf returns the host of an http or websocket URL, or "".
func hostOf(raw string) string {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "://") {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	switch u.Scheme {
	case "http", "https", "ws", "wss":
		return strings.ToLower(u.Hostname())
	}
	return ""
}

var (
	urlInText = regexp.MustCompile(`(?:https?|wss?)://[A-Za-z0-9.\-]+(?::\d+)?[^\s"'` + "`" + `<>)\]}]*`)
	envRef    = regexp.MustCompile(`\$\{?([A-Za-z_][A-Za-z0-9_]*)\}?`)
)

// expandEnvRefs fills ${NAME} from the given values; a name it does not know becomes empty.
func expandEnvRefs(s string, values map[string]string) string {
	return envRef.ReplaceAllStringFunc(s, func(m string) string {
		return values[envRef.FindStringSubmatch(m)[1]]
	})
}

// tomlTable reads the string values of one table of a TOML file ("" for the keys before any table). It is enough
// for foundry.toml and Anchor.toml, which keep endpoints as plain `key = "value"` lines.
func tomlTable(data, table string) map[string]string {
	out := map[string]string{}
	current := ""
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			current = strings.Trim(line, "[] \t")
			continue
		}
		if current != table {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if i := strings.Index(v, " #"); i >= 0 {
			v = strings.TrimSpace(v[:i])
		}
		// `{ url = "...", key = "..." }` and `{ endpoint = "..." }` both carry the URL in quotes.
		if strings.HasPrefix(v, "{") {
			if m := regexp.MustCompile(`(?:url|endpoint)\s*=\s*"([^"]*)"`).FindStringSubmatch(v); m != nil {
				v = m[1]
			}
		}
		out[strings.Trim(strings.TrimSpace(k), `"'`)] = strings.Trim(v, `"'`)
	}
	return out
}

// foundryEndpoints returns the named RPC endpoints of a foundry.toml, with ${VAR} filled in.
func foundryEndpoints(root string, values map[string]string) map[string]string {
	data, err := os.ReadFile(filepath.Join(root, "foundry.toml"))
	if err != nil {
		return nil
	}
	out := map[string]string{}
	for name, v := range tomlTable(string(data), "rpc_endpoints") {
		out[name] = expandEnvRefs(v, values)
	}
	return out
}

// envValues is the caller's environment over the project's top-level env files: what a tool would resolve.
func envValues(environ []string, files []EnvFile, root string) map[string]string {
	out := map[string]string{}
	for _, f := range files {
		if filepath.Dir(f.Path) != root {
			continue
		}
		for _, v := range f.Vars {
			if _, ok := out[v.Name]; !ok {
				out[v.Name] = v.Value
			}
		}
	}
	for _, kv := range environ {
		k, v, _ := strings.Cut(kv, "=")
		out[k] = v
	}
	return out
}

// projectHosts finds the hosts a project's own files point at, each with where it was found.
func projectHosts(root string, files []EnvFile, environ []string) map[string]string {
	found := map[string]string{}
	add := func(raw, where string) {
		if h := hostOf(raw); h != "" && !isLocalHost(h) {
			if _, ok := found[h]; !ok {
				found[h] = where
			}
		}
	}
	values := envValues(environ, files, root)
	if data, err := os.ReadFile(filepath.Join(root, "foundry.toml")); err == nil {
		for _, u := range urlInText.FindAllString(expandEnvRefs(string(data), values), -1) {
			add(u, "foundry.toml")
		}
	}
	if data, err := os.ReadFile(filepath.Join(root, "Anchor.toml")); err == nil {
		for _, u := range urlInText.FindAllString(string(data), -1) {
			add(u, "Anchor.toml")
		}
	}
	for _, name := range []string{"hardhat.config.ts", "hardhat.config.js", "hardhat.config.cjs", "hardhat.config.mjs", "hardhat.config.cts", "hardhat.config.mts"} {
		if data, err := os.ReadFile(filepath.Join(root, name)); err == nil {
			for _, u := range urlInText.FindAllString(string(data), -1) {
				add(u, name)
			}
		}
	}
	for _, f := range files {
		for _, v := range f.Vars {
			add(v.Value, filepath.Base(f.Path))
		}
	}
	return found
}

// solanaConfig reads the two values keymoat needs from the Solana command-line tool's own settings.
func solanaConfig(home string) (rpcURL, keypair string) {
	data, err := os.ReadFile(filepath.Join(home, ".config", "solana", "cli", "config.yml"))
	if err != nil {
		return "", filepath.Join(home, ".config", "solana", "id.json")
	}
	keypair = filepath.Join(home, ".config", "solana", "id.json")
	for _, line := range strings.Split(string(data), "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		switch strings.TrimSpace(k) {
		case "json_rpc_url":
			rpcURL = v
		case "keypair_path":
			if v != "" {
				keypair = v
			}
		}
	}
	return rpcURL, keypair
}

// newHosts lists the project's hosts nothing has decided about yet.
func newHosts(found map[string]string, cfg *Config, trust *ProjectTrust) []string {
	var out []string
	for h := range found {
		if hostMatches(h, defaultHosts) || hostMatches(h, cfg.AllowHosts) || hostMatches(h, trust.Hosts) || hostMatches(h, trust.Denied) {
			continue
		}
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}

func cmdAllow(args []string) int {
	global := false
	var hosts []string
	for _, a := range args {
		switch {
		case a == "--global":
			global = true
		case strings.HasPrefix(a, "-"):
			return usageError("keymoat allow [--global] HOST...")
		default:
			h := strings.ToLower(a)
			if x := hostOf(a); x != "" {
				h = x
			}
			hosts = append(hosts, h)
		}
	}
	if len(hosts) == 0 {
		return usageError("keymoat allow [--global] HOST...")
	}
	cfg := loadConfig()
	if global {
		cfg.AllowHosts = addUnique(cfg.AllowHosts, hosts...)
	} else {
		cwd, _ := os.Getwd()
		root := projectRoot(cwd, homeDir())
		if root == "" {
			say("not inside a project; use --global to allow a host everywhere")
			return 2
		}
		t := cfg.project(root)
		t.Hosts = addUnique(t.Hosts, hosts...)
		var denied []string
		for _, d := range t.Denied {
			if !hostMatches(d, hosts) {
				denied = append(denied, d)
			}
		}
		t.Denied = denied
	}
	if err := cfg.save(); err != nil {
		fatal("%v", err)
	}
	say("allowed: %s", strings.Join(hosts, ", "))
	return 0
}
