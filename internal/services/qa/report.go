package qa

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Bengo-Hub/reports"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/ent/qadefect"
	"github.com/bengobox/projects-service/internal/ent/qainvite"
	"github.com/bengobox/projects-service/internal/qamodel"
)

// reportData is everything the report shows beyond the session's own frozen fields. It is stored
// on the session at close (metadata.report) and every later render uses it, so the PDF and its
// SHA-256 never change after close, whatever happens to tickets or invites afterwards.
type reportData struct {
	Outcome  Outcome      `json:"outcome"`
	Defects  []defectLine `json:"defects"`
	Signers  []signerLine `json:"signers"`
	ClosedAt time.Time    `json:"closed_at"`
}

type defectLine struct {
	CriterionTitle string `json:"criterion_title"`
	AreaCode       string `json:"area_code,omitempty"`
	Severity       int    `json:"severity"`
	Status         string `json:"status"`
	TicketNumber   string `json:"ticket_number,omitempty"`
}

// signerLine is a signer as the report and the frozen close data keep them: the name only, never
// an address (not even a masked one). A signer without a name gets a blank line to fill by hand.
type signerLine struct {
	Name string `json:"name,omitempty"`
}

// frozenReport reads the data stored at close.
func frozenReport(sess *ent.QASession) (reportData, bool) {
	raw, ok := sess.Metadata["report"]
	if !ok || raw == nil {
		return reportData{}, false
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return reportData{}, false
	}
	var d reportData
	if json.Unmarshal(b, &d) != nil || d.ClosedAt.IsZero() {
		return reportData{}, false
	}
	return d, true
}

// liveReportData gathers the outcome, defects and signers as they stand now.
func (s *Service) liveReportData(ctx context.Context, sess *ent.QASession) (reportData, error) {
	views, _, err := s.currentViews(ctx, sess.ID)
	if err != nil {
		return reportData{}, err
	}
	data := reportData{Outcome: Evaluate(sess.PassRule, sess.CriteriaSnapshot, views), Defects: []defectLine{}, Signers: []signerLine{}}
	defects, err := s.client.QADefect.Query().Where(qadefect.SessionID(sess.ID)).
		Order(ent.Asc(qadefect.FieldSeverity), ent.Asc(qadefect.FieldCreatedAt)).All(ctx)
	if err != nil {
		return reportData{}, err
	}
	for _, d := range defects {
		c, _ := findCriterion(sess, d.CriterionID)
		data.Defects = append(data.Defects, defectLine{CriterionTitle: c.Title, AreaCode: c.AreaCode, Severity: d.Severity, Status: d.Status, TicketNumber: d.TicketNumber})
	}
	signers, err := s.client.QAInvite.Query().
		Where(qainvite.SessionID(sess.ID), qainvite.Role(qamodel.RoleSigner), qainvite.StatusNEQ(qamodel.InviteRevoked)).
		Order(ent.Asc(qainvite.FieldCreatedAt)).All(ctx)
	if err != nil {
		return reportData{}, err
	}
	for _, inv := range signers {
		data.Signers = append(data.Signers, signerLine{Name: inv.Name})
	}
	return data, nil
}

// RenderReport renders a session's acceptance report. Before close it is a live preview; after
// close it is rebuilt from the frozen data and checked against the stored hash.
func (s *Service) RenderReport(ctx context.Context, sess *ent.QASession) ([]byte, string, error) {
	data, frozen := frozenReport(sess)
	if !frozen {
		var err error
		if data, err = s.liveReportData(ctx, sess); err != nil {
			return nil, "", err
		}
	}
	pdf, err := renderReport(sess, data)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(pdf)
	hash := hex.EncodeToString(sum[:])
	if frozen && sess.DocumentHash != "" && hash != sess.DocumentHash {
		s.log.Error("qa report no longer matches its stored hash", zap.String("session", sess.ID.String()))
	}
	return pdf, hash, nil
}

// ReportFor renders the report of a tenant's session.
func (s *Service) ReportFor(ctx context.Context, tenantID, id uuid.UUID) ([]byte, string, *ent.QASession, error) {
	sess, err := s.GetSession(ctx, tenantID, id)
	if err != nil {
		return nil, "", nil, err
	}
	pdf, hash, err := s.RenderReport(ctx, sess)
	return pdf, hash, sess, err
}

func renderReport(sess *ent.QASession, data reportData) ([]byte, error) {
	b, _, err := reports.Generate(buildReport(sess, data), reports.FormatPDF)
	if err != nil {
		return nil, fmt.Errorf("render qa report: %w", err)
	}
	return b, nil
}

var resultLabel = map[string]string{
	qamodel.ResultPass: "Pass", qamodel.ResultFail: "Fail", qamodel.ResultBlocked: "Blocked", qamodel.ResultNotRun: "Not run",
}

func sevText(p *int) string {
	if p == nil {
		return ""
	}
	return fmt.Sprintf("S%d", *p)
}

