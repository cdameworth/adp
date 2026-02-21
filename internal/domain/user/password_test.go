package user

import "testing"

func TestHashPassword(t *testing.T) {
	hash, err := HashPassword("test-password-123")
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}
	if hash == "" {
		t.Fatal("HashPassword returned empty string")
	}
	if hash == "test-password-123" {
		t.Fatal("HashPassword returned plaintext password")
	}
}

func TestCheckPassword(t *testing.T) {
	password := "my-secure-password"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}

	if !CheckPassword(hash, password) {
		t.Error("CheckPassword returned false for correct password")
	}

	if CheckPassword(hash, "wrong-password") {
		t.Error("CheckPassword returned true for wrong password")
	}

	if CheckPassword(hash, "") {
		t.Error("CheckPassword returned true for empty password")
	}
}

func TestHashPasswordDifferentHashes(t *testing.T) {
	hash1, _ := HashPassword("same-password")
	hash2, _ := HashPassword("same-password")

	if hash1 == hash2 {
		t.Error("same password produced identical hashes (bcrypt should use random salt)")
	}
}
