package main

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

// A key that belongs to no one: random digits.
const fakeKey = "0x7d1e4b1f2c3a495867a0b1c2d3e4f5061728394a5b6c7d8e9fa0b1c2d3e4f506"

func TestSecretKind(t *testing.T) {
	phrase := "vivid lumber orange dance supply pencil rabbit coast melody window fabric tunnel"
	cases := []struct{ name, value, want string }{
		{"PRIVATE_KEY", fakeKey, "private key"},
		{"DEPLOYER", fakeKey[2:], "private key"},
		{"PRIVATE_KEY", "0X" + strings.ToUpper(fakeKey[2:]), "private key"},
		{"PRIVATE_KEY", "0XAC0974BEC39A17E36BA4A6B4D238FF944BACB478CBED5EFCAE784D7BF4F2FF80", ""},
		{"SOMETHING", fakeKey, "possible private key"},
		{"MERKLE_ROOT", fakeKey, ""},
		{"SALT", fakeKey, ""},
		{"DEPLOY_TX_HASH", fakeKey, ""},
		{"PRIVATE_KEY", "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80", ""}, // the local node's first account
		{"MNEMONIC", phrase, "seed phrase"},
		{"MNEMONIC", "test test test test test test test test test test test junk", ""},
		{"NOTE", phrase, "possible seed phrase"},
		{"NOTE", "this is just a short sentence", ""},
		{"MNEMONIC", "  Vivid lumber  orange dance supply pencil rabbit coast melody window fabric TUNNEL ", "seed phrase"},
		{"MNEMONIC", "Test test test test test test test test test test test JUNK", ""},
		{"DEPLOYER_PRIVATE_KEY", "q83vEjRWeJCrze8SNFZ4kKvN7xI0VniQq83vEjRWeJA=", "key in base64"},
		{"NEXTAUTH_SECRET", "q83vEjRWeJCrze8SNFZ4kKvN7xI0VniQq83vEjRWeJA=", ""},
		{"ETHERSCAN_API_KEY", "ABCDEFGHIJKLMNOPQRSTUVWXYZ12345678", ""},
		{"TELEGRAM_WEBHOOK_SECRET", "s3cr3t-value", ""},
		{"RPC_URL", "https://rpc.example.org/v2/abcdef", ""},
		{"WALLET", "[" + strings.TrimSuffix(strings.Repeat("7,", 64), ",") + "]", "Solana keypair"},
		{"SOLANA_SECRET", strings.Repeat("5K", 44), "Solana secret key"},
		{"LAST_TX_SIG", strings.Repeat("5K", 44), ""},
	}
	for _, c := range cases {
		if got := secretKind(c.name, c.value); got != c.want {
			t.Errorf("secretKind(%s) = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestParseEnv(t *testing.T) {
	vars := parseEnv("# comment\nA=1\nexport B=\"two words\"\nC='x' \nD=plain # trailing\n\nnot a line\nE=\n")
	want := []EnvVar{{"A", "1", 2}, {"B", "two words", 3}, {"C", "x", 4}, {"D", "plain", 5}, {"E", "", 8}}
	if !reflect.DeepEqual(vars, want) {
		t.Fatalf("got %+v", vars)
	}
}

func TestChildEnvHidesKeysAndKeepsTheRest(t *testing.T) {
	root := t.TempDir()
	file := EnvFile{Path: filepath.Join(root, ".env"), Vars: parseEnv("PRIVATE_KEY=" + fakeKey + "\nGREETING=hello\nPATH=/evil\n")}
	nested := EnvFile{Path: filepath.Join(root, "sub", "deep", ".env"), Vars: parseEnv("NESTED=1\n")}
	env, dropped := childEnv([]string{"PATH=/bin", "DEPLOYER_KEY=" + fakeKey, "SSH_AUTH_SOCK=/tmp/agent", "HOME=/h"}, []EnvFile{file, nested}, root)
	joined := "\n" + strings.Join(env, "\n") + "\n"
	for _, must := range []string{"\nPATH=/bin\n", "\nGREETING=hello\n", "\nHOME=/h\n"} {
		if !strings.Contains(joined, must) {
			t.Errorf("missing %q in %q", strings.TrimSpace(must), env)
		}
	}
	for _, mustNot := range []string{fakeKey, "SSH_AUTH_SOCK", "PATH=/evil", "NESTED"} {
		if strings.Contains(joined, mustNot) {
			t.Errorf("%q must not be handed over: %q", mustNot, env)
		}
	}
	if !reflect.DeepEqual(dropped, []string{"DEPLOYER_KEY", "PRIVATE_KEY"}) {
		t.Errorf("dropped = %v", dropped)
	}
}

func TestProjectEnvFiles(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, ".env"), "PRIVATE_KEY="+fakeKey+"\nA=1\n")
	write(t, filepath.Join(root, ".env.example"), "PRIVATE_KEY=\n")
	write(t, filepath.Join(root, "packages", "app", ".env.local"), "B=2\n")
	write(t, filepath.Join(root, "node_modules", "x", ".env"), "PRIVATE_KEY="+fakeKey+"\n")
	files := projectEnvFiles(root)
	if len(files) != 2 || filepath.Base(files[0].Path) != ".env" || len(files[0].Secrets) != 1 || len(files[1].Secrets) != 0 {
		t.Fatalf("got %+v", files)
	}
}

func TestHostMatches(t *testing.T) {
	list := []string{"github.com", "*.etherscan.io"}
	for host, want := range map[string]bool{"github.com": true, "GitHub.com.": true, "evil-github.com": false, "api.etherscan.io": true, "etherscan.io": true, "etherscan.io.evil.com": false, "notetherscan.io": false} {
		if got := hostMatches(host, list); got != want {
			t.Errorf("hostMatches(%s) = %v", host, got)
		}
	}
}

func TestProjectHostsAndTrust(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "foundry.toml"), "[profile.default]\nsrc = \"src\"\n\n[rpc_endpoints]\nmainnet = \"${MAINNET_RPC_URL}\"\nbase = \"https://mainnet.base.org\" # public\nlocal = \"http://localhost:8545\"\nkeyed = { endpoint = \"https://keyed.example.net/v1\" }\n")
	write(t, filepath.Join(root, ".env"), "MAINNET_RPC_URL=https://eth.example-rpc.io/v2/key\nPRIVATE_KEY="+fakeKey+"\n")
	write(t, filepath.Join(root, "hardhat.config.ts"), "export default { networks: { op: { url: 'https://op.example.org' } } }\n")
	files := projectEnvFiles(root)
	found := projectHosts(root, files, nil)
	for _, h := range []string{"eth.example-rpc.io", "mainnet.base.org", "keyed.example.net", "op.example.org"} {
		if found[h] == "" {
			t.Errorf("did not find %s in %v", h, found)
		}
	}
	if _, ok := found["localhost"]; ok {
		t.Errorf("a local host needs no approval")
	}
	if got := foundryEndpoints(root, envValues(nil, files, root)); got["mainnet"] != "https://eth.example-rpc.io/v2/key" || got["keyed"] != "https://keyed.example.net/v1" {
		t.Errorf("endpoints = %v", got)
	}
	cfg := &Config{AllowHosts: []string{"op.example.org"}}
	trust := &ProjectTrust{Hosts: []string{"mainnet.base.org"}, Denied: []string{"keyed.example.net"}}
	if fresh := newHosts(found, cfg, trust); !reflect.DeepEqual(fresh, []string{"eth.example-rpc.io"}) {
		t.Errorf("fresh = %v", fresh)
	}
}

