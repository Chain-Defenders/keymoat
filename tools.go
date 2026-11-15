package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// What keymoat does with a command depends on two things: whether the tool runs the project's code (and so its
// dependencies' code), and whether this command has to sign. Tools that run project code are sandboxed. A command
// that signs is shown to the user first, and gets the one key it names.

type Mode int

const (
	ModeNoKeys  Mode = iota // builds, tests, installs: no key is visible
	ModeSigning             // a deploy or a send: the project's env files and the named key are visible
	ModeAuth                // a publish or a login: the registry token is visible
)

type Plan struct {
	Tool    string
	Args    []string // what the real tool is run with
	Sandbox bool
	Mode    Mode
	NetOpen bool
	USB     bool

	KeyFiles   []string // key files this command may read
	ExtraWrite []string // more places it may write
	CLIHosts   []string // hosts named on the command line: the user typed them
	Gate       *Gate    // set when the user is asked first
	Notes      []string // one-line remarks printed before the run
	WarmUp     string   // a program to start once before the run
	VaultRefs  []vaultRef
}

// Gate is what the user is shown before a command that signs.
type Gate struct {
	Action  string // "Deploy with forge script"
	Network string // a name, or a host
	RPCURL  string // looked up for the chain when set
	Key     string // where the signature comes from
	Target  string
	Warn    []string
	Local   bool // a local node: nothing to confirm
	Solana  bool
}

// Context is where a command runs.
type Context struct {
	Cwd, Home, Root string
	Env             []string
	EnvFiles        []EnvFile
	values          map[string]string
}

func (c *Context) value(name string) string {
	if c.values == nil {
		c.values = envValues(c.Env, c.EnvFiles, c.Root)
	}
	return c.values[name]
}

// Tools keymoat stands in front of, and whether each runs project code.
var tools = map[string]bool{
	"forge": true, "anchor": true, "cargo": true,
	"npm": true, "npx": true, "pnpm": true, "pnpx": true, "yarn": true, "bun": true, "bunx": true,
	"cast": false, "solana": false, "spl-token": false,
}

// flagValue finds "--name value" or "--name=value" among the arguments.
func flagValue(args []string, names ...string) (string, bool) {
	for i, a := range args {
		for _, n := range names {
			if a == n && i+1 < len(args) {
				return args[i+1], true
			}
			if strings.HasPrefix(a, n+"=") {
				return a[len(n)+1:], true
			}
		}
	}
	return "", false
}

func hasFlag(args []string, names ...string) bool {
	for _, a := range args {
		for _, n := range names {
			if a == n || strings.HasPrefix(a, n+"=") {
				return true
			}
		}
	}
	return false
}

// positionals returns the arguments that are not flags, given the flags that take a value.
func positionals(args []string, valueFlags map[string]bool) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			return append(out, args[i+1:]...)
		}
		if strings.HasPrefix(a, "-") && a != "-" {
			if valueFlags[a] && !strings.Contains(a, "=") {
				i++
			}
			continue
		}
		out = append(out, a)
	}
	return out
}

func classify(tool string, args []string, c *Context) Plan {
	p := Plan{Tool: tool, Args: args, Sandbox: tools[tool]}
	// Asking for help is not doing the thing.
	if hasFlag(args, "--help", "-h") {
		return p
	}
	switch tool {
	case "forge":
		classifyForge(&p, c)
	case "cast":
		classifyCast(&p, c)
	case "solana", "spl-token":
		classifySolana(&p, c)
	case "anchor":
		classifyAnchor(&p, c)
	case "cargo":
		classifyCargo(&p, c)
	default:
		classifyNode(&p, c)
	}
	return p
}

// ---- Foundry

var foundryValueFlags = map[string]bool{"--rpc-url": true, "-f": true, "--fork-url": true, "--account": true, "--keystore": true, "--keystores": true, "--private-key": true, "--private-keys": true, "--sender": true, "--from": true, "--value": true, "--sig": true, "-s": true, "--password": true, "--password-file": true, "--mnemonics": true, "--mnemonic": true, "--chain": true, "--etherscan-api-key": true, "--verifier": true, "--verifier-url": true, "--gas-limit": true, "--gas-price": true, "--priority-gas-price": true, "--nonce": true, "--root": true, "--contracts": true, "-C": true, "--match-test": true, "--match-contract": true, "--match-path": true, "--fork-block-number": true, "--constructor-args": true, "--target-contract": true, "--tc": true, "--with-gas-price": true, "--hd-path": true, "--mnemonic-derivation-path": true, "--mnemonic-index": true, "-r": true}

