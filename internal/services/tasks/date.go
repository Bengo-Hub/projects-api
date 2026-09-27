package tasks

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Date is a task date as clients send it: a plain calendar date ("2026-10-01", what an HTML date
// input produces) or a full RFC 3339 timestamp. Decoding a plain date straight into time.Time
// failed the whole request, so tasks could not be created with a due date from the UI.
type Date struct{ time.Time }

func (d *Date) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	s = strings.TrimSpace(s)
	if s == "" {
		d.Time = time.Time{}
		return nil
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			d.Time = t
			return nil
		}
	}
	return fmt.Errorf("invalid date %q: use YYYY-MM-DD or RFC 3339", s)
}

func (d Date) MarshalJSON() ([]byte, error) { return json.Marshal(d.Time) }
