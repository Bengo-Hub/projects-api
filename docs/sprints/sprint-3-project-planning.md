# Sprint 3: Project Planning

**Duration**: 4 weeks  
**Sprint Goal**: Implement core project planning tools including WBS, task dependencies, and Gantt chart support.  
**Team Size**: x developers  
**Prerequisites**: Sprint 2 completed

---

## Sprint Objectives

1. Implement hierarchical Work Breakdown Structure (WBS) and Workplans
2. Develop task management with dependencies (FS, SS, FF, SF)
3. Implement milestone management and deliverable tracking
4. Provide data APIs for Gantt chart visualization

---

## User Stories

### Epic 1: Work Breakdown Structure & Workplans

#### US-3.1: Define Project WBS & Workplan
**As a** project planner  
**I want to** create a hierarchical WBS and a detailed workplan for my project  
**So that** I can organize work into manageable packages and track execution

**Acceptance Criteria**:
- Support for multi-level task nesting
- Automatic WBS code generation (e.g., 1.1, 1.1.1)
- Roll-up of progress and estimates from subtasks to parent tasks
- Ability to export the workplan as a structured document (PDF/Excel)

**Story Points**: 8  
**Tasks**:
- Implement hierarchical task structure in Ent
- Create WBS management APIs
- Implement progress roll-up logic
- Add WBS code generation utility
- Implement workplan export functionality (PDF/Excel)

---

### Epic 2: Task Dependencies

#### US-3.2: Manage Task Dependencies
**As a** project manager  
**I want to** link tasks with dependencies  
**So that** the project schedule reflects logical constraints

**Acceptance Criteria**:
- Support for Finish-to-Start (FS), Start-to-Start (SS), Finish-to-Finish (FF), and Start-to-Finish (SF)
- Circular dependency detection and prevention
- API to calculate the critical path

**Story Points**: 8  
**Tasks**:
- Create task_dependencies entity
- Implement dependency CRUD APIs
- Implement circular dependency detection algorithm
- Implement Critical Path Method (CPM) logic

---

### Epic 3: Milestones & Deliverables

#### US-3.3: Track Milestones & Deliverables
**As a** stakeholder  
**I want to** see key project milestones and their associated deliverables  
**So that** I can monitor high-level progress

**Acceptance Criteria**:
- Can define milestones with target dates
- Can link deliverables to milestones
- Deliverables support acceptance criteria and approval status

**Story Points**: 5  
**Tasks**:
- Create milestone and deliverable entities
- Implement milestone/deliverable CRUD APIs
- Add logic to link deliverables to milestones
- Implement deliverable approval workflow

---

### Epic 4: Activity Planning

#### US-3.4: Define Project Activities (Workshops, Site Visits)
**As a** project coordinator  
**I want to** define specific activities like workshops and site visits within the workplan  
**So that** I can plan for logistics, participants, and specific delivery modes.

**Acceptance Criteria**:
- Support for `mode_of_delivery` (Workshop, Site Visit, etc.)
- Ability to link activities to Workplan Outputs and Sub-outputs
- Capture planning details: Pax, Days, Frequency, and Quarterly flags (Q1-Q4)
- Assign a person responsible for each activity

**Story Points**: 5  
**Tasks**:
- Create `activities`, `activity_outputs`, and `activity_sub_outputs` entities
- Implement CRUD APIs for activity lookup tables
- Implement Activity management APIs with quarterly planning logic
- Add validation for pax/days/frequency constraints

---

## Implementation Status (as of 2026-05-22)

### Completed

| Story | Status | Notes |
|-------|--------|-------|
| US-3.1: WBS & Workplan | Partial | `parent_id` + `wbs_code` fields added to Task schema; hierarchical task CRUD APIs implemented; progress roll-up and PDF export deferred |
| US-3.2: Task Dependencies | ✅ | `task_dependencies` edge on Task entity; AddDependency/RemoveDependency APIs; DFS circular dependency detection; Gantt data endpoint |
| US-3.3: Milestones | ✅ | Milestone entity, service, handler; full CRUD at `/projects/{id}/milestones` |

### Deferred

| Story | Reason |
|-------|--------|
| US-3.1: Progress roll-up + export | Sprint 4 candidate — requires frontend integration first |
| US-3.4: Activity Planning | Deferred to Sprint 4/5 — activities, activity_outputs entities not yet created |
| Critical Path Method (CPM) | Sprint 5 candidate — dependencies graph exists, CPM calculation TBD |
| Deliverable approval workflow | Sprint 4 candidate |

### Additional Work Completed (Not in Original Sprint Scope)

- Projects CRUD service + handler (GET/POST /projects, GET/PUT/DELETE/summary /projects/{id})
- Members management (add/remove/update role on project members)
- Comments service + handler for project-level and task-level comments
- Activity feed handler (project + task activities)
- Time Logs entity (timelog schema, migration — API endpoints in Sprint 5)
- Budget + Expenses entities (schema, migration — Budget API in Sprint 5)
