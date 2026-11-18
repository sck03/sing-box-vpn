package adminauth

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const testPassword = "correct-password-123"

func TestEightBytePasswordBoundary(t *testing.T) {
	for _, password := range []string{"1234567", "一二", string(make([]byte, 73))} {
		if _, err := passwordHash(password); err == nil {
			t.Fatal("accepted invalid byte length", len(password))
		}
	}
	s, err := Open(filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Bootstrap(t.Context(), "admin", "12345678", false); err != nil {
		t.Fatal(err)
	}
	session, err := s.Login(t.Context(), "admin", "12345678", "", "local")
	if err != nil {
		t.Fatal(err)
	}
	admins, err := s.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Save(t.Context(), "admin", admins[0], "一二三"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Authenticate(t.Context(), session.Token, "", false); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("old session survived", err)
	}
	if _, err = s.Login(t.Context(), "admin", "一二三", "", "local"); err != nil {
		t.Fatal(err)
	}
	if err = s.Bootstrap(t.Context(), "admin", "87654321", true); err != nil {
		t.Fatal(err)
	}
}

func TestUnknownUserHashHasLoginCost(t *testing.T) {
	cost, err := bcrypt.Cost(dummyHash)
	if err != nil || cost != 12 {
		t.Fatalf("invalid unknown-user comparison hash: cost=%d, error=%v", cost, err)
	}
	if err := bcrypt.CompareHashAndPassword(dummyHash, []byte(testPassword)); !errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		t.Fatalf("unknown-user comparison must perform a valid bcrypt check: %v", err)
	}
}

func authFixture(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "control.db")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	if e = s.Bootstrap(t.Context(), "admin", testPassword, false); e != nil {
		t.Fatal(e)
	}
	return s, path
}
func TestSessionsCSRFAndRevocation(t *testing.T) {
	s, _ := authFixture(t)
	ctx := t.Context()
	first, e := s.Login(ctx, "admin", testPassword, "", "127.0.0.1")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Authenticate(ctx, first.Token, "bad", true); !errors.Is(e, ErrUnauthorized) {
		t.Fatal(e)
	}
	if _, e = s.Authenticate(ctx, first.Token, first.CSRF, true); e != nil {
		t.Fatal(e)
	}
	second, e := s.Login(ctx, "admin", testPassword, "", "127.0.0.1")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Authenticate(ctx, first.Token, "", false); !errors.Is(e, ErrUnauthorized) {
		t.Fatal("old login survived", e)
	}
	users, _ := s.List(ctx)
	a := users[0]
	a.Disabled = true
	if e = s.Save(ctx, "admin", a, ""); e == nil {
		t.Fatal("disabled last administrator")
	}
	a.Disabled = false
	if e = s.Save(ctx, "admin", a, "changed-password-123"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Authenticate(ctx, second.Token, "", false); !errors.Is(e, ErrUnauthorized) {
		t.Fatal("password change retained session", e)
	}
	var clear int
	if e = s.db.QueryRow(`SELECT count(*) FROM administrators WHERE password=?`, testPassword).Scan(&clear); e != nil || clear != 0 {
		t.Fatal("plaintext password", e)
	}
}
func TestPersistentGuardAndBoundedKeys(t *testing.T) {
	s, path := authFixture(t)
	ctx := t.Context()
	now := time.Now().Unix()
	for range 8 {
		if e := s.guard(ctx, "target", "peer", now); e != nil {
			t.Fatal(e)
		}
	}
	s.Close()
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.guard(ctx, "target", "peer", now); !errors.Is(e, ErrLimited) {
		t.Fatal("guard reset", e)
	}
	for i := 0; i < 1500; i++ {
		_ = s.guard(ctx, fmt.Sprint(i), fmt.Sprint(i), now)
	}
	var count int
	s.db.QueryRow(`SELECT count(*) FROM login_guard`).Scan(&count)
	if count > 1003 {
		t.Fatal("unbounded attacker keys", count)
	}
	if e = s.guard(ctx, "target", "peer", now+901); e != nil {
		t.Fatal(e)
	}
}
func TestMFAConfirmAndReplay(t *testing.T) {
	s, _ := authFixture(t)
	ctx := t.Context()
	session, e := s.Login(ctx, "admin", testPassword, "", "local")
	if e != nil {
		t.Fatal(e)
	}
	secret, e := s.Enroll(ctx, "admin", testPassword)
	if e != nil {
		t.Fatal(e)
	}
	step := time.Now().Unix() / 30
	code := totp(secret, step)
	if e = s.ConfirmMFA(ctx, "admin", code); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Authenticate(ctx, session.Token, "", false); !errors.Is(e, ErrUnauthorized) {
		t.Fatal(e)
	}
	if _, e = s.Login(ctx, "admin", testPassword, code, "local"); !errors.Is(e, ErrUnauthorized) {
		t.Fatal("TOTP replay", e)
	}
	next := totp(secret, step+1)
	if _, e = s.Login(ctx, "admin", testPassword, next, "local"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Login(ctx, "admin", testPassword, next, "local"); !errors.Is(e, ErrUnauthorized) {
		t.Fatal("login TOTP replay", e)
	}
}