func TestNeighbourEnvFiles(t *testing.T) {
	code := t.TempDir()
	write(t, filepath.Join(code, "mine", ".env"), "A=1")
	write(t, filepath.Join(code, "theirs", ".env"), "B=2")
	write(t, filepath.Join(code, "theirs", "app", ".env.local"), "C=3")
	write(t, filepath.Join(code, "theirs", ".env.example"), "B=")
	write(t, filepath.Join(code, "theirs", "node_modules", "x", ".env"), "D=4")
	got := neighbourEnvFiles(filepath.Join(code, "mine"))
	want := []string{filepath.Join(code, "theirs", ".env"), filepath.Join(code, "theirs", "app", ".env.local")}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
}

func TestProjectRoot(t *testing.T) {
	home := realPath(t.TempDir())
	repo := filepath.Join(home, "code", "repo")
	write(t, filepath.Join(repo, ".git", "HEAD"), "")
	write(t, filepath.Join(repo, "packages", "contracts", "foundry.toml"), "")
	write(t, filepath.Join(home, "loose", "pkg", "package.json"), "{}")
	write(t, filepath.Join(home, "package.json"), "{}")
	cases := map[string]string{
		filepath.Join(repo, "packages", "contracts"): repo,
		filepath.Join(home, "loose", "pkg"):          filepath.Join(home, "loose", "pkg"),
		home:                                         "",
		filepath.Join(home, "loose"):                 filepath.Join(home, "loose"),
	}
	for cwd, want := range cases {
		os.MkdirAll(cwd, 0o755)
		if got := projectRoot(cwd, home); got != want {
			t.Errorf("projectRoot(%s) = %q, want %q", cwd, got, want)
		}
	}
}

