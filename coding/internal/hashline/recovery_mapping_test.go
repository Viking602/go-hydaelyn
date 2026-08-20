package hashline

import (
	"math/rand/v2"
	"strconv"
	"strings"
	"testing"
)

func TestRemapSectionToLive(t *testing.T) {
	tests := []struct {
		name         string
		base         string
		live         string
		ops          []Op
		wantOK       bool
		wantStart    int
		wantEnd      int
		wantOffset   int
		wantAnchored bool
	}{
		{
			name:         "insertion before unique target",
			base:         "a\nb\nc\n",
			live:         "new\na\nb\nc\n",
			ops:          []Op{{Kind: OpReplace, Start: 2, End: 2, Body: []string{"B"}}},
			wantOK:       true,
			wantStart:    3,
			wantEnd:      3,
			wantOffset:   1,
			wantAnchored: true,
		},
		{
			name:         "deletion before unique target",
			base:         "a\nb\nc\nd\n",
			live:         "a\nc\nd\n",
			ops:          []Op{{Kind: OpReplace, Start: 4, End: 4, Body: []string{"D"}}},
			wantOK:       true,
			wantStart:    3,
			wantEnd:      3,
			wantOffset:   -1,
			wantAnchored: true,
		},
		{
			name:         "duplicate anchor with unique surrounding context",
			base:         "func first() {\n}\nfunc second() {\n}\n",
			live:         "// generated\nfunc first() {\n}\nfunc second() {\n}\n",
			ops:          []Op{{Kind: OpReplace, Start: 4, End: 4, Body: []string{"// changed"}}},
			wantOK:       true,
			wantStart:    5,
			wantEnd:      5,
			wantOffset:   1,
			wantAnchored: true,
		},
		{
			name:   "changed target",
			base:   "a\nb\nc\n",
			live:   "a\nB-from-disk\nc\n",
			ops:    []Op{{Kind: OpReplace, Start: 2, End: 2, Body: []string{"B-from-model"}}},
			wantOK: false,
		},
		{
			name:   "changed target copied later is not mistaken for a move",
			base:   "A\nX\nB\n",
			live:   "A\nY\nB\nA\nX\nB\n",
			ops:    []Op{{Kind: OpReplace, Start: 2, End: 2, Body: []string{"Z"}}},
			wantOK: false,
		},
		{
			name:   "changed edge target copied later has insufficient context",
			base:   "X\nB\n",
			live:   "Y\nB\nX\nB\n",
			ops:    []Op{{Kind: OpReplace, Start: 1, End: 1, Body: []string{"Z"}}},
			wantOK: false,
		},
		{
			name:         "unchanged original target wins over copied block",
			base:         "A\nX\nB\n",
			live:         "Y\nX\nB\nA\nX\nB\n",
			ops:          []Op{{Kind: OpReplace, Start: 2, End: 2, Body: []string{"Z"}}},
			wantOK:       true,
			wantStart:    2,
			wantEnd:      2,
			wantOffset:   0,
			wantAnchored: true,
		},
		{
			name: "anchor runs moved by different offsets",
			base: "a\nb\nc\nd\ne\n",
			live: "x\na\nb\nc\ny\nd\ne\n",
			ops: []Op{
				{Kind: OpReplace, Start: 2, End: 2, Body: []string{"B"}},
				{Kind: OpReplace, Start: 5, End: 5, Body: []string{"E"}},
			},
			wantOK: false,
		},
		{
			name:   "repeated duplicate context is ambiguous",
			base:   "start\n}\nend\nstart\n}\nend\n",
			live:   "prefix\nstart\n}\nend\nstart\n}\nend\n",
			ops:    []Op{{Kind: OpReplace, Start: 2, End: 2, Body: []string{"changed"}}},
			wantOK: false,
		},
		{
			name:         "head operation needs no anchors",
			base:         "a\nb\n",
			live:         "A\nb\n",
			ops:          []Op{{Kind: OpInsertHead, Body: []string{"notice"}}},
			wantOK:       true,
			wantAnchored: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := remapSectionToLive(tt.base, tt.live, Section{Path: "f.go", Tag: "ABCD", Ops: tt.ops})
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v; result=%+v", ok, tt.wantOK, got)
			}
			if !ok {
				return
			}
			if got.anchored != tt.wantAnchored {
				t.Fatalf("anchored = %v, want %v", got.anchored, tt.wantAnchored)
			}
			if !tt.wantAnchored {
				return
			}
			if got.offset != tt.wantOffset {
				t.Errorf("offset = %d, want %d", got.offset, tt.wantOffset)
			}
			if len(got.section.Ops) != 1 && len(tt.ops) == 1 {
				t.Fatalf("mapped ops = %+v", got.section.Ops)
			}
			if len(tt.ops) == 1 {
				if got.section.Ops[0].Start != tt.wantStart || got.section.Ops[0].End != tt.wantEnd {
					t.Errorf("mapped range = %d..%d, want %d..%d", got.section.Ops[0].Start, got.section.Ops[0].End, tt.wantStart, tt.wantEnd)
				}
			}
		})
	}
}

