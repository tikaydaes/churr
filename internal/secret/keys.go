// Package secret holds the server's key files and everything done with them:
// sealing columns in the database, signing, opening boxes other servers
// sealed to this one, keyed hashes, and making the random tokens, link
// values and recovery codes that let people and programs in.
//
// The keys live in the keys/ folder of the data directory
// (docs/OPERATIONS.md section 4):
//
//	seal.key   32 random bytes; the AES-256 key for sealed columns
//	sign.key   the 32-byte seed of the server's Ed25519 signing key
//	box.key    the server's 32-byte X25519 private key
//
// Nothing here logs, and no function returns key material except the two
// public halves.
package secret

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

// The key files, and how long each must be.
const (
	sealFile = "seal.key"
	signFile = "sign.key"
	boxFile  = "box.key"

	sealLen = 32 // AES-256
	signLen = ed25519.SeedSize
	boxLen  = 32 // X25519
)

// Keys is the opened contents of keys/. It is safe for use from many
// goroutines; nothing in it changes after Open.
type Keys struct {
	seal    []byte      // kept for deriving MAC keys
	sealing cipher.AEAD // AES-256-GCM under seal
	sign    ed25519.PrivateKey
	box     *ecdh.PrivateKey
}

// Open reads the keys in dir, the keys/ folder of a data directory. On first
// use it makes the folder with mode 0700 and each key file with mode 0600.
//
// On later starts it refuses to go on if the folder or a key file can be
// reached by the group or by others, because a key someone else could have
// read is no longer a secret, and the operator should hear about that
// before the server runs on it. It also refuses a key file of the wrong
// length, which means a damaged or half-restored folder.
//
// A file that is missing while others are present is made afresh, the same
// as on first use. That is what an interrupted first start needs. It also
// means that a restore which left out sign.key or box.key gives the server
// a new identity (docs/OPERATIONS.md section 4), which is all that can be
// done for a server whose keys are gone.
func Open(dir string) (*Keys, error) {
	if dir == "" {
		return nil, errors.New("secret: no keys directory given")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("secret: making the keys directory: %w", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("secret: reading the keys directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("secret: %s is not a directory", dir)
	}
	if tooOpen(info.Mode()) {
		return nil, fmt.Errorf("secret: the keys directory %s can be reached by other users (mode %04o); "+
			"it must be mode 0700: chmod 700 %s", dir, info.Mode().Perm(), dir)
	}

	seal, err := loadOrMake(dir, sealFile, sealLen, RandomBytes)
	if err != nil {
		return nil, err
	}
	seed, err := loadOrMake(dir, signFile, signLen, RandomBytes)
	if err != nil {
		return nil, err
	}
	boxBytes, err := loadOrMake(dir, boxFile, boxLen, func(int) []byte {
		// Let the curve package make the key, so whatever it requires of a
		// private key holds by construction.
		k, err := ecdh.X25519().GenerateKey(rand.Reader)
		if err != nil {
			panic("secret: generating an X25519 key: " + err.Error())
		}
		return k.Bytes()
	})
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(seal)
	if err != nil {
		return nil, fmt.Errorf("secret: %s: %w", sealFile, err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secret: %s: %w", sealFile, err)
	}
	box, err := ecdh.X25519().NewPrivateKey(boxBytes)
	if err != nil {
		return nil, fmt.Errorf("secret: %s: %w", boxFile, err)
	}
	return &Keys{
		seal:    seal,
		sealing: gcm,
		sign:    ed25519.NewKeyFromSeed(seed),
		box:     box,
	}, nil
}

// tooOpen reports whether a mode lets the group or others in at all.
// Windows has no such bits and reports made-up ones, so it is not asked.
func tooOpen(mode fs.FileMode) bool {
	if runtime.GOOS == "windows" {
		return false
	}
	return mode.Perm()&0o077 != 0
}

// loadOrMake returns the n bytes of the key file name in dir, making the
// file with new bytes from fresh if it does not exist yet.
func loadOrMake(dir, name string, n int, fresh func(int) []byte) ([]byte, error) {
	path := filepath.Join(dir, name)

	// O_EXCL means two servers started on the same folder at the same moment
	// cannot both write the file: one makes it and the other reads it.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	switch {
	case err == nil:
		key := fresh(n)
		_, werr := f.Write(key)
		if werr == nil {
			// The database will soon hold values only this key can open, so
			// the key has to be on the disk before anything is sealed.
			werr = f.Sync()
		}
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			os.Remove(path)
			return nil, fmt.Errorf("secret: writing %s: %w", path, werr)
		}
		return key, nil
	case !errors.Is(err, fs.ErrExist):
		return nil, fmt.Errorf("secret: making %s: %w", path, err)
	}

	f, err = os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("secret: reading %s: %w", path, err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("secret: reading %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("secret: %s is not an ordinary file", path)
	}
	if tooOpen(info.Mode()) {
		return nil, fmt.Errorf("secret: the key file %s can be read by other users (mode %04o); "+
			"it must be mode 0600: chmod 600 %s", path, info.Mode().Perm(), path)
	}
	// One byte more than wanted, so a file that is too long is noticed
	// without reading all of it.
	key, err := io.ReadAll(io.LimitReader(f, int64(n)+1))
	if err != nil {
		return nil, fmt.Errorf("secret: reading %s: %w", path, err)
	}
	if len(key) == 0 {
		return nil, fmt.Errorf("secret: the key file %s is empty; if the very first start was interrupted, "+
			"remove it and start again, otherwise restore it from a backup", path)
	}
	if len(key) != n {
		return nil, fmt.Errorf("secret: the key file %s has the wrong length (want %d bytes); "+
			"restore it from a backup", path, n)
	}
	// A file of the right length filled with zeros is what some file systems
	// leave behind after losing power in the middle of a write.
	if bytes.Equal(key, make([]byte, n)) {
		return nil, fmt.Errorf("secret: the key file %s is all zero bytes; restore it from a backup", path)
	}
	return key, nil
}

// Sign signs msg with the server's Ed25519 key (no prehash, RFC 8032).
func (k *Keys) Sign(msg []byte) []byte {
	return ed25519.Sign(k.sign, msg)
}

// SignPublic is the public half of the signing key: what other servers pin
// and what /.well-known/churr publishes as sign_key.
func (k *Keys) SignPublic() ed25519.PublicKey {
	return bytes.Clone(k.sign.Public().(ed25519.PublicKey))
}

// BoxPublic is the public half of the X25519 key, 32 bytes: what other
// servers seal to and what /.well-known/churr publishes as box_key.
func (k *Keys) BoxPublic() []byte {
	return k.box.PublicKey().Bytes()
}

// RandomBytes returns n bytes from the operating system's generator. It
// never returns fewer: on a system whose generator fails the Go runtime
// stops the program, which is the only safe answer for a server that makes
// credentials.
func RandomBytes(n int) []byte {
	b := make([]byte, n)
	rand.Read(b)
	return b
}
