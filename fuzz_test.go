package main

import (
	"strings"
	"testing"
)

// The parsers read files and command lines a hostile project controls. They must never panic, and a few
// properties must always hold.

func FuzzParseEnv(f *testing.F) {
	f.Add("A=1\nexport B=\"two\"\n# c\nPRIVATE_KEY=" + fakeKey)
	f.Add("=\n==\n\x00=\xff\n'\"'=\"")
	f.Fuzz(func(t *testing.T, data string) {
		for _, v := range parseEnv(data) {
			if v.Name == "" || strings.ContainsAny(v.Name, " \t\n") || v.Line < 1 {
				t.Fatalf("bad variable %+v", v)
			}
			secretKind(v.Name, v.Value)
		}
		childEnv(strings.Split(data, "\n"), []EnvFile{{Path: "/p/.env", Vars: parseEnv(data)}}, "/p")
	})
}

func FuzzClassify(f *testing.F) {
	for _, s := range []string{"forge script s.sol --broadcast --rpc-url mainnet --account a", "cast send 0x1 f() --value 1", "solana -u m transfer x 1 -k keymoat:main", "anchor --provider.cluster devnet test", "npm run deploy", "cargo install x", "spl-token transfer a 1 b", "forge --rpc-url", "cast wallet", "npx -- hardhat --network"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, line string) {
		parts := strings.Fields(line)
		if len(parts) == 0 {
			return
		}
		c := &Context{Cwd: "/nowhere/proj", Home: "/nowhere", Root: "/nowhere/proj"}
		tool := parts[0]
		if _, ok := tools[tool]; !ok {
			tool = "forge"
		}
		p := classify(tool, parts[1:], c)
		if p.Gate != nil && p.Mode != ModeSigning {
			t.Fatalf("asked about, but not treated as signing: %q", line)
		}
		for _, ref := range p.VaultRefs {
			withKeyPath(p.Args, ref, "/tmp/k.json")
		}
		redactArgs(parts)
	})
}

func FuzzConfigText(f *testing.F) {
	f.Add("[rpc_endpoints]\nmainnet = \"${RPC}\"\nx = { url = \"https://a.b/c\" }\n[profile.default]\neth_rpc_url = \"http://localhost:8545\"")
	f.Add("[[[\n=\n\"\"=\"\"\n[a]]\nk = {")
	f.Fuzz(func(t *testing.T, data string) {
		tomlTable(data, "rpc_endpoints")
		tomlTable(data, "")
		expandEnvRefs(data, map[string]string{"RPC": "https://x.y"})
		for _, u := range urlInText.FindAllString(data, -1) {
			if h := hostOf(u); strings.ContainsAny(h, " /\n") {
				t.Fatalf("bad host %q from %q", h, u)
			}
		}
	})
}

func FuzzHosts(f *testing.F) {
	f.Add("github.com", "*.github.com")
	f.Add("evil.com.", "com")
	f.Add("[::1]", "::1")
	f.Fuzz(func(t *testing.T, host, pattern string) {
		isLocalHost(host)
		if hostMatches(host, []string{pattern}) {
			h, p := strings.ToLower(strings.TrimSuffix(host, ".")), strings.ToLower(pattern)
			// A match is the name itself, or a name under a "*." pattern: never a mere suffix of letters.
			if h != p && !(strings.HasPrefix(p, "*.") && (h == p[2:] || strings.HasSuffix(h, p[1:]))) {
				t.Fatalf("%q matched %q", host, pattern)
			}
		}
	})
}

func FuzzKeyFiles(f *testing.F) {
	f.Add([]byte("[1,2,3]"), "pw")
	f.Add([]byte(`{"crypto":{"cipher":"aes-128-ctr","kdf":"scrypt","kdfparams":{"n":1073741824,"r":8,"p":1,"dklen":32}},"version":3}`), "")
	f.Add([]byte(`{"version":1,"kind":"solana-keypair","kdf":"scrypt","n":1073741824,"r":64,"p":64}`), "x")
	f.Fuzz(func(t *testing.T, data []byte, password string) {
		parseSolanaKeypair(data)
		parseEVMKey(string(data))
		// A hostile keystore or vault file must be rejected, not crash or exhaust memory.
		if len(data) < 4096 {
			decryptEVMKey(data, password)
			decryptSolanaKeypair(data, password)
		}
	})
}