// foundryKey describes where a Foundry command signs from, and adds the key file it needs to the plan.
//
// A keystore's name is a name, never a path: "../../.ssh" must not turn the keystore folder into a way to any
// other folder. A name that comes from the environment opens a keystore only for a command that signs, and only
// the caller's real environment counts: a project's env file is something a dependency can rewrite.
func foundryKey(p *Plan, c *Context, signing bool) (key string, warn []string) {
	args := p.Args
	keystores := filepath.Join(c.Home, ".foundry", "keystores")
	if name, ok := flagValue(args, "--account"); ok {
		if validName.MatchString(name) {
			p.KeyFiles = append(p.KeyFiles, filepath.Join(keystores, name))
			key = "encrypted keystore \"" + name + "\""
		} else {
			key = "a keystore name keymoat does not accept"
			warn = append(warn, "The keystore name is not a plain name, so keymoat did not open it.")
		}
	} else if name := realEnv(c, "ETH_KEYSTORE_ACCOUNT"); name != "" && validName.MatchString(name) {
		if signing {
			p.KeyFiles = append(p.KeyFiles, filepath.Join(keystores, name))
		}
		key = "encrypted keystore \"" + name + "\""
	}
	if path, ok := flagValue(args, "--keystore", "--keystores"); ok {
		p.KeyFiles = append(p.KeyFiles, absFrom(c.Cwd, path))
		key = "encrypted keystore " + path
	}
	if path, ok := flagValue(args, "--password-file"); ok {
		p.KeyFiles = append(p.KeyFiles, absFrom(c.Cwd, path))
	}
	if hasFlag(args, "--ledger", "-l", "--trezor", "-t") {
		p.USB = true
		key = "hardware wallet"
	}
	if hasFlag(args, "--interactive", "-i", "--interactives") {
		key = "typed in when asked"
	}
	if hasFlag(args, "--private-key", "--private-keys") {
		key = "a private key on the command line"
		warn = append(warn, "The private key is on the command line: it stays in your shell history and any program can read it there.")
	}
	if hasFlag(args, "--mnemonic", "--mnemonics") {
		key = "a seed phrase on the command line"
		warn = append(warn, "The seed phrase is on the command line or in a plain file.")
	}
	if hasFlag(args, "--unlocked") {
		key = "an account the node has unlocked"
	}
	if key == "" {
		var names []string
		for _, f := range c.EnvFiles {
			for _, s := range f.Secrets {
				names = append(names, s.Name+" in "+relTo(c.Root, f.Path))
			}
		}
		for _, kv := range c.Env {
			if n, v, _ := strings.Cut(kv, "="); secretKind(n, v) != "" {
				names = append(names, n+" in your shell's environment")
			}
		}
		if len(names) > 0 {
			key = "plain text: " + strings.Join(names, ", ")
			warn = append(warn, "The key is stored in plain text. `keymoat keys move` puts it in an encrypted keystore.")
		} else {
			key = "not named on the command line (the script decides)"
		}
	}
	return key, warn
}

// realEnv reads a variable from the caller's own environment, not from the project's env files.
func realEnv(c *Context, name string) string {
	for _, kv := range c.Env {
		if k, v, _ := strings.Cut(kv, "="); k == name {
			return v
		}
	}
	return ""
}

// foundryRPC resolves the endpoint a Foundry command will use: the flag (which may be a name from foundry.toml),
// the environment, the project's default, or the local node.
func foundryRPC(p *Plan, c *Context) string {
	raw, ok := flagValue(p.Args, "--rpc-url", "--fork-url", "-f", "-r")
	if !ok {
		raw = c.value("ETH_RPC_URL")
	}
	if raw == "" && c.Root != "" {
		if data, err := os.ReadFile(filepath.Join(c.Root, "foundry.toml")); err == nil {
			for _, table := range []string{"profile.default", "profile." + orDefault(c.value("FOUNDRY_PROFILE"), "default")} {
				if v := tomlTable(string(data), table)["eth_rpc_url"]; v != "" {
					raw = v
				}
			}
		}
	}
	if raw == "" {
		return "http://localhost:8545"
	}
	if !strings.Contains(raw, "://") && c.Root != "" {
		if u := foundryEndpoints(c.Root, envMap(c))[raw]; u != "" {
			return u
		}
	}
	return expandEnvRefs(raw, envMap(c))
}

