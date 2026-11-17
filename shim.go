package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// After `keymoat install`, typing `forge` runs keymoat, which runs forge. Each tool gets a link named after it in
// one folder that goes first on the PATH; keymoat sees the name it was called by and acts as that tool.

func toolNames() []string {
	var names []string
	for t := range tools {
		names = append(names, t)
	}
	sort.Strings(names)
	return names
}

const rcMarker = "# keymoat: its tool links go first on the PATH"

func rcLine() string {
	return fmt.Sprintf(`export PATH="%s:$PATH"  %s`, shimDir(), rcMarker)
}

func rcFiles() []string {
	home := homeDir()
	shell := filepath.Base(os.Getenv("SHELL"))
	switch shell {
	case "zsh":
		return []string{filepath.Join(home, ".zshrc")}
	case "bash":
		return []string{filepath.Join(home, ".bashrc"), filepath.Join(home, ".bash_profile")}
	}
	return nil
}

func cmdInstall(args []string) int {
	editRC := false
	var only []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--shell":
			editRC = true
		case "--tools":
			if i+1 >= len(args) {
				return usageError("keymoat install [--shell] [--tools a,b,c]")
			}
			only = strings.Split(args[i+1], ",")
			i++
		default:
			return usageError("keymoat install [--shell] [--tools a,b,c]")
		}
	}
	self, err := os.Executable()
	if err != nil {
		fatal("%v", err)
	}
	self = realPath(self)
	if err := os.MkdirAll(shimDir(), 0o755); err != nil {
		fatal("%v", err)
	}
	names := toolNames()
	if only != nil {
		names = only
	}
	var linked, missing []string
	for _, t := range names {
		t = strings.TrimSpace(t)
		if t == "" || strings.ContainsAny(t, "/ ") {
			continue
		}
		link := filepath.Join(shimDir(), t)
		os.Remove(link)
		if _, err := findReal(t); err != nil {
			missing = append(missing, t)
			continue
		}
		if err := os.Symlink(self, link); err != nil {
			fatal("%v", err)
		}
		linked = append(linked, t)
	}
	say("standing in front of: %s", strings.Join(linked, ", "))
	if len(missing) > 0 {
		say("not installed, so skipped: %s (run `keymoat install` again after installing one)", strings.Join(missing, ", "))
	}
	if pathHasShimsFirst() {
		say("your PATH already has %s first. Done.", shimDir())
		return 0
	}
	if editRC {
		files := rcFiles()
		if len(files) == 0 {
			say("this shell is not zsh or bash; add this to its start-up file yourself:\n\n  %s\n", rcLine())
			return 0
		}
		for _, f := range files {
			data, err := os.ReadFile(f)
			if err != nil && !os.IsNotExist(err) {
				continue
			}
			if strings.Contains(string(data), rcMarker) {
				continue
			}
			out, err := os.OpenFile(f, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
			if err != nil {
				say("cannot write %s: %v", f, err)
				continue
			}
			fmt.Fprintf(out, "\n%s\n", rcLine())
			out.Close()
			say("added a line to the end of %s", f)
		}
		say("open a new terminal, then run `keymoat doctor`")
		return 0
	}
	say("last step: put this at the END of your shell's start-up file (after nvm, cargo and foundry set the PATH), or run `keymoat install --shell` to have it added:\n\n  %s\n", rcLine())
	return 0
}

func pathHasShimsFirst() bool {
	shims := realPath(shimDir())
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir != "" {
			return realPath(dir) == shims
		}
	}
	return false
}

func cmdUninstall(args []string) int {
	if len(args) > 0 {
		return usageError("keymoat uninstall")
	}
	entries, _ := os.ReadDir(shimDir())
	for _, e := range entries {
		os.Remove(filepath.Join(shimDir(), e.Name()))
	}
	os.Remove(shimDir())
	for _, f := range rcFiles() {
		data, err := os.ReadFile(f)
		if err != nil || !strings.Contains(string(data), rcMarker) {
			continue
		}
		var keep []string
		for _, line := range strings.Split(string(data), "\n") {
			if !strings.Contains(line, rcMarker) {
				keep = append(keep, line)
				continue
			}
			// And the empty line that was put in front of it.
			if n := len(keep); n > 0 && keep[n-1] == "" {
				keep = keep[:n-1]
			}
		}
		if err := os.WriteFile(f, []byte(strings.Join(keep, "\n")), 0o644); err == nil {
			say("removed keymoat's line from %s", f)
		}
	}
	say("the tool links are gone. Your settings, the log and the vault are still in %s.", stateDir())
	return 0
}

// DoctorReport is what doctor found, also printed as JSON for editors (`keymoat doctor --json`).
type DoctorReport struct {
	Version   string        `json:"version"`
	Sandbox   DoctorCheck   `json:"sandbox"`
	Tools     []DoctorTool  `json:"tools"`
	PathFirst bool          `json:"pathFirst"`
	ShimDir   string        `json:"shimDir"`
	Project   string        `json:"project,omitempty"`
	PlainKeys []DoctorPlain `json:"plainKeys"`
}