func ruleText(r qamodel.PassRule) string {
	parts := []string{}
	if r.RequireAllMust {
		parts = append(parts, "every must passes")
	}
	if len(r.BlockingSeverities) > 0 {
		sev := make([]string, len(r.BlockingSeverities))
		for i, v := range r.BlockingSeverities {
			sev[i] = fmt.Sprintf("S%d", v)
		}
		parts = append(parts, "no open "+strings.Join(sev, "/")+" failure")
	}
	if r.MinScore > 0 {
		parts = append(parts, fmt.Sprintf("score at least %.0f%%", r.MinScore))
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}

// buildReport maps a session to the shared report model. It reads only frozen session fields and
// data, never the session status, so a closed report renders the same after acceptance.
func buildReport(sess *ent.QASession, data reportData) *reports.Report {
	o := data.Outcome
	tenant := metaString(sess.Metadata, "tenant_name")
	if tenant == "" {
		tenant = metaString(sess.Metadata, "tenant_slug")
	}
	generated := data.ClosedAt.UTC()
	state := "Closed " + reports.DateTime(generated)
	if data.ClosedAt.IsZero() {
		generated = time.Now().UTC()
		state = "Preview, still open"
	}
	subject := sess.SubjectKind
	if sess.SubjectRef != "" {
		subject += ": " + sess.SubjectRef
	}
	meta := [][2]string{
		{"Template", fmt.Sprintf("%s (v%d)", metaString(sess.Metadata, "template_name"), sess.TemplateVersion)},
		{"Subject", subject},
		{"Pass rule", ruleText(sess.PassRule)},
		{"State", state},
	}
	if sess.OpenedAt != nil {
		meta = append(meta, [2]string{"Opened", reports.DateTime(sess.OpenedAt.UTC())})
	}
	verdict := "Not accepted"
	if o.Accepted {
		verdict = "Meets the pass rule"
	}
	r := &reports.Report{
		Title:       "Acceptance report",
		Subtitle:    sess.Title,
		TenantName:  tenant,
		GeneratedAt: generated,
		Meta:        meta,
		Footer:      "QA session " + sess.ID.String(),
		Cards: []reports.Card{
			{Label: "Weighted score", Value: fmt.Sprintf("%.1f%%", o.Score), Sub: verdict},
			{Label: "Passed", Value: fmt.Sprint(o.Counts[qamodel.ResultPass])},
			{Label: "Failed", Value: fmt.Sprint(o.Counts[qamodel.ResultFail])},
			{Label: "Not run or blocked", Value: fmt.Sprint(o.Counts[qamodel.ResultNotRun] + o.Counts[qamodel.ResultBlocked])},
		},
	}
	pairs := []reports.KV{
		{Label: "Outcome", Value: verdict, Bold: true, Rule: true},
		{Label: "Must criteria passed", Value: fmt.Sprintf("%d of %d", o.MustPassed, o.MustTotal)},
		{Label: "Open blocking failures", Value: fmt.Sprint(o.OpenBlocking)},
	}
	for _, why := range o.Reasons {
		pairs = append(pairs, reports.KV{Label: "Reason", Value: why})
	}
	r.Sections = append(r.Sections, reports.Section{Kind: reports.SectionKeyValue, Title: "Outcome", Pairs: pairs})

	// Results by area, in snapshot order.
	byID := map[uuid.UUID]CriterionOutcome{}
	for _, c := range o.Criteria {
		byID[c.ID] = c
	}
	var areas []string
	rows := map[string][][]reports.Cell{}
	for _, c := range sess.CriteriaSnapshot {
		area := c.AreaCode
		if area == "" {
			area = "General"
		}
		if _, ok := rows[area]; !ok {
			areas = append(areas, area)
		}
		co := byID[c.ID]
		status := co.Status
		if status == "" {
			status = qamodel.ResultNotRun
		}
		rows[area] = append(rows[area], []reports.Cell{
			reports.Text(c.RequirementRef), reports.Text(c.Title), reports.Text(c.Weight),
			reports.Text(resultLabel[status]), reports.Text(sevText(co.Severity)), reports.Text(fmt.Sprint(co.Testers)),
		})
	}
	cols := []reports.Column{
		{Header: "Ref", Weight: 1}, {Header: "Criterion", Weight: 4}, {Header: "Weight", Weight: 1},
		{Header: "Result", Weight: 1}, {Header: "Sev", Weight: 0.7, Align: "C"}, {Header: "Testers", Weight: 0.9, Align: "R"},
	}
	for _, a := range areas {
		r.Sections = append(r.Sections, reports.Section{Kind: reports.SectionTable, Title: "Results: " + a, Columns: cols, Rows: rows[a]})
	}

	if len(data.Defects) > 0 {
		drows := make([][]reports.Cell, len(data.Defects))
		for i, d := range data.Defects {
			drows[i] = []reports.Cell{reports.Text(fmt.Sprintf("S%d", d.Severity)), reports.Text(d.AreaCode), reports.Text(d.CriterionTitle), reports.Text(d.TicketNumber), reports.Text(d.Status)}
		}
		r.Sections = append(r.Sections, reports.Section{
			Kind: reports.SectionTable, Title: "Defects",
			Columns: []reports.Column{{Header: "Sev", Weight: 0.7}, {Header: "Area", Weight: 1}, {Header: "Criterion", Weight: 4}, {Header: "Ticket", Weight: 1.4}, {Header: "Status", Weight: 1.2}},
			Rows:    drows,
		})
	}

	sigs := make([]reports.Signature, 0, len(data.Signers))
	for _, sg := range data.Signers {
		sigs = append(sigs, reports.Signature{Role: "Signed for acceptance", Name: sg.Name, DateLabel: "Date"})
	}
	if len(sigs) == 0 {
		sigs = []reports.Signature{{Role: "Accepted by", DateLabel: "Date"}, {Role: "Witness", DateLabel: "Date"}}
	}
	r.Sections = append(r.Sections, reports.Section{
		Kind: reports.SectionSignature, Title: "Sign-off",
		Note:       "Print, sign and return a scan, or sign in the app once stored signatures are enabled.",
		Signatures: sigs,
	})
	return r
}
