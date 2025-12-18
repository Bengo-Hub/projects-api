# Sprint 2: Tender to Project Conversion

**Duration**: 2 weeks  
**Sprint Goal**: Implement the seamless transition from an awarded tender to a live project.  
**Team Size**: x developers  
**Prerequisites**: Sprint 1 completed

---

## Sprint Objectives

1. Implement tender-to-project conversion logic
2. Develop data migration for tender documents and team members
3. Automate project kickoff activities (Charter, Stakeholder Register)
4. Implement status tracking for the conversion process

---

## User Stories

### Epic 1: Conversion Logic

#### US-2.1: Convert Awarded Tender
**As a** project manager  
**I want to** convert an awarded tender into a project with a single click  
**So that** I can start project planning immediately using existing data

**Acceptance Criteria**:
- "Convert to Project" button available for tenders with "Awarded" status
- All core tender data (client, budget, timeline) is copied to the new project
- Tender documents are linked to the project document repository
- Tender committee members are added as initial project team members

**Story Points**: 8  
**Tasks**:
- Implement `POST /api/v1/tenders/{id}/convert` endpoint
- Create project entity from tender data
- Migrate tender documents to project scope
- Assign tender team to project roles
- Write integration tests for the conversion flow

---

### Epic 2: Kickoff Automation

#### US-2.2: Generate Project Charter
**As a** project manager  
**I want to** have a pre-populated project charter after conversion  
**So that** I can quickly finalize the project's vision and scope

**Acceptance Criteria**:
- Project charter is automatically created upon conversion
- Charter includes objectives and scope from the tender document
- Support for manual editing and versioning of the charter

**Story Points**: 5  
**Tasks**:
- Implement project charter entity and CRUD APIs
- Add logic to extract objectives from tender description
- Implement charter versioning support

---

#### US-2.3: Automated Stakeholder Register
**As a** project manager  
**I want to** automatically populate the stakeholder register from tender contacts  
**So that** I can manage communications from day one

**Acceptance Criteria**:
- Client contacts from the tender are added to the stakeholder register
- Internal tender team members are added as internal stakeholders
- Ability to add additional stakeholders manually

**Story Points**: 3  
**Tasks**:
- Create stakeholder entity and CRUD APIs
- Implement auto-population logic during conversion
- Write unit tests for stakeholder management
