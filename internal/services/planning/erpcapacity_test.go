package planning

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/platform/erp"
)

// fakeCapacity stands in for erp-api's capacity read, counting the people it is asked about.
type fakeCapacity struct {
	answers map[uuid.UUID]erp.Capacity
	asked   [][]uuid.UUID
	fail    bool
}

func (f *fakeCapacity) Enabled() bool { return true }

func (f *fakeCapacity) Capacity(_ context.Context, _ uuid.UUID, ids []uuid.UUID, _, _ time.Time) (map[uuid.UUID]erp.Capacity, error) {
	f.asked = append(f.asked, ids)
	if f.fail {
		return nil, errors.New("erp down")
	}
	out := map[uuid.UUID]erp.Capacity{}
	for _, id := range ids {
		if c, ok := f.answers[id]; ok {
			out[id] = c
		}
	}
	return out, nil
}

// A person ERP holds hours for takes ERP's weekly figure and each week's own hours (leave off),
// with the load recomputed; everyone else keeps the setting; the grid says mixed.
func TestApplyERPCapacity(t *testing.T) {
	ana, ben := uuid.New(), uuid.New()
	start := d("2026-11-16")
	rows := []DemandRow{
		{UserID: &ana, Week: d("2026-11-16"), Hours: 20, Tasks: 2},
		{UserID: &ana, Week: d("2026-11-23"), Hours: 20, Tasks: 1},
		{UserID: &ben, Week: d("2026-11-16"), Hours: 20, Tasks: 1},
	}
	settings := WorkloadSettings{WeeklyHours: 40, PersonHours: map[uuid.UUID]float64{}, CapacitySource: "settings"}
	w := BuildWorkload(rows, []uuid.UUID{ana, ben}, start, 2, settings, true)
	ApplyERPCapacity(&w, map[uuid.UUID]erp.Capacity{
		ana: {AuthUserID: ana, Configured: true, WorkShift: "Office", WeeklyHours: 40,
			Weeks: []erp.CapacityWeek{{WeekStart: "2026-11-16", Hours: 16}}}, // the second week falls back to 40
		ben: {AuthUserID: ben, HasEmployee: true}, // an employee without a shift keeps the setting
	})
	a, b := w.People[0], w.People[1]
	if a.CapacitySource != "erp" || a.WorkShift != "Office" || a.WeeklyCapacity != 40 {
		t.Fatalf("ana: %+v", a)
	}
	if a.Weeks[0].Capacity != 16 || a.Weeks[0].LoadPct != 125 || !a.Weeks[0].Overloaded ||
		a.Weeks[1].Capacity != 40 || a.Weeks[1].LoadPct != 50 || a.Weeks[1].Overloaded || a.OverloadedWeeks != 1 {
		t.Fatalf("ana weeks: %+v (%d overloaded)", a.Weeks, a.OverloadedWeeks)
	}
	if b.CapacitySource != "settings" || b.WeeklyCapacity != 40 || b.Weeks[0].LoadPct != 50 {
		t.Fatalf("ben: %+v", b)
	}
	if w.CapacitySource != "mixed" {
		t.Fatalf("grid source %q, want mixed", w.CapacitySource)
	}

	all := BuildWorkload(rows, []uuid.UUID{ana}, start, 2, settings, true)
	ApplyERPCapacity(&all, map[uuid.UUID]erp.Capacity{ana: {Configured: true, WeeklyHours: 0}})
	if all.CapacitySource != "erp" || !all.People[0].Weeks[0].Overloaded || all.People[0].Weeks[0].LoadPct != 100 {
		t.Fatalf("zero ERP hours with demand is overloaded and the grid is erp: %+v", all)
	}
	none := BuildWorkload(rows, []uuid.UUID{ana}, start, 2, settings, true)
	ApplyERPCapacity(&none, nil)
	if none.CapacitySource != "settings" || none.People[0].CapacitySource != "settings" {
		t.Fatalf("no ERP answer keeps the settings: %+v", none)
	}
}

// ERP answers are cached per person and range: a second read asks ERP only about people it has
// not answered yet; a failed read keeps what the cache had and reports the error.
func TestERPCapacityCache(t *testing.T) {
	tenant, ana, ben := uuid.New(), uuid.New(), uuid.New()
	f := &fakeCapacity{answers: map[uuid.UUID]erp.Capacity{ana: {Configured: true, WeeklyHours: 30}}}
	s := (&Service{log: zap.NewNop()}).WithERP(f)
	start, end := d("2026-11-16"), d("2026-11-29")

	got, err := s.erpCapacity(context.Background(), tenant, []uuid.UUID{ana}, start, end)
	if err != nil || got[ana].WeeklyHours != 30 || len(f.asked) != 1 {
		t.Fatalf("first read: %+v %v (%d calls)", got, err, len(f.asked))
	}
	got, err = s.erpCapacity(context.Background(), tenant, []uuid.UUID{ana, ben}, start, end)
	if err != nil || len(f.asked) != 2 || len(f.asked[1]) != 1 || f.asked[1][0] != ben || got[ben].Configured || got[ben].AuthUserID != ben {
		t.Fatalf("second read asked %v: %+v %v", f.asked, got, err)
	}
	if _, err := s.erpCapacity(context.Background(), tenant, []uuid.UUID{ana, ben}, start, end); err != nil || len(f.asked) != 2 {
		t.Fatalf("a fully cached read must not call ERP: %v (%d calls)", err, len(f.asked))
	}
	f.fail = true
	got, err = s.erpCapacity(context.Background(), tenant, []uuid.UUID{ana, uuid.New()}, start, end)
	if err == nil || got[ana].WeeklyHours != 30 {
		t.Fatalf("failed read: %+v %v", got, err)
	}
	if got, err := (&Service{log: zap.NewNop()}).erpCapacity(context.Background(), tenant, []uuid.UUID{ana}, start, end); err != nil || len(got) != 0 {
		t.Fatalf("without ERP the settings apply: %v %v", got, err)
	}
}
