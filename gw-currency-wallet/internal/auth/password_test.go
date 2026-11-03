package auth

import "testing"

func TestPasswordHashAndCheck(t *testing.T) {
	hash, err := HashPassword("secret123")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	if hash == "secret123" {
		t.Fatal("пароль сохранён в открытом виде")
	}

	if err := CheckPassword(hash, "secret123"); err != nil {
		t.Fatalf("CheckPassword() для верного пароля: %v", err)
	}

	if err := CheckPassword(hash, "wrong"); err == nil {
		t.Fatal("CheckPassword() для неверного пароля вернул nil")
	}
}