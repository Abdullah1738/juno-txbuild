package txbuild

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Abdullah1738/juno-sdk-go/types"
)

func TestSignerCompatiblePlanRequiresCanonicalUniqueNoteIDs(t *testing.T) {
	txid := strings.Repeat("a", 64)
	otherTxID := strings.Repeat("b", 64)
	tests := []struct {
		name        string
		noteIDs     []string
		wantMessage string
	}{
		{name: "canonical zero and max uint32", noteIDs: []string{txid + ":0", otherTxID + ":4294967295"}},
		{name: "missing", noteIDs: []string{""}, wantMessage: "note_id required"},
		{name: "malformed txid", noteIDs: []string{"abc:0"}, wantMessage: "must match"},
		{name: "uppercase txid", noteIDs: []string{strings.Repeat("A", 64) + ":0"}, wantMessage: "must match"},
		{name: "leading zero action", noteIDs: []string{txid + ":01"}, wantMessage: "must match"},
		{name: "action overflow", noteIDs: []string{txid + ":4294967296"}, wantMessage: "fit uint32"},
		{name: "duplicate", noteIDs: []string{txid + ":0", txid + ":0"}, wantMessage: "duplicates"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := types.TxPlan{Notes: make([]types.OrchardSpendNote, len(tt.noteIDs))}
			for i, noteID := range tt.noteIDs {
				plan.Notes[i].NoteID = noteID
			}

			_, err := signerCompatiblePlan(plan, false)
			if tt.wantMessage == "" {
				if err != nil {
					t.Fatalf("canonical note IDs rejected: %v", err)
				}
				return
			}
			assertCodedError(t, err, types.ErrCodeInvalidRequest, tt.wantMessage)
		})
	}
}

func TestSignerCompatiblePlanAppliesNoteIDContractToEveryPlannerMode(t *testing.T) {
	tests := []struct {
		name      string
		kind      types.TxPlanKind
		outputs   int
		hasChange bool
	}{
		{name: "send", kind: types.TxPlanKindWithdrawal, outputs: 1, hasChange: true},
		{name: "send-many", kind: types.TxPlanKindWithdrawal, outputs: 2, hasChange: true},
		{name: "sweep", kind: types.TxPlanKindSweep, outputs: 1},
		{name: "consolidate", kind: types.TxPlanKindRebalance, outputs: 1},
		{name: "rebalance", kind: types.TxPlanKindRebalance, outputs: 2, hasChange: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := types.TxPlan{
				Kind:    tt.kind,
				Notes:   makePlanNotes(2),
				Outputs: make([]types.TxOutput, tt.outputs),
			}
			if _, err := signerCompatiblePlan(plan, tt.hasChange); err != nil {
				t.Fatalf("valid %s plan rejected: %v", tt.name, err)
			}

			plan.Notes[0].NoteID = ""
			_, err := signerCompatiblePlan(plan, tt.hasChange)
			assertCodedError(t, err, types.ErrCodeInvalidRequest, "notes[0].note_id required")
		})
	}
}

func TestTxPlanSchemaRequiresCanonicalNoteID(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "api", "txplan.v0.schema.json"))
	if err != nil {
		t.Fatalf("read txplan schema: %v", err)
	}

	var schema struct {
		Defs map[string]struct {
			Required   []string `json:"required"`
			Properties map[string]struct {
				Pattern     string `json:"pattern"`
				Description string `json:"description"`
			} `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("decode txplan schema: %v", err)
	}

	note, ok := schema.Defs["OrchardSpendNote"]
	if !ok {
		t.Fatal("schema missing OrchardSpendNote")
	}
	if !slices.Contains(note.Required, "note_id") {
		t.Fatalf("required fields %v omit note_id", note.Required)
	}
	if got := note.Properties["note_id"].Pattern; got != canonicalNoteIDPattern {
		t.Fatalf("note_id pattern=%q want %q", got, canonicalNoteIDPattern)
	}
	if description := note.Properties["note_id"].Description; !strings.Contains(description, "uint32") {
		t.Fatalf("note_id description does not state uint32 bound: %q", description)
	}
	if description := note.Properties["action_nullifier"].Description; !strings.Contains(description, "created this note") || !strings.Contains(description, "not the nullifier later derived") {
		t.Fatalf("action_nullifier description does not distinguish creating and spending contexts: %q", description)
	}
}

func makePlanNotes(count int) []types.OrchardSpendNote {
	notes := make([]types.OrchardSpendNote, count)
	for i := range notes {
		notes[i].NoteID = fmt.Sprintf("%064x:%d", i+1, i)
	}
	return notes
}
