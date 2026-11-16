package tasks

// ganttLimit caps one Gantt read; a larger project answers the first ganttLimit tasks (oldest
// first) with Truncated set, so the chart never loads an unbounded plan in one call.
const ganttLimit = 2000

// taskCommentsLimit is how many of a task's latest comments GetTask returns; the rest come from
// the paged comments route.
const taskCommentsLimit = 50

// maxDependencyDepth bounds the cycle check's walk through the dependency graph.
const maxDependencyDepth = 1000
