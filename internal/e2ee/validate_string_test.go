package e2ee

import (
	"fmt"
	"strings"
	"testing"
)

func TestValidateEncryptedString(t *testing.T) {
	id := strings.Repeat("a", 32)
	svc := NewService()
	svc.masterKeys[id] = []byte(strings.Repeat("b", 64))
	ciphertext, err := svc.EncryptString(id, "disposable private content")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateEncryptedString(ciphertext); err != nil {
		t.Fatal(err)
	}
	for _, method := range []int{5, 7, 10} {
		// Legacy methods have different authentication rules; validate only
		// the shared framing/JSON/base64 shape, not cryptographic validity.
		packet := `{"salt":"AAAAAAAAAAA=","iv":"AAAAAAAAAAA=","ct":"AAAAAAAAAAA="}`
		text := fmt.Sprintf("JED01000022%02x%s%06x%s", method, id, len(packet), packet)
		if err := ValidateEncryptedString(text); err != nil {
			t.Fatalf("method %d: %v", method, err)
		}
	}
	for _, text := range []string{
		"private invalid input",
		ciphertext[:45],
		"JED0100002209" + id + ciphertext[45:],
		"JED010000220a" + strings.Repeat("z", 32) + ciphertext[45:],
		"JED010000220a" + id + "000002{}",
		"JED010000220a" + id + "000001{",
		"JED01-00001",
		"JED010000220a" + id + "-00001",
	} {
		if err := ValidateEncryptedString(text); err == nil || strings.Contains(err.Error(), "private") {
			t.Fatalf("malformed packet was accepted or disclosed")
		}
	}
}
