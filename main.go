// keymoat keeps wallet keys out of reach of the code a web3 developer runs every day: dependencies, install
// scripts, build scripts and tests. Tools that run a project's code are run in a sandbox that cannot read keys and
// cannot connect anywhere but the hosts the work needs; a command that signs is shown to the user first.
package main

import (
	"fmt"
	"os"
	"path/filepath"
)

var version = "0.1.0"

const usage = `keymoat keeps wallet keys away from the code you build, test and install.

Set up
  keymoat install [--shell]      stand in front of forge, cast, anchor, solana, cargo, npm, pnpm, yarn and bun
  keymoat doctor                 check that it is working, and what is still in plain text
  keymoat uninstall              remove the tool links

Every day (nothing to type: the tools go through keymoat once installed)
  keymoat run [--keys] [--open-net] -- COMMAND ...     run any command in the sandbox
  KEYMOAT_KEYS=1 COMMAND ...     let this one command see the project's keys
  KEYMOAT=off COMMAND ...        run this one command without keymoat

Keys
  keymoat scan [--history] [DIR...]     find wallet keys kept in plain text
  keymoat keys move [FILE]              encrypt a key from an env file or a Solana keypair file
  keymoat keys list
  keymoat keys export NAME --to FILE    write a Solana keypair back out in plain text

Network
  keymoat allow [--global] HOST...      let commands connect to a host

Also
  keymoat extensions             editor extensions worth a second look
  keymoat log [-n COUNT]         what was blocked, asked and allowed
  keymoat version
`

func main() {
	// Called by a tool's name: act as that tool.
	if name := filepath.Base(os.Args[0]); name != "keymoat" {
		// A known tool, or one the user asked for a link for (`keymoat install --tools node`).
		_, known := tools[name]
		if _, err := os.Lstat(filepath.Join(shimDir(), name)); known || err == nil {
			os.Exit(runTool(name, os.Args[1:], runOptions{}))
		}
	}
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(0)
	}
	args := os.Args[2:]
	code := 0
	switch os.Args[1] {
	case "run":
		code = cmdRun(args)
	case "install":
		code = cmdInstall(args)
	case "uninstall":
		code = cmdUninstall(args)
	case "doctor":
		code = cmdDoctor(args)
	case "scan":
		code = cmdScan(args)
	case "keys":
		code = cmdKeys(args)
	case "allow":
		code = cmdAllow(args)
	case "extensions":
		code = cmdExtensions(args)
	case "log":
		code = cmdLog(args)
	case "__bridge":
		code = bridgeMain(args)
	case "version", "--version", "-V":
		fmt.Println("keymoat " + version)
	case "help", "--help", "-h":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "keymoat: unknown command %q\n\n%s", os.Args[1], usage)
		code = 2
	}
	os.Exit(code)
}
