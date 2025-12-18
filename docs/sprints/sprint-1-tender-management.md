# Sprint 1: Tender Management Module

**Duration**: 4 weeks  
**Sprint Goal**: Implement complete tender management lifecycle from opportunity identification through submission  
**Team Size**: 4-6 developers  
**Prerequisites**: Sprint 0 (Foundations) completed

---

## Sprint Objectives

1. Enable business development teams to log and track tender opportunities
2. Support tender evaluation workflow with committee management
3. Implement tender document preparation with task assignment
4. Enable tender submission tracking (physical/email)
5. Integrate with meeting platforms for evaluation meetings

---

## User Stories

### Epic 1: Tender Discovery & Logging

#### US-1.1: Log Tender Opportunity
**As a** business development officer  
**I want to** log new tender opportunities I discover  
**So that** the organization can evaluate and potentially bid on them

**Acceptance Criteria**:
- Can create tender with essential fields (title, client, deadline, estimated value)
- Can upload tender documents
- Can specify tender source (government portal, referral, etc.)
- Can set priority level
- System generates unique tender number automatically
- Can add tags/categories for organization

**Story Points**: 8  
**Tasks**:
- Create tender entity schema in Ent
- Implement tender creation API endpoint
- Add file upload to S3/MinIO
- Create tender list/detail API endpoints
- Add validation for required fields
- Add unique tender number generation logic
- Write unit tests for tender service
- Write integration tests for API endpoints

---

#### US-1.2: View Tender Pipeline
**As a** business development manager  
**I want to** view all tenders in various stages  
**So that** I can track our tender pipeline

**Acceptance Criteria**:
- Can list tenders with filters (status, priority, deadline)
- Can search tenders by title, client, or tender number
- Can sort by deadline, estimated value, created date
- Pagination support for large lists
- Shows key metrics (total tenders, by status, total estimated value)

**Story Points**: 5  
**Tasks**:
- Implement tender filtering and search logic
- Add full-text search on title and description
- Implement pagination (cursor-based)
- Create tender metrics/summary endpoint
- Add database indexes for performance
- Write tests for filtering and search

---

### Epic 2: Tender Evaluation

#### US-1.3: Create Evaluation Committee
**As a** tender coordinator  
**I want to** form evaluation committees for tenders  
**So that** we can systematically evaluate opportunities

**Acceptance Criteria**:
- Can create committee for a tender
- Can add/remove committee members
- Can designate committee chair
- Can specify member roles (chair, member, observer)
- Committee members receive notifications

**Story Points**: 5  
**Tasks**:
- Create committee and committee_members entities
- Implement committee CRUD APIs
- Add member management endpoints
- Integrate with notifications service for member invites
- Write tests

---

#### US-1.4: Schedule Evaluation Meeting
**As a** committee chair  
**I want to** schedule evaluation meetings with integration to calendar tools  
**So that** committee members can attend and discuss

**Acceptance Criteria**:
- Can create meeting for tender evaluation
- Can select meeting platform (Zoom, Teams, Google Meet, Zoho Meet)
- For virtual meetings, optionally auto-create meeting via platform API
- Can add agenda and attendees
- Sends calendar invites to attendees
- Can record meeting minutes

**Story Points**: 13  
**Tasks**:
- Create tender_meetings entity
- Implement meeting CRUD APIs
- Integrate with Google Meet API (OAuth 2.0)
- Integrate with Microsoft Teams API
- Integrate with Zoom API
- Add meeting link generation
- Integrate with notification service for invites
- Add meeting minutes field
- Write integration tests

---

#### US-1.5: Submit Tender Evaluation
**As a** committee member  
**I want to** submit my evaluation scores and comments  
**So that** the committee can make informed go/no-go decisions

**Acceptance Criteria**:
- Can submit evaluation for assigned tender
- Can provide scores for: financial, technical, resource, risk
- Can provide overall recommendation (pursue, drop, conditional)
- Can add detailed comments
- Can perform SWOT analysis
- Can cast vote (yes, no, abstain)
- Can save draft and submit later
- Once submitted, evaluation is locked (or require approval to edit)

