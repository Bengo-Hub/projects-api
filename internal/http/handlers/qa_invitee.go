package handlers

import (
	"net/http"
	"strings"
	"time"

	authclient "github.com/Bengo-Hub/shared-auth-client"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/qamodel"
	"github.com/bengobox/projects-service/internal/services/qa"
)

// Invitee access: the people a session owner invited by email, who may have no account in the
// tenant. They open /qa/{code} in projects-ui, prove their email (signed in through SSO with the
// same email, or with auth-api's email code), and get a short access token bound to that one
// session, sent as an HttpOnly cookie and also returned for clients that cannot keep cookies
// (X-QA-Access header).
const (
	qaAccessCookie = "qa_access"
	qaAccessHeader = "X-QA-Access"
	qaCookiePath   = "/api/v1/qa"
)

// QAInviteeHandler serves the public /api/v1/qa routes (outside the tenant JWT group).
type QAInviteeHandler struct {
	log          *zap.Logger
	svc          *qa.Service
	cookieSecure bool
}

func NewQAInviteeHandler(log *zap.Logger, svc *qa.Service, cookieSecure bool) *QAInviteeHandler {
	return &QAInviteeHandler{log: log.Named("qa.invitee"), svc: svc, cookieSecure: cookieSecure}
}

// RegisterRoutes mounts the invitee routes on the root router. requireAuth wraps the SSO route;
// nil leaves that route out.
func (h *QAInviteeHandler) RegisterRoutes(r chi.Router, requireAuth func(http.Handler) http.Handler) {
	r.Get("/api/v1/qa/access/{code}", h.AccessInfo)
	r.Post("/api/v1/qa/access/{code}/email", h.AccessByEmailCode)
	if requireAuth != nil {
		r.With(requireAuth).Post("/api/v1/qa/access/{code}/sso", h.AccessBySSO)
	}
	r.Get("/api/v1/qa/invitee/session", h.Session)
	r.Put("/api/v1/qa/invitee/results/{criterionID}", h.RecordResult)
	r.Get("/api/v1/qa/invitee/report.pdf", h.Report)
	r.Post("/api/v1/qa/invitee/logout", h.Logout)
}

func (h *QAInviteeHandler) AccessInfo(w http.ResponseWriter, r *http.Request) {
	info, err := h.svc.LookupShareCode(r.Context(), chi.URLParam(r, "code"))
	if err != nil {
		qaError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, info)
}

// AccessByEmailCode takes {"otp_token"} from auth-api's /auth/public-otp/verify.
func (h *QAInviteeHandler) AccessByEmailCode(w http.ResponseWriter, r *http.Request) {
	var in struct {
		OTPToken string `json:"otp_token"`
	}
	if !decodeBody(w, r, 4096, &in) {
		return
	}
	g, err := h.svc.GrantByOTP(r.Context(), chi.URLParam(r, "code"), in.OTPToken)
	h.grant(w, g, err)
}

// AccessBySSO lets in a signed-in user whose token email holds an invite.
func (h *QAInviteeHandler) AccessBySSO(w http.ResponseWriter, r *http.Request) {
	claims, ok := authclient.ClaimsFromContext(r.Context())
	if !ok || claims == nil || claims.Email == "" || claims.IsService {
		qaError(w, qa.ErrAccessDenied)
		return
	}
	g, err := h.svc.GrantByEmail(r.Context(), chi.URLParam(r, "code"), claims.Email)
	h.grant(w, g, err)
}

func (h *QAInviteeHandler) grant(w http.ResponseWriter, g *qa.AccessGrant, err error) {
	if err != nil {
		qaError(w, err)
		return
	}
	h.setCookie(w, g.Token, g.ExpiresAt)
	respondJSON(w, http.StatusOK, g)
}

