package e2ee

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/crypto/pbkdf2"
)

// FileV1ChunkSize is the plaintext chunk size for FileV1 (method 9) encryption,
// matching Joplin's EncryptionService.chunkSize(FileV1) = 131072 (128 KiB).
// Chunk boundaries are declared per chunk in the envelope so any size decrypts;
// matching Joplin keeps our generated ciphertext shaped like theirs.
const FileV1ChunkSize = 131072

// fileSaltCounter is the 56-bit counter half of Joplin's salt derivation.
var fileSaltCounter atomic.Uint64

// newSalt generates a FileV1 chunk salt the way Joplin's native crypto does:
// sha256(168-bit rand ‖ 64-bit ms ‖ 56-bit counter) — 36 bytes in, 32 out (the
// 36-byte nonce seed in Joplin's EncryptionService constructor is this shape).
// The salt travels inside the chunk packet, so its derivation is never
// load-bearing for decryption; we match Joplin anyway so our ciphertext
// generation does not differ from theirs in inspectable ways. Field byte order
// is big-endian here; only the shape is shared with Joplin (see INTENT.md).
func newSalt() ([]byte, error) {
	var seed [36]byte
	if _, err := rand.Read(seed[:21]); err != nil {
		return nil, err
	}
	binary.BigEndian.PutUint64(seed[21:29], uint64(time.Now().UnixMilli()))
	c := fileSaltCounter.Add(1) & (1<<56 - 1)
	for i := 0; i < 7; i++ {
		seed[29+i] = byte(c >> (8 * (6 - i)))
	}
	salt := sha256.Sum256(seed[:])
	return salt[:], nil
}

// encryptFileChunk encrypts one FileV1 chunk: PBKDF2-HMAC-SHA512(masterKey,
// salt, 3 iterations, 32-byte key) + AES-256-GCM, packaged as the JSON cipher
// packet {"salt","iv","ct"} that DecryptFile's decryptChunk consumes.
func encryptFileChunk(masterKey, plaintext []byte) ([]byte, error) {
	salt, err := newSalt()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, 12)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	derivedKey := pbkdf2.Key(masterKey, salt, 3, 32, sha512.New)
	ct, err := encryptAESGCM(derivedKey, nonce, plaintext)
	if err != nil {
		return nil, err
	}
	pkt := cipherPacket{
		Salt: base64.StdEncoding.EncodeToString(salt),
		IV:   base64.StdEncoding.EncodeToString(nonce),
		CT:   base64.StdEncoding.EncodeToString(ct),
	}
	return json.Marshal(pkt)
}

// EncryptFile encrypts raw binary data (a resource blob) with the given master
// key and returns a JED01 envelope (method 9 = FileV1) that DecryptFile
// round-trips byte-for-byte. Additive counterpart of DecryptFile — e2ee.go is
// frozen and untouched by design.
//
// The master key must already be loaded via LoadMasterKey. As everywhere in
// this package, PBKDF2 eats the bytes of the 512-char hex string, never
// hex-decoded ones.
func (s *Service) EncryptFile(masterKeyID string, data []byte) (string, error) {
	ctx := context.Background()
	tr := otel.Tracer(e2eeTracerName)
	_, span := tr.Start(ctx, "e2ee.encrypt_file", trace.WithAttributes(attribute.String("master_key_id", masterKeyID)))
	defer span.End()

	masterKey, ok := s.masterKeys[masterKeyID]
	if !ok {
		err := fmt.Errorf("e2ee: master key %s not loaded", masterKeyID)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", err
	}
	if len(masterKeyID) != 32 {
		return "", fmt.Errorf("e2ee: invalid master key ID length %d", len(masterKeyID))
	}

	var chunks [][]byte
	for off := 0; off < len(data); {
		end := off + FileV1ChunkSize
		if end > len(data) {
			end = len(data)
		}
		chunk, err := encryptFileChunk(masterKey, data[off:end])
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return "", fmt.Errorf("e2ee: encrypt chunk: %w", err)
		}
		chunks = append(chunks, chunk)
		off = end
	}

	// JED01: "JED01" + 6-char hex metadata length + metadata (2-char hex method
	// + 32-char master key ID) + chunks (6-char hex length + chunk data each).
	// Empty data yields zero chunks, matching EncryptString's shape.
	metadata := fmt.Sprintf("%02x%s", MethodFileV1, masterKeyID)
	var b strings.Builder
	b.WriteString("JED01")
	b.WriteString(fmt.Sprintf("%06x", len(metadata)))
	b.WriteString(metadata)
	for _, chunk := range chunks {
		b.WriteString(fmt.Sprintf("%06x", len(chunk)))
		b.Write(chunk)
	}
	return b.String(), nil
}