**Story Points**: 8  
**Tasks**:
- Create tender_evaluations entity
- Implement evaluation submission API
- Add score validation logic
- Add draft/submitted status handling
- Create evaluation summary view API
- Write tests

---

#### US-1.6: Make Go/No-Go Decision
**As a** decision authority  
**I want to** review all evaluations and make final go/no-go decision  
**So that** we only pursue viable opportunities

**Acceptance Criteria**:
- Can view all committee evaluations in summary
- Can see aggregated scores and recommendations
- Can make final decision (go, no-go)
- Can record decision rationale
- Decision updates tender status
- Notifies relevant stakeholders

**Story Points**: 5  
**Tasks**:
- Add decision fields to tender entity
- Implement decision submission API
- Add status transition logic
- Integrate with notifications
- Write tests

---

### Epic 3: Tender Document Preparation

#### US-1.7: Assign Tender Sections to Team
**As a** tender lead  
**I want to** break tender document into sections and assign to team members  
**So that** work is distributed and trackable

**Acceptance Criteria**:
- Can create sections from tender requirements
- Can assign section to team member
- Can set individual section deadlines
- Can designate reviewer for each section
- Team members notified of assignments
- Can track section completion status

**Story Points**: 8  
**Tasks**:
- Create tender_sections entity
- Implement section CRUD APIs
- Add assignment and deadline logic
- Add status tracking (not_started, in_progress, review, approved)
- Integrate with notifications
- Write tests

---

#### US-1.8: Submit Section for Review
**As a** team member  
**I want to** submit my completed section for review  
**So that** it can be approved and included in final tender

**Acceptance Criteria**:
- Can upload section document
- Can mark section as submitted for review
- Reviewer notified
- Can track review status
- Reviewer can approve or request changes
- If changes requested, can see comments and resubmit

**Story Points**: 8  
**Tasks**:
- Add document upload to tender_sections
- Implement review workflow
- Add review comments field
- Add approval/rejection logic
- Integrate with notifications
- Write tests

---

#### US-1.9: Compile Final Tender Document
**As a** tender coordinator  
**I want to** compile all approved sections into final document  
**So that** we have a complete tender ready for submission

**Acceptance Criteria**:
- Can view all sections and their status
- Can see which sections are pending/approved
- Can generate final compiled document (PDF)
- Can upload manually compiled document
- Final document versioned
- Can set document as "ready for submission"

**Story Points**: 8  
**Tasks**:
- Implement section compilation logic
- Add PDF generation (if automatic compilation)
- Add document versioning
- Add "ready for submission" flag
- Write tests

---

### Epic 4: Tender Submission

#### US-1.10: Submit Tender (Email)
**As a** tender coordinator  
**I want to** submit tender via email automatically  
**So that** submission is documented and timely

**Acceptance Criteria**:
- Can specify client submission email
- Can compose submission email (subject, body)
- Can attach final tender document
- System sends email and records submission
- Receives email delivery confirmation
- Records submission timestamp and confirmation number

**Story Points**: 8  
**Tasks**:
- Add email submission fields to tender
- Integrate with notification service email provider
- Implement email sending logic
- Add delivery confirmation tracking
- Create tender_submissions record
- Add submission proof/receipt
- Write tests

---

#### US-1.11: Record Physical Submission
**As a** tender coordinator  
**I want to** record details of physical submission  
**So that** we have complete submission records

**Acceptance Criteria**:
- Can mark tender as physically submitted
- Can record courier service used
- Can record tracking number
- Can record submission address
- Can upload proof of delivery/receipt
- Records submission timestamp

**Story Points**: 3  
**Tasks**:
- Add physical submission fields
- Implement submission recording API
- Add proof of delivery upload
- Write tests

---

### Epic 5: Post-Submission Tracking

#### US-1.12: Update Tender Status
**As a** tender coordinator  
**I want to** update tender status as it progresses  
**So that** stakeholders know the current state

**Acceptance Criteria**:
- Can update status: submitted, under_review, shortlisted, interview, awarded, lost
- Can record status change reason/notes
- Status changes logged in activity feed
- Stakeholders notified of key status changes (shortlisted, awarded)

**Story Points**: 5  
**Tasks**:
- Add status transition validation
- Implement status update API
- Add activity logging
- Integrate with notifications
- Write tests

