package tasks

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/ent"
	enttask "github.com/bengobox/projects-service/internal/ent/task"
	"github.com/bengobox/projects-service/internal/platform/realtime"
	"github.com/bengobox/projects-service/internal/services/activity"
	"github.com/bengobox/projects-service/internal/services/txn"
)

// The task checklist lives in task metadata under "checklist" as [{id, text, done}] in display
// order. It changes only through item operations applied under the task row lock, so two people
// ticking different items at once both keep their tick, and an offline replay with the same
// Idempotency-Key (kept under checklist_mutations like the status keys) applies once.
const (
	MetaChecklist          = "checklist"
	metaChecklistMutations = "checklist_mutations"
	maxChecklistItems      = 200
	maxChecklistText       = 500
)

// Checklist operations.
const (
	ChecklistAdd     = "add"
	ChecklistToggle  = "toggle"
	ChecklistRename  = "rename"
	ChecklistRemove  = "remove"
	ChecklistReorder = "reorder"
)

// ErrChecklistItemNotFound is an operation on an item the checklist no longer has (someone
// removed it meanwhile), answered 404.
var ErrChecklistItemNotFound = errors.New("checklist item not found")

var checklistItemID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// ChecklistItem is one line of a task's checklist.
type ChecklistItem struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	Done bool   `json:"done"`
}

// ChecklistOp is one change to a checklist. add takes text, an optional client item_id (so an
// offline add replays onto the same item) and an optional position (default last); toggle takes
// item_id and done (absent flips it); rename takes item_id and text; remove takes item_id (a
// missing item is already removed); reorder takes item_id and the 0-based position to move to.
type ChecklistOp struct {
	Op       string  `json:"op"`
	ItemID   string  `json:"item_id"`
	Text     *string `json:"text"`
	Done     *bool   `json:"done"`
	Position *int    `json:"position"`
}

// readChecklist reads the stored checklist, skipping malformed and repeated items.
func readChecklist(meta map[string]any) []ChecklistItem {
	var raw []any
	switch v := meta[MetaChecklist].(type) {
	case []any:
		raw = v
	case []ChecklistItem:
		return append([]ChecklistItem(nil), v...)
	}
	out := make([]ChecklistItem, 0, len(raw))
	seen := map[string]bool{}
	for _, r := range raw {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		id, _ := m["id"].(string)
		text, _ := m["text"].(string)
		done, _ := m["done"].(bool)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, ChecklistItem{ID: id, Text: text, Done: done})
	}
	return out
}

func checklistText(p *string) (string, error) {
	if p == nil || strings.TrimSpace(*p) == "" {
		return "", ErrValidation("text is required")
	}
	text := strings.TrimSpace(*p)
	if len([]rune(text)) > maxChecklistText {
		return "", ErrValidation(fmt.Sprintf("a checklist item is at most %d characters", maxChecklistText))
	}
	return text, nil
}

func clampPosition(p *int, n int) int {
	if p == nil || *p > n {
		return n
	}
	if *p < 0 {
		return 0
	}
	return *p
}

func indexOfItem(items []ChecklistItem, id string) int {
	for i, it := range items {
		if it.ID == id {
			return i
		}
	}
	return -1
}