func TestRedactArgs(t *testing.T) {
	got := redactArgs([]string{"script", "--private-key", fakeKey, "--rpc-url", "x", "--mnemonic=a b", fakeKey})
	want := []string{"script", "--private-key", "<hidden>", "--rpc-url", "x", "--mnemonic=<hidden>", "<hidden>"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
}

func TestExtensionsReview(t *testing.T) {
	now := time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)
	list := []extension{
		{ID: "juanblanco.solidity", Publisher: "JuanBlanco", Name: "solidity", Web3: true},
		{ID: "juanbianco.solidity", Publisher: "juanbianco", Name: "solidity", Web3: true},
		{ID: "someone.solidity-language", Publisher: "someone", Name: "solidity-language", Web3: true, Installed: now.Add(-48 * time.Hour), FromFile: true},
		{ID: "esbenp.prettier-vscode", Publisher: "esbenp", Name: "prettier-vscode"},
		{ID: "evilcorp.hardhat-solidity", Publisher: "evilcorp", Name: "hardhat-solidity", Web3: true},
	}
	report := reviewExtensions(list, now)
	ids := map[string][]string{}
	for _, r := range report {
		ids[r.ID] = r.Reasons
	}
	if len(report) != 3 || ids["juanblanco.solidity"] != nil || ids["esbenp.prettier-vscode"] != nil {
		t.Fatalf("report = %+v", report)
	}
	if !strings.Contains(strings.Join(ids["juanbianco.solidity"], "|"), "juanblanco.solidity") || !strings.Contains(strings.Join(ids["evilcorp.hardhat-solidity"], "|"), "nomicfoundation.hardhat-solidity") {
		t.Errorf("lookalikes not named: %+v", ids)
	}
	if len(ids["someone.solidity-language"]) != 3 {
		t.Errorf("reasons = %v", ids["someone.solidity-language"])
	}
}

func TestScanTree(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, ".env"), "PRIVATE_KEY="+fakeKey+"\nA=1\n")
	write(t, filepath.Join(root, "hardhat.config.ts"), "accounts: ['"+fakeKey+"'],\nother: ['0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80']\n")
	keypair := "[" + strings.TrimSuffix(strings.Repeat("7,", 64), ",") + "]"
	write(t, filepath.Join(root, "wallet.json"), keypair)
	write(t, filepath.Join(root, "target", "deploy", "prog-keypair.json"), keypair)
	findings := scanTree(root, 6)
	if len(findings) != 3 {
		t.Fatalf("findings = %+v", findings)
	}
	for _, f := range findings {
		if strings.Contains(f.Kind+f.Fix+f.Name, fakeKey[2:10]) {
			t.Errorf("a finding must not carry the key: %+v", f)
		}
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestProjectSecretFiles(t *testing.T) {
	root := t.TempDir()
	keypair := "[" + strings.TrimSuffix(strings.Repeat("7,", 64), ",") + "]"
	write(t, filepath.Join(root, "deployer.json"), keypair)
	write(t, filepath.Join(root, "keys", "admin.json"), keypair)
	write(t, filepath.Join(root, "tests", "fixtures", "payer.json"), keypair)
	write(t, filepath.Join(root, "target", "deploy", "prog-keypair.json"), keypair)
	write(t, filepath.Join(root, "package.json"), `{"name":"x"}`)
	write(t, filepath.Join(root, ".secret"), "vivid lumber orange dance supply pencil rabbit coast melody window fabric tunnel\n")
	write(t, filepath.Join(root, "deploy.key"), fakeKey+"\n")
	write(t, filepath.Join(root, "notes.key"), "not a key at all, just some words in a file\n")
	got := projectSecretFiles(root)
	sort.Strings(got)
	want := []string{filepath.Join(root, ".secret"), filepath.Join(root, "deploy.key"), filepath.Join(root, "deployer.json"), filepath.Join(root, "keys", "admin.json")}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
}
