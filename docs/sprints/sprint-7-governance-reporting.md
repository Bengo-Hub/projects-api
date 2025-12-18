# Sprint 7: Governance & Reporting

**Duration**: 3 weeks  
**Sprint Goal**: Implement project governance, change control, and standard reporting.  
**Team Size**: x developers  
**Prerequisites**: Sprint 6 completed

---

## Sprint Objectives

1. Implement project governance hierarchy and decision logs
2. Develop a formal change control process
3. Create standard project reports (Status, Progress, Financial) and Activity Reports
4. Implement project dashboards and KPIs

---

## User Stories

### Epic 1: Governance & Change Control

#### US-7.1: Project Governance & RACI
**As a** project sponsor  
**I want to** define the governance structure and RACI matrix  
**So that** accountability is clearly defined

**Acceptance Criteria**:
- Define Steering Committee and other governance roles
- Create RACI matrix for project activities
- Log key project decisions with rationale and approvals

**Story Points**: 5  
**Tasks**:
- Create governance_role and decision_log entities
- Implement governance management APIs
- Create RACI matrix management APIs
- Implement decision log CRUD APIs

---

#### US-7.2: Change Request Management
**As a** project manager  
**I want to** manage project changes through a formal process  
**So that** scope, time, and cost impacts are evaluated

**Acceptance Criteria**:
- Create Change Requests (CRs) with impact analysis
- Approval workflow for CRs
- Approved CRs update project baselines

**Story Points**: 8  
**Tasks**:
- Create change_request entity
- Implement CR management and approval APIs
- Add logic to update project baselines upon CR approval

---

### Epic 2: Reporting & Activity Tracking

#### US-7.3: Standard Project & Activity Reports
**As a** stakeholder  
**I want to** generate standard project reports and detailed activity reports  
**So that** I can review project performance and granular team activities

**Acceptance Criteria**:
- Support for: Status Report, Progress Report, Budget Report, and Activity Reports
- Export to PDF and Excel
- Scheduled report distribution via email
- Activity reports show granular task-level updates and team contributions

**Story Points**: 8  
**Tasks**:
- Implement report data aggregation logic for status and activity reports
- Integrate with PDF/Excel generation libraries
- Implement scheduled report background jobs
---

### Epic 3: Activity Reporting & Signing Sheets

#### US-7.4: Activity Reports & Digital Signing Sheets
**As a** project coordinator  
**I want to** submit detailed activity reports and capture participant signatures  
**So that** I can document the outcomes of workshops and site visits for compliance and audit.

**Acceptance Criteria**:
- Submit activity reports with title, description, and status
- Multi-stage approval workflow for activity reports (Reviewer -> Approver)
- Capture digital signing sheets for participants (Name, Org, Designation, Signature)
- Link signing sheets to specific activities

**Story Points**: 8  
**Tasks**:
- Create `activity_reports`, `activity_report_reviews`, and `activity_signing_sheets` entities
- Implement Activity Report submission and review APIs
- Implement Digital Signing Sheet capture APIs (with signature image storage)
- Add logic to link signing sheets to activity reports for audit trails
