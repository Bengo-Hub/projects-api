package tasks

import (
	"errors"
	"strings"
	"testing"
)

func ptr[T any](v T) *T { return &v }

func ids(items []ChecklistItem) string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.ID
	}
	return strings.Join(out, ",")
}

func TestApplyChecklistOp(t *testing.T) {
	base := []ChecklistItem{{ID: "a", Text: "Dig"}, {ID: "b", Text: "Pour"}, {ID: "c", Text: "Cure", Done: true}}
	gen := func() string { return "new" }
	cases := []struct {
		name    string
		op      ChecklistOp
		want    string
		changed bool
		check   func([]ChecklistItem) bool
		err     error
	}{
		{name: "add last with server id", op: ChecklistOp{Op: "add", Text: ptr(" Seal ")}, want: "a,b,c,new", changed: true,
			check: func(l []ChecklistItem) bool { return l[3].Text == "Seal" && !l[3].Done }},
		{name: "add at position", op: ChecklistOp{Op: "add", ItemID: "x", Text: ptr("Mark"), Position: ptr(1)}, want: "a,x,b,c", changed: true},
		{name: "add again is a no-op", op: ChecklistOp{Op: "add", ItemID: "b", Text: ptr("Pour")}, want: "a,b,c"},
		{name: "add without text", op: ChecklistOp{Op: "add", Text: ptr("  ")}, err: ValidationError("")},
		{name: "toggle flips", op: ChecklistOp{Op: "toggle", ItemID: "a"}, want: "a,b,c", changed: true,
			check: func(l []ChecklistItem) bool { return l[0].Done }},
		{name: "toggle to the same state", op: ChecklistOp{Op: "toggle", ItemID: "c", Done: ptr(true)}, want: "a,b,c"},
		{name: "toggle a removed item", op: ChecklistOp{Op: "toggle", ItemID: "z"}, err: ErrChecklistItemNotFound},
		{name: "rename", op: ChecklistOp{Op: "rename", ItemID: "b", Text: ptr("Pour slab")}, want: "a,b,c", changed: true,
			check: func(l []ChecklistItem) bool { return l[1].Text == "Pour slab" }},
		{name: "remove", op: ChecklistOp{Op: "remove", ItemID: "b"}, want: "a,c", changed: true},
		{name: "remove a missing item", op: ChecklistOp{Op: "remove", ItemID: "z"}, want: "a,b,c"},
		{name: "reorder to the front", op: ChecklistOp{Op: "reorder", ItemID: "c", Position: ptr(0)}, want: "c,a,b", changed: true},
		{name: "reorder past the end", op: ChecklistOp{Op: "reorder", ItemID: "a", Position: ptr(9)}, want: "b,c,a", changed: true},
		{name: "reorder in place", op: ChecklistOp{Op: "reorder", ItemID: "b", Position: ptr(1)}, want: "a,b,c"},
		{name: "reorder without position", op: ChecklistOp{Op: "reorder", ItemID: "b"}, err: ValidationError("")},
		{name: "bad item id", op: ChecklistOp{Op: "toggle", ItemID: "a b"}, err: ValidationError("")},
		{name: "unknown op", op: ChecklistOp{Op: "replace", ItemID: "a"}, err: ValidationError("")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := ids(base)
			got, changed, err := applyChecklistOp(base, tc.op, gen)
			if ids(base) != before || base[0].Done {
				t.Fatal("the input checklist was modified")
			}
			if tc.err != nil {
				var verr ValidationError
				if _, isV := tc.err.(ValidationError); isV && !errors.As(err, &verr) {
					t.Fatalf("err = %v, want a validation error", err)
				}
				if !errors.As(tc.err, &verr) && !errors.Is(err, tc.err) {
					t.Fatalf("err = %v, want %v", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if ids(got) != tc.want || changed != tc.changed {
				t.Errorf("got %s changed=%v, want %s changed=%v", ids(got), changed, tc.want, tc.changed)
			}
			if tc.check != nil && !tc.check(got) {
				t.Errorf("items = %+v", got)
			}
		})
	}
}

func TestReadChecklistSkipsMalformed(t *testing.T) {
	got := readChecklist(map[string]any{MetaChecklist: []any{
		map[string]any{"id": "a", "text": "Dig", "done": true},
		map[string]any{"id": "a", "text": "again"},
		map[string]any{"text": "no id"},
		"junk",
	}})
	if len(got) != 1 || got[0].ID != "a" || !got[0].Done {
		t.Errorf("got %+v", got)
	}
}
