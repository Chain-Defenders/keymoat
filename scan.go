package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// `keymoat scan` looks for wallet keys kept in plain text: env files, project settings, keypair files and, when
// asked, the shell's history. It prints where each one is and never the key itself.

type Finding struct {
	File string `json:"file"`
	Line int    `json:"line,omitempty"`
	Name string `json:"name,omitempty"`
	Kind string `json:"kind"`
	Fix  string `json:"fix"`
}

var (
	keyInCode    = regexp.MustCompile(`(?i)(private_?key|accounts|mnemonic|secret_?key|deployer)[^\n]{0,60}?["'` + "`" + `](?:0x)?[0-9a-fA-F]{64}["'` + "`" + `]`)
	keyOnCommand = regexp.MustCompile(`(?i)(--private-keys?[= ]+|PRIVATE_KEY=|_PK=|--mnemonics?[= ]+["']?)((?:0x)?[0-9a-fA-F]{64}\b|[a-z]{3,8}( [a-z]{3,8}){11,23})`)
)

var hexIn = regexp.MustCompile(`[0-9a-fA-F]{64}`)

func scanTree(root string, depth int) []Finding {
	var out []Finding
	walkProject(root, depth, func(path, name string) {
		info, err := os.Stat(path)
		if err != nil || info.Size() > 1<<20 {
			return
		}
		switch {
		case isEnvFileName(name):
			data, err := os.ReadFile(path)
			if err != nil {
				return
			}
			for _, v := range parseEnv(string(data)) {
				if kind := secretKind(v.Name, v.Value); kind != "" {
					fix := "keymoat keys move " + path
					if !strings.HasSuffix(kind, "private key") {
						fix = "import it into an encrypted keystore (cast wallet import NAME --interactive) and delete the line"
					}
					out = append(out, Finding{File: path, Line: v.Line, Name: v.Name, Kind: kind, Fix: fix})
				}
			}
		case (secretFileNames[name] || strings.HasSuffix(name, ".key")) && isSecretFile(path):
			out = append(out, Finding{File: path, Kind: "a key or seed phrase in a plain file", Fix: "import it into an encrypted keystore and delete the file"})
		case strings.HasSuffix(name, ".json") && isSolanaKeypairFile(path):
			if strings.HasSuffix(name, "-keypair.json") && filepath.Base(filepath.Dir(path)) == "deploy" {
				// A program's own keypair: it names the program, it does not hold funds.
				return
			}
			out = append(out, Finding{File: path, Kind: "Solana keypair", Fix: "keymoat keys move " + path})
		case strings.HasPrefix(name, "hardhat.config.") || name == "foundry.toml" || name == "truffle-config.js" || name == "Anchor.toml":
			data, err := os.ReadFile(path)
			if err != nil {
				return
			}
			for i, line := range strings.Split(string(data), "\n") {
				if m := keyInCode.FindString(line); m != "" && !publicTestKeys[strings.ToLower(hexIn.FindString(m))] {
					out = append(out, Finding{File: path, Line: i + 1, Kind: "private key written into a settings file", Fix: "take it out of the file; sign with an encrypted keystore"})
				}
			}
		}
	})
	return out
}

// scanHistory looks for keys typed on a command line.
func scanHistory(home string) []Finding {
	var out []Finding
	for _, rel := range []string{".zsh_history", ".bash_history", ".zhistory", ".local/share/fish/fish_history"} {
		path := filepath.Join(home, rel)
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		count, firstLine := 0, 0
		for n := 1; sc.Scan(); n++ {
			if keyOnCommand.MatchString(sc.Text()) {
				if count == 0 {
					firstLine = n
				}
				count++
			}
		}
		f.Close()
		if count > 0 {
			out = append(out, Finding{File: path, Line: firstLine, Kind: fmt.Sprintf("%s typed on a command line", plural(count, "key", "keys")), Fix: "delete those lines from the file, and treat the keys as exposed"})
		}
	}
	return out
}

func cmdScan(args []string) int {
	asJSON, history := false, false
	var dirs []string
	for _, a := range args {
		switch {
		case a == "--json":
			asJSON = true
		case a == "--history":
			history = true
		case strings.HasPrefix(a, "-"):
			return usageError("keymoat scan [--history] [--json] [DIR...]")
		default:
			dirs = append(dirs, a)
		}
	}
	home := homeDir()
	if len(dirs) == 0 {
		cwd := mustGetwd()
		if root := projectRoot(cwd, home); root != "" {
			cwd = root
		}
		dirs = []string{cwd}
	}
	var findings []Finding
	for _, d := range dirs {
		findings = append(findings, scanTree(absFrom(mustGetwd(), d), 6)...)
	}
	if _, key := solanaConfig(home); isSolanaKeypairFile(expandHome(key)) {
		findings = append(findings, Finding{File: expandHome(key), Kind: "Solana keypair (the command-line tool's default wallet)", Fix: "keymoat keys move " + expandHome(key)})
	}
	if history {
		findings = append(findings, scanHistory(home)...)
	}
	if asJSON {
		if findings == nil {
			findings = []Finding{}
		}
		data, _ := json.MarshalIndent(findings, "", "  ")
		fmt.Println(string(data))
	} else if len(findings) == 0 {
		fmt.Println("No wallet key in plain text found.")
	} else {
		fmt.Printf("%s in plain text:\n\n", plural(len(findings), "wallet key", "wallet keys"))
		for _, f := range findings {
			where := f.File
			if f.Line > 0 {
				where = fmt.Sprintf("%s:%d", f.File, f.Line)
			}
			what := f.Kind
			if f.Name != "" {
				what = f.Name + ": " + f.Kind
			}
			fmt.Printf("  %s\n    %s\n    fix: %s\n\n", where, what, f.Fix)
		}
		fmt.Println("Any program you run can read these, including a dependency's install script and an editor extension.")
	}
	if len(findings) > 0 {
		return 1
	}
	return 0
}