func envMap(c *Context) map[string]string {
	c.value("")
	return c.values
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func absFrom(cwd, p string) string {
	p = expandHome(p)
	if !filepath.IsAbs(p) {
		p = filepath.Join(cwd, p)
	}
	return p
}

func relTo(root, p string) string {
	if root != "" {
		if r, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(r, "..") {
			return r
		}
	}
	return p
}

func classifyForge(p *Plan, c *Context) {
	pos := positionals(p.Args, foundryValueFlags)
	sub := first(pos)
	rpc := foundryRPC(p, c)
	if h := hostOf(rpc); h != "" && hasFlag(p.Args, "--rpc-url", "--fork-url", "-f", "-r") && strings.Contains(firstFlagValue(p.Args, "--rpc-url", "--fork-url", "-f", "-r"), "://") {
		p.CLIHosts = append(p.CLIHosts, h)
	}
	if u, ok := flagValue(p.Args, "--verifier-url"); ok {
		p.CLIHosts = append(p.CLIHosts, hostOf(u))
	}
	// `--resume` sends what an earlier run left unsent. `create` is asked about with or without `--broadcast`:
	// older versions of forge deploy without it.
	script := sub == "script" && hasFlag(p.Args, "--broadcast", "--resume")
	create := sub == "create" || sub == "c"
	// Naming a keystore on the command line is asking for it, broadcast or not.
	key, warn := foundryKey(p, c, script || create)
	if !script && !create {
		return
	}
	p.Mode = ModeSigning
	action, target := "Deploy with forge create", second(pos)
	if script {
		action = "Run a script that sends transactions (forge script --broadcast)"
	}
	p.Gate = &Gate{Action: action, RPCURL: rpc, Network: hostOf(rpc), Key: key, Target: target, Warn: warn, Local: isLocalHost(hostOf(rpc))}
}

// The cast commands that sign or send, by every name cast accepts for them.
var (
	castSend    = map[string]string{"send": "Send a transaction (cast send)", "s": "Send a transaction (cast send)", "batch-send": "Send a batch of transactions (cast batch-send)", "bs": "Send a batch of transactions (cast batch-send)"}
	castPublish = map[string]bool{"publish": true, "p": true}
	castSign    = map[string]bool{"mktx": true, "m": true, "batch-mktx": true, "bm": true}
	castErc20   = map[string]bool{"transfer": true, "t": true, "send": true, "approve": true, "a": true, "mint": true, "m": true, "burn": true, "bu": true}
	castWallet  = map[string]string{"sign": "Sign a message or typed data (cast wallet sign)", "s": "Sign a message or typed data (cast wallet sign)", "sign-auth": "Sign an account delegation (cast wallet sign-auth)", "sa": "Sign an account delegation (cast wallet sign-auth)"}
)

func classifyCast(p *Plan, c *Context) {
	pos := positionals(p.Args, foundryValueFlags)
	sub, next := first(pos), second(pos)
	rpc := foundryRPC(p, c)
	g := &Gate{RPCURL: rpc, Network: hostOf(rpc), Local: isLocalHost(hostOf(rpc)), Target: strings.Join(tail(pos, 1), " ")}
	switch {
	case castSend[sub] != "":
		g.Action = castSend[sub]
	case castPublish[sub]:
		g.Action = "Publish a signed transaction (cast publish)"
	case castSign[sub]:
		g.Action = "Sign a transaction (cast mktx)"
		g.Warn = append(g.Warn, "The transaction is signed, not sent: whoever holds the result can send it.")
	case (sub == "erc20-token" || sub == "erc20") && castErc20[next]:
		g.Action, g.Target = "Send a token transaction (cast erc20 "+next+")", strings.Join(tail(pos, 2), " ")
	case (sub == "wallet" || sub == "w") && castWallet[next] != "":
		// A signature is not tied to an endpoint, and can be as binding as a transaction: always asked about.
		g.Action, g.Target = castWallet[next], strings.Join(tail(pos, 2), " ")
		g.RPCURL, g.Network, g.Local = "", "none: a signature, not a transaction", false
	default:
		return
	}
	key, warn := foundryKey(p, c, true)
	g.Key, g.Warn = key, append(g.Warn, warn...)
	if castPublish[sub] {
		g.Key, g.Warn = "already signed", nil
	}
	if v, ok := flagValue(p.Args, "--value"); ok {
		g.Target += "  (value " + v + ")"
	}
	p.Mode, p.Gate = ModeSigning, g
}

// ---- Solana

var solanaValueFlags = map[string]bool{"-u": true, "--url": true, "-k": true, "--keypair": true, "-C": true, "--config": true, "--commitment": true, "--ws": true, "--output": true, "--fee-payer": true, "--from": true, "--program-id": true, "--buffer": true, "--upgrade-authority": true, "--authority": true, "--owner": true, "--with-compute-unit-price": true, "--max-len": true, "--max-sign-attempts": true, "--signer": true, "--nonce": true, "--nonce-authority": true, "--blockhash": true, "--provider.cluster": true, "--provider.wallet": true, "--program-name": true, "-p": true, "--program-keypair": true, "--filepath": true, "-f": true}

var solanaSigning = map[string]bool{
	"transfer": true, "program": true, "program-v4": true, "deploy": true,
	"create-stake-account": true, "delegate-stake": true, "deactivate-stake": true, "withdraw-stake": true, "split-stake": true, "merge-stake": true, "stake-authorize": true, "stake-set-lockup": true, "redelegate-stake": true,
	"create-vote-account": true, "vote-authorize-voter": true, "vote-authorize-withdrawer": true, "withdraw-from-vote-account": true, "close-vote-account": true, "vote-update-validator": true, "vote-update-commission": true,
	"create-nonce-account": true, "authorize-nonce-account": true, "new-nonce": true, "withdraw-from-nonce-account": true, "upgrade-nonce-account": true,
	"address-lookup-table": true, "feature": true, "validator-info": true,
}

// Commands that send nothing and still load the wallet.
var solanaUsesWallet = map[string]bool{"address": true, "balance": true, "airdrop": true, "sign-offchain-message": true}

// Sub-commands under a signing command that only read.
var solanaReadOnly = map[string]bool{"show": true, "dump": true, "get": true, "status": true, "help": true}

var splTokenReadOnly = map[string]bool{"accounts": true, "balance": true, "supply": true, "display": true, "address": true, "account-info": true, "multisig-info": true, "help": true, "bench": true, "": true}

// solanaCluster resolves a cluster name or URL to a URL.
func solanaCluster(raw string) string {
	if u, ok := solanaClusters[raw]; ok {
		return u
	}
	return raw
}

func solanaNetworkName(rpc string) string {
	for name, u := range solanaClusters {
		if u == rpc && len(name) > 1 && name != "mainnet" && name != "localhost" {
			return name
		}
	}
	return hostOf(rpc)
}

func classifySolana(p *Plan, c *Context) {
	pos := positionals(p.Args, solanaValueFlags)
	sub := first(pos)
	cfgURL, cfgKey := solanaConfig(c.Home)
	rpc := cfgURL
	if v, ok := flagValue(p.Args, "-u", "--url"); ok {
		rpc = solanaCluster(v)
	}
	keypair := cfgKey
	explicit := false
	if v, ok := flagValue(p.Args, "-k", "--keypair"); ok {
		keypair, explicit = v, true
	}
	if ref, ok := parseVaultRef(keypair); ok {
		// A key in keymoat's vault: the tool gets a temporary copy for this one command.
		ref.flag, ref.explicit = "--keypair", explicit
		p.VaultRefs = append(p.VaultRefs, ref)
		keypair = "keymoat vault \"" + ref.name + "\""
	}
	signing := solanaSigning[sub] && !solanaReadOnly[second(pos)]
	if p.Tool == "spl-token" {
		signing = !splTokenReadOnly[sub]
	}
	if !signing {
		// A command that only reads needs no key, unless it is one that prints or uses the wallet's own address.
		if !explicit && !solanaUsesWallet[sub] {
			p.VaultRefs = nil
		}
		return
	}
	g := &Gate{Action: strings.TrimSpace(p.Tool + " " + sub + " " + subIfWord(second(pos))), RPCURL: rpc, Network: solanaNetworkName(rpc), Key: keypair, Target: strings.Join(tail(pos, 1), " "), Local: isLocalHost(hostOf(rpc)), Solana: true}
	if strings.HasPrefix(keypair, "usb://") {
		g.Key = "hardware wallet"
	} else if len(p.VaultRefs) == 0 && !strings.Contains(keypair, "://") && keypair != "ASK" {
		g.Key = keypair + keypairAddress(absFrom(c.Cwd, keypair))
		g.Warn = append(g.Warn, "This keypair file is plain text. `keymoat keys move "+keypair+"` encrypts it.")
	}
	p.Mode, p.Gate = ModeSigning, g
}

// keypairAddress returns " (address ...)" for a readable keypair file, or "".
func keypairAddress(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	key, err := parseSolanaKeypair(data)
	if err != nil {
		return ""
	}
	return " (address " + solanaAddress(key) + ")"
}

func subIfWord(s string) string {
	if regexp.MustCompile(`^[a-z][a-z0-9-]*$`).MatchString(s) {
		return s
	}
	return ""
}

// ---- Anchor

var anchorSigning = map[string]bool{"deploy": true, "upgrade": true, "migrate": true, "run": true, "airdrop": true}
var anchorIdlReadOnly = map[string]bool{"build": true, "parse": true, "fetch": true, "convert": true, "type": true, "authority": true, "": true}

func classifyAnchor(p *Plan, c *Context) {
	pos := positionals(p.Args, solanaValueFlags)
	sub := first(pos)
	var cfg map[string]string
	if c.Root != "" {
		if data, err := os.ReadFile(filepath.Join(c.Root, "Anchor.toml")); err == nil {
			cfg = tomlTable(string(data), "provider")
		}
	}
	cluster := orDefault(cfg["cluster"], "localnet")
	if v, ok := flagValue(p.Args, "--provider.cluster"); ok {
		cluster = v
	}
	rpc := cluster
	if u, ok := solanaClusters[strings.ToLower(cluster)]; ok {
		rpc = u
	}
	local := isLocalHost(hostOf(rpc))
	_, defaultKey := solanaConfig(c.Home)
	wallet := orDefault(cfg["wallet"], defaultKey)
	explicit := false
	if v, ok := flagValue(p.Args, "--provider.wallet"); ok {
		wallet, explicit = v, true
	}

	needsWallet := sub == "test" || sub == "localnet" || anchorSigning[sub] || (sub == "idl" && !anchorIdlReadOnly[second(pos)])
	if !needsWallet {
		return
	}
	// Tests on a local validator get a wallet that is worth nothing, so the real one is never in reach of test code.
	if (sub == "test" || sub == "localnet") && local && !explicit {
		p.Args = append([]string{"--provider.wallet", devWalletPath()}, p.Args...)
		p.WarmUp = "solana-test-validator"
		p.Notes = append(p.Notes, "tests run with a throwaway wallet, not your own (pass --provider.wallet to choose one)")
		return
	}
	if ref, ok := parseVaultRef(wallet); ok {
		ref.flag, ref.explicit, ref.prepend = "--provider.wallet", explicit, true
		p.VaultRefs = append(p.VaultRefs, ref)
		wallet = "keymoat vault \"" + ref.name + "\""
	} else {
		p.KeyFiles = append(p.KeyFiles, absFrom(c.Cwd, wallet))
	}
	p.Mode = ModeSigning
	// A Solana deploy sends to validators directly, which no proxy can carry.
	p.NetOpen = !local
	g := &Gate{Action: strings.TrimSpace("anchor " + sub + " " + subIfWord(second(pos))), RPCURL: rpc, Network: solanaNetworkName(rpc), Key: wallet, Local: local, Solana: true}
	if !local {
		g.Warn = append(g.Warn, "The network is not restricted for this command: a Solana deploy talks to validators directly.")
		if sub == "test" || sub == "run" || sub == "migrate" {
			g.Warn = append(g.Warn, "This runs the project's scripts with the wallet readable.")
		}
	}
	if len(p.VaultRefs) == 0 {
		g.Key = wallet + keypairAddress(absFrom(c.Cwd, wallet))
		g.Warn = append(g.Warn, "This wallet file is plain text. `keymoat keys move "+wallet+"` encrypts it.")
	}
	p.Gate = g
}

// ---- Cargo

func classifyCargo(p *Plan, c *Context) {
	switch first(positionals(p.Args, map[string]bool{"--manifest-path": true, "--target": true, "-p": true, "--package": true, "--features": true, "-F": true, "--profile": true, "--target-dir": true, "-j": true, "--jobs": true, "--config": true, "-Z": true, "--color": true})) {
	case "publish", "login", "logout", "owner", "yank":
		p.Mode = ModeAuth
		p.KeyFiles = append(p.KeyFiles, filepath.Join(c.Home, ".cargo", "credentials.toml"), filepath.Join(c.Home, ".cargo", "credentials"))
		p.ExtraWrite = append(p.ExtraWrite, filepath.Join(c.Home, ".cargo", "credentials.toml"))
	case "install", "uninstall", "binstall":
		p.ExtraWrite = append(p.ExtraWrite, filepath.Join(c.Home, ".cargo", "bin"), filepath.Join(c.Home, ".cargo", ".crates.toml"), filepath.Join(c.Home, ".cargo", ".crates2.json"))
		p.Notes = append(p.Notes, "this install may write to ~/.cargo/bin")
	}
}

// ---- npm, pnpm, yarn, bun

var nodeAuthCommands = map[string]bool{"publish": true, "login": true, "adduser": true, "logout": true, "whoami": true, "token": true, "owner": true, "access": true, "dist-tag": true, "deprecate": true, "unpublish": true, "team": true, "org": true, "profile": true}

var (
	hardhatNetwork = regexp.MustCompile(`\bhardhat\b[^|;&]*?--network[= ]+([A-Za-z0-9_.\-]+)`)
	deployWords    = regexp.MustCompile(`--broadcast\b|\banchor +(deploy|upgrade|migrate)\b|\bsolana +program +deploy\b|\bcast +send\b`)
)

var localNetworks = map[string]bool{"hardhat": true, "localhost": true, "local": true, "anvil": true, "default": true, "hardhatMainnet": true, "hardhatOp": true}

// packageScript returns the text of a script in the nearest package.json.
func packageScript(dir, name string) string {
	for ; ; dir = filepath.Dir(dir) {
		if data, err := os.ReadFile(filepath.Join(dir, "package.json")); err == nil {
			var pkg struct {
				Scripts map[string]string `json:"scripts"`
			}
			if json.Unmarshal(data, &pkg) == nil {
				return pkg.Scripts[name]
			}
			return ""
		}
		if filepath.Dir(dir) == dir {
			return ""
		}
	}
}

func classifyNode(p *Plan, c *Context) {
	pos := positionals(p.Args, map[string]bool{"--prefix": true, "-C": true, "--dir": true, "--cwd": true, "--filter": true, "-F": true, "-w": true, "--workspace": true, "--registry": true})
	sub := first(pos)
	if nodeAuthCommands[sub] {
		p.Mode = ModeAuth
		p.KeyFiles = append(p.KeyFiles, filepath.Join(c.Home, ".npmrc"), filepath.Join(c.Home, ".yarnrc.yml"), filepath.Join(c.Home, ".bunfig.toml"))
		p.ExtraWrite = append(p.ExtraWrite, filepath.Join(c.Home, ".npmrc"))
		return
	}
	// What will actually run: the command line itself, and the script it names.
	text := strings.Join(p.Args, " ")
	name := sub
	if (sub == "run" || sub == "run-script") && len(pos) > 1 {
		name = pos[1]
	}
	if s := packageScript(c.Cwd, name); s != "" {
		text += " ; " + s
	}
	network := ""
	if m := hardhatNetwork.FindStringSubmatch(text); m != nil && !localNetworks[m[1]] {
		network = m[1]
	}
	if network == "" && !deployWords.MatchString(text) {
		return
	}
	p.Mode = ModeSigning
	g := &Gate{Action: "Run a script that can deploy or send transactions", Network: orDefault(network, "set by the script"), Target: strings.TrimSpace(strings.TrimPrefix(text, strings.Join(p.Args, " ")+" ; "))}
	if network != "" {
		g.Action = "Run Hardhat against a live network"
	}
	g.Key, g.Warn = foundryKey(&Plan{}, c, true)
	if strings.HasPrefix(g.Key, "not named") {
		g.Key = "set in the project's configuration"
	}
	p.Gate = g
}

func first(s []string) string {
	if len(s) > 0 {
		return s[0]
	}
	return ""
}

func second(s []string) string {
	if len(s) > 1 {
		return s[1]
	}
	return ""
}

func tail(s []string, n int) []string {
	if len(s) > n {
		return s[n:]
	}
	return nil
}

func firstFlagValue(args []string, names ...string) string {
	v, _ := flagValue(args, names...)
	return v
}
