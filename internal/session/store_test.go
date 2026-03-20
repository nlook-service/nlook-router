package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreRegisterAndGet(t *testing.T) {
	s := NewStore("", time.Hour)
	defer s.Close()

	sess := s.Register("cloud-123", TypeChat, 1)
	if sess.ID != "cloud-123" {
		t.Errorf("expected ID cloud-123, got %s", sess.ID)
	}
	if sess.Type != TypeChat {
		t.Errorf("expected TypeChat, got %s", sess.Type)
	}

	got := s.Get("cloud-123")
	if got == nil {
		t.Fatal("expected to find session")
	}
	if got.ID != "cloud-123" {
		t.Errorf("expected cloud-123, got %s", got.ID)
	}
}

func TestStoreRegisterDuplicate(t *testing.T) {
	s := NewStore("", time.Hour)
	defer s.Close()

	sess1 := s.Register("dup-1", TypeChat, 1)
	sess2 := s.Register("dup-1", TypeAgent, 2) // should return existing

	if sess1 != sess2 {
		t.Error("expected same session object for duplicate register")
	}
	if sess2.Type != TypeChat {
		t.Error("expected original type to be preserved")
	}
}

func TestStoreGetOrRegister(t *testing.T) {
	s := NewStore("", time.Hour)
	defer s.Close()

	// First call: registers
	sess1 := s.GetOrRegister("new-1", TypeWorkflow, 1)
	if sess1 == nil {
		t.Fatal("expected session")
	}

	// Second call: returns existing
	sess2 := s.GetOrRegister("new-1", TypeChat, 2)
	if sess1.ID != sess2.ID {
		t.Error("expected same session")
	}
}

func TestStoreGetMissing(t *testing.T) {
	s := NewStore("", time.Hour)
	defer s.Close()

	if s.Get("nonexistent") != nil {
		t.Error("expected nil for missing session")
	}
}

func TestStoreTTLExpiry(t *testing.T) {
	s := NewStore("", 10*time.Millisecond)
	defer s.Close()

	s.Register("expire-me", TypeChat, 1)

	time.Sleep(20 * time.Millisecond)

	if s.Get("expire-me") != nil {
		t.Error("expected session to be expired")
	}
}

func TestStoreTouch(t *testing.T) {
	s := NewStore("", 50*time.Millisecond)
	defer s.Close()

	s.Register("touch-me", TypeChat, 1)

	time.Sleep(30 * time.Millisecond)
	s.Touch("touch-me")

	time.Sleep(30 * time.Millisecond)

	if s.Get("touch-me") == nil {
		t.Error("expected session to still be active after touch")
	}
}

func TestStoreDelete(t *testing.T) {
	s := NewStore("", time.Hour)
	defer s.Close()

	s.Register("del-1", TypeChat, 1)
	s.Delete("del-1")

	if s.Get("del-1") != nil {
		t.Error("expected session to be deleted")
	}
}

func TestStoreList(t *testing.T) {
	s := NewStore("", time.Hour)
	defer s.Close()

	s.Register("a", TypeChat, 1)
	s.Register("b", TypeAgent, 1)

	list := s.List()
	if len(list) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(list))
	}
}

func TestStoreCount(t *testing.T) {
	s := NewStore("", time.Hour)
	defer s.Close()

	s.Register("a", TypeChat, 1)
	s.Register("b", TypeChat, 1)

	if s.Count() != 2 {
		t.Errorf("expected count 2, got %d", s.Count())
	}
}

func TestStorePersistence(t *testing.T) {
	dir := t.TempDir()

	// Create and save
	s1 := NewStore(dir, time.Hour)
	s1.Register("persist-1", TypeChat, 1)
	s1.Register("persist-2", TypeAgent, 2)
	if err := s1.Save(); err != nil {
		t.Fatalf("save error: %v", err)
	}
	s1.Close()

	// Verify file exists
	if _, err := os.Stat(filepath.Join(dir, "sessions.json")); err != nil {
		t.Fatalf("sessions.json not found: %v", err)
	}

	// Load from file
	s2 := NewStore(dir, time.Hour)
	defer s2.Close()

	if s2.Count() != 2 {
		t.Errorf("expected 2 sessions after load, got %d", s2.Count())
	}
	sess := s2.Get("persist-1")
	if sess == nil {
		t.Fatal("expected persist-1 to be loaded")
	}
	if sess.Type != TypeChat {
		t.Errorf("expected TypeChat, got %s", sess.Type)
	}
}

func TestStoreGC(t *testing.T) {
	s := NewStore("", 10*time.Millisecond)
	defer s.Close()

	s.Register("gc-1", TypeChat, 1)
	s.Register("gc-2", TypeChat, 1)

	time.Sleep(20 * time.Millisecond)

	// Manually trigger GC
	s.gc()

	if s.Count() != 0 {
		t.Errorf("expected 0 after GC, got %d", s.Count())
	}
}
