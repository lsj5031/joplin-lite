package e2ee

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

// newFileTestService loads a deterministic 256-byte master key (stored as its
// 512-char hex string, like Joplin) and returns the service plus its key ID.
func newFileTestService(t *testing.T) (*Service, string) {
	t.Helper()
	rawKey := make([]byte, 256)
	for i := range rawKey {
		rawKey[i] = byte(i)
	}
	keyID := "0123456789abcdef0123456789abcdef"
	password := "test-password"
	salt := make([]byte, 32)
	for i := range salt {
		salt[i] = byte(i)
	}
	iv := make([]byte, 12)
	for i := range iv {
		iv[i] = byte(i + 10)
	}
	packet := buildKeyV1Packet(t, password, rawKey, salt, iv)
	s := NewService()
	if err := s.LoadMasterKey(keyID, packet, password, MethodKeyV1); err != nil {
		t.Fatalf("LoadMasterKey failed: %v", err)
	}
	return s, keyID
}

// deterministicBytes produces reproducible data that hits every byte value.
func deterministicBytes(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i * 131) // 131 is odd, so residues cycle through all values
	}
	return b
}

func TestEncryptFile_RoundTrip(t *testing.T) {
	s, keyID := newFileTestService(t)

	cases := []struct {
		name       string
		data       []byte
		wantChunks int
	}{
		{"empty", nil, 0},
		{"one byte", []byte{0x00}, 1},
		{"short text", []byte("hello resource blob"), 1},
		{"nul heavy", make([]byte, 4096), 1},
		{"just under one chunk", deterministicBytes(FileV1ChunkSize - 1), 1},
		{"exactly one chunk", deterministicBytes(FileV1ChunkSize), 1},
		{"one over a chunk", deterministicBytes(FileV1ChunkSize + 1), 2},
		{"two chunks plus tail", deterministicBytes(2*FileV1ChunkSize + 7), 3},
		{"all byte values", deterministicBytes(300_000), 3},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			enc, err := s.EncryptFile(keyID, tc.data)
			if err != nil {
				t.Fatalf("EncryptFile failed: %v", err)
			}

			got, err := s.DecryptFile(enc)
			if err != nil {
				t.Fatalf("DecryptFile failed: %v", err)
			}
			if !bytes.Equal(got, tc.data) {
				t.Fatalf("round trip mismatch: got %d bytes, want %d", len(got), len(tc.data))
			}

			// Auto-detect dispatch must land on the file path too.
			_, fileBytes, err := s.DecryptStringOrFile(enc)
			if err != nil {
				t.Fatalf("DecryptStringOrFile failed: %v", err)
			}
			if !bytes.Equal(fileBytes, tc.data) {
				t.Fatalf("DecryptStringOrFile round trip mismatch: got %d bytes, want %d", len(fileBytes), len(tc.data))
			}

			gotKeyID, method, chunks, err := parseJED01(enc)
			if err != nil {
				t.Fatalf("parseJED01 failed: %v", err)
			}
			if method != MethodFileV1 {
				t.Errorf("method = %d, want %d (FileV1)", method, MethodFileV1)
			}
			if gotKeyID != keyID {
				t.Errorf("master key ID = %q, want %q", gotKeyID, keyID)
			}
			if len(chunks) != tc.wantChunks {
				t.Errorf("chunk count = %d, want %d", len(chunks), tc.wantChunks)
			}
		})
	}
}

func TestEncryptFile_EnvelopeShape(t *testing.T) {
	s, keyID := newFileTestService(t)
	data := deterministicBytes(FileV1ChunkSize + 1)

	enc, err := s.EncryptFile(keyID, data)
	if err != nil {
		t.Fatalf("EncryptFile failed: %v", err)
	}

	if !strings.HasPrefix(enc, "JED01") {
		t.Fatalf("envelope does not start with JED01: %.16q", enc)
	}
	if enc[5:11] != "000022" {
		t.Errorf("metadata length field = %q, want %q", enc[5:11], "000022")
	}
	wantMeta := "09" + keyID
	if enc[11:45] != wantMeta {
		t.Errorf("metadata = %q, want %q", enc[11:45], wantMeta)
	}

	_, _, chunks, err := parseJED01(enc)
	if err != nil {
		t.Fatalf("parseJED01 failed: %v", err)
	}
	if len(chunks) != 2 {
		t.Fatalf("chunk count = %d, want 2", len(chunks))
	}
	wantPlain := []int{FileV1ChunkSize, 1}
	for i, chunk := range chunks {
		var pkt cipherPacket
		if err := json.Unmarshal(chunk, &pkt); err != nil {
			t.Fatalf("chunk %d: cannot parse cipher packet: %v", i, err)
		}
		salt, err := base64.StdEncoding.DecodeString(pkt.Salt)
		if err != nil {
			t.Fatalf("chunk %d: bad salt: %v", i, err)
		}
		if len(salt) != 32 {
			t.Errorf("chunk %d: salt length = %d, want 32", i, len(salt))
		}
		iv, err := base64.StdEncoding.DecodeString(pkt.IV)
		if err != nil {
			t.Fatalf("chunk %d: bad IV: %v", i, err)
		}
		if len(iv) != 12 {
			t.Errorf("chunk %d: IV length = %d, want 12", i, len(iv))
		}
		ct, err := base64.StdEncoding.DecodeString(pkt.CT)
		if err != nil {
			t.Fatalf("chunk %d: bad ciphertext: %v", i, err)
		}
		if len(ct) != wantPlain[i]+16 {
			t.Errorf("chunk %d: ciphertext length = %d, want %d (plaintext + 16-byte GCM tag)",
				i, len(ct), wantPlain[i]+16)
		}
	}
}

func TestEncryptFile_MethodDispatch(t *testing.T) {
	s, keyID := newFileTestService(t)

	strEnc, err := s.EncryptString(keyID, "a note, not a file")
	if err != nil {
		t.Fatalf("EncryptString failed: %v", err)
	}
	if _, err := s.DecryptFile(strEnc); err == nil {
		t.Fatal("DecryptFile accepted a StringV1 (method 10) envelope; it must reject non-FileV1 methods")
	}
}

func TestEncryptFile_TamperRejected(t *testing.T) {
	s, keyID := newFileTestService(t)

	enc, err := s.EncryptFile(keyID, deterministicBytes(2048))
	if err != nil {
		t.Fatalf("EncryptFile failed: %v", err)
	}

	// Flip the last ciphertext byte; GCM authentication must reject it.
	tampered := enc[:len(enc)-1] + string(enc[len(enc)-1]^0xff)
	if _, err := s.DecryptFile(tampered); err == nil {
		t.Fatal("DecryptFile accepted tampered ciphertext")
	}
}

func TestEncryptFile_KeyErrors(t *testing.T) {
	s, keyID := newFileTestService(t)

	if _, err := s.EncryptFile("ffffffffffffffffffffffffffffffff", []byte("x")); err == nil {
		t.Error("EncryptFile accepted an unloaded master key")
	}
	if _, err := s.EncryptFile(keyID[:16], []byte("x")); err == nil {
		t.Error("EncryptFile accepted a short master key ID")
	}
}

func TestNewSalt_Shape(t *testing.T) {
	a, err := newSalt()
	if err != nil {
		t.Fatalf("newSalt failed: %v", err)
	}
	b, err := newSalt()
	if err != nil {
		t.Fatalf("newSalt failed: %v", err)
	}
	if len(a) != 32 {
		t.Errorf("salt length = %d, want 32", len(a))
	}
	if bytes.Equal(a, b) {
		t.Error("two salts collided; the counter half is not working")
	}
}
