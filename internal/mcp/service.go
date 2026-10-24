package mcp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/qivs/agent-deck/internal/channel"
)

// Member is the current public roster and posting policy for one member.
type Member struct {
	ID    string `json:"id,omitempty"`
	Name  string `json:"name"`
	Role  string `json:"role"`
	Muted bool   `json:"muted"`
}

// Options supplies live roster and policy; callbacks must be safe for concurrent calls.
type Options struct {
	Team         string
	Journal      *channel.Journal
	Members      func() []Member
	ReadOnly     func() bool
	Now          func() time.Time
	MaxBytes     int
	MaxHops      int
	Rate         int
	Transport    Transport
	Instructions func(context.Context, string) (string, error)
}

// Service validates authenticated channel actions before any journal side effect.
type Service struct {
	mu            sync.Mutex
	options       Options
	deliveryLocks sync.Map
}

// Post describes content and recipients, with an optional visible parent message.
type Post struct {
	Text     string   `json:"text"`
	Mentions []string `json:"mentions,omitempty"`
	Member   string   `json:"member,omitempty"`
	ReplyTo  string   `json:"reply_to,omitempty"`
}

func NewService(config *Options) (*Service, error) {
	if config == nil {
		return nil, fmt.Errorf("MCP service options are required")
	}
	options := *config
	if options.Team == "" || options.Journal == nil || options.Members == nil {
		return nil, fmt.Errorf("MCP service requires team, journal and current roster")
	}
	if options.Transport != nil && options.Instructions == nil {
		return nil, fmt.Errorf("delivery transport requires current member instructions")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.MaxBytes <= 0 {
		options.MaxBytes = 16 * 1024
	}
	if options.MaxHops <= 0 {
		options.MaxHops = 3
	}
	if options.Rate <= 0 {
		options.Rate = 30
	}
	for _, member := range options.Members() {
		if member.Name == "agent-deck" || member.Name == "user" || member.ID == "" {
			return nil, fmt.Errorf("members require immutable membership IDs and nonreserved names")
		}
	}
	return &Service{options: options}, nil
}

func (s *Service) member(identity Identity) (Member, error) {
	if identity.Team != s.options.Team {
		return Member{}, fmt.Errorf("member is not in this team")
	}
	for _, member := range s.options.Members() {
		if member.Name == identity.Member {
			if member.ID == "" || member.Name == "agent-deck" || member.Name == "user" {
				return Member{}, fmt.Errorf("invalid membership identity")
			}
			return member, nil
		}
	}
	return Member{}, fmt.Errorf("member is no longer in this team")
}

func (s *Service) Members(identity Identity) ([]Member, error) {
	if _, err := s.member(identity); err != nil {
		return nil, err
	}
	return slices.Clone(s.options.Members()), nil
}

func (s *Service) Read(ctx context.Context, identity Identity, since string) ([]channel.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	records, err := s.read(ctx, identity, since)
	if err != nil {
		return nil, err
	}
	if s.options.ReadOnly != nil && s.options.ReadOnly() {
		return records, nil
	}
	if err := s.acknowledge(context.WithoutCancel(ctx), identity.Member, records); err != nil {
		return nil, err
	}
	return records, nil
}

func (s *Service) read(ctx context.Context, identity Identity, since string) ([]channel.Record, error) {
	member, err := s.member(identity)
	if err != nil {
		return nil, err
	}
	records, err := s.options.Journal.ReadMessages(ctx, identity.Member, memberID(&member), since, 100)
	if err != nil {
		return nil, fmt.Errorf("read team channel: %w", err)
	}
	messages := make([]channel.Record, 0, len(records))
	for i := range records {
		if deliveryMessage(&records[i]) {
			messages = append(messages, records[i])
		}
	}
	return messages, nil
}

func (s *Service) Post(ctx context.Context, identity Identity, post Post, private bool) (channel.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	member, err := s.member(identity)
	if err != nil {
		return channel.Record{}, err
	}
	if member.Muted || (s.options.ReadOnly != nil && s.options.ReadOnly()) {
		return channel.Record{}, fmt.Errorf("channel posting is disabled")
	}
	if strings.TrimSpace(post.Text) == "" || len(post.Text) > s.options.MaxBytes {
		return channel.Record{}, fmt.Errorf("message must contain text and fit %d bytes", s.options.MaxBytes)
	}
	recipients, err := s.recipients(post, private)
	if err != nil {
		return channel.Record{}, err
	}
	r, err := s.message(ctx, identity, post, recipients, private)
	if err != nil {
		return channel.Record{}, err
	}
	if err := s.inheritHops(ctx, identity.Member, &r); err != nil {
		return channel.Record{}, err
	}
	s.bindAudience(&r)
	if !s.hopsAllowed(&r, member.Role) {
		return channel.Record{}, fmt.Errorf("thread hop limit reached; master authorization required")
	}
	now := s.options.Now()
	count, err := s.recent(ctx, identity.Member, now)
	if err != nil {
		return channel.Record{}, err
	}
	if count >= s.options.Rate {
		return channel.Record{}, fmt.Errorf("channel rate limit reached")
	}
	r.Time = now.UTC()
	if err := s.options.Journal.Append(ctx, &r); err != nil {
		return channel.Record{}, fmt.Errorf("post team channel: %w", err)
	}
	return r, nil
}

func (s *Service) recipients(post Post, private bool) ([]string, error) {
	recipients := slices.Clone(post.Mentions)
	if private {
		if post.Member == "" || len(post.Mentions) != 0 {
			return nil, fmt.Errorf("DM requires one member and no mentions")
		}
		recipients = []string{post.Member}
	} else if post.Member != "" {
		return nil, fmt.Errorf("channel posts use mentions, not a DM member")
	}
	names := make(map[string]bool)
	for _, member := range s.options.Members() {
		names[member.Name] = true
	}
	for _, name := range recipients {
		if !names[name] {
			return nil, fmt.Errorf("unknown recipient")
		}
	}
	slices.Sort(recipients)
	return slices.Compact(recipients), nil
}

func (s *Service) message(
	ctx context.Context, identity Identity, post Post, recipients []string, private bool,
) (channel.Record, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return channel.Record{}, fmt.Errorf("generate channel message id: %w", err)
	}
	id := hex.EncodeToString(b)
	r := channel.Record{
		ID: id, Kind: "message", Thread: id, Sender: identity.Member, Text: post.Text,
		Recipients: recipients, Private: private, Hops: 1,
	}
	if post.ReplyTo == "" {
		return r, nil
	}
	records, err := s.options.Journal.Snapshot(ctx)
	if err != nil {
		return channel.Record{}, fmt.Errorf("read parent messages: %w", err)
	}
	for i := range records {
		parent := &records[i]
		if parent.ID == post.ReplyTo && parent.Kind == "message" {
			member, err := s.member(identity)
			if err != nil || !recordVisible(parent, identity.Member, memberID(&member)) {
				return channel.Record{}, fmt.Errorf("unknown parent message")
			}
			if parent.Private && (!private || !sameParticipants(parent, &r)) {
				return channel.Record{}, fmt.Errorf("private replies must keep the same participants")
			}
			r.Thread, r.Hops = parent.Thread, parent.Hops+1
			return r, nil
		}
	}
	return channel.Record{}, fmt.Errorf("unknown parent message")
}

func sameParticipants(a, b *channel.Record) bool {
	left := append(slices.Clone(a.Recipients), a.Sender)
	right := append(slices.Clone(b.Recipients), b.Sender)
	slices.Sort(left)
	slices.Sort(right)
	return slices.Equal(slices.Compact(left), slices.Compact(right))
}

func (s *Service) recent(ctx context.Context, member string, now time.Time) (int, error) {
	records, err := s.options.Journal.Snapshot(ctx)
	if err != nil {
		return 0, fmt.Errorf("read channel rate state: %w", err)
	}
	count := 0
	for i := range records {
		r := &records[i]
		matches := r.Kind == "message" && r.Sender == member
		if member == "" {
			matches = r.Kind == "roster"
		}
		if matches && now.Sub(r.Time) < time.Minute {
			count++
		}
	}
	return count, nil
}
