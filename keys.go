package main

import (
	"bufio"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/term"
)

// Moving keys out of plain text, and handing an encrypted Solana keypair to a tool for one command.

func vaultDir() string         { return filepath.Join(stateDir(), "vault") }
func devWalletPath() string    { return filepath.Join(stateDir(), "dev-wallet.json") }
func foundryKeystores() string { return filepath.Join(homeDir(), ".foundry", "keystores") }

var validName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// vaultRef is a key in the vault that a command needs as a file.
type vaultRef struct {
	name     string
	flag     string // the flag the tool takes the path with
	explicit bool   // the flag is already on the command line
	prepend  bool   // the flag goes before the sub-command
}

// vaultIndex maps old keypair paths to vault names; it is read once per run.
var vaultIndex map[string]string

// parseVaultRef recognises "keymoat:NAME" and the path a keypair had before it was moved into the vault.
func parseVaultRef(path string) (vaultRef, bool) {
	if name, ok := strings.CutPrefix(path, "keymoat:"); ok && validName.MatchString(name) {
		return vaultRef{name: name}, true
	}
	if vaultIndex == nil {
		vaultIndex = loadConfig().Vault
	}
	if name := vaultIndex[realPath(expandHome(path))]; name != "" {
		if _, err := os.Stat(expandHome(path)); err != nil {
			return vaultRef{name: name}, true
		}
	}
	return vaultRef{}, false
}

// readSecret asks for a password without showing it. Tests replace it.
var readSecret = func(prompt string) (string, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return "", fmt.Errorf("no terminal to ask for a password on")
	}
	defer tty.Close()
	fmt.Fprint(tty, prompt)
	b, err := term.ReadPassword(int(tty.Fd()))
	fmt.Fprintln(tty)
	return string(b), err
}

// readLine asks a question on the terminal and returns the answer. Tests replace it.
var readLine = func(prompt string) (string, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return "", fmt.Errorf("no terminal to ask on")
	}
	defer tty.Close()
	fmt.Fprint(tty, prompt)
	line, err := bufio.NewReader(tty).ReadString('\n')
	return strings.TrimSpace(line), err
}

func newPassword(what string) (string, error) {
	for {
		a, err := readSecret("Password for " + what + ": ")
		if err != nil {
			return "", err
		}
		if len(a) < 8 {
			fmt.Fprintln(os.Stderr, "Use at least 8 characters.")
			continue
		}
		b, err := readSecret("Again: ")
		if err != nil {
			return "", err
		}
		if a == b {
			return a, nil
		}
		fmt.Fprintln(os.Stderr, "The two did not match.")
	}
}

func writePrivate(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// wipe overwrites a file before removing it. On a modern disk the old bytes may still exist somewhere (snapshots,
// backups, the drive's own spare blocks), which is why the tool also tells the user to rotate a key that was exposed.
func wipe(path string) error {
	if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
		if f, err := os.OpenFile(path, os.O_WRONLY, 0); err == nil {
			f.Write(make([]byte, info.Size()))
			f.Sync()
			f.Close()
		}
	}
	return os.Remove(path)
}

// devWallet makes sure the throwaway wallet for local tests exists, and returns its address.
func devWallet() (string, error) {
	path := devWalletPath()
	if data, err := os.ReadFile(path); err == nil {
		if key, err := parseSolanaKeypair(data); err == nil {
			return solanaAddress(key), nil
		}
		os.Remove(path)
	}
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", err
	}
	if err := writePrivate(path, solanaKeypairJSON(key)); err != nil {
		return "", err
	}
	return solanaAddress(key), nil
}

// materialize decrypts a vault entry into a file only its owner can read, for the length of one command.
func materialize(name string) (path string, cleanup func(), err error) {
	data, err := os.ReadFile(filepath.Join(vaultDir(), name+".json"))
	if err != nil {
		return "", nil, fmt.Errorf("no key named %q in the vault", name)
	}
	var key ed25519.PrivateKey
	for try := 0; ; try++ {
		pw, err := readSecret(fmt.Sprintf("Password for the key %q: ", name))
		if err != nil {
			return "", nil, err
		}
		key, err = decryptSolanaKeypair(data, pw)
		if err == nil {
			break
		}
		if err != errWrongPassword || try == 2 {
			return "", nil, err
		}
		fmt.Fprintln(os.Stderr, "Wrong password.")
	}
	dir := filepath.Join(stateDir(), "run", hex.EncodeToString(randomBytes(8)))
	path = filepath.Join(dir, name+".json")
	if err := writePrivate(path, solanaKeypairJSON(key)); err != nil {
		return "", nil, err
	}
	return path, func() { wipe(path); os.Remove(dir) }, nil
}

