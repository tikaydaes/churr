package ref

import (
	"encoding/binary"
	"errors"
)

// Files (docs/SEALED.md section 9): a file is cut into chunks and each is
// encrypted under a key used for that one file.

// ChunkSize is the plaintext length of every chunk but the last.
const ChunkSize = 65536

// sealedChunk is a full chunk with its tag.
const sealedChunk = ChunkSize + 16

// ErrFile is the refusal of an encrypted file: a chunk changed, moved or
// missing, or the file cut short.
var ErrFile = errors.New("ref: the file does not check out")

// chunkNonce is the nonce of chunk i, counting from 0: 3 zero bytes, i as a
// u64, then one byte that is 1 for the last chunk and 0 otherwise.
func chunkNonce(i uint64, last bool) []byte {
	n := make([]byte, 3, 12)
	n = binary.BigEndian.AppendUint64(n, i)
	if last {
		return append(n, 1)
	}
	return append(n, 0)
}

// EncryptFile encrypts a file under its 32-byte key and returns the blob
// to upload: the chunks, each followed by its tag, joined end to end.
//
// A file of n bytes has max(1, ceil(n / 65536)) chunks: an empty file is
// one empty chunk, and a file whose length is an exact multiple of the
// chunk size ends with a full chunk, not an empty one.
func EncryptFile(key, plaintext []byte) []byte {
	chunks := (len(plaintext) + ChunkSize - 1) / ChunkSize
	if chunks == 0 {
		chunks = 1
	}
	out := make([]byte, 0, len(plaintext)+16*chunks)
	for i := 0; i < chunks; i++ {
		end := (i + 1) * ChunkSize
		if end > len(plaintext) {
			end = len(plaintext)
		}
		out = append(out, gcmSeal(key, chunkNonce(uint64(i), i == chunks-1), plaintext[i*ChunkSize:end], []byte(LabelFile))...)
	}
	return out
}

// DecryptFile decrypts a blob chunk by chunk. The chunk number in the
// nonce stops chunks being reordered and the last-chunk byte stops the file
// being cut short; a file is used only after its last chunk has checked
// out.
func DecryptFile(key, blob []byte) ([]byte, error) {
	if len(blob) < 16 {
		return nil, ErrFile
	}
	chunks := (len(blob) + sealedChunk - 1) / sealedChunk
	// A trailing piece shorter than a tag cannot be a chunk.
	if rest := len(blob) % sealedChunk; rest != 0 && rest < 16 {
		return nil, ErrFile
	}
	out := make([]byte, 0, len(blob)-16*chunks)
	for i := 0; i < chunks; i++ {
		end := (i + 1) * sealedChunk
		if end > len(blob) {
			end = len(blob)
		}
		plain, err := gcmOpen(key, chunkNonce(uint64(i), i == chunks-1), blob[i*sealedChunk:end], []byte(LabelFile))
		if err != nil {
			return nil, ErrFile
		}
		out = append(out, plain...)
	}
	return out, nil
}