---

#### US-1.13: Record Tender Outcome
**As a** project manager  
**I want to** record final tender outcome (won/lost)  
**So that** we can track success metrics and learn

**Acceptance Criteria**:
- If awarded: record award date, award value, contract details
- If lost: record loss reason, competitor info (if known)
- Can record lessons learned
- Can mark tender for conversion to project (if won)
- Updates tender win rate metrics

**Story Points**: 5  
**Tasks**:
- Add outcome fields to tender
- Implement outcome recording API
- Add metrics calculation
- Write tests

---

## Technical Tasks

### Database & Schema
- [ ] Define Ent schemas for all tender entities
- [ ] Generate Ent code
- [ ] Create database migrations
- [ ] Add database indexes for performance
- [ ] Add vector embeddings column for semantic search (optional for Sprint 1)

### API Development
- [ ] Implement tender CRUD operations
- [ ] Implement committee management APIs
- [ ] Implement meeting scheduling APIs
- [ ] Implement evaluation APIs
- [ ] Implement section management APIs
- [ ] Implement submission APIs
- [ ] Add input validation with custom validators
- [ ] Add error handling and proper HTTP status codes
- [ ] Generate OpenAPI/Swagger documentation

### External Integrations
- [ ] Integrate Google Meet API for meeting creation
- [ ] Integrate Microsoft Teams API
- [ ] Integrate Zoom API
- [ ] Integrate with notifications service (NATS events)
- [ ] Integrate with auth-service for user data
- [ ] Setup S3/MinIO for document storage

### Testing
- [ ] Write unit tests for all service layer functions
- [ ] Write integration tests for all API endpoints
- [ ] Write tests for external integrations (with mocks)
- [ ] Setup Testcontainers for database tests
- [ ] Achieve >80% code coverage

### DevOps
- [ ] Update Helm chart values for new config
- [ ] Add environment variables for meeting platform credentials
- [ ] Setup secrets for OAuth tokens
- [ ] Update CI/CD pipeline

---

## API Endpoints to Implement

### Tenders
```
POST   /api/v1/{tenantID}/tenders                    - Create tender
GET    /api/v1/{tenantID}/tenders                    - List tenders (with filters)
GET    /api/v1/{tenantID}/tenders/{id}               - Get tender details
PUT    /api/v1/{tenantID}/tenders/{id}               - Update tender
DELETE /api/v1/{tenantID}/tenders/{id}               - Soft delete tender
GET    /api/v1/{tenantID}/tenders/{id}/documents     - List tender documents
POST   /api/v1/{tenantID}/tenders/{id}/documents     - Upload document
GET    /api/v1/{tenantID}/tenders/metrics            - Tender metrics
```

### Committees
```
POST   /api/v1/{tenantID}/tenders/{id}/committees          - Create committee
GET    /api/v1/{tenantID}/tenders/{id}/committees          - List committees
POST   /api/v1/{tenantID}/committees/{id}/members          - Add member
DELETE /api/v1/{tenantID}/committees/{id}/members/{userId} - Remove member
```

### Meetings
```
POST   /api/v1/{tenantID}/tenders/{id}/meetings            - Create meeting
GET    /api/v1/{tenantID}/tenders/{id}/meetings            - List meetings
PUT    /api/v1/{tenantID}/meetings/{id}                    - Update meeting
POST   /api/v1/{tenantID}/meetings/{id}/minutes            - Add minutes
```

### Evaluations
```
POST   /api/v1/{tenantID}/tenders/{id}/evaluations         - Submit evaluation
GET    /api/v1/{tenantID}/tenders/{id}/evaluations         - List evaluations
GET    /api/v1/{tenantID}/tenders/{id}/evaluation-summary  - Evaluation summary
```

### Decisions
```
POST   /api/v1/{tenantID}/tenders/{id}/decision            - Make go/no-go decision
```

### Sections
```
POST   /api/v1/{tenantID}/tenders/{id}/sections            - Create section
GET    /api/v1/{tenantID}/tenders/{id}/sections            - List sections
PUT    /api/v1/{tenantID}/sections/{id}                    - Update section
POST   /api/v1/{tenantID}/sections/{id}/submit             - Submit for review
POST   /api/v1/{tenantID}/sections/{id}/approve            - Approve section
POST   /api/v1/{tenantID}/sections/{id}/request-changes    - Request changes
```

