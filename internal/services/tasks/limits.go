package tasks

// maxPageSize caps list pages (the shared pagination limit), so no request can pull a whole
// table in one call.
const maxPageSize = 100
