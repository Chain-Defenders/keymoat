package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"golang.org/x/crypto/scrypt"
	"golang.org/x/crypto/sha3"
)

// Two encrypted formats. An EVM key goes into the standard keystore file (Web3 Secret Storage, version 3) that
// Foundry reads with --account, so nothing about keymoat is needed to use it afterwards. A Solana keypair has no
// standard encrypted form, so it goes into keymoat's own vault and is handed to the tool for one command at a time.

const (
	scryptN = 1 << 17
	scryptR = 8
	scryptP = 1
)

var errWrongPassword = errors.New("wrong password")

type evmKeystore struct {
	Crypto struct {
		Cipher       string `json:"cipher"`
		CipherParams struct {
			IV string `json:"iv"`
		} `json:"cipherparams"`
		CipherText string `json:"ciphertext"`
		KDF        string `json:"kdf"`
		KDFParams  struct {
			DKLen int    `json:"dklen"`
			N     int    `json:"n"`
			P     int    `json:"p"`
			R     int    `json:"r"`
			Salt  string `json:"salt"`
		} `json:"kdfparams"`
		MAC string `json:"mac"`
	} `json:"crypto"`
	ID      string `json:"id"`
	Version int    `json:"version"`
}

// saneScrypt keeps a damaged or hostile file from asking for more memory than any real keystore needs.
func saneScrypt(n, r, p int) bool {
	return n >= 2 && n <= 1<<22 && n&(n-1) == 0 && r >= 1 && r <= 16 && p >= 1 && p <= 4
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		fatal("no random numbers: %v", err)
	}
	return b
}

func keccak256(parts ...[]byte) []byte {
	h := sha3.NewLegacyKeccak256()
	for _, p := range parts {
		h.Write(p)
	}
	return h.Sum(nil)
}

// parseEVMKey reads a private key as it is written in an env file.
func parseEVMKey(s string) ([]byte, error) {
	s = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(s), "0x"), "0X")
	key, err := hex.DecodeString(s)
	if err != nil || len(key) != 32 {
		return nil, errors.New("not a 32-byte private key")
	}
	if bytes.Equal(key, make([]byte, 32)) {
		return nil, errors.New("the key is all zeros")
	}
	return key, nil
}