// applyChecklistOp returns the checklist after op and whether it changed. newID names an added
// item that came without one.
func applyChecklistOp(items []ChecklistItem, op ChecklistOp, newID func() string) ([]ChecklistItem, bool, error) {
	id := strings.TrimSpace(op.ItemID)
	if id != "" && !checklistItemID.MatchString(id) {
		return nil, false, ErrValidation("item_id is at most 64 letters, digits, dashes or underscores")
	}
	if op.Op != ChecklistAdd && id == "" {
		return nil, false, ErrValidation("item_id is required")
	}
	at := indexOfItem(items, id)
	out := append([]ChecklistItem(nil), items...)
	switch op.Op {
	case ChecklistAdd:
		text, err := checklistText(op.Text)
		if err != nil {
			return nil, false, err
		}
		if id == "" {
			id = newID()
		} else if at >= 0 {
			return out, false, nil // already added (a replay without its key)
		}
		if len(out) >= maxChecklistItems {
			return nil, false, ErrValidation(fmt.Sprintf("a checklist has at most %d items", maxChecklistItems))
		}
		pos := clampPosition(op.Position, len(out))
		out = append(out[:pos], append([]ChecklistItem{{ID: id, Text: text}}, out[pos:]...)...)
		return out, true, nil
	case ChecklistRemove:
		if at < 0 {
			return out, false, nil
		}
		return append(out[:at], out[at+1:]...), true, nil
	case ChecklistToggle, ChecklistRename, ChecklistReorder:
		if at < 0 {
			return nil, false, ErrChecklistItemNotFound
		}
	default:
		return nil, false, ErrValidation("op must be add, toggle, rename, remove or reorder")
	}
	switch op.Op {
	case ChecklistToggle:
		done := !out[at].Done
		if op.Done != nil {
			done = *op.Done
		}
		if done == out[at].Done {
			return out, false, nil
		}
		out[at].Done = done
	case ChecklistRename:
		text, err := checklistText(op.Text)
		if err != nil {
			return nil, false, err
		}
		if text == out[at].Text {
			return out, false, nil
		}
		out[at].Text = text
	case ChecklistReorder:
		if op.Position == nil {
			return nil, false, ErrValidation("position is required")
		}
		item := out[at]
		out = append(out[:at], out[at+1:]...)
		pos := clampPosition(op.Position, len(out))
		if pos == at {
			return items, false, nil
		}
		out = append(out[:pos], append([]ChecklistItem{item}, out[pos:]...)...)
	}
	return out, true, nil
}

// ApplyChecklist applies one checklist operation to a task under its row lock. key is the
// optional Idempotency-Key: a key already applied to this task within the window changes nothing
// and replayed is set. The task comes back with its assignees.
func (s *Service) ApplyChecklist(ctx context.Context, tenantID, projectID, taskID uuid.UUID, op ChecklistOp, key string) (task *ent.Task, replayed bool, err error) {
	key = strings.TrimSpace(key)
	if len(key) > maxKeyLength {
		return nil, false, ErrValidation(fmt.Sprintf("Idempotency-Key is at most %d characters", maxKeyLength))
	}
	changed := false
	err = txn.Run(ctx, s.client, func(tx *ent.Tx) error {
		if err := lockTask(ctx, tx, tenantID, projectID, taskID); err != nil {
			return err
		}
		t, err := tx.Task.Query().Where(enttask.ID(taskID)).Only(ctx)
		if err != nil {
			return fmt.Errorf("fetch task: %w", err)
		}
		now := time.Now().UTC()
		keys := recentMutations(t.Metadata, metaChecklistMutations, now)
		if key != "" {
			if _, seen := keys[key]; seen {
				replayed = true
				task = t
				return loadAssignees(ctx, tx, task)
			}
		}
		next, didChange, err := applyChecklistOp(readChecklist(t.Metadata), op, uuid.NewString)
		if err != nil {
			return err
		}
		changed = didChange
		if !changed && key == "" {
			task = t
			return loadAssignees(ctx, tx, task)
		}
		meta := make(map[string]any, len(t.Metadata)+2)
		for k, v := range t.Metadata {
			meta[k] = v
		}
		if changed {
			meta[MetaChecklist] = next
		}
		if key != "" {
			keys[key] = now
			meta[metaChecklistMutations] = keepNewest(keys, maxKeptMutations)
		}
		if task, err = t.Update().SetMetadata(meta).Save(ctx); err != nil {
			return fmt.Errorf("save checklist: %w", err)
		}
		if err := loadAssignees(ctx, tx, task); err != nil {
			return err
		}
		if !changed {
			return nil
		}
		return activity.Record(ctx, tx, activity.Entry{
			TenantID: tenantID, ProjectID: activity.Ref(projectID), TaskID: activity.Ref(taskID),
			EntityType: "task", EntityID: taskID, Action: activity.ActionUpdated,
			Changes: map[string]any{"checklist": op.Op},
		})
	})
	if err != nil {
		return nil, false, err
	}
	if changed {
		s.emit(ctx, tenantID, realtime.TaskUpdated, task)
	}
	return task, replayed, nil
}
