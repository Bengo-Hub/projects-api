package tenders

import "slices"

// Tender statuses. The pipeline is draft, evaluating, then the go/no-go decision (preparing on
// go, no_go on no-go), submitted (the submit route), under_review and shortlisted, then the
// outcome (awarded or lost). cancelled can be reached from any open status. A won tender stays
// awarded after conversion; metadata.conversion names the project.
const (
	StatusDraft       = "draft"
	StatusEvaluating  = "evaluating"
	StatusPreparing   = "preparing"
	StatusNoGo        = "no_go"
	StatusSubmitted   = "submitted"
	StatusUnderReview = "under_review"
	StatusShortlisted = "shortlisted"
	StatusAwarded     = "awarded"
	StatusLost        = "lost"
	StatusCancelled   = "cancelled"
)

// Statuses is every tender status in pipeline order (the metrics report each one).
var Statuses = []string{
	StatusDraft, StatusEvaluating, StatusPreparing, StatusNoGo, StatusSubmitted,
	StatusUnderReview, StatusShortlisted, StatusAwarded, StatusLost, StatusCancelled,
}

// manualMoves are the moves the status route and a tender edit may make. The decision, submit and
// outcome routes make the others (to preparing or no_go, to submitted, to awarded or lost), each
// with its own record and event.
var manualMoves = map[string][]string{
	StatusDraft:       {StatusEvaluating, StatusCancelled},
	StatusEvaluating:  {StatusDraft, StatusCancelled},
	StatusPreparing:   {StatusEvaluating, StatusCancelled},
	StatusNoGo:        {StatusEvaluating},
	StatusSubmitted:   {StatusUnderReview, StatusShortlisted, StatusCancelled},
	StatusUnderReview: {StatusShortlisted, StatusCancelled},
	StatusShortlisted: {StatusUnderReview, StatusCancelled},
	StatusCancelled:   {StatusDraft},
}

// legacyMoves are the moves out of a status this service does not know (a value written before
// the 2026-11-17 vocabulary): back into the pipeline as draft or evaluating, or cancelled. The UI
// shows such a tender as draft; nothing else acts on it until it is moved.
var legacyMoves = []string{StatusDraft, StatusEvaluating, StatusCancelled}

// IsKnownStatus reports whether status is one of Statuses.
func IsKnownStatus(status string) bool { return slices.Contains(Statuses, status) }

func movesFrom(status string) []string {
	if !IsKnownStatus(status) {
		return legacyMoves
	}
	return manualMoves[status]
}

// CanMove reports whether a manual status change from one status to another is allowed.
func CanMove(from, to string) bool { return IsKnownStatus(to) && slices.Contains(movesFrom(from), to) }

// ManualMoves lists the statuses a tender in status can be moved to by hand.
func ManualMoves(status string) []string {
	out := slices.Clone(movesFrom(status))
	if out == nil {
		out = []string{}
	}
	return out
}

// CanDecide reports whether a go/no-go decision can be made (or made again) in status: any time
// before the bid goes out.
func CanDecide(status string) bool {
	return slices.Contains([]string{StatusDraft, StatusEvaluating, StatusPreparing, StatusNoGo}, status)
}

// CanSubmit reports whether a submission can be recorded in status: the first one after a go
// decision, later ones (an amended bid, a clarification) while the bid is out.
func CanSubmit(status string) bool {
	return slices.Contains([]string{StatusPreparing, StatusSubmitted, StatusUnderReview, StatusShortlisted}, status)
}

// CanRecordOutcome reports whether the award or loss can be recorded in status.
func CanRecordOutcome(status string) bool {
	return slices.Contains([]string{StatusSubmitted, StatusUnderReview, StatusShortlisted}, status)
}

// openStatuses are the statuses whose estimated value counts as pipeline.
var openStatuses = []string{StatusDraft, StatusEvaluating, StatusPreparing, StatusSubmitted, StatusUnderReview, StatusShortlisted}

// validCreateStatus is the status a new tender may start in.
func validCreateStatus(s string) bool { return s == StatusDraft || s == StatusEvaluating }

// Priorities a tender may carry.
var Priorities = []string{"low", "medium", "high", "critical"}

// SubmissionMethods are how a bid is delivered (the tender's submission_type uses the same values).
var SubmissionMethods = []string{"online", "email", "physical"}
