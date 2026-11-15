package main

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// What a wallet secret looks like. keymoat goes by the shape of the value, not the name of the variable: a 64-digit
// hex string named DEPLOYER is a key, and an API key named SECRET is not a wallet key and is left alone.

var (
	hex64     = regexp.MustCompile(`^(0[xX])?[0-9a-fA-F]{64}$`)
	base58Key = regexp.MustCompile(`^[1-9A-HJ-NP-Za-km-z]{86,88}$`)
	byteArray = regexp.MustCompile(`^\[\s*\d{1,3}(\s*,\s*\d{1,3}){63}\s*\]$`)
	base64Key = regexp.MustCompile(`^([A-Za-z0-9+/]{43}=|[A-Za-z0-9+/]{86}==)$`)
	wordList  = regexp.MustCompile(`^[a-z]{3,8}( [a-z]{3,8})+$`)

	// Names of values that have the shape of a key and are not one.
	benignName = regexp.MustCompile(`(?i)(HASH|ROOT|SALT|TOPIC|SELECTOR|DIGEST|DOMAIN|SEPARATOR|BYTECODE|SLOT|NONCE|TXN?(_|$)|SIG(NATURE)?(_|$)|_ID$|^ID$|UID)`)
	walletName = regexp.MustCompile(`(?i)(PRIVATE|MNEMONIC|SEED|KEYPAIR|WALLET|SIGNER|DEPLOYER)`)
	keyName    = regexp.MustCompile(`(?i)(PRIVATE|PRIV_?KEY|(^|_)PK($|_)|SECRET|MNEMONIC|SEED|PHRASE|DEPLOYER|SIGNER|WALLET|KEYPAIR|ACCOUNT_KEY)`)
)

// The keys every local node hands out. Everyone knows them, so they are not secrets.
var publicTestKeys = map[string]bool{
	"ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80": true,
	"59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d": true,
	"5de4111afa1a4b94908f83103eb1f1706367c2e68ca870fc3fb9a804cdab365a": true,
	"7c852118294e51e653712a81e05800f419141751be58f605c371e15141b007a6": true,
	"47e179ec197488593b187f80a00eb0da91f1b9d0b13f8733639f19c30a34926a": true,
	"test test test test test test test test test test test junk":      true,
}

// walletSecretKind names the kind of wallet secret a value is, or "" when it is not one.
func walletSecretKind(value string) string {
	v := strings.TrimSpace(value)
	if publicTestKeys[strings.TrimPrefix(strings.ToLower(v), "0x")] {
		return ""
	}
	switch {
	case hex64.MatchString(v):
		return "private key"
	case byteArray.MatchString(v):
		return "Solana keypair"
	case base58Key.MatchString(v):
		return "Solana secret key"
	}
	// A seed phrase, however it is spaced or capitalised.
	if phrase := strings.Join(strings.Fields(strings.ToLower(v)), " "); wordList.MatchString(phrase) && !publicTestKeys[phrase] {
		if n := len(strings.Fields(phrase)); n == 12 || n == 15 || n == 18 || n == 21 || n == 24 {
			return "seed phrase"
		}
	}
	return ""
}

// secretKind is walletSecretKind with the variable's name taken into account.
func secretKind(name, value string) string {
	kind := walletSecretKind(value)
	if kind == "" {
		// A 32- or 64-byte key written in base64 looks like many other secrets, so the name has to say it is a key.
		if base64Key.MatchString(strings.TrimSpace(value)) && walletName.MatchString(name) {
			return "key in base64"
		}
		return ""
	}
	if keyName.MatchString(name) {
		return kind
	}
	if benignName.MatchString(name) {
		return ""
	}
	if kind == "seed phrase" {
		return "possible seed phrase"
	}
	return "possible " + kind
}

// EnvVar is one assignment in an env file.
type EnvVar struct {
	Name, Value string
	Line        int
}

func parseEnv(data string) []EnvVar {
	var out []EnvVar
	sc := bufio.NewScanner(strings.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		name, value, ok := strings.Cut(line, "=")
		name = strings.TrimSpace(name)
		if !ok || name == "" || strings.ContainsAny(name, " \t") {
			continue
		}
		out = append(out, EnvVar{Name: name, Value: unquote(value), Line: n})
	}
	return out
}

func unquote(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1]
	}
	// An unquoted value ends at a comment.
	if i := strings.Index(v, " #"); i >= 0 {
		v = strings.TrimSpace(v[:i])
	}
	return v
}

// EnvFile is an env file of the project and what keymoat found in it.
type EnvFile struct {
	Path    string
	Vars    []EnvVar
	Secrets []EnvVar // the ones that are wallet secrets
}

func isEnvFileName(name string) bool {
	if name != ".env" && !strings.HasPrefix(name, ".env.") {
		return false
	}
	for _, s := range []string{".example", ".sample", ".template", ".dist", ".defaults"} {
		if strings.HasSuffix(name, s) {
			return false
		}
	}
	return true
}

var skipDirs = map[string]bool{"node_modules": true, ".git": true, "lib": true, "target": true, "out": true, "cache": true, "dist": true, "artifacts": true, "build": true, ".next": true, "vendor": true, "dependencies": true, "coverage": true, "broadcast": true, ".anchor": true, "test-ledger": true}

