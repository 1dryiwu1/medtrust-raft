package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

func TestArgon2idPasswordHashUsesUniqueSalt(t *testing.T) {
	const password = "correct horse battery staple"
	first, err := hashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	second, err := hashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("identical passwords produced identical hashes; random salt is missing")
	}
	if !strings.HasPrefix(first, "$argon2id$") || strings.Contains(first, password) {
		t.Fatalf("unexpected encoded password hash: %q", first)
	}
	if valid, upgrade := verifyPassword(first, password); !valid || upgrade {
		t.Fatalf("current Argon2id hash verification = (%v, %v), want (true, false)", valid, upgrade)
	}
	if valid, _ := verifyPassword(first, "wrong password"); valid {
		t.Fatal("wrong password was accepted")
	}
}

func TestLegacyBcryptPasswordRequestsUpgrade(t *testing.T) {
	legacy, err := bcrypt.GenerateFromPassword([]byte("legacy-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	valid, upgrade := verifyPassword(string(legacy), "legacy-password")
	if !valid || !upgrade {
		t.Fatalf("legacy bcrypt verification = (%v, %v), want (true, true)", valid, upgrade)
	}
}

func TestMalformedArgon2idHashIsRejected(t *testing.T) {
	cases := []string{
		"",
		"$argon2id$v=19$m=999999999,t=3,p=2$bad$bad",
		"$argon2id$v=18$m=65536,t=3,p=2$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAA",
	}
	for _, encoded := range cases {
		if valid, _ := verifyPassword(encoded, "anything"); valid {
			t.Fatalf("malformed hash was accepted: %q", encoded)
		}
	}
}

func TestLoginUpgradesLegacyBcryptHash(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv := newPortalTestServer(t)
	srv.users = make(map[string]authUser)
	srv.sessions = make(map[string]authUser)
	legacy, err := bcrypt.GenerateFromPassword([]byte("legacy-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	srv.users["patient-a"] = authUser{Username: "patient-a", Role: "patient", PasswordHash: string(legacy)}

	body, _ := json.Marshal(map[string]string{"username": "patient-a", "password": "legacy-password"})
	writer := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(writer)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	srv.handleLogin(ctx)
	if writer.Code != http.StatusOK {
		t.Fatalf("login returned %d: %s", writer.Code, writer.Body.String())
	}
	upgraded := srv.users["patient-a"].PasswordHash
	if !strings.HasPrefix(upgraded, "$argon2id$") {
		t.Fatalf("in-memory password was not upgraded: %q", upgraded)
	}
	stored, err := srv.store.LoadPortalUsers()
	if err != nil || len(stored) != 1 || stored[0].PasswordHash != upgraded {
		t.Fatalf("upgraded hash was not persisted: %#v, %v", stored, err)
	}
}

func TestPasswordLengthPolicy(t *testing.T) {
	if passwordIsAcceptable("short") {
		t.Fatal("short password was accepted")
	}
	if !passwordIsAcceptable("twelve-chars") {
		t.Fatal("12-character password was rejected")
	}
	if passwordIsAcceptable(strings.Repeat("x", maximumPasswordLength+1)) {
		t.Fatal("oversized password was accepted")
	}
}

func TestInitialAdminUsesConfiguredSaltedHash(t *testing.T) {
	srv := newPortalTestServer(t)
	srv.users = make(map[string]authUser)
	t.Setenv("MEDTRUST_ADMIN_PASSWORD", "strong-admin-password")
	srv.loadPortalUsers()
	admin, ok := srv.users["admin"]
	if !ok || admin.Role != "admin" {
		t.Fatalf("initial administrator was not created: %#v", admin)
	}
	if valid, upgrade := verifyPassword(admin.PasswordHash, "strong-admin-password"); !valid || upgrade {
		t.Fatalf("initial administrator hash verification = (%v, %v), want (true, false)", valid, upgrade)
	}
}