func cmdKeys(args []string) int {
	switch first(args) {
	case "move":
		return keysMove(args[1:])
	case "list":
		return keysList()
	case "export":
		return keysExport(args[1:])
	}
	return usageError("keymoat keys move [FILE] | list | export NAME --to FILE")
}

func keysList() int {
	names := func(dir, suffix string) []string {
		var out []string
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), suffix) {
				out = append(out, strings.TrimSuffix(e.Name(), suffix))
			}
		}
		sort.Strings(out)
		return out
	}
	evm, sol := names(foundryKeystores(), ""), names(vaultDir(), ".json")
	if len(evm)+len(sol) == 0 {
		fmt.Println("No encrypted keys yet. `keymoat scan` finds keys in plain text; `keymoat keys move` encrypts them.")
		return 0
	}
	if len(evm) > 0 {
		fmt.Println("Foundry keystores (use with --account NAME):")
		for _, n := range evm {
			fmt.Println("  " + n)
		}
	}
	if len(sol) > 0 {
		fmt.Println("Solana keypairs in the keymoat vault (use with --keypair keymoat:NAME):")
		for _, n := range sol {
			addr := ""
			if data, err := os.ReadFile(filepath.Join(vaultDir(), n+".json")); err == nil {
				if m := regexp.MustCompile(`"address":\s*"([^"]+)"`).FindSubmatch(data); m != nil {
					addr = string(m[1])
				}
			}
			fmt.Printf("  %-20s %s\n", n, addr)
		}
	}
	return 0
}

func keysMove(args []string) int {
	if len(args) > 1 {
		return usageError("keymoat keys move [FILE]")
	}
	var targets []string
	if len(args) == 1 {
		targets = []string{absFrom(mustGetwd(), args[0])}
	} else {
		root := projectRoot(mustGetwd(), homeDir())
		if root == "" {
			say("not inside a project; name the file to move")
			return 2
		}
		for _, f := range projectEnvFiles(root) {
			if len(f.Secrets) > 0 {
				targets = append(targets, f.Path)
			}
		}
		if len(targets) == 0 {
			say("no key in plain text found in this project's env files")
			return 0
		}
	}
	code := 0
	for _, t := range targets {
		var err error
		if isSolanaKeypairFile(t) {
			err = moveSolanaKeypair(t)
		} else {
			err = moveEnvKeys(t)
		}
		if err != nil {
			say("%s: %v", t, err)
			code = 1
		}
	}
	return code
}

func mustGetwd() string {
	cwd, err := os.Getwd()
	if err != nil {
		fatal("%v", err)
	}
	return cwd
}

func askName(what, suggestion, dir, suffix string) (string, error) {
	for {
		name, err := readLine(fmt.Sprintf("Name for %s [%s]: ", what, suggestion))
		if err != nil {
			return "", err
		}
		if name == "" {
			name = suggestion
		}
		if !validName.MatchString(name) {
			fmt.Fprintln(os.Stderr, "Use letters, digits, dots, dashes and underscores.")
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, name+suffix)); err == nil {
			fmt.Fprintf(os.Stderr, "%q is taken.\n", name)
			continue
		}
		return name, nil
	}
}

