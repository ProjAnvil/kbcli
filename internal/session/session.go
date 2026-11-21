// Package session implements Porto-style per-workspace session memory:
// durable facts, older-turn compaction and per-round prompt injection.
package session

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ProjAnvil/kbcli/internal/i18n"
)

// Fact categories mirror Porto's SessionFact enum.
const (
	CategoryDecision   = "user_decision"
	CategoryPreference = "user_preference"
	CategoryContext    = "project_context"
	CategoryQuestion   = "open_question"

	StatusActive    = "active"
	StatusRetracted = "retracted"
)

var categoryPriority = map[string]int{
	CategoryDecision:   0,
	CategoryPreference: 1,
	CategoryContext:    2,
	CategoryQuestion:   3,
}

var categoryKey = map[string]string{
	CategoryDecision:   "session.decision",
	CategoryPreference: "session.preference",
	CategoryContext:    "session.context",
	CategoryQuestion:   "session.question",
}

// Turn is one chat message. Role is "user" or "assistant".
type Turn struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Fact is one durable statement extracted from the conversation.
type Fact struct {
	ID        string    `json:"id"`
	Category  string    `json:"category"`
	Content   string    `json:"content"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Session is the persisted state of one workspace conversation.
type Session struct {
	ID          string    `json:"id"`
	Workspace   string    `json:"workspace"`
	Title       string    `json:"title"`
	CreatedAt   time.Time `json:"created_at"`
	Turns       []Turn    `json:"turns"`
	Summary     string    `json:"summary"`
	SummaryUpTo int       `json:"summary_up_to"`
	Facts       []Fact    `json:"facts"`
}

// Options configures a session store. Complete may be nil; every LLM-backed
// feature then uses an offline fallback.
type Options struct {
	Dir                 string
	Workspace           string
	Lang                i18n.Lang
	Enabled             bool
	FactsEnabled        bool
	MaxFactsPerCategory int
	SimilarityThreshold float64
	RecentContextTurns  int
	MemoryTopK          int
	CompactAfterTurns   int
	Complete            func(ctx context.Context, system, user string) (string, error)
}

// Store owns one workspace's session state. It is safe for concurrent use.
type Store struct {
	mu      sync.RWMutex
	writeMu sync.Mutex
	opts    Options
	path    string
	data    Session
}

// Open loads a session from disk, or creates a fresh in-memory one.
func Open(opts Options) (*Store, error) {
	root, err := filepath.Abs(opts.Workspace)
	if err != nil {
		return nil, fmt.Errorf("session: resolve workspace %q: %w", opts.Workspace, err)
	}
	root = filepath.Clean(root)
	digest := sha256.Sum256([]byte(root))
	id := hex.EncodeToString(digest[:8])
	applyDefaults(&opts)

	s := &Store{
		opts: opts,
		path: filepath.Join(opts.Dir, id+".json"),
		data: Session{
			ID:        newID(),
			Workspace: root,
			CreatedAt: time.Now().UTC(),
			Turns:     make([]Turn, 0, 16),
			Facts:     make([]Fact, 0, 8),
		},
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func applyDefaults(o *Options) {
	if o.MaxFactsPerCategory <= 0 {
		o.MaxFactsPerCategory = 20
	}
	if o.SimilarityThreshold <= 0 {
		o.SimilarityThreshold = 0.5
	}
	if o.RecentContextTurns <= 0 {
		o.RecentContextTurns = 6
	}
	// MemoryTopK == 0 explicitly disables recent-turn injection; only a
	// negative value means "not configured".
	if o.MemoryTopK < 0 {
		o.MemoryTopK = 5
	}
	if o.CompactAfterTurns <= 0 {
		o.CompactAfterTurns = 20
	}
	if o.Lang == "" {
		o.Lang = i18n.LangEn
	}
}

func newID() string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

// ID returns the workspace-derived session identifier.
func (s *Store) ID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.ID
}

// Path returns the session file path.
func (s *Store) Path() string { return s.path }

// SetLang switches the language used for prompt blocks.
func (s *Store) SetLang(l i18n.Lang) {
	s.mu.Lock()
	s.opts.Lang = l
	s.mu.Unlock()
}

// AddTurn appends one conversation turn.
func (s *Store) AddTurn(role, content string) {
	content = trimRunes(content, 4000)
	if content == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Turns = append(s.data.Turns, Turn{Role: role, Content: content})
	if s.data.Title == "" && role == "user" {
		s.data.Title = trimRunes(content, 40)
	}
}

// Turns returns a defensive copy of the full history.
func (s *Store) Turns() []Turn {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Turn, len(s.data.Turns))
	copy(out, s.data.Turns)
	return out
}

// Recent returns the last n turns. A non-positive n uses the configured
// recent-context count.
func (s *Store) Recent(n int) []Turn {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if n <= 0 {
		n = s.opts.RecentContextTurns
	}
	start := len(s.data.Turns) - n
	if start < 0 {
		start = 0
	}
	out := make([]Turn, len(s.data.Turns)-start)
	copy(out, s.data.Turns[start:])
	return out
}

// Summary returns the compacted history summary.
func (s *Store) Summary() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.Summary
}

// Title returns the auto-generated session title.
func (s *Store) Title() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.Title
}

// Reset clears the conversation but keeps the session identity.
func (s *Store) Reset() error {
	s.mu.Lock()
	s.data.Turns = nil
	s.data.Summary = ""
	s.data.SummaryUpTo = 0
	s.data.Facts = nil
	s.data.Title = ""
	s.mu.Unlock()
	return s.Save()
}

func trimRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func runeLen(s string) int { return utf8.RuneCountInString(s) }

func ensureDir(path string) error {
	return os.MkdirAll(filepath.Dir(path), 0o700)
}
