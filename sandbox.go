package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Spec is what one sandboxed run may and may not touch. Paths are absolute and resolved (no symlinks), because the
// operating system compares the real path.
type Spec struct {
	Home string
	Root string // the project; "" when the command runs outside one

	DenyRead  []string // secrets: unreadable
	AllowRead []string // the exceptions for this run, such as the one key a deploy names

	HideEnvFiles  bool     // every .env file anywhere is unreadable...
	AllowEnvFiles []string // ...except these

	AllowWrite []string // the only places under the home directory that can be written
	DenyWrite  []string // and the places inside those that still cannot

	// Folders where a command may make and use socket files (how many tools talk to their own helpers). Not the
	// system's temporary folders: other programs keep sockets there, an editor's among them.
	SocketDirs []string

	NetOpen   bool // no network restriction (a Solana deploy talks to validators directly)
	ProxyPort int  // otherwise: only this local port, and other local services
	AllowUSB  bool // a hardware wallet
}

// Folders and files that hold secrets, relative to the home directory. A command run through keymoat cannot read
// them, whatever it is.
var protectedCommon = []string{
	// keys and wallets
	".ssh", ".gnupg", ".foundry/keystores", ".brownie/accounts", ".ethereum/keystore", ".electrum", ".bitcoin", ".config/solana/id.json",
	// cloud and registry credentials
	".aws", ".azure", ".kube", ".config/gcloud", ".config/gh", ".docker/config.json", ".netrc", ".git-credentials", ".cargo/credentials", ".cargo/credentials.toml",
	".config/hub", ".terraform.d/credentials.tfrc.json", ".vercel", ".config/vercel", ".fly", ".config/doctl",
	// password stores
	".password-store", ".config/op", ".config/Bitwarden CLI",
	// what was typed before
	".zsh_history", ".bash_history", ".zhistory", ".local/share/fish/fish_history", ".node_repl_history", ".python_history", ".psql_history",
}

var protectedDarwin = []string{
	"Library/Keychains", "Library/Ethereum/keystore", "Library/Cookies",
	// browsers: profiles hold wallet extensions' vaults and session cookies
	"Library/Application Support/Google", "Library/Application Support/BraveSoftware", "Library/Application Support/Firefox", "Library/Application Support/Microsoft Edge",
	"Library/Application Support/Arc", "Library/Application Support/Chromium", "Library/Application Support/Vivaldi", "Library/Application Support/com.operasoftware.Opera",
	"Library/Application Support/zen", "Library/Safari", "Library/Containers/com.apple.Safari",
	// wallet and password apps
	"Library/Application Support/Ledger Live", "Library/Application Support/Exodus", "Library/Application Support/Electrum", "Library/Application Support/atomic",
	"Library/Application Support/Bitwarden", "Library/Application Support/1Password", "Library/Group Containers/2BUA8C4S2C.com.1password", "Library/Containers/com.bitwarden.desktop",
	"Library/Application Support/Phantom", "Library/Application Support/Backpack", "Library/Application Support/Frame",
	// chat apps: sessions and saved messages
	"Library/Application Support/Telegram Desktop", "Library/Application Support/Slack", "Library/Application Support/discord", "Library/Group Containers/6N38VWS5BX.ru.keepcoder.Telegram",
	"Library/Messages", "Library/Mail",
}

var protectedLinux = []string{
	".local/share/keyrings", ".pki",
	".config/google-chrome", ".config/chromium", ".config/BraveSoftware", ".mozilla", ".config/microsoft-edge", ".config/vivaldi", ".config/opera",
	".config/Ledger Live", ".config/Exodus", ".config/Electrum", ".config/Bitwarden", ".config/1Password", ".config/Frame",
	".local/share/TelegramDesktop", ".config/Slack", ".config/discord", ".thunderbird",
}

// Where the toolchains keep downloads and caches, relative to the home directory: writable, because a build needs
// them. Nothing here is run by the shell or the editor on its own.
var writableCommon = []string{
	".foundry/cache", ".svm", ".local/share/svm", ".solcx", ".vvm",
	".npm", ".cache", ".pnpm-store", ".local/share/pnpm/store", ".local/state/pnpm", ".yarn/berry", ".yarn-cache", ".bun/install/cache", ".node-gyp",
	".cargo/registry", ".cargo/git", ".cargo/.package-cache", ".cargo/.package-cache-mutate", ".cargo/.global-cache",
	".config/hardhat-nodejs", ".local/share/hardhat-nodejs", ".config/.foundry",
	// The Solana build tools register their compiler with rustup under this one name; the compiler itself is in the cache.
	".rustup/toolchains/solana",
}