// moveEnvKeys moves every private key in an env file into a Foundry keystore, and takes it out of the file.
func moveEnvKeys(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	moved := 0
	for _, v := range parseEnv(string(data)) {
		kind := secretKind(v.Name, v.Value)
		if kind == "" {
			continue
		}
		if !strings.HasSuffix(kind, "private key") {
			say("%s line %d: %s is a %s, which keymoat does not move. Import it yourself with `cast wallet import NAME --interactive` (or --mnemonic-path), then delete the line.", filepath.Base(path), v.Line, v.Name, kind)
			continue
		}
		key, err := parseEVMKey(v.Value)
		if err != nil {
			return fmt.Errorf("line %d: %v", v.Line, err)
		}
		fmt.Fprintf(os.Stderr, "\n%s line %d: %s is a private key in plain text.\n", path, v.Line, v.Name)
		name, err := askName("its keystore", strings.ToLower(strings.TrimSuffix(strings.TrimSuffix(v.Name, "_PRIVATE_KEY"), "_KEY")), foundryKeystores(), "")
		if err != nil {
			return err
		}
		pw, err := newPassword("the keystore")
		if err != nil {
			return err
		}
		sealed, err := encryptEVMKey(key, pw)
		if err != nil {
			return err
		}
		// Proof that the keystore opens, before anything is removed.
		if back, err := decryptEVMKey(sealed, pw); err != nil || hex.EncodeToString(back) != hex.EncodeToString(key) {
			return fmt.Errorf("the new keystore did not open again; nothing was changed")
		}
		dest := filepath.Join(foundryKeystores(), name)
		if err := writePrivate(dest, sealed); err != nil {
			return err
		}
		lines[v.Line-1] = fmt.Sprintf("# %s is now the encrypted keystore %q (moved by keymoat). Sign with: --account %s", v.Name, name, name)
		if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o600); err != nil {
			return fmt.Errorf("the keystore %s was written, but %s could not be updated: %v", dest, path, err)
		}
		moved++
		fmt.Fprintf(os.Stderr, "Moved to %s and removed from %s.\n", dest, filepath.Base(path))
		fmt.Fprintf(os.Stderr, "  Sign with:      forge script ... --account %s     (or cast send ... --account %s)\n", name, name)
		fmt.Fprintf(os.Stderr, "  In scripts:     use vm.startBroadcast() with no argument in place of vm.envUint(%q)\n", v.Name)
		fmt.Fprintf(os.Stderr, "  Its address:    cast wallet address --account %s\n", name)
	}
	if moved > 0 {
		fmt.Fprintln(os.Stderr, "\nThe key was in a plain file until now. If this machine may have run untrusted code, move the funds to a new key.")
	}
	return nil
}

// moveSolanaKeypair seals a keypair file into the vault and removes the plain file.
func moveSolanaKeypair(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	key, err := parseSolanaKeypair(data)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "\n%s is a Solana keypair in plain text (address %s).\n", path, solanaAddress(key))
	name, err := askName("it in the vault", strings.TrimSuffix(filepath.Base(path), ".json"), vaultDir(), ".json")
	if err != nil {
		return err
	}
	pw, err := newPassword("the key")
	if err != nil {
		return err
	}
	sealed, err := encryptSolanaKeypair(key, pw)
	if err != nil {
		return err
	}
	if back, err := decryptSolanaKeypair(sealed, pw); err != nil || !back.Equal(key) {
		return fmt.Errorf("the new vault entry did not open again; nothing was changed")
	}
	dest := filepath.Join(vaultDir(), name+".json")
	if err := writePrivate(dest, sealed); err != nil {
		return err
	}
	cfg := loadConfig()
	cfg.Vault[realPath(path)] = name
	if err := cfg.save(); err != nil {
		return fmt.Errorf("the vault entry %s was written, but the settings could not be saved, so the plain file was kept: %v", dest, err)
	}
	if err := wipe(path); err != nil {
		return fmt.Errorf("the vault entry %s was written, but the plain file could not be removed: %v", dest, err)
	}
	fmt.Fprintf(os.Stderr, "Moved to the vault as %q and removed the plain file.\n", name)
	fmt.Fprintf(os.Stderr, "  Commands that used %s keep working through keymoat: it asks for the password and hands the tool the key for that one command.\n", path)
	fmt.Fprintf(os.Stderr, "  Elsewhere:  --keypair keymoat:%s   (Anchor: --provider.wallet keymoat:%s)\n", name, name)
	fmt.Fprintf(os.Stderr, "  To get the plain file back:  keymoat keys export %s --to FILE\n", name)
	fmt.Fprintln(os.Stderr, "\nThe key was in a plain file until now. If this machine may have run untrusted code, move the funds to a new key.")
	return nil
}

func keysExport(args []string) int {
	to, _ := flagValue(args, "--to")
	pos := positionals(args, map[string]bool{"--to": true})
	if len(pos) != 1 || to == "" {
		return usageError("keymoat keys export NAME --to FILE")
	}
	name, dest := pos[0], absFrom(mustGetwd(), to)
	data, err := os.ReadFile(filepath.Join(vaultDir(), name+".json"))
	if err != nil {
		say("no key named %q in the vault (Foundry keystores are exported with `cast wallet decrypt-keystore`)", name)
		return 1
	}
	pw, err := readSecret(fmt.Sprintf("Password for the key %q: ", name))
	if err != nil {
		say("%v", err)
		return 1
	}
	key, err := decryptSolanaKeypair(data, pw)
	if err != nil {
		say("%v", err)
		return 1
	}
	if err := writePrivate(dest, solanaKeypairJSON(key)); err != nil {
		say("%v", err)
		return 1
	}
	say("wrote the keypair in plain text to %s; the vault entry is still there", dest)
	return 0
}
