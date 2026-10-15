package taskcontrol

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"testing"
	"time"
)

func testSign(key ed25519.PrivateKey, a Assertion) string {
	raw, _ := json.Marshal(a)
	s := "test." + base64.RawURLEncoding.EncodeToString(raw)
	return s + "." + base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, []byte("teamos-task-control/v1\n"+s)))
}
func TestAssertionValidation(t *testing.T) {
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Now().Truncate(time.Second)
	valid := Assertion{Audience: "teamos-task-control", ID: uuid.NewString(), IssuedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix(), CompanyID: uuid.New(), InstallationID: uuid.New(), IntegrationID: uuid.New(), AccountID: "123", ActorID: "7", Product: Product, Generation: 1, Purpose: "runtime", RequestID: uuid.New()}
	keys := map[string]ed25519.PublicKey{"test": pub}
	if _, _, e := Verify(testSign(key, valid), keys, now); e != nil {
		t.Fatal(e)
	}
	for name, change := range map[string]func(*Assertion){"expired": func(a *Assertion) { a.ExpiresAt = now.Unix() }, "ttl": func(a *Assertion) { a.ExpiresAt++ }, "audience": func(a *Assertion) { a.Audience = "teamos-api" }, "product": func(a *Assertion) { a.Product = "lead-distribution" }, "tenant": func(a *Assertion) { a.CompanyID = uuid.Nil }, "unsafe": func(a *Assertion) { a.ActorID = "9007199254740993" }, "future": func(a *Assertion) { a.IssuedAt += 30; a.ExpiresAt += 30 }, "purpose": func(a *Assertion) { a.Purpose = "admin" }} {
		t.Run(name, func(t *testing.T) {
			a := valid
			change(&a)
			if _, _, e := Verify(testSign(key, a), keys, now); e == nil {
				t.Fatal("invalid assertion accepted")
			}
		})
	}
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	if _, _, e := Verify(testSign(other, valid), keys, now); e == nil {
		t.Fatal("wrong key")
	}
}

func TestCoreWireFixture(t *testing.T) {
	raw, e := os.ReadFile("../../../../../contracts/fixtures/task-control-handoff-v1.json")
	if e != nil {
		t.Fatal(e)
	}
	var f struct {
		PublicKey string    `json:"publicKey"`
		Assertion string    `json:"assertion"`
		Claims    Assertion `json:"claims"`
	}
	if e = json.Unmarshal(raw, &f); e != nil {
		t.Fatal(e)
	}
	pub, e := base64.StdEncoding.DecodeString(f.PublicKey)
	if e != nil {
		t.Fatal(e)
	}
	a, _, e := Verify(f.Assertion, map[string]ed25519.PublicKey{"fixture": pub}, time.Unix(1900000000, 0))
	if e != nil || a != f.Claims {
		t.Fatal("Core/TeamOS wire mismatch", e)
	}
}
