package qa

import (
	"bytes"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/qamodel"
)

// TestFrozenReportStable guards the stored document hash: the report rendered at close and the one
// rebuilt later from the stored metadata must be the same bytes.
func TestFrozenReportStable(t *testing.T) {
	c1 := qamodel.SnapshotCriterion{ID: uuid.New(), AreaCode: "OWN", Title: "Owner can register", Weight: qamodel.WeightMust, RequirementRef: "FR-01"}
	c2 := qamodel.SnapshotCriterion{ID: uuid.New(), AreaCode: "REG", Title: "Registry search works", Weight: qamodel.WeightShould}
	opened := time.Date(2026, 11, 13, 8, 0, 0, 0, time.UTC)
	sess := &ent.QASession{
		ID: uuid.New(), Title: "Hadia release 1", SubjectKind: qamodel.SessionSubjectService, SubjectRef: "hadia",
		TemplateVersion: 3, CriteriaSnapshot: []qamodel.SnapshotCriterion{c1, c2}, PassRule: qamodel.DefaultPassRule(),
		OpenedAt: &opened, Metadata: map[string]any{"tenant_name": "Codevertex", "template_name": "Hadia acceptance plan"},
	}
	data := reportData{
		Outcome: Evaluate(sess.PassRule, sess.CriteriaSnapshot, []ResultView{
			{CriterionID: c1.ID, Result: qamodel.ResultPass},
			{CriterionID: c2.ID, Result: qamodel.ResultFail, Severity: sev(2)},
		}),
		Defects:  []defectLine{{CriterionTitle: c2.Title, AreaCode: "REG", Severity: 2, Status: qamodel.DefectTicketed, TicketNumber: "TKT-1"}},
		Signers:  []signerLine{{Name: "Product owner"}, {}},
		ClosedAt: time.Date(2026, 11, 13, 10, 30, 0, 0, time.UTC),
	}
	first, err := renderReport(sess, data)
	if err != nil || !bytes.HasPrefix(first, []byte("%PDF")) {
		t.Fatalf("render: %v", err)
	}
	stored, err := toMap(data)
	if err != nil {
		t.Fatal(err)
	}
	sess.Metadata["report"] = stored
	again, ok := frozenReport(sess)
	if !ok {
		t.Fatal("frozen report not read back")
	}
	second, err := renderReport(sess, again)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("the report rebuilt from stored data differs from the one hashed at close")
	}
	// Accepting the session afterwards must not change the document.
	sess.Status = qamodel.SessionAccepted
	if third, _ := renderReport(sess, again); !bytes.Equal(first, third) {
		t.Fatal("the session status leaked into the frozen report")
	}
}
