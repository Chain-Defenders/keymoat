package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func testContext(t *testing.T) *Context {
	t.Helper()
	home := realPath(t.TempDir())
	t.Setenv("HOME", home)
	t.Setenv("KEYMOAT_HOME", filepath.Join(home, ".keymoat"))
	vaultIndex = nil
	root := filepath.Join(home, "proj")
	write(t, filepath.Join(root, "foundry.toml"), "[rpc_endpoints]\nmainnet = \"https://eth.example-rpc.io\"\nlocal = \"http://127.0.0.1:8545\"\n")
	return &Context{Cwd: root, Home: home, Root: root}
}

func plan(t *testing.T, c *Context, line string) Plan {
	t.Helper()
	parts := strings.Fields(line)
	return classify(parts[0], parts[1:], c)
}

func TestForge(t *testing.T) {
	c := testContext(t)
	for _, line := range []string{"forge build", "forge test -vvv --fork-url mainnet", "forge script script/Deploy.s.sol --rpc-url mainnet", "forge install foundry-rs/forge-std"} {
		if p := plan(t, c, line); !p.Sandbox || p.Mode != ModeNoKeys || p.Gate != nil {
			t.Errorf("%q should run without keys and without a question: %+v", line, p)
		}
	}
	p := plan(t, c, "forge script script/Deploy.s.sol --rpc-url mainnet --broadcast --account deployer")
	if p.Mode != ModeSigning || p.Gate == nil || p.Gate.Local || p.Gate.RPCURL != "https://eth.example-rpc.io" || !strings.Contains(p.Gate.Key, `"deployer"`) {
		t.Fatalf("broadcast: %+v %+v", p, p.Gate)
	}
	if len(p.KeyFiles) != 1 || p.KeyFiles[0] != filepath.Join(c.Home, ".foundry", "keystores", "deployer") {
		t.Errorf("the named keystore, and only it, becomes readable: %v", p.KeyFiles)
	}
	if len(p.CLIHosts) != 0 {
		t.Errorf("a name from foundry.toml is not a host the user typed: %v", p.CLIHosts)
	}
	if p := plan(t, c, "forge script s.sol --broadcast --rpc-url https://typed.example.com/x"); len(p.CLIHosts) != 1 || p.CLIHosts[0] != "typed.example.com" {
		t.Errorf("a URL on the command line is allowed for the run: %v", p.CLIHosts)
	}
	if p := plan(t, c, "forge script s.sol --broadcast --rpc-url local"); p.Gate == nil || !p.Gate.Local {
		t.Errorf("a local node needs no question: %+v", p.Gate)
	}
	for _, line := range []string{"forge script s.sol --resume --rpc-url mainnet", "forge create src/C.sol:C --rpc-url mainnet", "forge c src/C.sol:C --rpc-url mainnet --broadcast"} {
		if p := plan(t, c, line); p.Gate == nil || p.Gate.Local || p.Mode != ModeSigning {
			t.Errorf("%q sends and must be asked about: %+v", line, p.Gate)
		}
	}
	if p := plan(t, c, "forge create src/C.sol:C --broadcast --private-key "+fakeKey); p.Gate == nil || len(p.Gate.Warn) == 0 || !p.Gate.Local {
		t.Errorf("no endpoint means the local node; a key on the command line is warned about: %+v", p.Gate)
	}
	c.EnvFiles = []EnvFile{{Path: filepath.Join(c.Root, ".env"), Secrets: []EnvVar{{Name: "PRIVATE_KEY", Value: fakeKey, Line: 1}}}}
	if p := plan(t, c, "forge script s.sol --broadcast --rpc-url mainnet"); !strings.Contains(p.Gate.Key, "PRIVATE_KEY in .env") || strings.Contains(p.Gate.Key, fakeKey[2:12]) {
		t.Errorf("a key in an env file is named, never shown: %q", p.Gate.Key)
	}
}