func TestRemapSectionToLive_ExcessiveDriftFailsClosed(t *testing.T) {
	base := "anchor\ntarget\ncontext\n"
	live := strings.Repeat("inserted\n", maxRecoveryEditDistance+1) + base
	_, ok := remapSectionToLive(base, live, Section{
		Path: "f.go",
		Tag:  "ABCD",
		Ops:  []Op{{Kind: OpReplace, Start: 2, End: 2, Body: []string{"changed"}}},
	})
	if ok {
		t.Fatal("recovery beyond the edit-distance budget must fail closed")
	}

	live = base + strings.Repeat("inserted\n", maxRecoveryEditDistance+1)
	_, ok = remapSectionToLive(base, live, Section{
		Path: "f.go",
		Tag:  "ABCD",
		Ops:  []Op{{Kind: OpReplace, Start: 2, End: 2, Body: []string{"changed"}}},
	})
	if ok {
		t.Fatal("same-position anchors must not bypass the edit-distance budget")
	}
}

func TestUnchangedLineMap_InsertOnlyFuzz(t *testing.T) {
	rng := rand.New(rand.NewPCG(0xA11CE, 0xC0FFEE))
	for iteration := 0; iteration < 2000; iteration++ {
		lineCount := 1 + rng.IntN(40)
		base := make([]string, lineCount)
		live := make([]string, 0, lineCount+16)
		for line := 0; line < lineCount; line++ {
			if rng.IntN(4) == 0 {
				live = append(live, "insert-"+strconv.Itoa(iteration)+"-"+strconv.Itoa(line))
			}
			base[line] = "base-" + strconv.Itoa(iteration) + "-" + strconv.Itoa(line)
			live = append(live, base[line])
		}
		if rng.IntN(2) == 0 {
			live = append(live, "tail-"+strconv.Itoa(iteration))
		}

		mapped, ok := unchangedLineMap(base, live)
		if !ok {
			t.Fatalf("iteration %d: insert-only drift should map", iteration)
		}
		previous := 0
		for line := 1; line <= len(base); line++ {
			liveLine, exists := mapped[line]
			if !exists {
				t.Fatalf("iteration %d: base line %d was not mapped", iteration, line)
			}
			if liveLine <= previous {
				t.Fatalf("iteration %d: map is not monotonic at base line %d: %d <= %d", iteration, line, liveLine, previous)
			}
			if base[line-1] != live[liveLine-1] {
				t.Fatalf("iteration %d: mapped unequal lines %q and %q", iteration, base[line-1], live[liveLine-1])
			}
			previous = liveLine
		}
	}
}

func TestUnchangedLineMap_RandomEditsMapEveryRetainedUniqueLine(t *testing.T) {
	rng := rand.New(rand.NewPCG(0x5AFE, 0xA11CE))
	for iteration := 0; iteration < 2000; iteration++ {
		lineCount := 1 + rng.IntN(40)
		base := make([]string, lineCount)
		live := make([]string, 0, lineCount+16)
		want := make(map[int]int, lineCount)
		for line := 0; line < lineCount; line++ {
			base[line] = "base-" + strconv.Itoa(iteration) + "-" + strconv.Itoa(line)
			if rng.IntN(5) == 0 {
				live = append(live, "insert-"+strconv.Itoa(iteration)+"-"+strconv.Itoa(line))
			}
			switch rng.IntN(6) {
			case 0:
				// Delete this base line.
			case 1:
				live = append(live, "replace-"+strconv.Itoa(iteration)+"-"+strconv.Itoa(line))
			default:
				live = append(live, base[line])
				want[line+1] = len(live)
			}
		}

		mapped, ok := unchangedLineMap(base, live)
		if !ok {
			t.Fatalf("iteration %d: bounded random drift should map", iteration)
		}
		if len(mapped) != len(want) {
			t.Fatalf("iteration %d: mapped %d retained lines, want %d", iteration, len(mapped), len(want))
		}
		for baseLine, liveLine := range want {
			if got, exists := mapped[baseLine]; !exists || got != liveLine {
				t.Fatalf("iteration %d: base line %d mapped to %d (exists=%t), want %d", iteration, baseLine, got, exists, liveLine)
			}
		}
	}
}