// encryptEVMKey writes a private key as a version 3 keystore file.
func encryptEVMKey(key []byte, password string) ([]byte, error) {
	salt, iv := randomBytes(32), randomBytes(16)
	derived, err := scrypt.Key([]byte(password), salt, scryptN, scryptR, scryptP, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(derived[:16])
	if err != nil {
		return nil, err
	}
	ct := make([]byte, len(key))
	cipher.NewCTR(block, iv).XORKeyStream(ct, key)

	var ks evmKeystore
	ks.Crypto.Cipher = "aes-128-ctr"
	ks.Crypto.CipherParams.IV = hex.EncodeToString(iv)
	ks.Crypto.CipherText = hex.EncodeToString(ct)
	ks.Crypto.KDF = "scrypt"
	ks.Crypto.KDFParams.DKLen, ks.Crypto.KDFParams.N, ks.Crypto.KDFParams.R, ks.Crypto.KDFParams.P = 32, scryptN, scryptR, scryptP
	ks.Crypto.KDFParams.Salt = hex.EncodeToString(salt)
	ks.Crypto.MAC = hex.EncodeToString(keccak256(derived[16:32], ct))
	id := randomBytes(16)
	id[6], id[8] = id[6]&0x0f|0x40, id[8]&0x3f|0x80
	ks.ID = fmt.Sprintf("%x-%x-%x-%x-%x", id[0:4], id[4:6], id[6:8], id[8:10], id[10:16])
	ks.Version = 3
	return json.Marshal(ks)
}

func decryptEVMKey(data []byte, password string) ([]byte, error) {
	var ks evmKeystore
	if err := json.Unmarshal(data, &ks); err != nil {
		return nil, err
	}
	if ks.Version != 3 || ks.Crypto.KDF != "scrypt" || ks.Crypto.Cipher != "aes-128-ctr" {
		return nil, errors.New("not a keystore KeyMoat can read")
	}
	salt, err1 := hex.DecodeString(ks.Crypto.KDFParams.Salt)
	iv, err2 := hex.DecodeString(ks.Crypto.CipherParams.IV)
	ct, err3 := hex.DecodeString(ks.Crypto.CipherText)
	mac, err4 := hex.DecodeString(ks.Crypto.MAC)
	if err := errors.Join(err1, err2, err3, err4); err != nil {
		return nil, err
	}
	if !saneScrypt(ks.Crypto.KDFParams.N, ks.Crypto.KDFParams.R, ks.Crypto.KDFParams.P) || ks.Crypto.KDFParams.DKLen != 32 {
		return nil, errors.New("not a keystore KeyMoat can read")
	}
	derived, err := scrypt.Key([]byte(password), salt, ks.Crypto.KDFParams.N, ks.Crypto.KDFParams.R, ks.Crypto.KDFParams.P, ks.Crypto.KDFParams.DKLen)
	if err != nil || len(derived) < 32 {
		return nil, errors.New("not a keystore KeyMoat can read")
	}
	if subtle.ConstantTimeCompare(keccak256(derived[16:32], ct), mac) != 1 {
		return nil, errWrongPassword
	}
	block, err := aes.NewCipher(derived[:16])
	if err != nil {
		return nil, err
	}
	key := make([]byte, len(ct))
	cipher.NewCTR(block, iv).XORKeyStream(key, ct)
	return key, nil
}

// ---- Solana keypairs

// parseSolanaKeypair reads a keypair file (a JSON array of 64 bytes) and checks that its two halves belong together.
func parseSolanaKeypair(data []byte) (ed25519.PrivateKey, error) {
	var nums []int
	if err := json.Unmarshal(bytes.TrimSpace(data), &nums); err != nil || len(nums) != 64 {
		return nil, errors.New("not a Solana keypair file (a list of 64 numbers)")
	}
	key := make([]byte, 64)
	for i, n := range nums {
		if n < 0 || n > 255 {
			return nil, errors.New("not a Solana keypair file (a list of 64 numbers)")
		}
		key[i] = byte(n)
	}
	if !bytes.Equal(ed25519.NewKeyFromSeed(key[:32])[32:], key[32:]) {
		return nil, errors.New("the keypair's public half does not match its secret half")
	}
	return ed25519.PrivateKey(key), nil
}

func solanaKeypairJSON(key ed25519.PrivateKey) []byte {
	parts := make([]string, len(key))
	for i, b := range key {
		parts[i] = fmt.Sprint(b)
	}
	return []byte("[" + strings.Join(parts, ",") + "]")
}

const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

func base58(b []byte) string {
	n := new(big.Int).SetBytes(b)
	radix, mod := big.NewInt(58), new(big.Int)
	var out []byte
	for n.Sign() > 0 {
		n.DivMod(n, radix, mod)
		out = append(out, base58Alphabet[mod.Int64()])
	}
	for _, x := range b {
		if x != 0 {
			break
		}
		out = append(out, '1')
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return string(out)
}

func solanaAddress(key ed25519.PrivateKey) string { return base58(key[32:]) }

type vaultEntry struct {
	Version    int    `json:"version"`
	Kind       string `json:"kind"` // "solana-keypair"
	Address    string `json:"address"`
	KDF        string `json:"kdf"`
	N          int    `json:"n"`
	R          int    `json:"r"`
	P          int    `json:"p"`
	Salt       string `json:"salt"`
	Nonce      string `json:"nonce"`
	CipherText string `json:"ciphertext"`
}

func vaultAEAD(password string, salt []byte, n, r, p int) (cipher.AEAD, error) {
	key, err := scrypt.Key([]byte(password), salt, n, r, p, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// encryptSolanaKeypair seals a keypair. The address is kept readable so the entry can be listed without a password,
// and it is bound to the sealed key: an entry whose address was edited does not open.
func encryptSolanaKeypair(key ed25519.PrivateKey, password string) ([]byte, error) {
	e := vaultEntry{Version: 1, Kind: "solana-keypair", Address: solanaAddress(key), KDF: "scrypt", N: scryptN, R: scryptR, P: scryptP}
	salt := randomBytes(32)
	aead, err := vaultAEAD(password, salt, e.N, e.R, e.P)
	if err != nil {
		return nil, err
	}
	nonce := randomBytes(aead.NonceSize())
	e.Salt = base64.StdEncoding.EncodeToString(salt)
	e.Nonce = base64.StdEncoding.EncodeToString(nonce)
	e.CipherText = base64.StdEncoding.EncodeToString(aead.Seal(nil, nonce, key, []byte("keymoat-vault-v1:"+e.Address)))
	return json.MarshalIndent(e, "", "  ")
}

func decryptSolanaKeypair(data []byte, password string) (ed25519.PrivateKey, error) {
	var e vaultEntry
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, err
	}
	if e.Version != 1 || e.Kind != "solana-keypair" || e.KDF != "scrypt" {
		return nil, errors.New("not a vault entry KeyMoat can read")
	}
	salt, err1 := base64.StdEncoding.DecodeString(e.Salt)
	nonce, err2 := base64.StdEncoding.DecodeString(e.Nonce)
	ct, err3 := base64.StdEncoding.DecodeString(e.CipherText)
	if err := errors.Join(err1, err2, err3); err != nil {
		return nil, err
	}
	if !saneScrypt(e.N, e.R, e.P) {
		return nil, errors.New("not a vault entry KeyMoat can read")
	}
	aead, err := vaultAEAD(password, salt, e.N, e.R, e.P)
	if err != nil || len(nonce) != aead.NonceSize() {
		return nil, errors.New("not a vault entry KeyMoat can read")
	}
	key, err := aead.Open(nil, nonce, ct, []byte("keymoat-vault-v1:"+e.Address))
	if err != nil {
		return nil, errWrongPassword
	}
	if len(key) != 64 || solanaAddress(ed25519.PrivateKey(key)) != e.Address {
		return nil, errors.New("the vault entry is damaged")
	}
	return ed25519.PrivateKey(key), nil
}