func TestCast(t *testing.T) {
	c := testContext(t)
	if p := plan(t, c, "cast call 0xabc f()(uint256) --rpc-url mainnet"); p.Sandbox || p.Gate != nil {
		t.Errorf("a read needs nothing: %+v", p)
	}
	for _, line := range []string{"cast s 0xabc f() --rpc-url mainnet", "cast publish 0xdead --rpc-url mainnet", "cast p 0xdead --rpc-url mainnet", "cast batch-send --rpc-url mainnet", "cast mktx 0xabc f() --rpc-url mainnet", "cast m 0xabc f() --rpc-url mainnet", "cast erc20 transfer 0xtoken 0xto 5 --rpc-url mainnet", "cast erc20-token approve 0xtoken 0xsp 5 --rpc-url mainnet", "cast erc20 t 0xtoken 0xto 5 --rpc-url mainnet", "cast wallet sign hello --account a", "cast w sa 0xabc --account a", "cast wallet sign-auth 0xabc --rpc-url local"} {
		if p := plan(t, c, line); p.Gate == nil || p.Gate.Local {
			t.Errorf("%q signs or sends and must be asked about: %+v", line, p.Gate)
		}
	}
	for _, line := range []string{"cast erc20 balance 0xtoken 0xme --rpc-url mainnet", "cast wallet address --account a", "cast wallet list", "cast run 0xhash --rpc-url mainnet", "cast storage 0xabc 0 --rpc-url mainnet"} {
		if p := plan(t, c, line); p.Gate != nil {
			t.Errorf("%q only reads: %+v", line, p.Gate)
		}
	}
	p := plan(t, c, "cast send 0xabc f(uint256) 5 --value 1ether --rpc-url mainnet --ledger")
	if p.Sandbox || p.Gate == nil || p.Gate.Key != "hardware wallet" || !strings.Contains(p.Gate.Target, "0xabc f(uint256) 5") || !strings.Contains(p.Gate.Target, "1ether") {
		t.Errorf("send: %+v", p.Gate)
	}
}

func TestSolana(t *testing.T) {
	c := testContext(t)
	write(t, filepath.Join(c.Home, ".config", "solana", "cli", "config.yml"), "json_rpc_url: https://api.devnet.solana.com\nkeypair_path: "+filepath.Join(c.Home, "wallet.json")+"\n")
	for _, line := range []string{"solana balance", "solana config get", "solana program show Prog111", "solana -u m account Abc", "spl-token accounts", "spl-token balance Mint1"} {
		if p := plan(t, c, line); p.Gate != nil || p.Sandbox {
			t.Errorf("%q only reads: %+v", line, p.Gate)
		}
	}
	p := plan(t, c, "solana program deploy target/deploy/prog.so -u mainnet-beta")
	if p.Gate == nil || p.Gate.Network != "mainnet-beta" || p.Gate.Local || !strings.Contains(p.Gate.Key, "wallet.json") || p.Gate.Action != "solana program deploy" {
		t.Errorf("deploy: %+v", p.Gate)
	}
	if p := plan(t, c, "solana transfer Dest1 1"); p.Gate == nil || p.Gate.Network != "devnet" {
		t.Errorf("the cluster comes from the tool's settings: %+v", p.Gate)
	}
	if p := plan(t, c, "solana transfer Dest1 1 --url localhost"); p.Gate == nil || !p.Gate.Local {
		t.Errorf("local: %+v", p.Gate)
	}
	if p := plan(t, c, "spl-token transfer Mint1 5 Dest1 -u m"); p.Gate == nil || p.Gate.Network != "mainnet-beta" {
		t.Errorf("spl-token: %+v", p.Gate)
	}
	p = plan(t, c, "solana transfer Dest1 1 -k keymoat:main")
	if len(p.VaultRefs) != 1 || p.VaultRefs[0].name != "main" || !p.VaultRefs[0].explicit || !strings.Contains(p.Gate.Key, "vault") {
		t.Errorf("vault: %+v %+v", p.VaultRefs, p.Gate)
	}
	if got := withKeyPath(p.Args, p.VaultRefs[0], "/tmp/k.json"); strings.Join(got, " ") != "transfer Dest1 1 -k /tmp/k.json" {
		t.Errorf("args = %v", got)
	}
}