// Where the Solana build tools link the compiler they downloaded into the installed release: that one folder, in
// whichever releases are installed.
var writableGlobs = []string{
	".local/share/solana/install/releases/*/solana-release/bin/platform-tools-sdk/sbf/dependencies",
	".local/share/solana/install/releases/*/solana-release/bin/sdk/sbf/dependencies",
}

var writableDarwin = []string{
	"Library/Caches", "Library/Application Support/svm", "Library/pnpm/store", "Library/Preferences/hardhat-nodejs", "Library/Application Support/hardhat-nodejs",
	"Library/Application Support/hardhat-nodejs-nodejs", "Library/Preferences/pnpm",
}

func protectedPaths(home string, cfg *Config) []string {
	rel := append([]string{}, protectedCommon...)
	if runtime.GOOS == "darwin" {
		rel = append(rel, protectedDarwin...)
	} else {
		rel = append(rel, protectedLinux...)
	}
	var out []string
	for _, r := range rel {
		out = append(out, filepath.Join(home, r))
	}
	// keymoat's own: the vault, keys handed out for a run, the settings and the log.
	for _, name := range []string{"vault", "run", "config.json", "events.jsonl", canaryName} {
		out = append(out, filepath.Join(stateDir(), name))
	}
	// Any other Solana keypair kept next to the default one.
	if entries, err := os.ReadDir(filepath.Join(home, ".config", "solana")); err == nil {
		for _, e := range entries {
			p := filepath.Join(home, ".config", "solana", e.Name())
			if strings.HasSuffix(e.Name(), ".json") && isSolanaKeypairFile(p) {
				out = append(out, p)
			}
		}
	}
	for _, p := range cfg.Protect {
		out = append(out, expandHome(p))
	}
	allowed := map[string]bool{}
	for _, p := range cfg.AllowRead {
		allowed[realPath(expandHome(p))] = true
	}
	var final []string
	for _, p := range out {
		if p = realPath(p); !allowed[p] {
			final = append(final, p)
		}
	}
	return dedupe(final)
}

func writablePaths(home string, cfg *Config) []string {
	rel := append([]string{}, writableCommon...)
	if runtime.GOOS == "darwin" {
		rel = append(rel, writableDarwin...)
	}
	// Both the path as written and where it really leads: a link is created at the first and written through to
	// the second.
	var out []string
	add := func(p string) {
		out = append(out, filepath.Join(realPath(filepath.Dir(p)), filepath.Base(p)), realPath(p))
	}
	for _, r := range rel {
		add(filepath.Join(home, r))
	}
	for _, pattern := range writableGlobs {
		matches, _ := filepath.Glob(filepath.Join(home, pattern))
		for _, m := range matches {
			add(m)
		}
	}
	for _, p := range cfg.AllowWrite {
		add(expandHome(p))
	}
	return dedupe(out)
}

// realPath resolves symlinks as far as the path exists, so a rule names what the system will actually compare.
func realPath(p string) string {
	p = filepath.Clean(p)
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	dir, base := filepath.Split(p)
	dir = filepath.Clean(dir)
	if dir == p || dir == "." {
		return p
	}
	return filepath.Join(realPath(dir), base)
}

func dedupe(list []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range list {
		if x != "" && !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

// within reports whether path is dir or inside it.
func within(path, dir string) bool {
	if dir == "" {
		return false
	}
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

var projectMarkers = []string{"foundry.toml", "Anchor.toml", "hardhat.config.ts", "hardhat.config.js", "hardhat.config.cjs", "hardhat.config.mjs", "package.json", "Cargo.toml"}

// projectRoot finds the project a folder belongs to: the repository it is in, or else the nearest folder with a
// project file. The home directory and the filesystem root are never a project: a command must not get to write
// all of either.
func projectRoot(cwd, home string) string {
	cwd, home = realPath(cwd), realPath(home)
	ok := func(dir string) bool { return dir != home && dir != "/" && !within(home, dir) }
	nearest := ""
	for dir := cwd; ; dir = filepath.Dir(dir) {
		if !ok(dir) {
			break
		}
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		if nearest == "" {
			for _, m := range projectMarkers {
				if _, err := os.Stat(filepath.Join(dir, m)); err == nil {
					nearest = dir
					break
				}
			}
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	if nearest != "" {
		return nearest
	}
	if ok(cwd) {
		return cwd
	}
	return ""
}

func contains(list []string, x string) bool {
	for _, y := range list {
		if x == y {
			return true
		}
	}
	return false
}