// walkProject calls fn for every file under root down to a depth, skipping dependency and build folders.
func walkProject(root string, depth int, fn func(path string, name string)) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		p := filepath.Join(root, e.Name())
		if e.IsDir() {
			if depth > 0 && !skipDirs[e.Name()] {
				walkProject(p, depth-1, fn)
			}
			continue
		}
		if e.Type().IsRegular() {
			fn(p, e.Name())
		}
	}
}

// projectEnvFiles reads the project's env files, the ones at the top first.
func projectEnvFiles(root string) []EnvFile {
	var files []EnvFile
	walkProject(root, 3, func(path, name string) {
		if !isEnvFileName(name) {
			return
		}
		info, err := os.Stat(path)
		if err != nil || info.Size() > 1<<20 {
			return
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return
		}
		f := EnvFile{Path: path, Vars: parseEnv(string(data))}
		for _, v := range f.Vars {
			if secretKind(v.Name, v.Value) != "" {
				f.Secrets = append(f.Secrets, v)
			}
		}
		files = append(files, f)
	})
	sort.SliceStable(files, func(i, j int) bool {
		return strings.Count(files[i].Path, string(filepath.Separator)) < strings.Count(files[j].Path, string(filepath.Separator))
	})
	return files
}

// childEnv is the environment a command that must not see keys runs with: the caller's, without anything that is a
// wallet secret, plus the harmless variables of env files keymoat hides from it. It returns the names it left out.
func childEnv(environ []string, hidden []EnvFile, dirs ...string) (env []string, dropped []string) {
	set := map[string]bool{}
	for _, kv := range environ {
		name, value, _ := strings.Cut(kv, "=")
		if secretKind(name, value) != "" {
			dropped = append(dropped, name)
			continue
		}
		if name == "SSH_AUTH_SOCK" {
			continue
		}
		set[name] = true
		env = append(env, kv)
	}
	// A hidden file can no longer be read by the tool, so what is safe in it is handed over directly. Only files a
	// tool would load from where it runs, the plain ".env" last so that a more specific file wins.
	inDirs := map[string]bool{}
	for _, d := range dirs {
		inDirs[d] = true
	}
	var load []EnvFile
	for _, f := range hidden {
		if inDirs[filepath.Dir(f.Path)] && filepath.Base(f.Path) != ".env" {
			load = append(load, f)
		}
	}
	for _, f := range hidden {
		if inDirs[filepath.Dir(f.Path)] && filepath.Base(f.Path) == ".env" {
			load = append(load, f)
		}
	}
	for _, f := range load {
		for _, v := range f.Vars {
			if secretKind(v.Name, v.Value) != "" {
				dropped = append(dropped, v.Name)
				continue
			}
			if !set[v.Name] {
				set[v.Name] = true
				env = append(env, v.Name+"="+v.Value)
			}
		}
	}
	sort.Strings(dropped)
	return env, uniq(dropped)
}

func uniq(sorted []string) []string {
	var out []string
	for i, s := range sorted {
		if i == 0 || s != sorted[i-1] {
			out = append(out, s)
		}
	}
	return out
}

// Folders where a keypair file is a test fixture, not a wallet.
var fixtureDirs = map[string]bool{"test": true, "tests": true, "fixtures": true, "__tests__": true, "testdata": true, "e2e": true, "examples": true}

// Names of files that hold a secret and nothing else.
var secretFileNames = map[string]bool{".secret": true, ".secrets": true, ".mnemonic": true, "mnemonic.txt": true, "secret.txt": true, "seed.txt": true, ".private-key": true, "private-key.txt": true}

// isSecretFile reports whether a small file's whole content is a wallet secret.
func isSecretFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 2048 || info.Size() < 32 {
		return false
	}
	data, err := os.ReadFile(path)
	return err == nil && walletSecretKind(unquote(string(data))) != ""
}

// projectSecretFiles finds wallet files kept inside a project: Solana keypair files and files such as `.secret`
// that hold a key or a seed phrase. Keypairs in test folders are fixtures, and a program's own keypair (in a
// `deploy` folder) names the program; neither is hidden.
func projectSecretFiles(root string) []string {
	var out []string
	walkProject(root, 3, func(path, name string) {
		rel, _ := filepath.Rel(root, path)
		for _, part := range strings.Split(filepath.Dir(rel), string(filepath.Separator)) {
			if fixtureDirs[part] {
				return
			}
		}
		switch {
		case strings.HasSuffix(name, ".json"):
			if isSolanaKeypairFile(path) && !(strings.HasSuffix(name, "-keypair.json") && filepath.Base(filepath.Dir(path)) == "deploy") {
				out = append(out, path)
			}
		case secretFileNames[name] || strings.HasSuffix(name, ".key"):
			if isSecretFile(path) {
				out = append(out, path)
			}
		}
	})
	return out
}

// isSolanaKeypairFile reports whether a file holds a Solana keypair: a JSON array of 64 bytes.
func isSolanaKeypairFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1024 || info.Size() < 128 {
		return false
	}
	data, err := os.ReadFile(path)
	return err == nil && byteArray.MatchString(strings.TrimSpace(string(data)))
}