func TestAnchor(t *testing.T) {
	c := testContext(t)
	write(t, filepath.Join(c.Root, "Anchor.toml"), "[provider]\ncluster = \"localnet\"\nwallet = \"~/.config/solana/id.json\"\n")
	if p := plan(t, c, "anchor build"); !p.Sandbox || p.Mode != ModeNoKeys || len(p.KeyFiles) != 0 || len(p.Args) != 1 {
		t.Errorf("build: %+v", p)
	}
	p := plan(t, c, "anchor test --skip-build")
	if p.Mode != ModeNoKeys || len(p.KeyFiles) != 0 || strings.Join(p.Args, " ") != "--provider.wallet "+devWalletPath()+" test --skip-build" {
		t.Errorf("local tests get the throwaway wallet and never the real one: %+v", p)
	}
	p = plan(t, c, "anchor test --provider.cluster devnet")
	if p.Mode != ModeSigning || p.Gate == nil || p.Gate.Network != "devnet" || !p.NetOpen || len(p.KeyFiles) != 1 || p.KeyFiles[0] != filepath.Join(c.Home, ".config", "solana", "id.json") {
		t.Errorf("tests on a live cluster: %+v %+v", p, p.Gate)
	}
	p = plan(t, c, "anchor deploy --provider.cluster mainnet --provider.wallet keymoat:main")
	if p.Gate == nil || p.Gate.Network != "mainnet-beta" || len(p.VaultRefs) != 1 || len(p.KeyFiles) != 0 {
		t.Errorf("deploy: %+v %+v", p, p.Gate)
	}
	if p := plan(t, c, "anchor deploy"); p.Gate == nil || !p.Gate.Local || p.NetOpen {
		t.Errorf("a local deploy needs no question and no open network: %+v", p)
	}
	if p := plan(t, c, "anchor idl build"); p.Mode != ModeNoKeys {
		t.Errorf("idl build: %+v", p)
	}
	if p := plan(t, c, "anchor idl init -f target/idl/p.json Prog1 --provider.cluster devnet"); p.Gate == nil {
		t.Errorf("idl init signs: %+v", p)
	}
}

func TestNodeTools(t *testing.T) {
	c := testContext(t)
	write(t, filepath.Join(c.Root, "package.json"), `{"scripts":{"test":"hardhat test","deploy":"hardhat run scripts/deploy.ts --network mainnet","deploy:local":"hardhat run scripts/deploy.ts --network localhost","ship":"forge script s.sol --broadcast"}}`)
	for _, line := range []string{"npm install", "npm ci", "pnpm add viem", "npm test", "npm run deploy:local", "npx hardhat compile", "yarn", "bun install", "npx hardhat test --network hardhat"} {
		if p := plan(t, c, line); !p.Sandbox || p.Mode != ModeNoKeys || p.Gate != nil {
			t.Errorf("%q should run without keys: %+v", line, p)
		}
	}
	for line, network := range map[string]string{"npm run deploy": "mainnet", "yarn deploy": "mainnet", "pnpm deploy": "mainnet", "npx hardhat ignition deploy ignition/modules/M.ts --network sepolia": "sepolia", "npm run ship": "set by the script"} {
		p := plan(t, c, line)
		if p.Mode != ModeSigning || p.Gate == nil || p.Gate.Network != network {
			t.Errorf("%q should ask first (network %s): %+v", line, network, p.Gate)
		}
	}
	if p := plan(t, c, "npm publish"); p.Mode != ModeAuth || len(p.KeyFiles) == 0 {
		t.Errorf("publish needs the registry token: %+v", p)
	}
}

func TestCargo(t *testing.T) {
	c := testContext(t)
	if p := plan(t, c, "cargo build-sbf"); !p.Sandbox || p.Mode != ModeNoKeys || len(p.ExtraWrite) != 0 {
		t.Errorf("build: %+v", p)
	}
	if p := plan(t, c, "cargo install anchor-cli"); len(p.ExtraWrite) == 0 {
		t.Errorf("install writes to ~/.cargo/bin: %+v", p)
	}
	if p := plan(t, c, "cargo publish"); p.Mode != ModeAuth {
		t.Errorf("publish: %+v", p)
	}
}

func TestGateDescribe(t *testing.T) {
	lookup := func(id uint64, ok bool) func(string) (uint64, bool) {
		return func(string) (uint64, bool) { return id, ok }
	}
	g := &Gate{RPCURL: "https://x", Network: "x"}
	if n, test := g.describe(lookup(8453, true)); n != "Base (chain 8453) through x" || test {
		t.Errorf("%q %v", n, test)
	}
	if n, test := g.describe(lookup(11155111, true)); !strings.HasPrefix(n, "Sepolia") || !test {
		t.Errorf("%q %v", n, test)
	}
	if n, test := g.describe(lookup(31337, true)); test || strings.Contains(n, "a local node (") {
		t.Errorf("a local node's id through a host that is not local is not a test network: %q %v", n, test)
	}
	if n, test := g.describe(lookup(0, false)); !strings.Contains(n, "did not say") || test {
		t.Errorf("an endpoint that does not answer is treated as a live network: %q %v", n, test)
	}
	if n, test := (&Gate{Solana: true, Network: "mainnet-beta"}).describe(lookup(0, false)); n != "mainnet-beta" || test {
		t.Errorf("%q %v", n, test)
	}
}

