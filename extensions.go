package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// An editor extension runs with everything the editor can reach, outside any sandbox, and fake web3 extensions
// have been used to empty wallets. `keymoat extensions` lists the installed ones that deal with web3 and are worth
// a second look. It works from what is on the disk and makes no claim that an extension is malicious: it says why
// it is worth checking.

var editorFolders = map[string]string{
	"VS Code": ".vscode/extensions", "VS Code Insiders": ".vscode-insiders/extensions", "VSCodium": ".vscode-oss/extensions", "Cursor": ".cursor/extensions",
	"Windsurf": ".windsurf/extensions", "Antigravity": ".antigravity/extensions", "Trae": ".trae/extensions", "Kiro": ".kiro/extensions", "VS Code Server": ".vscode-server/extensions",
}

var web3Words = regexp.MustCompile(`(?i)\b(solidity|ethereum|evm|web3|hardhat|foundry|forge|vyper|solana|anchor|blockchain|smart[ -]?contracts?|crypto|wallet|defi|cairo|starknet|sui|aptos|move lang|yul|huff|noir)\b`)

// Extensions many web3 developers install, by publisher and name. A different publisher with a near-identical name
// is how the fakes have looked.
var wellKnown = []string{
	"juanblanco.solidity", "nomicfoundation.hardhat-solidity", "tintinweb.solidity-visual-auditor", "tintinweb.solidity-metrics", "tintinweb.vscode-solidity-flattener",
	"tintinweb.vscode-ethover", "tintinweb.vscode-vyper", "chaindefenders.forgedefender", "cyfrin.aderyn", "certora.evmspec-lsp", "trailofbits.weaudit", "trailofbits.slither-vscode",
	"ackeeblockchain.tools-for-solidity", "olympixai.olympix", "rust-lang.rust-analyzer", "mysten.move", "starkware.cairo1", "runtimeverification.simbolik", "noir-lang.vscode-noir",
}

type extension struct {
	Editor, ID, Publisher, Name, Display, Dir string
	Installed                                 time.Time
	FromFile                                  bool
	Web3                                      bool
}

type extensionReport struct {
	Editor  string   `json:"editor"`
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Folder  string   `json:"folder"`
	Reasons []string `json:"reasons"`
}

func readExtensions(home string) []extension {
	var out []extension
	for editor, rel := range editorFolders {
		dir := filepath.Join(home, rel)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		// The editor's own record says when each was installed and whether it came from the store.
		type record struct {
			RelativeLocation string `json:"relativeLocation"`
			Metadata         struct {
				InstalledTimestamp int64  `json:"installedTimestamp"`
				Source             string `json:"source"`
			} `json:"metadata"`
		}
		records := map[string]record{}
		if data, err := os.ReadFile(filepath.Join(dir, "extensions.json")); err == nil {
			var list []record
			if json.Unmarshal(data, &list) == nil {
				for _, r := range list {
					records[r.RelativeLocation] = r
				}
			}
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, e.Name(), "package.json"))
			if err != nil {
				continue
			}
			var pkg struct {
				Name        string   `json:"name"`
				Publisher   string   `json:"publisher"`
				DisplayName string   `json:"displayName"`
				Description string   `json:"description"`
				Keywords    []string `json:"keywords"`
			}
			if json.Unmarshal(data, &pkg) != nil || pkg.Name == "" {
				continue
			}
			x := extension{Editor: editor, Publisher: pkg.Publisher, Name: pkg.Name, Display: pkg.DisplayName, Dir: filepath.Join(dir, e.Name())}
			x.ID = strings.ToLower(pkg.Publisher + "." + pkg.Name)
			if x.Display == "" || strings.HasPrefix(x.Display, "%") {
				x.Display = pkg.Name
			}
			x.Web3 = web3Words.MatchString(strings.Join(append([]string{pkg.Name, pkg.DisplayName, pkg.Description}, pkg.Keywords...), " "))
			if r, ok := records[e.Name()]; ok {
				if r.Metadata.InstalledTimestamp > 0 {
					x.Installed = time.UnixMilli(r.Metadata.InstalledTimestamp)
				}
				x.FromFile = r.Metadata.Source == "vsix"
			}
			out = append(out, x)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Editor+out[i].ID < out[j].Editor+out[j].ID })
	return out
}

func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}

// lookalikeOf returns the well-known extension an id imitates, or "".
func lookalikeOf(id string) string {
	pub, name, _ := strings.Cut(id, ".")
	for _, known := range wellKnown {
		if id == known {
			return ""
		}
	}
	for _, known := range wellKnown {
		kpub, kname, _ := strings.Cut(known, ".")
		// The same name under another publisher (for a name that is specific enough), or an id a letter or two away.
		if (name == kname && pub != kpub && len(kname) > 6) || editDistance(id, known) <= 2 || (editDistance(pub, kpub) <= 2 && pub != kpub && editDistance(name, kname) <= 2) {
			return known
		}
	}
	return ""
}

func knownPublisher(pub string) bool {
	pub = strings.ToLower(pub)
	for _, known := range wellKnown {
		if kp, _, _ := strings.Cut(known, "."); kp == pub {
			return true
		}
	}
	return false
}

// reviewExtensions picks the extensions worth a look, with the reasons.
func reviewExtensions(list []extension, now time.Time) []extensionReport {
	var out []extensionReport
	for _, x := range list {
		var reasons []string
		if like := lookalikeOf(x.ID); like != "" {
			reasons = append(reasons, "its name is very close to the well-known "+like+", from a different publisher")
		}
		if x.Web3 {
			if !knownPublisher(x.Publisher) {
				reasons = append(reasons, "a web3 extension from a publisher keymoat does not know: look the publisher up")
			}
			if x.FromFile {
				reasons = append(reasons, "installed from a file, not from the store")
			}
			if !x.Installed.IsZero() && now.Sub(x.Installed) < 14*24*time.Hour && !knownPublisher(x.Publisher) {
				reasons = append(reasons, "installed in the last two weeks")
			}
		}
		if len(reasons) > 0 {
			out = append(out, extensionReport{Editor: x.Editor, ID: x.ID, Name: x.Display, Folder: x.Dir, Reasons: reasons})
		}
	}
	return out
}

func cmdExtensions(args []string) int {
	asJSON := false
	for _, a := range args {
		if a != "--json" {
			return usageError("keymoat extensions [--json]")
		}
		asJSON = true
	}
	list := readExtensions(homeDir())
	report := reviewExtensions(list, time.Now())
	if asJSON {
		if report == nil {
			report = []extensionReport{}
		}
		data, _ := json.MarshalIndent(report, "", "  ")
		fmt.Println(string(data))
		return 0
	}
	web3 := 0
	for _, x := range list {
		if x.Web3 {
			web3++
		}
	}
	fmt.Printf("%s installed, %d to do with web3.\n", plural(len(list), "editor extension", "editor extensions"), web3)
	if len(report) == 0 {
		fmt.Println("None of them stands out.")
		return 0
	}
	fmt.Printf("\nWorth a second look (%d):\n\n", len(report))
	for _, r := range report {
		fmt.Printf("  %s  (%s, %s)\n", r.ID, r.Name, r.Editor)
		for _, reason := range r.Reasons {
			fmt.Printf("    - %s\n", reason)
		}
		fmt.Printf("    %s\n\n", r.Folder)
	}
	fmt.Println("This is a list of things to check, not a verdict. An extension runs outside keymoat's sandbox and can read any file you can; keys in an encrypted keystore are what it cannot use.")
	return 0
}
