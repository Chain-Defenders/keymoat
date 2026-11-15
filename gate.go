package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"
)

// Before a command that signs on a live network, keymoat says in plain words what is about to happen and waits for
// the user. A local node needs no question. Declining ends the command with an error code, so a script that called
// it stops too.

type chain struct {
	name string
	test bool
}

var chains = map[uint64]chain{
	1: {"Ethereum", false}, 10: {"OP Mainnet", false}, 56: {"BNB Chain", false}, 100: {"Gnosis", false}, 130: {"Unichain", false}, 137: {"Polygon", false}, 146: {"Sonic", false},
	324: {"zkSync Era", false}, 999: {"HyperEVM", false}, 5000: {"Mantle", false}, 8453: {"Base", false}, 42161: {"Arbitrum One", false}, 42220: {"Celo", false}, 43114: {"Avalanche", false},
	59144: {"Linea", false}, 80094: {"Berachain", false}, 81457: {"Blast", false}, 534352: {"Scroll", false},
	97: {"BNB Chain testnet", true}, 17000: {"Holesky", true}, 43113: {"Avalanche Fuji", true}, 80002: {"Polygon Amoy", true}, 84532: {"Base Sepolia", true}, 421614: {"Arbitrum Sepolia", true},
	560048: {"Hoodi", true}, 11155111: {"Sepolia", true}, 11155420: {"OP Sepolia", true},
	// The ids local nodes use. Reached through a host that is not local, they prove nothing: an endpoint can claim
	// any id, so this is asked about like a live network.
	1337: {"a chain with a local node's id", false}, 31337: {"a chain with a local node's id", false},
}

// chainID asks an endpoint which chain it is. It is the endpoint the command itself is about to use.
func chainID(rpcURL string) (uint64, bool) {
	if !strings.HasPrefix(rpcURL, "http://") && !strings.HasPrefix(rpcURL, "https://") {
		return 0, false
	}
	client := &http.Client{Timeout: 4 * time.Second}
	resp, err := client.Post(rpcURL, "application/json", bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":1,"method":"eth_chainId","params":[]}`)))
	if err != nil {
		return 0, false
	}
	defer resp.Body.Close()
	var out struct {
		Result string `json:"result"`
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if json.Unmarshal(body, &out) != nil {
		return 0, false
	}
	id, err := strconv.ParseUint(strings.TrimPrefix(out.Result, "0x"), 16, 64)
	return id, err == nil
}

// describe fills in the network line and says whether this is a test network.
func (g *Gate) describe(lookup func(string) (uint64, bool)) (network string, test bool) {
	if g.Solana {
		return g.Network, g.Network == "devnet" || g.Network == "testnet"
	}
	if g.RPCURL != "" {
		if id, ok := lookup(g.RPCURL); ok {
			if c, known := chains[id]; known {
				return fmt.Sprintf("%s (chain %d) through %s", c.name, id, g.Network), c.test
			}
			return fmt.Sprintf("chain %d through %s", id, g.Network), false
		}
		return g.Network + " (the endpoint did not say which chain it is)", false
	}
	return g.Network, false
}

func interactive() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stderr.Fd()))
}

// confirmGate shows the gate and returns whether to go on.
func confirmGate(g *Gate, tool string) bool {
	network, test := g.describe(chainID)
	w := os.Stderr
	fmt.Fprintf(w, "\nkeymoat: this command signs with a real key.\n\n")
	fmt.Fprintf(w, "  What       %s\n", g.Action)
	fmt.Fprintf(w, "  Network    %s\n", network)
	fmt.Fprintf(w, "  Signs with %s\n", g.Key)
	if g.Target != "" {
		fmt.Fprintf(w, "  Target     %s\n", g.Target)
	}
	for _, warn := range g.Warn {
		fmt.Fprintf(w, "  Note       %s\n", warn)
	}
	fmt.Fprintln(w)
	if os.Getenv("KEYMOAT_ASSUME_YES") == "1" {
		fmt.Fprintln(w, "keymoat: KEYMOAT_ASSUME_YES is set, going on without asking.")
		return true
	}
	if !interactive() {
		fmt.Fprintln(w, "keymoat: there is no terminal to confirm on, so the command was not run. In a pipeline, set KEYMOAT_ASSUME_YES=1.")
		return false
	}
	if test {
		answer, _ := readLine("Continue? [y/N] ")
		return strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes")
	}
	answer, _ := readLine(`Type "yes" to continue: `)
	return strings.EqualFold(answer, "yes")
}