// A keystore's name must never work as a path, and a name from a project file must never open anything.
func TestKeyFilesCannotBeSteered(t *testing.T) {
	c := testContext(t)
	keystores := filepath.Join(c.Home, ".foundry", "keystores")
	for _, line := range []string{
		"forge test --account ../../.ssh",
		"forge script s.sol --broadcast --rpc-url mainnet --account ../../.ssh",
		"forge script s.sol --broadcast --rpc-url mainnet --account /etc/passwd",
		"forge script s.sol --broadcast --rpc-url mainnet --account a/b",
	} {
		if p := plan(t, c, line); len(p.KeyFiles) != 0 {
			t.Errorf("%q opened %v", line, p.KeyFiles)
		}
	}
	// From the project's env file: ignored. From the caller's own environment: a plain name, and only when signing.
	c.EnvFiles = []EnvFile{{Path: filepath.Join(c.Root, ".env"), Vars: []EnvVar{{Name: "ETH_KEYSTORE_ACCOUNT", Value: "../../.ssh", Line: 1}}}}
	c.values = nil
	for _, line := range []string{"forge test", "forge script s.sol --broadcast --rpc-url mainnet"} {
		if p := plan(t, c, line); len(p.KeyFiles) != 0 {
			t.Errorf("%q with a poisoned env file opened %v", line, p.KeyFiles)
		}
	}
	c.Env = []string{"ETH_KEYSTORE_ACCOUNT=deployer"}
	if p := plan(t, c, "forge test"); len(p.KeyFiles) != 0 {
		t.Errorf("a test run opened a keystore: %v", p.KeyFiles)
	}
	if p := plan(t, c, "forge script s.sol --broadcast --rpc-url mainnet"); len(p.KeyFiles) != 1 || p.KeyFiles[0] != filepath.Join(keystores, "deployer") {
		t.Errorf("a signing run should open the one named keystore: %v", p.KeyFiles)
	}
	c.Env = []string{"ETH_KEYSTORE_ACCOUNT=../../.ssh"}
	if p := plan(t, c, "forge script s.sol --broadcast --rpc-url mainnet"); len(p.KeyFiles) != 0 {
		t.Errorf("a path as a keystore name opened %v", p.KeyFiles)
	}
}

func TestKeyFileVerdict(t *testing.T) {
	c := testContext(t)
	home := c.Home
	ssh := filepath.Join(home, ".ssh")
	keystores := filepath.Join(home, ".foundry", "keystores")
	solana := filepath.Join(home, ".config", "solana", "id.json")
	keypair := "[" + strings.TrimSuffix(strings.Repeat("7,", 64), ",") + "]"
	write(t, filepath.Join(ssh, "id_ed25519"), "ssh key")
	write(t, filepath.Join(keystores, "deployer"), "{}")
	write(t, filepath.Join(keystores, "nested", "x"), "{}")
	write(t, solana, keypair)
	write(t, filepath.Join(home, ".npmrc"), "//registry/:_authToken=x")
	write(t, filepath.Join(c.Root, "wallet.json"), keypair)
	protected := []string{ssh, keystores, solana, filepath.Join(home, ".npmrc")}
	cases := []struct {
		path string
		mode Mode
		want int
	}{
		{filepath.Join(keystores, "deployer"), ModeSigning, keyOpen},
		{solana, ModeSigning, keyOpen},
		{ssh, ModeSigning, keyRefused},                              // a folder
		{filepath.Join(ssh, "id_ed25519"), ModeSigning, keyRefused}, // a protected file that is not a wallet
		{keystores, ModeSigning, keyRefused},                        // the whole keystore folder
		{filepath.Join(keystores, "nested", "x"), ModeSigning, keyRefused},
		{filepath.Join(home, ".npmrc"), ModeSigning, keyRefused},
		{filepath.Join(home, ".npmrc"), ModeAuth, keyOpen},
		{filepath.Join(c.Root, "wallet.json"), ModeSigning, keyNotProtected},
		{filepath.Join(ssh, "missing"), ModeSigning, keyNotProtected},
	}
	for _, k := range cases {
		if got := keyFileVerdict(k.path, protected, home, k.mode); got != k.want {
			t.Errorf("keyFileVerdict(%s) = %d, want %d", relTo(home, k.path), got, k.want)
		}
	}
}

func TestHelpIsNotAskedAbout(t *testing.T) {
	c := testContext(t)
	for _, line := range []string{"forge create --help", "cast send -h", "anchor deploy --help", "solana transfer --help"} {
		if p := plan(t, c, line); p.Gate != nil || len(p.KeyFiles) != 0 {
			t.Errorf("%q: %+v", line, p.Gate)
		}
	}
}