### Submissions
```
POST   /api/v1/{tenantID}/tenders/{id}/submit              - Submit tender (email/physical)
GET    /api/v1/{tenantID}/tenders/{id}/submissions         - List submissions
```

### Status Updates
```
POST   /api/v1/{tenantID}/tenders/{id}/status              - Update status
POST   /api/v1/{tenantID}/tenders/{id}/outcome             - Record outcome
```

---

## Database Migrations

### Migration 001: Create tender tables
```sql
-- tenders
-- tender_documents
-- tender_committees
-- tender_committee_members
-- tender_evaluations
-- tender_meetings
-- tender_sections
-- tender_submissions
```

See `docs/erd.md` for complete schema definitions.

---

## Event Publishing (NATS)

Publish events for key tender lifecycle changes:

```
projects.tender.created                 - New tender logged
projects.tender.committee.formed        - Committee created
projects.tender.meeting.scheduled       - Meeting scheduled
projects.tender.evaluation.submitted    - Evaluation submitted
projects.tender.decision.made           - Go/no-go decision made
projects.tender.section.assigned        - Section assigned to member
projects.tender.section.submitted       - Section submitted for review
projects.tender.section.approved        - Section approved
projects.tender.submitted               - Tender submitted
projects.tender.status.changed          - Status changed
projects.tender.awarded                 - Tender won
projects.tender.lost                    - Tender lost
```

---

## Testing Strategy

### Unit Tests
- Service layer business logic
- Input validation
- Status transition logic
- Score calculation logic
- Date validation (deadlines)

### Integration Tests
- API endpoint tests with real database (Testcontainers)
- File upload/download tests
- External API integration tests (with mocks for meeting platforms)
- Event publishing tests (NATS)

### Manual Testing Scenarios
1. **Full tender lifecycle**: Create tender → Form committee → Schedule meeting → Submit evaluations → Make decision → Assign sections → Review sections → Submit tender → Update outcome
2. **Email submission**: Test actual email sending (staging environment)
3. **Meeting creation**: Test Google Meet/Teams/Zoom meeting creation (with test accounts)

---

## Definition of Done

- [ ] All user stories implemented and tested
- [ ] All API endpoints implemented with OpenAPI docs
- [ ] Database migrations created and tested
- [ ] Integration with meeting platforms working
- [ ] Integration with notifications service working
- [ ] Unit test coverage >80%
- [ ] Integration tests passing
- [ ] Code reviewed and approved
- [ ] Documentation updated
- [ ] Deployed to staging environment
- [ ] User acceptance testing passed

---

## Risks & Mitigation

| Risk | Impact | Likelihood | Mitigation |
|------|--------|------------|------------|
| Meeting platform API rate limits | Medium | Low | Implement rate limiting, caching, fallback to manual link entry |
| Complex evaluation scoring logic | Medium | Medium | Start with simple scoring, iterate based on feedback |
| File upload/download performance | High | Low | Use S3 presigned URLs for large files |
| Email deliverability issues | High | Medium | Use reliable email provider (SendGrid), implement retry logic |
| OAuth token management complexity | Medium | Medium | Use established OAuth libraries, implement token refresh |

---

## Sprint Ceremonies

### Sprint Planning
- Review and estimate all user stories
- Identify dependencies
- Assign stories to developers

### Daily Standup
- What did I complete yesterday?
- What will I work on today?
- Any blockers?

### Sprint Review
- Demo tender management features to stakeholders
- Gather feedback
- Adjust backlog based on feedback

### Sprint Retrospective
- What went well?
- What could be improved?
- Action items for next sprint

---

## Success Metrics

- All 13 user stories completed
- >80% test coverage
- <200ms p95 API response time
- Zero critical bugs in production
- Positive feedback from business development team

---

**Document Version**: 1.0  
**Created**: 2024-12-05  
**Sprint Start**: TBD  
**Sprint End**: TBD  
**Sprint Master**: TBD

