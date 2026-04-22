package blackboard

import (
	"sort"
	"strings"
)

// Conflict describes a contested blackboard key where independent writers
// have published divergent payloads under the same key. Conflicts are
// never auto-resolved — they are surfaced to the supervisor so a human or
// policy-driven adjudicator can decide which version wins.
type Conflict struct {
	Key        string             `json:"key"`
	Namespaces []string           `json:"namespaces,omitempty"`
	TaskIDs    []string           `json:"taskIds,omitempty"`
	Exchanges  []ConflictExchange `json:"exchanges"`
	Reason     string             `json:"reason,omitempty"`
}

// ConflictExchange is a narrow view of the competing exchange — enough for
// the supervisor to reason about which to accept without replicating the
// full Exchange payload in every digest.
type ConflictExchange struct {
	ID        string `json:"id,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	TaskID    string `json:"taskId,omitempty"`
	Version   int    `json:"version,omitempty"`
	ETag      string `json:"etag,omitempty"`
	Excerpt   string `json:"excerpt,omitempty"`
}

const conflictExcerptMaxRunes = 160

// DetectConflicts scans exchanges for keys whose same (key, namespace) slot
// has been written by more than one task with non-matching text, or whose
// non-namespaced key has parallel writers from distinct tasks with divergent
// text. Conflicts are ordered by key for deterministic output so replays
// produce identical sequences.
//
// The detector is intentionally conservative: matching text across writers
// is *not* a conflict (redundant publication is fine), and an exchange
// overwriting its own earlier version via CAS is not a conflict either.
// What we flag is independent tasks racing to own the same key.
func (s *State) DetectConflicts() []Conflict {
	if s == nil || len(s.Exchanges) == 0 {
		return nil
	}
	type bucket struct {
		slot       string
		key        string
		namespaces map[string]struct{}
		tasks      map[string]struct{}
		entries    []ConflictExchange
		texts      map[string]struct{}
	}
	buckets := map[string]*bucket{}
	for _, ex := range s.Exchanges {
		key := strings.TrimSpace(ex.Key)
		if key == "" {
			continue
		}
		slot := conflictSlot(key, strings.TrimSpace(ex.Namespace))
		b, ok := buckets[slot]
		if !ok {
			b = &bucket{
				slot:       slot,
				key:        key,
				namespaces: map[string]struct{}{},
				tasks:      map[string]struct{}{},
				texts:      map[string]struct{}{},
			}
			buckets[slot] = b
		}
		if ex.Namespace != "" {
			b.namespaces[ex.Namespace] = struct{}{}
		}
		if ex.TaskID != "" {
			b.tasks[ex.TaskID] = struct{}{}
		}
		b.texts[normalizedConflictText(ex)] = struct{}{}
		b.entries = append(b.entries, ConflictExchange{
			ID:        ex.ID,
			Namespace: ex.Namespace,
			TaskID:    ex.TaskID,
			Version:   ex.Version,
			ETag:      ex.ETag,
			Excerpt:   truncateConflictText(ex.Text),
		})
	}

	conflicts := make([]Conflict, 0)
	for _, b := range buckets {
		if len(b.tasks) < 2 {
			continue
		}
		if len(b.texts) < 2 {
			continue
		}
		conflicts = append(conflicts, Conflict{
			Key:        b.key,
			Namespaces: sortedKeys(b.namespaces),
			TaskIDs:    sortedKeys(b.tasks),
			Exchanges:  b.entries,
			Reason:     "independent writers produced divergent exchanges for the same key",
		})
	}
	sort.Slice(conflicts, func(i, j int) bool {
		return conflicts[i].Key < conflicts[j].Key
	})
	for i := range conflicts {
		sort.Slice(conflicts[i].Exchanges, func(a, b int) bool {
			ea, eb := conflicts[i].Exchanges[a], conflicts[i].Exchanges[b]
			if ea.TaskID != eb.TaskID {
				return ea.TaskID < eb.TaskID
			}
			if ea.Version != eb.Version {
				return ea.Version < eb.Version
			}
			return ea.ID < eb.ID
		})
	}
	return conflicts
}

func conflictSlot(key, namespace string) string {
	return key + "\x00" + namespace
}

func normalizedConflictText(ex Exchange) string {
	text := strings.TrimSpace(ex.Text)
	if text != "" {
		return text
	}
	parts := make([]string, 0, len(ex.ArtifactIDs)+len(ex.ClaimIDs)+len(ex.FindingIDs))
	parts = append(parts, ex.ArtifactIDs...)
	parts = append(parts, ex.ClaimIDs...)
	parts = append(parts, ex.FindingIDs...)
	sort.Strings(parts)
	return strings.Join(parts, "|")
}

func truncateConflictText(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	runes := []rune(trimmed)
	if len(runes) <= conflictExcerptMaxRunes {
		return trimmed
	}
	return string(runes[:conflictExcerptMaxRunes]) + "…"
}

func sortedKeys(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
