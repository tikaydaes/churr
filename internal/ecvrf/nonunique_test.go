package ecvrf

import (
	"bytes"
	"crypto/sha256"
	"testing"

	secp256k1 "github.com/decred/dcrd/dcrec/secp256k1/v4"
)

// This file is Churr's, not the library's: it shows why docs/FAIR.md does
// not chain a draw to the proof before it. An ECVRF proof is not unique:
// whoever holds the key may pick any nonce k in place of RFC 6979's and get
// another proof of the same alpha that verifies just as well. Only the
// output, beta (and Gamma), is unique. A hash of the previous proof in the
// next alpha would let the key holder choose among next draws.

// proveWithNonce is Prove with a nonce of the caller's choosing.
func proveWithNonce(sk, alpha []byte, k secp256k1.ModNScalar) []byte {
	privKey := secp256k1.PrivKeyFromBytes(sk)
	pubKey := privKey.PubKey()
	h, _, err := encodeToCurveTAI(pubKey, alpha)
	if err != nil {
		panic(err)
	}
	var gamma, u, v, yJac secp256k1.JacobianPoint
	secp256k1.ScalarMultNonConst(&privKey.Key, &h, &gamma)
	secp256k1.ScalarBaseMultNonConst(&k, &u)
	secp256k1.ScalarMultNonConst(&k, &h, &v)
	pubKey.AsJacobian(&yJac)
	c := challengeGeneration(&yJac, &h, &gamma, &u, &v)
	var cx, s secp256k1.ModNScalar
	cx.Mul2(&c, &privKey.Key)
	s.Add2(&k, &cx)
	cb, sb := c.Bytes(), s.Bytes()
	pi := append(compressPoint(&gamma), cb[16:]...)
	return append(pi, sb[:]...)
}

func TestProofsAreNotUniqueOutputsAre(t *testing.T) {
	sum := sha256.Sum256([]byte("a server's VRF key"))
	sk := sum[:]
	pk, err := DerivePublicKey(sk)
	if err != nil {
		t.Fatal(err)
	}
	alpha := []byte("churr-fair-1 some draw")
	honest, err := Prove(sk, alpha)
	if err != nil {
		t.Fatal(err)
	}
	var k secp256k1.ModNScalar
	k.SetInt(12345)
	other := proveWithNonce(sk, alpha, k)
	if bytes.Equal(honest, other) {
		t.Fatal("a chosen nonce gave the same proof")
	}
	ok1, beta1 := Verify(pk, honest, alpha)
	ok2, beta2 := Verify(pk, other, alpha)
	if !ok1 || !ok2 {
		t.Fatalf("both proofs should verify: %v %v", ok1, ok2)
	}
	if !bytes.Equal(beta1, beta2) {
		t.Fatal("two proofs of one alpha gave two outputs")
	}
	if sha256.Sum256(honest) == sha256.Sum256(other) {
		t.Fatal("the two proofs hash alike")
	}
}
