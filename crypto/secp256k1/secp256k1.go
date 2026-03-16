package secp256k1

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"io"
	"math/big"

	secp256k1 "github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/ecdsa"
	"golang.org/x/crypto/ripemd160" //nolint: staticcheck // necessary for Bitcoin address format

	"github.com/cometbft/cometbft/crypto"
	cmtjson "github.com/cometbft/cometbft/libs/json"
)

// -------------------------------------
const (
	PrivKeyName = "tendermint/PrivKeySecp256k1"
	PubKeyName  = "tendermint/PubKeySecp256k1"

	KeyType     = "secp256k1"
	PrivKeySize = 32
)

func init() {
	cmtjson.RegisterType(PubKey{}, PubKeyName)
	cmtjson.RegisterType(PrivKey{}, PrivKeyName)
}

var _ crypto.PrivKey = PrivKey{}

// PrivKey implements PrivKey.
type PrivKey []byte

// Bytes marshalls the private key using amino encoding.
func (privKey PrivKey) Bytes() []byte {
	return []byte(privKey)
}

// PubKey performs the point-scalar multiplication from the privKey on the
// generator point to get the pubkey.
func (privKey PrivKey) PubKey() crypto.PubKey {
	_, pubkeyObject := secp256k1.PrivKeyFromBytes(privKey)

	pk := pubkeyObject.SerializeCompressed()

	return PubKey(pk)
}

// Equals - you probably don't need to use this.
// Runs in constant time based on length of the keys.
func (privKey PrivKey) Equals(other crypto.PrivKey) bool {
	if otherSecp, ok := other.(PrivKey); ok {
		return subtle.ConstantTimeCompare(privKey[:], otherSecp[:]) == 1
	}
	return false
}

func (privKey PrivKey) Type() string {
	return KeyType
}

// GenPrivKey generates a new ECDSA private key on curve secp256k1 private key.
// It uses OS randomness to generate the private key.
func GenPrivKey() PrivKey {
	return genPrivKey(crypto.CReader())
}

// genPrivKey generates a new secp256k1 private key using the provided reader.
func genPrivKey(rand io.Reader) PrivKey {
	var privKeyBytes [PrivKeySize]byte
	d := new(big.Int)
	curveOrder := new(big.Int).Set(secp256k1.S256().N)

	for {
		privKeyBytes = [PrivKeySize]byte{}
		_, err := io.ReadFull(rand, privKeyBytes[:])
		if err != nil {
			panic(fmt.Errorf("error reading randomness: %w", err))
		}

		d.SetBytes(privKeyBytes[:])
		// break if we found a valid point (i.e., > 0 and < N)
		if d.Cmp(big.NewInt(0)) > 0 && d.Cmp(curveOrder) < 0 {
			break
		}
	}

	return PrivKey(privKeyBytes[:])
}

// GenPrivKeySecp256k1 hashes the secret with SHA256, and uses
// that 32 byte output to create the private key.
// NOTE: secret should be the output of a KDF like bcrypt,
// if it's derived from user input.
func GenPrivKeySecp256k1(secret []byte) PrivKey {
	seed := crypto.Sha256(secret) // Use SHA256 to get 32 bytes
	return genPrivKey(bytes.NewReader(seed))
}

// Sign creates an ECDSA signature on the secp256k1 curve.
func (privKey PrivKey) Sign(msg []byte) ([]byte, error) {
	priv, _ := secp256k1.PrivKeyFromBytes(privKey)
	hash := sha256.Sum256(msg)
	sig, err := ecdsa.SignCompact(priv, hash[:], false)
	if err != nil {
		return nil, err
	}
	// remove the first byte which is compactSigRecoveryCode
	return sig[1:], nil
}

//-------------------------------------

var _ crypto.PubKey = PubKey{}

// PubKeySize is comprised of 32 bytes for one field element
// (the x-coordinate), plus one byte for the parity of the y-coordinate.
const PubKeySize = 33

// PubKey implements crypto.PubKey.
// It is the compressed form of the pubkey. The first byte depends is a 0x02 byte
// if the y-coordinate is the lexicographically largest of the two associated with
// the x-coordinate. Otherwise the first byte is a 0x03.
// This prefix is followed with the x-coordinate.
type PubKey []byte

// Address returns a Bitcoin style addresses: RIPEMD160(SHA256(pubkey))
func (pubKey PubKey) Address() crypto.Address {
	if len(pubKey) != PubKeySize {
		panic("length of pubkey is incorrect")
	}
	hasherSHA256 := sha256.New()
	_, _ = hasherSHA256.Write(pubKey) // does not error
	sha := hasherSHA256.Sum(nil)

	hasherRIPEMD160 := ripemd160.New()
	_, _ = hasherRIPEMD160.Write(sha) // does not error

	return crypto.Address(hasherRIPEMD160.Sum(nil))
}

// Bytes returns the pubkey marshaled with amino encoding.
func (pubKey PubKey) Bytes() []byte {
	return []byte(pubKey)
}

func (pubKey PubKey) String() string {
	return fmt.Sprintf("PubKeySecp256k1{%X}", []byte(pubKey))
}

func (pubKey PubKey) Equals(other crypto.PubKey) bool {
	if otherSecp, ok := other.(PubKey); ok {
		return bytes.Equal(pubKey[:], otherSecp[:])
	}
	return false
}

func (pubKey PubKey) Type() string {
	return KeyType
}

// VerifySignature verifies a signature of the form R || S.
// It rejects signatures which are not in lower-S form.
func (pubKey PubKey) VerifySignature(msg []byte, sigStr []byte) bool {
	if len(sigStr) != 64 {
		return false
	}

	pub, err := secp256k1.ParsePubKey(pubKey)
	if err != nil {
		return false
	}

	// parse the signature:
	var r secp256k1.ModNScalar
	r.SetByteSlice(sigStr[:32])
	var s secp256k1.ModNScalar
	s.SetByteSlice(sigStr[32:64])
	if s.IsOverHalfOrder() {
		return false // reject signatures not in lower-S form
	}

	signature := ecdsa.NewSignature(&r, &s)
	hash := sha256.Sum256(msg)
	return signature.Verify(hash[:], pub)
}