type DoctorCheck struct {
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

type DoctorTool struct {
	Name    string `json:"name"`
	Through bool   `json:"through"` // the name on the PATH is keymoat
	Found   string `json:"found,omitempty"`
	Real    string `json:"real"`
}

type DoctorPlain struct {
	File string `json:"file"`
	Line int    `json:"line,omitempty"`
	Name string `json:"name,omitempty"`
	Kind string `json:"kind"`
	Fix  string `json:"fix"`
}

func doctorReport() DoctorReport {
	r := DoctorReport{Version: version, PathFirst: pathHasShimsFirst(), ShimDir: shimDir(), Tools: []DoctorTool{}, PlainKeys: []DoctorPlain{}}
	if err := sandboxAvailable(); err != nil {
		r.Sandbox = DoctorCheck{Detail: err.Error()}
	} else if out, err := sandboxSelfTest(); err != nil {
		r.Sandbox = DoctorCheck{Detail: fmt.Sprintf("the sandbox did not hold: %v %s", err, out)}
	} else {
		r.Sandbox = DoctorCheck{OK: true, Detail: "works: a sandboxed command could not read a protected file"}
	}

	self, _ := os.Executable()
	self = realPath(self)
	for _, t := range toolNames() {
		real, err := findReal(t)
		if err != nil {
			continue
		}
		tool := DoctorTool{Name: t, Real: real}
		if found, err := exec.LookPath(t); err == nil {
			tool.Found = found
			tool.Through = realPath(found) == self
		}
		r.Tools = append(r.Tools, tool)
	}

	home := homeDir()
	if _, key := solanaConfig(home); isSolanaKeypairFile(expandHome(key)) {
		r.PlainKeys = append(r.PlainKeys, DoctorPlain{File: key, Kind: "Solana keypair", Fix: "keymoat keys move " + key})
	}
	if cwd, err := os.Getwd(); err == nil {
		if root := projectRoot(cwd, home); root != "" {
			r.Project = root
			for _, f := range projectEnvFiles(root) {
				for _, s := range f.Secrets {
					r.PlainKeys = append(r.PlainKeys, DoctorPlain{File: f.Path, Line: s.Line, Name: s.Name, Kind: secretKind(s.Name, s.Value), Fix: "keymoat keys move"})
				}
			}
		}
	}
	return r
}

func (r DoctorReport) problems() int {
	n := len(r.PlainKeys)
	if !r.Sandbox.OK {
		n++
	}
	for _, t := range r.Tools {
		if !t.Through {
			n++
		}
	}
	return n
}

// cmdDoctor checks that keymoat is where it should be, and says in one line each what is wrong.
func cmdDoctor(args []string) int {
	asJSON := false
	for _, a := range args {
		if a != "--json" {
			return usageError("keymoat doctor [--json]")
		}
		asJSON = true
	}
	r := doctorReport()
	code := 0
	if r.problems() > 0 {
		code = 1
	}
	if asJSON {
		data, _ := json.MarshalIndent(r, "", "  ")
		fmt.Println(string(data))
		return code
	}

	bad := func(format string, a ...any) { fmt.Printf("  ✗ "+format+"\n", a...) }
	good := func(format string, a ...any) { fmt.Printf("  ✓ "+format+"\n", a...) }

	fmt.Println("Sandbox")
	if r.Sandbox.OK {
		good("%s", r.Sandbox.Detail)
	} else {
		bad("%s", r.Sandbox.Detail)
	}

	fmt.Println("Tools")
	for _, t := range r.Tools {
		switch {
		case t.Found == "":
			bad("%s: not found on the PATH", t.Name)
		case t.Through:
			good("%-10s goes through keymoat  (the tool itself: %s)", t.Name, t.Real)
		default:
			bad("%-10s runs directly from %s, not through keymoat", t.Name, t.Found)
		}
	}
	if !r.PathFirst {
		fmt.Printf("\n  %s is not first on the PATH in this terminal. Put this at the end of your shell's start-up file and open a new terminal:\n\n    %s\n\n", r.ShimDir, rcLine())
		fmt.Println("  With nvm, `nvm use` puts Node's folder first again: open a new terminal afterwards.")
	}

	fmt.Println("Keys in plain text")
	for _, p := range r.PlainKeys {
		if p.Line == 0 {
			bad("%s is a %s in plain text: `%s`", p.File, p.Kind, p.Fix)
			continue
		}
		bad("%s line %d: %s (%s): `%s`", relTo(r.Project, p.File), p.Line, p.Name, p.Kind, p.Fix)
	}
	if len(r.PlainKeys) == 0 {
		good("none in this project or the Solana settings (`keymoat scan DIR` looks further)")
	}
	return code
}

// sandboxSelfTest runs a small command in the sandbox that must fail: reading a protected file.
func sandboxSelfTest() (string, error) {
	home := realPath(homeDir())
	dir, err := os.MkdirTemp("", "keymoat-check-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	secret := filepath.Join(realPath(dir), "secret")
	if err := os.WriteFile(secret, []byte("secret"), 0o600); err != nil {
		return "", err
	}
	spec := Spec{Home: home, DenyRead: []string{secret}, HideEnvFiles: true, NetOpen: true}
	cmd, cleanup, err := sandboxCommand(spec, "/bin/cat", []string{secret})
	if err != nil {
		return "", err
	}
	defer cleanup()
	if out, err := cmd.CombinedOutput(); err == nil {
		return string(out), fmt.Errorf("a protected file was readable")
	}
	return "", nil
}
