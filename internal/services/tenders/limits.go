package tenders

// maxPageSize caps list pages (the shared pagination limit), so no request can pull a whole
// table in one call.
const maxPageSize = 100

// subListLimit bounds the committee, evaluation, meeting and document lists of one tender.
const subListLimit = 500
