package milestones

// subListLimit bounds a list read that returns a plain array to the UI, so no request can load
// an unbounded number of rows.
const subListLimit = 500