func (h *QAInviteeHandler) setCookie(w http.ResponseWriter, value string, exp time.Time) {
	c := &http.Cookie{
		Name: qaAccessCookie, Value: value, Path: qaCookiePath, Expires: exp,
		HttpOnly: true, Secure: h.cookieSecure, SameSite: http.SameSiteLaxMode,
	}
	if h.cookieSecure {
		// projects-ui and the API sit on different hosts, so the cookie must travel cross-site.
		c.SameSite = http.SameSiteNoneMode
	}
	if value == "" {
		c.MaxAge = -1
	}
	http.SetCookie(w, c)
}

// invitee resolves the access token from the header or the cookie.
func (h *QAInviteeHandler) invitee(w http.ResponseWriter, r *http.Request) (*qa.Invitee, bool) {
	tok := strings.TrimSpace(r.Header.Get(qaAccessHeader))
	if tok == "" {
		if c, err := r.Cookie(qaAccessCookie); err == nil {
			tok = c.Value
		}
	}
	if tok == "" {
		respondError(w, http.StatusUnauthorized, "sign in with your invited email first")
		return nil, false
	}
	inv, err := h.svc.ResolveAccess(r.Context(), tok)
	if err != nil {
		respondError(w, http.StatusUnauthorized, "your access has ended; sign in again")
		return nil, false
	}
	return inv, true
}

// inviteeSession is what an invitee sees: the frozen criteria, their own current verdicts and
// their role. Other testers' identities are not shown.
type inviteeSession struct {
	SessionID uuid.UUID                   `json:"session_id"`
	Title     string                      `json:"title"`
	Status    string                      `json:"status"`
	Role      string                      `json:"role"`
	Criteria  []qamodel.SnapshotCriterion `json:"criteria"`
	MyResults any                         `json:"my_results"`
	Outcome   *qa.Outcome                 `json:"outcome,omitempty"`
}

func (h *QAInviteeHandler) Session(w http.ResponseWriter, r *http.Request) {
	inv, ok := h.invitee(w, r)
	if !ok {
		return
	}
	mine, err := h.svc.TesterResults(r.Context(), inv.Session.ID, inv.Invite.EmailHash)
	if err != nil {
		qaError(w, err)
		return
	}
	out := inviteeSession{
		SessionID: inv.Session.ID, Title: inv.Session.Title, Status: inv.Session.Status, Role: inv.Invite.Role,
		Criteria: inv.Session.CriteriaSnapshot, MyResults: mine,
	}
	// Signers and observers see the standing they are asked to sign or watch.
	if inv.Invite.Role != qamodel.RoleTester {
		if o, err := h.svc.Outcome(r.Context(), inv.Session.TenantID, inv.Session.ID); err == nil {
			out.Outcome = o
		}
	}
	respondJSON(w, http.StatusOK, out)
}

// RecordResult takes {"result", "severity", "notes", "device"} for {criterionID}. Testers only.
func (h *QAInviteeHandler) RecordResult(w http.ResponseWriter, r *http.Request) {
	inv, ok := h.invitee(w, r)
	if !ok {
		return
	}
	if inv.Invite.Role != qamodel.RoleTester {
		respondError(w, http.StatusForbidden, "only testers record results")
		return
	}
	cid, ok := uuidParam(w, r, "criterionID")
	if !ok {
		return
	}
	var in qa.ResultInput
	if !decodeBody(w, r, qaBodyLimit, &in) {
		return
	}
	in.CriterionID = cid
	out, err := h.svc.RecordResult(r.Context(), inv.Session, qa.TesterFromInvite(inv.Invite), in)
	if err != nil {
		qaError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, out)
}

// Report lets an invitee print the report (signers print and sign it).
func (h *QAInviteeHandler) Report(w http.ResponseWriter, r *http.Request) {
	inv, ok := h.invitee(w, r)
	if !ok {
		return
	}
	pdf, hash, err := h.svc.RenderReport(r.Context(), inv.Session)
	if err != nil {
		qaError(w, err)
		return
	}
	writePDF(w, pdf, hash, "acceptance-"+inv.Session.ID.String()+".pdf")
}

func (h *QAInviteeHandler) Logout(w http.ResponseWriter, r *http.Request) {
	h.setCookie(w, "", time.Unix(0, 0))
	w.WriteHeader(http.StatusNoContent)
}
