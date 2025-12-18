# Projects Service - Entity Relationship Diagram

## Overview

This document describes the complete data model for the Projects Service, including all entities, relationships, and special columns like vector embeddings for AI-powered features.

---

## Database Extensions Required

```sql
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";      -- UUID generation
CREATE EXTENSION IF NOT EXISTS "pgcrypto";       -- Encryption functions
CREATE EXTENSION IF NOT EXISTS "vector";         -- pgvector for embeddings
CREATE EXTENSION IF NOT EXISTS "pg_trgm";        -- Fuzzy text search
CREATE EXTENSION IF NOT EXISTS "btree_gist";     -- Advanced indexing
CREATE EXTENSION IF NOT EXISTS "postgis";        -- Geospatial (optional for location-based features)
```

---

## Entity Categories

### 1. Tender Management Entities
### 2. Project Management Entities  
### 3. Task Management Entities
### 4. Resource & Budget Entities
### 5. Collaboration Entities
### 6. Governance & Compliance Entities
### 7. Integration & System Entities

---

## 1. Tender Management Entities

### 1.1 `tenders`

Core tender/opportunity tracking.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | Unique tender identifier |
| `tenant_id` | VARCHAR(100) | NOT NULL, INDEX | Tenant slug for multi-tenancy |
| `tender_number` | VARCHAR(50) | UNIQUE NOT NULL | Human-readable tender number (e.g., TND-2024-001) |
| `title` | VARCHAR(500) | NOT NULL | Tender title |
| `description` | TEXT | | Detailed tender description |
| `description_embedding` | VECTOR(1536) | | OpenAI embedding for semantic search |
| `source` | VARCHAR(100) | | Source of tender (government portal, referral, etc.) |
| `source_url` | TEXT | | URL to original tender posting |
| `tender_type` | VARCHAR(50) | NOT NULL | Type: open, restricted, negotiated, etc. |
| `category` | VARCHAR(100) | | Category: construction, IT, consulting, etc. |
| `client_name` | VARCHAR(255) | NOT NULL | Client/issuing organization |
| `client_contact_email` | VARCHAR(255) | | Primary contact email |
| `client_contact_phone` | VARCHAR(50) | | Primary contact phone |
| `estimated_value` | DECIMAL(15,2) | | Estimated contract value |
| `currency` | VARCHAR(3) | DEFAULT 'KES' | ISO 4217 currency code |
| `submission_deadline` | TIMESTAMPTZ | NOT NULL | Official submission deadline |
| `internal_deadline` | TIMESTAMPTZ | | Internal team deadline (earlier than official) |
| `start_date` | DATE | | Expected project start date |
| `duration_months` | INTEGER | | Expected project duration |
| `status` | VARCHAR(50) | NOT NULL, DEFAULT 'opportunity' | Status: opportunity, evaluating, preparing, submitted, shortlisted, interview, awarded, lost, cancelled |
| `stage` | VARCHAR(50) | NOT NULL | Current workflow stage |
| `priority` | VARCHAR(20) | DEFAULT 'medium' | Priority: critical, high, medium, low |
| `win_probability` | INTEGER | CHECK (win_probability >= 0 AND win_probability <= 100) | Estimated win probability (0-100%) |
| `discovered_by` | UUID | FK → users.id | User who logged the tender |
| `discovered_at` | TIMESTAMPTZ | DEFAULT NOW() | When tender was discovered |
| `assigned_to` | UUID | FK → users.id | Overall tender lead |
| `evaluation_due_date` | DATE | | Date by which evaluation must be completed |
| `go_no_go_decision` | BOOLEAN | | Go/No-Go decision result |
| `decision_made_by` | UUID | FK → users.id | User who made final decision |
| `decision_made_at` | TIMESTAMPTZ | | When decision was made |
| `decision_rationale` | TEXT | | Justification for go/no-go decision |
| `submission_method` | VARCHAR(50) | | Method: physical, email, online_portal |
| `submission_address` | TEXT | | Physical address or email for submission |
| `submitted_at` | TIMESTAMPTZ | | When tender was actually submitted |
| `submitted_by` | UUID | FK → users.id | User who performed submission |
| `confirmation_number` | VARCHAR(100) | | Submission confirmation/tracking number |
| `shortlist_notification_date` | DATE | | Expected date for shortlist announcement |
| `interview_date` | TIMESTAMPTZ | | Scheduled interview/presentation date |
| `interview_location` | TEXT | | Interview location (address or virtual meeting link) |
| `award_date` | DATE | | Date tender was awarded |
| `award_value` | DECIMAL(15,2) | | Actual awarded contract value |
| `loss_reason` | TEXT | | Reason if tender was lost |
| `lessons_learned` | TEXT | | Post-mortem notes |
| `converted_to_project_id` | UUID | FK → projects.id | Link to project if awarded |
| `metadata` | JSONB | | Flexible JSON for custom fields |
| `created_at` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | |
| `updated_at` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | |
| `created_by` | UUID | FK → users.id | |
| `updated_by` | UUID | FK → users.id | |
| `deleted_at` | TIMESTAMPTZ | | Soft delete timestamp |

**Indexes:**
```sql
CREATE INDEX idx_tenders_tenant ON tenders(tenant_id);
CREATE INDEX idx_tenders_status ON tenders(status, tenant_id);
CREATE INDEX idx_tenders_stage ON tenders(stage, tenant_id);
CREATE INDEX idx_tenders_deadline ON tenders(submission_deadline, tenant_id);
CREATE INDEX idx_tenders_assigned ON tenders(assigned_to, tenant_id);
CREATE INDEX idx_tenders_full_text ON tenders USING GIN(to_tsvector('english', title || ' ' || COALESCE(description, '')));
CREATE INDEX idx_tenders_embedding ON tenders USING ivfflat(description_embedding vector_cosine_ops);
```

---

### 1.2 `tender_documents`

Documents and attachments related to tenders.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | |
| `tenant_id` | VARCHAR(100) | NOT NULL, INDEX | |
| `tender_id` | UUID | FK → tenders.id, NOT NULL | |
| `document_type` | VARCHAR(50) | NOT NULL | Type: tender_doc, requirement, specification, addendum, our_proposal, supporting_doc |
| `name` | VARCHAR(255) | NOT NULL | Document name |
| `description` | TEXT | | Document description |
| `file_path` | TEXT | NOT NULL | S3/MinIO path to file |
| `file_size` | BIGINT | | File size in bytes |
| `mime_type` | VARCHAR(100) | | MIME type |
| `version` | INTEGER | DEFAULT 1 | Document version number |
| `is_latest` | BOOLEAN | DEFAULT true | Whether this is the latest version |
| `content_embedding` | VECTOR(1536) | | Embedding of document content |
| `uploaded_by` | UUID | FK → users.id | |
| `uploaded_at` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | |
| `metadata` | JSONB | | Additional metadata |

**Indexes:**
```sql
CREATE INDEX idx_tender_docs_tender ON tender_documents(tender_id, tenant_id);
CREATE INDEX idx_tender_docs_type ON tender_documents(document_type, tender_id);
CREATE INDEX idx_tender_docs_embedding ON tender_documents USING ivfflat(content_embedding vector_cosine_ops);
```

---

### 1.3 `tender_committees`

Evaluation committees for tenders.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | |
| `tenant_id` | VARCHAR(100) | NOT NULL, INDEX | |
| `tender_id` | UUID | FK → tenders.id, NOT NULL | |
| `name` | VARCHAR(255) | NOT NULL | Committee name |
| `description` | TEXT | | Committee purpose |
| `chair_user_id` | UUID | FK → users.id | Committee chairperson |
| `status` | VARCHAR(50) | DEFAULT 'active' | Status: active, completed, disbanded |
| `created_at` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | |
| `created_by` | UUID | FK → users.id | |

**Indexes:**
```sql
CREATE INDEX idx_tender_committees_tender ON tender_committees(tender_id, tenant_id);
```

---

### 1.4 `tender_committee_members`

Members of tender evaluation committees.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | |
| `tenant_id` | VARCHAR(100) | NOT NULL, INDEX | |
| `committee_id` | UUID | FK → tender_committees.id, NOT NULL | |
| `user_id` | UUID | FK → users.id, NOT NULL | |
| `role` | VARCHAR(50) | | Role: chair, member, observer, secretary |
| `added_at` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | |
| `added_by` | UUID | FK → users.id | |

**Indexes:**
```sql
CREATE UNIQUE INDEX idx_committee_member_unique ON tender_committee_members(committee_id, user_id);
CREATE INDEX idx_committee_members_user ON tender_committee_members(user_id, tenant_id);
```

---

### 1.5 `tender_evaluations`

Evaluation scores and comments from committee members.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | |
| `tenant_id` | VARCHAR(100) | NOT NULL, INDEX | |
| `tender_id` | UUID | FK → tenders.id, NOT NULL | |
| `user_id` | UUID | FK → users.id, NOT NULL | Evaluator |
| `evaluation_type` | VARCHAR(50) | | Type: initial_screening, detailed_analysis, final_review |
| `score` | DECIMAL(5,2) | | Overall score (0-100) |
| `recommendation` | VARCHAR(50) | | Recommendation: pursue, drop, conditional |
| `financial_score` | DECIMAL(5,2) | | Financial viability score |
| `technical_score` | DECIMAL(5,2) | | Technical capability score |
| `resource_score` | DECIMAL(5,2) | | Resource availability score |
| `risk_score` | DECIMAL(5,2) | | Risk assessment score |
| `comments` | TEXT | | Detailed comments |
| `strengths` | TEXT[] | ARRAY | List of strengths |
| `weaknesses` | TEXT[] | ARRAY | List of weaknesses |
| `opportunities` | TEXT[] | ARRAY | SWOT opportunities |
| `threats` | TEXT[] | ARRAY | SWOT threats |
| `vote` | VARCHAR(20) | | Vote: yes, no, abstain |
| `submitted_at` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | |
| `metadata` | JSONB | | Custom evaluation criteria |

**Indexes:**
```sql
CREATE INDEX idx_tender_eval_tender ON tender_evaluations(tender_id, tenant_id);
CREATE INDEX idx_tender_eval_user ON tender_evaluations(user_id, tender_id);
```

---

### 1.6 `tender_meetings`

Meetings related to tender evaluation.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | |
| `tenant_id` | VARCHAR(100) | NOT NULL, INDEX | |
| `tender_id` | UUID | FK → tenders.id, NOT NULL | |
| `committee_id` | UUID | FK → tender_committees.id | |
| `title` | VARCHAR(255) | NOT NULL | Meeting title |
| `description` | TEXT | | Meeting agenda |
| `meeting_type` | VARCHAR(50) | | Type: evaluation, kickoff, review, final_decision |
| `start_time` | TIMESTAMPTZ | NOT NULL | |
| `end_time` | TIMESTAMPTZ | NOT NULL | |
| `location` | TEXT | | Physical location or virtual |
| `meeting_platform` | VARCHAR(50) | | Platform: zoom, teams, google_meet, zoho_meet, physical |
| `meeting_link` | TEXT | | Virtual meeting link |
| `meeting_id` | VARCHAR(255) | | External meeting ID (e.g., Zoom meeting ID) |
| `organizer_id` | UUID | FK → users.id | Meeting organizer |
| `status` | VARCHAR(50) | DEFAULT 'scheduled' | Status: scheduled, in_progress, completed, cancelled |
| `minutes` | TEXT | | Meeting minutes/notes |
| `recording_url` | TEXT | | Recording link |
| `attendees` | UUID[] | ARRAY | Array of user IDs who attended |
| `created_at` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | |
| `created_by` | UUID | FK → users.id | |

**Indexes:**
```sql
CREATE INDEX idx_tender_meetings_tender ON tender_meetings(tender_id, tenant_id);
CREATE INDEX idx_tender_meetings_time ON tender_meetings(start_time, tenant_id);
```

---

### 1.7 `tender_sections`

Sections of tender document assigned to team members.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | |
| `tenant_id` | VARCHAR(100) | NOT NULL, INDEX | |
| `tender_id` | UUID | FK → tenders.id, NOT NULL | |
| `section_number` | VARCHAR(20) | | Section numbering (e.g., 1.2.3) |
| `title` | VARCHAR(255) | NOT NULL | Section title |
| `description` | TEXT | | Section requirements |
| `assigned_to` | UUID | FK → users.id | Responsible team member |
| `reviewer_id` | UUID | FK → users.id | Designated reviewer |
| `deadline` | TIMESTAMPTZ | | Section completion deadline |
| `status` | VARCHAR(50) | DEFAULT 'not_started' | Status: not_started, in_progress, review, approved, changes_requested |
| `word_count_estimate` | INTEGER | | Estimated word count |
| `actual_word_count` | INTEGER | | Actual word count |
| `completion_percentage` | INTEGER | CHECK (completion_percentage >= 0 AND completion_percentage <= 100) | Completion % |
| `submitted_at` | TIMESTAMPTZ | | When section was submitted for review |
| `reviewed_at` | TIMESTAMPTZ | | When section was reviewed |
| `approved_at` | TIMESTAMPTZ | | When section was approved |
| `review_comments` | TEXT | | Reviewer feedback |
| `document_path` | TEXT | | Path to section document |
| `sort_order` | INTEGER | DEFAULT 0 | Display order |
| `created_at` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | |
| `updated_at` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | |

**Indexes:**
```sql
CREATE INDEX idx_tender_sections_tender ON tender_sections(tender_id, tenant_id);
CREATE INDEX idx_tender_sections_assigned ON tender_sections(assigned_to, status);
CREATE INDEX idx_tender_sections_sort ON tender_sections(tender_id, sort_order);
```

---

### 1.8 `tender_submissions`

Records of tender submissions.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | |
| `tenant_id` | VARCHAR(100) | NOT NULL, INDEX | |
| `tender_id` | UUID | FK → tenders.id, NOT NULL | |
| `submission_method` | VARCHAR(50) | NOT NULL | Method: physical, email, online_portal |
| `submitted_at` | TIMESTAMPTZ | NOT NULL | Actual submission timestamp |
| `submitted_by` | UUID | FK → users.id, NOT NULL | |
| `recipient_email` | VARCHAR(255) | | Recipient email (if email submission) |
| `tracking_number` | VARCHAR(100) | | Tracking/confirmation number |
| `delivery_address` | TEXT | | Physical delivery address |
| `courier_service` | VARCHAR(100) | | Courier used (if physical) |
| `portal_url` | TEXT | | Online portal URL (if online) |
| `confirmation_received` | BOOLEAN | DEFAULT false | Whether confirmation was received |
| `confirmation_received_at` | TIMESTAMPTZ | | When confirmation was received |
| `final_document_path` | TEXT | | Path to final submitted document |
| `document_checksum` | VARCHAR(64) | | SHA-256 checksum of submitted document |
| `notes` | TEXT | | Additional submission notes |
| `metadata` | JSONB | | Additional metadata |

**Indexes:**
```sql
CREATE INDEX idx_tender_submissions_tender ON tender_submissions(tender_id, tenant_id);
CREATE INDEX idx_tender_submissions_time ON tender_submissions(submitted_at);
```

---

## 2. Project Management Entities

### 2.1 `projects`

Core project information.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | |
| `tenant_id` | VARCHAR(100) | NOT NULL, INDEX | |
| `project_number` | VARCHAR(50) | UNIQUE NOT NULL | Human-readable project number (e.g., PRJ-2024-001) |
| `name` | VARCHAR(500) | NOT NULL | Project name |
| `description` | TEXT | | Project description |
| `description_embedding` | VECTOR(1536) | | Embedding for semantic search |
| `project_type` | VARCHAR(50) | | Type: client_project, internal, tender_conversion, maintenance, research |
| `category` | VARCHAR(100) | | Category/domain |
| `source_tender_id` | UUID | FK → tenders.id | Link to tender if converted |
| `client_id` | UUID | FK (external CRM) | Client from CRM |
| `client_name` | VARCHAR(255) | | Client name (denormalized) |
| `client_contact_name` | VARCHAR(255) | | Primary client contact |
| `client_contact_email` | VARCHAR(255) | | |
| `client_contact_phone` | VARCHAR(50) | | |
| `start_date` | DATE | | Planned/actual start date |
| `end_date` | DATE | | Planned/actual end date |
| `actual_start_date` | DATE | | Actual start date |
| `actual_end_date` | DATE | | Actual completion date |
| `duration_days` | INTEGER | | Planned duration |
| `status` | VARCHAR(50) | NOT NULL, DEFAULT 'planning' | Status: planning, active, on_hold, completed, cancelled, closed |
| `health` | VARCHAR(20) | DEFAULT 'green' | Health: green, yellow, red |
| `priority` | VARCHAR(20) | DEFAULT 'medium' | Priority: critical, high, medium, low |
| `completion_percentage` | INTEGER | CHECK (completion_percentage >= 0 AND completion_percentage <= 100) | Overall completion % |
| `project_manager_id` | UUID | FK → users.id | Project manager |
| `sponsor_id` | UUID | FK → users.id | Project sponsor |
| `technical_lead_id` | UUID | FK → users.id | Technical lead |
| `department_id` | UUID | FK (external HRM) | Owning department |
| `cost_center_id` | UUID | FK (external Finance) | Cost center |
| `budget_total` | DECIMAL(15,2) | | Total approved budget |
| `budget_spent` | DECIMAL(15,2) | DEFAULT 0 | Amount spent to date |
| `budget_committed` | DECIMAL(15,2) | DEFAULT 0 | Committed (not yet spent) |
| `currency` | VARCHAR(3) | DEFAULT 'KES' | |
| `baseline_start_date` | DATE | | Original baseline start |
| `baseline_end_date` | DATE | | Original baseline end |
| `baseline_budget` | DECIMAL(15,2) | | Original baseline budget |
| `visibility` | VARCHAR(20) | DEFAULT 'private' | Visibility: private, team, org, public |
| `tags` | VARCHAR(50)[] | ARRAY | Array of tags |
| `custom_fields` | JSONB | | Flexible JSON for custom attributes |
| `created_at` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | |
| `updated_at` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | |
| `created_by` | UUID | FK → users.id | |
| `updated_by` | UUID | FK → users.id | |
| `deleted_at` | TIMESTAMPTZ | | Soft delete |

**Indexes:**
```sql
CREATE INDEX idx_projects_tenant ON projects(tenant_id);
CREATE INDEX idx_projects_status ON projects(status, tenant_id);
CREATE INDEX idx_projects_health ON projects(health, tenant_id);
CREATE INDEX idx_projects_manager ON projects(project_manager_id, tenant_id);
CREATE INDEX idx_projects_dates ON projects(start_date, end_date, tenant_id);
CREATE INDEX idx_projects_full_text ON projects USING GIN(to_tsvector('english', name || ' ' || COALESCE(description, '')));
CREATE INDEX idx_projects_embedding ON projects USING ivfflat(description_embedding vector_cosine_ops);
CREATE INDEX idx_projects_tags ON projects USING GIN(tags);
```

---

### 2.2 `project_members`

Project team membership.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | |
| `tenant_id` | VARCHAR(100) | NOT NULL, INDEX | |
| `project_id` | UUID | FK → projects.id, NOT NULL | |
| `user_id` | UUID | FK → users.id, NOT NULL | |
| `role` | VARCHAR(50) | | Role: manager, lead, member, contributor, viewer |
| `is_external` | BOOLEAN | DEFAULT false | External consultant/contractor |
| `hourly_rate` | DECIMAL(10,2) | | Billing rate (if applicable) |
| `allocation_percentage` | INTEGER | CHECK (allocation_percentage >= 0 AND allocation_percentage <= 100) | % of time allocated |
| `joined_at` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | |
| `left_at` | TIMESTAMPTZ | | When member left project |
| `added_by` | UUID | FK → users.id | |

**Indexes:**
```sql
CREATE UNIQUE INDEX idx_project_member_unique ON project_members(project_id, user_id) WHERE left_at IS NULL;
CREATE INDEX idx_project_members_user ON project_members(user_id, tenant_id);
CREATE INDEX idx_project_members_role ON project_members(role, project_id);
```

---

### 2.3 `project_roles`

Custom roles definition for projects.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | |
| `tenant_id` | VARCHAR(100) | NOT NULL, INDEX | |
| `project_id` | UUID | FK → projects.id | NULL for global roles |
| `code` | VARCHAR(50) | NOT NULL | Role code (e.g., 'technical_reviewer') |
| `name` | VARCHAR(100) | NOT NULL | Display name |
| `description` | TEXT | | Role description |
| `permissions` | VARCHAR(100)[] | ARRAY | Array of permission codes |
| `is_system_role` | BOOLEAN | DEFAULT false | Whether role is system-defined |
| `created_at` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | |
| `created_by` | UUID | FK → users.id | |

**Indexes:**
```sql
CREATE UNIQUE INDEX idx_project_role_code ON project_roles(tenant_id, project_id, code);
CREATE INDEX idx_project_roles_project ON project_roles(project_id, tenant_id);
```

---

## 3. Task Management Entities

### 3.1 `tasks`

Project tasks.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | |
| `tenant_id` | VARCHAR(100) | NOT NULL, INDEX | |
| `project_id` | UUID | FK → projects.id, NOT NULL | |
| `parent_task_id` | UUID | FK → tasks.id | For subtasks |
| `task_number` | VARCHAR(50) | | Human-readable task number |
| `title` | VARCHAR(500) | NOT NULL | Task title |
| `description` | TEXT | | Detailed description |
| `description_embedding` | VECTOR(768) | | Embedding (smaller than projects) |
| `task_type` | VARCHAR(50) | DEFAULT 'task' | Type: task, bug, enhancement, research, meeting |
| `status` | VARCHAR(50) | NOT NULL, DEFAULT 'todo' | Status: todo, in_progress, review, blocked, done, cancelled |
| `priority` | VARCHAR(20) | DEFAULT 'medium' | Priority: p0-critical, p1-high, p2-medium, p3-low, p4-trivial |
| `milestone_id` | UUID | FK → milestones.id | Associated milestone |
| `start_date` | DATE | | Planned start |
| `due_date` | DATE | | Due date |
| `actual_start_date` | DATE | | Actual start |
| `completed_at` | TIMESTAMPTZ | | Completion timestamp |
| `estimated_hours` | DECIMAL(8,2) | | Estimated effort |
| `actual_hours` | DECIMAL(8,2) | | Actual effort |
| `completion_percentage` | INTEGER | CHECK (completion_percentage >= 0 AND completion_percentage <= 100) | Completion % |
| `is_billable` | BOOLEAN | DEFAULT true | Whether task is billable |
| `assigned_to` | UUID | FK → users.id | Primary assignee |
| `created_by` | UUID | FK → users.id | Task creator |
| `sort_order` | INTEGER | DEFAULT 0 | Display order within project |
| `labels` | VARCHAR(50)[] | ARRAY | Labels/tags |
| `custom_fields` | JSONB | | Custom fields |
| `created_at` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | |
| `updated_at` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | |
| `deleted_at` | TIMESTAMPTZ | | Soft delete |

**Indexes:**
```sql
CREATE INDEX idx_tasks_project ON tasks(project_id, tenant_id);
CREATE INDEX idx_tasks_assigned ON tasks(assigned_to, status);
CREATE INDEX idx_tasks_milestone ON tasks(milestone_id);
CREATE INDEX idx_tasks_parent ON tasks(parent_task_id);
CREATE INDEX idx_tasks_due_date ON tasks(due_date, status);
CREATE INDEX idx_tasks_full_text ON tasks USING GIN(to_tsvector('english', title || ' ' || COALESCE(description, '')));
CREATE INDEX idx_tasks_embedding ON tasks USING ivfflat(description_embedding vector_cosine_ops);
CREATE INDEX idx_tasks_labels ON tasks USING GIN(labels);
```

---

### 3.2 `task_assignments`

Multiple assignees for a task (beyond primary).

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | |
| `tenant_id` | VARCHAR(100) | NOT NULL, INDEX | |
| `task_id` | UUID | FK → tasks.id, NOT NULL | |
| `user_id` | UUID | FK → users.id, NOT NULL | |
| `role` | VARCHAR(50) | | Role: assignee, reviewer, observer |
| `assigned_at` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | |
| `assigned_by` | UUID | FK → users.id | |

**Indexes:**
```sql
CREATE UNIQUE INDEX idx_task_assignment_unique ON task_assignments(task_id, user_id);
CREATE INDEX idx_task_assignments_user ON task_assignments(user_id, tenant_id);
```

---

### 3.3 `task_dependencies`

Task dependencies and relationships.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | |
| `tenant_id` | VARCHAR(100) | NOT NULL, INDEX | |
| `task_id` | UUID | FK → tasks.id, NOT NULL | Dependent task |
| `depends_on_task_id` | UUID | FK → tasks.id, NOT NULL | Task it depends on |
| `dependency_type` | VARCHAR(20) | NOT NULL, DEFAULT 'finish_to_start' | Type: finish_to_start, start_to_start, finish_to_finish, start_to_finish |
| `lag_days` | INTEGER | DEFAULT 0 | Lag time in days (can be negative for lead time) |
| `created_at` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | |
| `created_by` | UUID | FK → users.id | |

**Indexes:**
```sql
CREATE UNIQUE INDEX idx_task_dependency_unique ON task_dependencies(task_id, depends_on_task_id);
CREATE INDEX idx_task_dependencies_depends ON task_dependencies(depends_on_task_id);
```

---

### 3.4 `task_checklists`

Checklist items within tasks.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | |
| `tenant_id` | VARCHAR(100) | NOT NULL, INDEX | |
| `task_id` | UUID | FK → tasks.id, NOT NULL | |
| `title` | VARCHAR(500) | NOT NULL | Checklist item |
| `is_completed` | BOOLEAN | DEFAULT false | Completion status |
| `completed_at` | TIMESTAMPTZ | | When completed |
| `completed_by` | UUID | FK → users.id | Who completed |
| `sort_order` | INTEGER | DEFAULT 0 | Display order |
| `created_at` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | |

**Indexes:**
```sql
CREATE INDEX idx_task_checklists_task ON task_checklists(task_id, sort_order);
```

---

### 3.5 `milestones`

Project milestones.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | |
| `tenant_id` | VARCHAR(100) | NOT NULL, INDEX | |
| `project_id` | UUID | FK → projects.id, NOT NULL | |
| `name` | VARCHAR(255) | NOT NULL | Milestone name |
| `description` | TEXT | | Description |
| `due_date` | DATE | NOT NULL | Target date |
| `completed_at` | TIMESTAMPTZ | | Actual completion |
| `status` | VARCHAR(50) | NOT NULL, DEFAULT 'not_started' | Status: not_started, in_progress, completed, overdue |
| `completion_percentage` | INTEGER | CHECK (completion_percentage >= 0 AND completion_percentage <= 100) | Calculated from tasks |
| `is_baseline` | BOOLEAN | DEFAULT false | Whether this is a baseline milestone |
| `sort_order` | INTEGER | DEFAULT 0 | Display order |
| `created_at` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | |
| `created_by` | UUID | FK → users.id | |

**Indexes:**
```sql
CREATE INDEX idx_milestones_project ON milestones(project_id, tenant_id);
CREATE INDEX idx_milestones_due_date ON milestones(due_date, status);
CREATE INDEX idx_milestones_sort ON milestones(project_id, sort_order);
```

---

### 3.6 `deliverables`

Project deliverables.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | |
| `tenant_id` | VARCHAR(100) | NOT NULL, INDEX | |
| `project_id` | UUID | FK → projects.id, NOT NULL | |
| `milestone_id` | UUID | FK → milestones.id | Associated milestone |
| `name` | VARCHAR(255) | NOT NULL | Deliverable name |
| `description` | TEXT | | Description |
| `deliverable_type` | VARCHAR(50) | | Type: document, software, hardware, service, report |
| `acceptance_criteria` | TEXT | | Acceptance criteria |
| `due_date` | DATE | | Target delivery date |
| `submitted_at` | TIMESTAMPTZ | | When submitted for approval |
| `approved_at` | TIMESTAMPTZ | | When approved |
| `approved_by` | UUID | FK → users.id | Approver |
| `status` | VARCHAR(50) | NOT NULL, DEFAULT 'not_started' | Status: not_started, in_progress, review, approved, rejected, delivered |
| `version` | INTEGER | DEFAULT 1 | Version number |
| `document_path` | TEXT | | Path to deliverable file |
| `rejection_reason` | TEXT | | Reason if rejected |
| `created_at` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | |
| `created_by` | UUID | FK → users.id | |

**Indexes:**
```sql
CREATE INDEX idx_deliverables_project ON deliverables(project_id, tenant_id);
CREATE INDEX idx_deliverables_milestone ON deliverables(milestone_id);
CREATE INDEX idx_deliverables_status ON deliverables(status, project_id);
```

---

## 4. Resource & Budget Entities

### 4.1 `budgets`

Project budget allocation.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | |
| `tenant_id` | VARCHAR(100) | NOT NULL, INDEX | |
| `project_id` | UUID | FK → projects.id, NOT NULL | |
| `budget_type` | VARCHAR(50) | DEFAULT 'project' | Type: project, phase, category |
| `name` | VARCHAR(255) | | Budget name |
| `total_amount` | DECIMAL(15,2) | NOT NULL | Total budget |
| `spent_amount` | DECIMAL(15,2) | DEFAULT 0 | Amount spent |
| `committed_amount` | DECIMAL(15,2) | DEFAULT 0 | Committed (POs, contracts) |
| `currency` | VARCHAR(3) | DEFAULT 'KES' | |
| `fiscal_year` | INTEGER | | Fiscal year |
| `status` | VARCHAR(50) | DEFAULT 'draft' | Status: draft, approved, active, closed |
| `approved_by` | UUID | FK → users.id | Budget approver |
| `approved_at` | TIMESTAMPTZ | | Approval timestamp |
| `created_at` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | |
| `created_by` | UUID | FK → users.id | |

**Indexes:**
```sql
CREATE INDEX idx_budgets_project ON budgets(project_id, tenant_id);
CREATE INDEX idx_budgets_status ON budgets(status, tenant_id);
```

---

### 4.2 `budget_lines`

Budget line items by category.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | |
| `tenant_id` | VARCHAR(100) | NOT NULL, INDEX | |
| `budget_id` | UUID | FK → budgets.id, NOT NULL | |
| `category` | VARCHAR(100) | NOT NULL | Category: labor, materials, equipment, travel, overhead, contingency |
| `subcategory` | VARCHAR(100) | | Subcategory |
| `description` | TEXT | | Line item description |
| `planned_amount` | DECIMAL(15,2) | NOT NULL | Planned budget |
| `actual_amount` | DECIMAL(15,2) | DEFAULT 0 | Actual spent |
| `variance` | DECIMAL(15,2) | GENERATED ALWAYS AS (planned_amount - actual_amount) STORED | Variance |
| `sort_order` | INTEGER | DEFAULT 0 | Display order |

**Indexes:**
```sql
CREATE INDEX idx_budget_lines_budget ON budget_lines(budget_id);
CREATE INDEX idx_budget_lines_category ON budget_lines(category, budget_id);
```

---

### 4.3 `expenses`

Project expenses.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | |
| `tenant_id` | VARCHAR(100) | NOT NULL, INDEX | |
| `project_id` | UUID | FK → projects.id, NOT NULL | |
| `budget_line_id` | UUID | FK → budget_lines.id | Associated budget line |
| `expense_number` | VARCHAR(50) | UNIQUE NOT NULL | Expense number |
| `expense_date` | DATE | NOT NULL | Date of expense |
| `category` | VARCHAR(100) | NOT NULL | Expense category |
| `description` | TEXT | NOT NULL | Description |
| `amount` | DECIMAL(15,2) | NOT NULL | Amount |
| `currency` | VARCHAR(3) | DEFAULT 'KES' | |
| `vendor` | VARCHAR(255) | | Vendor/supplier name |
| `payment_method` | VARCHAR(50) | | Method: cash, bank_transfer, mpesa, card |
| `receipt_path` | TEXT | | Path to receipt document |
| `is_reimbursable` | BOOLEAN | DEFAULT false | Whether reimbursable |
| `submitted_by` | UUID | FK → users.id, NOT NULL | Person who incurred expense |
| `approved_by` | UUID | FK → users.id | Approver |
| `approved_at` | TIMESTAMPTZ | | Approval timestamp |
| `paid_at` | TIMESTAMPTZ | | Payment timestamp |
| `status` | VARCHAR(50) | NOT NULL, DEFAULT 'draft' | Status: draft, submitted, approved, rejected, paid |
| `rejection_reason` | TEXT | | Reason if rejected |
| `metadata` | JSONB | | Additional metadata |
| `created_at` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | |

**Indexes:**
```sql
CREATE INDEX idx_expenses_project ON expenses(project_id, tenant_id);
CREATE INDEX idx_expenses_submitted_by ON expenses(submitted_by);
CREATE INDEX idx_expenses_status ON expenses(status, project_id);
CREATE INDEX idx_expenses_date ON expenses(expense_date, project_id);
```

---

### 4.4 `vouchers`

Payment vouchers for project costs.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | |
| `tenant_id` | VARCHAR(100) | NOT NULL, INDEX | |
| `project_id` | UUID | FK → projects.id, NOT NULL | |
| `budget_line_id` | UUID | FK → budget_lines.id | |
| `voucher_number` | VARCHAR(50) | UNIQUE NOT NULL | Voucher number |
| `voucher_type` | VARCHAR(50) | NOT NULL | Type: consultant_payment, casual_payment, vendor_payment, advance, reimbursement |
| `payee_name` | VARCHAR(255) | NOT NULL | Payee name |
| `payee_type` | VARCHAR(50) | | Type: employee, consultant, vendor, contractor |
| `payee_id` | UUID | | Reference to external entity (HRM employee, CRM vendor) |
| `amount` | DECIMAL(15,2) | NOT NULL | Payment amount |
| `currency` | VARCHAR(3) | DEFAULT 'KES' | |
| `description` | TEXT | NOT NULL | Purpose of payment |
| `payment_method` | VARCHAR(50) | | Method: bank_transfer, mpesa, cheque, cash |
| `bank_name` | VARCHAR(100) | | Payee bank |
| `account_number` | VARCHAR(50) | | Payee account number (encrypted) |
| `mobile_number` | VARCHAR(20) | | M-Pesa mobile number |
| `requested_by` | UUID | FK → users.id, NOT NULL | Requester |
| `requested_at` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | Request timestamp |
| `approved_by` | UUID[] | ARRAY | Array of approvers (multi-stage approval) |
| `approval_status` | VARCHAR(50) | NOT NULL, DEFAULT 'pending' | Status: pending, approved, rejected, paid, cancelled |
| `approval_level` | INTEGER | DEFAULT 1 | Current approval level |
| `approved_at` | TIMESTAMPTZ | | Final approval timestamp |
| `paid_at` | TIMESTAMPTZ | | Payment timestamp |
| `rejection_reason` | TEXT | | Reason if rejected |
| `treasury_payment_id` | UUID | | Link to treasury service payment record |
| `supporting_documents` | TEXT[] | ARRAY | Paths to supporting documents |
| `metadata` | JSONB | | Additional metadata |

**Indexes:**
```sql
CREATE INDEX idx_vouchers_project ON vouchers(project_id, tenant_id);
CREATE INDEX idx_vouchers_status ON vouchers(approval_status, tenant_id);
CREATE INDEX idx_vouchers_requested_by ON vouchers(requested_by);
CREATE INDEX idx_vouchers_payee ON vouchers(payee_id, payee_type);
```

---

### 4.5 `time_logs`

Time tracking entries.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | |
| `tenant_id` | VARCHAR(100) | NOT NULL, INDEX | |
| `project_id` | UUID | FK → projects.id, NOT NULL | |
| `task_id` | UUID | FK → tasks.id | Specific task (optional) |
| `user_id` | UUID | FK → users.id, NOT NULL | User who logged time |
| `log_date` | DATE | NOT NULL | Date of work |
| `start_time` | TIME | | Start time (if timer used) |
| `end_time` | TIME | | End time (if timer used) |
| `hours` | DECIMAL(8,2) | NOT NULL | Hours worked |
| `is_billable` | BOOLEAN | DEFAULT true | Whether billable |
| `description` | TEXT | | Work description |
| `approved_by` | UUID | FK → users.id | Approver |
| `approved_at` | TIMESTAMPTZ | | Approval timestamp |
| `status` | VARCHAR(50) | NOT NULL, DEFAULT 'draft' | Status: draft, submitted, approved, rejected, invoiced |
| `created_at` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | |
| `updated_at` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | |

**Indexes:**
```sql
CREATE INDEX idx_time_logs_project ON time_logs(project_id, tenant_id);
CREATE INDEX idx_time_logs_task ON time_logs(task_id);
CREATE INDEX idx_time_logs_user ON time_logs(user_id, log_date);
CREATE INDEX idx_time_logs_date ON time_logs(log_date, project_id);
```

---

### 4.6 `resources`

Resource pool (people, equipment, facilities).

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | |
| `tenant_id` | VARCHAR(100) | NOT NULL, INDEX | |
| `resource_type` | VARCHAR(50) | NOT NULL | Type: person, equipment, facility, vehicle |
| `resource_id` | UUID | | External ID (HRM employee, inventory equipment) |
| `name` | VARCHAR(255) | NOT NULL | Resource name |
| `description` | TEXT | | Description |
| `skills` | VARCHAR(100)[] | ARRAY | Skills/capabilities |
| `hourly_rate` | DECIMAL(10,2) | | Standard hourly rate |
| `availability_percentage` | INTEGER | DEFAULT 100 CHECK (availability_percentage >= 0 AND availability_percentage <= 100) | Availability % |
| `capacity_hours_per_week` | DECIMAL(8,2) | DEFAULT 40 | Capacity per week |
| `is_active` | BOOLEAN | DEFAULT true | Active status |
| `metadata` | JSONB | | Additional metadata |
| `created_at` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | |

**Indexes:**
```sql
CREATE INDEX idx_resources_tenant ON resources(tenant_id, resource_type);
CREATE INDEX idx_resources_external ON resources(resource_id, resource_type);
CREATE INDEX idx_resources_skills ON resources USING GIN(skills);
```

---

### 4.7 `resource_allocations`

Resource assignments to projects.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | |
| `tenant_id` | VARCHAR(100) | NOT NULL, INDEX | |
| `project_id` | UUID | FK → projects.id, NOT NULL | |
| `resource_id` | UUID | FK → resources.id, NOT NULL | |
| `allocation_percentage` | INTEGER | NOT NULL CHECK (allocation_percentage > 0 AND allocation_percentage <= 100) | % of resource time |
| `start_date` | DATE | NOT NULL | Allocation start |
| `end_date` | DATE | | Allocation end (NULL = ongoing) |
| `role` | VARCHAR(100) | | Role in project |
| `notes` | TEXT | | Additional notes |
| `created_at` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | |
| `created_by` | UUID | FK → users.id | |

**Indexes:**
```sql
CREATE INDEX idx_resource_alloc_project ON resource_allocations(project_id);
CREATE INDEX idx_resource_alloc_resource ON resource_allocations(resource_id, start_date, end_date);
CREATE INDEX idx_resource_alloc_dates ON resource_allocations(start_date, end_date);
```

---

## 5. Collaboration Entities

### 5.1 `comments`

Comments on projects, tasks, tenders, etc.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | |
| `tenant_id` | VARCHAR(100) | NOT NULL, INDEX | |
| `entity_type` | VARCHAR(50) | NOT NULL | Type: project, task, tender, deliverable, expense |
| `entity_id` | UUID | NOT NULL | ID of entity being commented on |
| `parent_comment_id` | UUID | FK → comments.id | For threaded replies |
| `content` | TEXT | NOT NULL | Comment text |
| `mentions` | UUID[] | ARRAY | Array of mentioned user IDs |
| `author_id` | UUID | FK → users.id, NOT NULL | Comment author |
| `is_edited` | BOOLEAN | DEFAULT false | Whether comment was edited |
| `edited_at` | TIMESTAMPTZ | | Last edit timestamp |
| `created_at` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | |
| `deleted_at` | TIMESTAMPTZ | | Soft delete |

**Indexes:**
```sql
CREATE INDEX idx_comments_entity ON comments(entity_type, entity_id, tenant_id);
CREATE INDEX idx_comments_author ON comments(author_id);
CREATE INDEX idx_comments_parent ON comments(parent_comment_id);
CREATE INDEX idx_comments_created ON comments(created_at DESC);
```

---

### 5.2 `attachments`

File attachments.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | |
| `tenant_id` | VARCHAR(100) | NOT NULL, INDEX | |
| `entity_type` | VARCHAR(50) | NOT NULL | Type: project, task, tender, comment, expense, voucher |
| `entity_id` | UUID | NOT NULL | ID of entity |
| `file_name` | VARCHAR(255) | NOT NULL | Original file name |
| `file_path` | TEXT | NOT NULL | S3/MinIO path |
| `file_size` | BIGINT | | File size in bytes |
| `mime_type` | VARCHAR(100) | | MIME type |
| `is_public` | BOOLEAN | DEFAULT false | Public access |
| `uploaded_by` | UUID | FK → users.id, NOT NULL | Uploader |
| `uploaded_at` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | |
| `metadata` | JSONB | | Additional metadata (dimensions for images, etc.) |

**Indexes:**
```sql
CREATE INDEX idx_attachments_entity ON attachments(entity_type, entity_id, tenant_id);
CREATE INDEX idx_attachments_uploaded_by ON attachments(uploaded_by);
```

---

### 5.3 `activity_logs`

Activity feed for projects and tasks.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | |
| `tenant_id` | VARCHAR(100) | NOT NULL, INDEX | |
| `entity_type` | VARCHAR(50) | NOT NULL | Type: project, task, tender, etc. |
| `entity_id` | UUID | NOT NULL | Entity ID |
| `activity_type` | VARCHAR(50) | NOT NULL | Type: created, updated, commented, status_changed, assigned, completed |
| `actor_id` | UUID | FK → users.id, NOT NULL | User who performed action |
| `description` | TEXT | | Human-readable description |
| `changes` | JSONB | | JSON of field changes (old/new values) |
| `metadata` | JSONB | | Additional context |
| `created_at` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | |

**Indexes:**
```sql
CREATE INDEX idx_activity_logs_entity ON activity_logs(entity_type, entity_id, tenant_id);
CREATE INDEX idx_activity_logs_actor ON activity_logs(actor_id);
CREATE INDEX idx_activity_logs_created ON activity_logs(created_at DESC);
CREATE INDEX idx_activity_logs_type ON activity_logs(activity_type, entity_id);
```

---

### 5.4 `notifications`

User notifications.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | |
| `tenant_id` | VARCHAR(100) | NOT NULL, INDEX | |
| `user_id` | UUID | FK → users.id, NOT NULL | Recipient |
| `notification_type` | VARCHAR(50) | NOT NULL | Type: task_assigned, deadline_approaching, comment_mention, status_change |
| `entity_type` | VARCHAR(50) | | Related entity type |
| `entity_id` | UUID | | Related entity ID |
| `title` | VARCHAR(255) | NOT NULL | Notification title |
| `message` | TEXT | | Notification message |
| `is_read` | BOOLEAN | DEFAULT false | Read status |
| `read_at` | TIMESTAMPTZ | | When read |
| `action_url` | TEXT | | URL to navigate to |
| `priority` | VARCHAR(20) | DEFAULT 'normal' | Priority: critical, high, normal, low |
| `created_at` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | |
| `expires_at` | TIMESTAMPTZ | | Expiration timestamp |

**Indexes:**
```sql
CREATE INDEX idx_notifications_user ON notifications(user_id, is_read, created_at DESC);
CREATE INDEX idx_notifications_entity ON notifications(entity_type, entity_id);
CREATE INDEX idx_notifications_expires ON notifications(expires_at) WHERE expires_at IS NOT NULL;
```

---

### 5.5 `meetings`

Project meetings and minutes.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | |
| `tenant_id` | VARCHAR(100) | NOT NULL, INDEX | |
| `project_id` | UUID | FK → projects.id | Related project (NULL for cross-project) |
| `title` | VARCHAR(255) | NOT NULL | Meeting title |
| `description` | TEXT | | Meeting agenda |
| `meeting_type` | VARCHAR(50) | | Type: kickoff, status, review, governance, closeout |
| `start_time` | TIMESTAMPTZ | NOT NULL | |
| `end_time` | TIMESTAMPTZ | NOT NULL | |
| `location` | TEXT | | Physical location or "Virtual" |
| `meeting_platform` | VARCHAR(50) | | Platform: zoom, teams, google_meet, zoho_meet, webex |
| `meeting_link` | TEXT | | Virtual meeting link |
| `meeting_id` | VARCHAR(255) | | External meeting ID |
| `organizer_id` | UUID | FK → users.id, NOT NULL | |
| `attendees` | UUID[] | ARRAY | Array of attendee user IDs |
| `required_attendees` | UUID[] | ARRAY | Required attendees |
| `optional_attendees` | UUID[] | ARRAY | Optional attendees |
| `status` | VARCHAR(50) | DEFAULT 'scheduled' | Status: scheduled, in_progress, completed, cancelled |
| `minutes` | TEXT | | Meeting minutes |
| `recording_url` | TEXT | | Recording link |
| `action_items` | JSONB | | Action items from meeting |
| `created_at` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | |
| `created_by` | UUID | FK → users.id | |

**Indexes:**
```sql
CREATE INDEX idx_meetings_project ON meetings(project_id, tenant_id);
CREATE INDEX idx_meetings_time ON meetings(start_time, tenant_id);
CREATE INDEX idx_meetings_organizer ON meetings(organizer_id);
```

---

## 6. Governance & Compliance Entities

### 6.1 `governance_teams`

Project governance structure.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | |
| `tenant_id` | VARCHAR(100) | NOT NULL, INDEX | |
| `project_id` | UUID | FK → projects.id, NOT NULL | |
| `role` | VARCHAR(50) | NOT NULL | Role: steering_committee, sponsor, pmo, quality_assurance |
| `user_id` | UUID | FK → users.id, NOT NULL | Member |
| `is_chair` | BOOLEAN | DEFAULT false | Whether member is chair/lead |
| `responsibilities` | TEXT | | Role responsibilities |
| `assigned_at` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | |
| `assigned_by` | UUID | FK → users.id | |

**Indexes:**
```sql
CREATE INDEX idx_governance_project ON governance_teams(project_id, tenant_id);
CREATE INDEX idx_governance_user ON governance_teams(user_id, role);
```

---

### 6.2 `decisions`

Project decision log.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | |
| `tenant_id` | VARCHAR(100) | NOT NULL, INDEX | |
| `project_id` | UUID | FK → projects.id, NOT NULL | |
| `decision_number` | VARCHAR(50) | | Decision reference number |
| `title` | VARCHAR(500) | NOT NULL | Decision title |
| `description` | TEXT | NOT NULL | Decision details |
| `decision_type` | VARCHAR(50) | | Type: strategic, tactical, operational |
| `context` | TEXT | | Background and context |
| `options_considered` | JSONB | | Array of options that were considered |
| `decision` | TEXT | NOT NULL | The actual decision made |
| `rationale` | TEXT | | Justification |
| `impact` | TEXT | | Expected impact |
| `decided_by` | UUID | FK → users.id, NOT NULL | Decision maker |
| `decided_at` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | |
| `stakeholders` | UUID[] | ARRAY | Involved/affected stakeholders |
| `status` | VARCHAR(50) | DEFAULT 'approved' | Status: proposed, approved, implemented, reversed |
| `implemented_at` | TIMESTAMPTZ | | Implementation date |

**Indexes:**
```sql
CREATE INDEX idx_decisions_project ON decisions(project_id, tenant_id);
CREATE INDEX idx_decisions_decided_by ON decisions(decided_by);
CREATE INDEX idx_decisions_decided_at ON decisions(decided_at DESC);
```

---

### 6.3 `change_requests`

Change control/request management.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | |
| `tenant_id` | VARCHAR(100) | NOT NULL, INDEX | |
| `project_id` | UUID | FK → projects.id, NOT NULL | |
| `request_number` | VARCHAR(50) | UNIQUE NOT NULL | Change request number |
| `title` | VARCHAR(500) | NOT NULL | Change title |
| `description` | TEXT | NOT NULL | Detailed description |
| `change_type` | VARCHAR(50) | NOT NULL | Type: scope, schedule, budget, resource, requirement |
| `rationale` | TEXT | NOT NULL | Why change is needed |
| `impact_scope` | TEXT | | Impact on scope |
| `impact_schedule` | TEXT | | Impact on timeline |
| `impact_budget` | DECIMAL(15,2) | | Financial impact |
| `impact_quality` | TEXT | | Impact on quality |
| `requested_by` | UUID | FK → users.id, NOT NULL | |
| `requested_at` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | |
| `priority` | VARCHAR(20) | DEFAULT 'medium' | Priority level |
| `status` | VARCHAR(50) | NOT NULL, DEFAULT 'submitted' | Status: submitted, under_review, approved, rejected, implemented |
| `reviewed_by` | UUID[] | ARRAY | Array of reviewers |
| `approved_by` | UUID | FK → users.id | Final approver |
| `approved_at` | TIMESTAMPTZ | | Approval timestamp |
| `rejection_reason` | TEXT | | Reason if rejected |
| `implemented_at` | TIMESTAMPTZ | | Implementation date |

**Indexes:**
```sql
CREATE INDEX idx_change_requests_project ON change_requests(project_id, tenant_id);
CREATE INDEX idx_change_requests_status ON change_requests(status, project_id);
CREATE INDEX idx_change_requests_requested ON change_requests(requested_at DESC);
```

---

### 6.4 `risks`

Risk register.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | |
| `tenant_id` | VARCHAR(100) | NOT NULL, INDEX | |
| `project_id` | UUID | FK → projects.id, NOT NULL | |
| `risk_number` | VARCHAR(50) | | Risk ID |
| `title` | VARCHAR(500) | NOT NULL | Risk title |
| `description` | TEXT | NOT NULL | Risk description |
| `category` | VARCHAR(50) | | Category: technical, financial, resource, external, regulatory |
| `probability` | VARCHAR(20) | NOT NULL | Probability: very_low, low, medium, high, very_high |
| `probability_score` | INTEGER | CHECK (probability_score >= 1 AND probability_score <= 5) | Numeric probability (1-5) |
| `impact` | VARCHAR(20) | NOT NULL | Impact: very_low, low, medium, high, very_high |
| `impact_score` | INTEGER | CHECK (impact_score >= 1 AND impact_score <= 5) | Numeric impact (1-5) |
| `risk_score` | INTEGER | GENERATED ALWAYS AS (probability_score * impact_score) STORED | Risk score (1-25) |
| `status` | VARCHAR(50) | NOT NULL, DEFAULT 'identified' | Status: identified, analyzing, mitigating, monitoring, closed, occurred |
| `mitigation_strategy` | TEXT | | Mitigation plan |
| `contingency_plan` | TEXT | | Contingency if risk occurs |
| `owner_id` | UUID | FK → users.id | Risk owner |
| `identified_by` | UUID | FK → users.id, NOT NULL | |
| `identified_at` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | |
| `review_date` | DATE | | Next review date |
| `closed_at` | TIMESTAMPTZ | | Closure date |
| `occurred_at` | TIMESTAMPTZ | | Date risk became an issue |

**Indexes:**
```sql
CREATE INDEX idx_risks_project ON risks(project_id, tenant_id);
CREATE INDEX idx_risks_status ON risks(status, project_id);
CREATE INDEX idx_risks_score ON risks(risk_score DESC, project_id);
CREATE INDEX idx_risks_owner ON risks(owner_id);
```

---

### 6.5 `quality_checks`

Quality assurance tracking.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | |
| `tenant_id` | VARCHAR(100) | NOT NULL, INDEX | |
| `project_id` | UUID | FK → projects.id, NOT NULL | |
| `deliverable_id` | UUID | FK → deliverables.id | Related deliverable |
| `task_id` | UUID | FK → tasks.id | Related task |
| `check_type` | VARCHAR(50) | NOT NULL | Type: review, audit, inspection, test |
| `title` | VARCHAR(255) | NOT NULL | Check title |
| `description` | TEXT | | Check description |
| `checklist` | JSONB | | JSON array of checklist items |
| `scheduled_date` | DATE | | Scheduled date |
| `completed_date` | DATE | | Actual completion date |
| `result` | VARCHAR(50) | | Result: passed, failed, conditional_pass |
| `findings` | TEXT | | Findings and observations |
| `recommendations` | TEXT | | Recommendations for improvement |
| `conducted_by` | UUID | FK → users.id | Quality checker |
| `status` | VARCHAR(50) | NOT NULL, DEFAULT 'planned' | Status: planned, in_progress, completed, cancelled |

**Indexes:**
```sql
CREATE INDEX idx_quality_checks_project ON quality_checks(project_id, tenant_id);
CREATE INDEX idx_quality_checks_deliverable ON quality_checks(deliverable_id);
CREATE INDEX idx_quality_checks_status ON quality_checks(status, scheduled_date);
```

---

### 6.6 `audit_logs`

Comprehensive audit trail (write-only).

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | |
| `tenant_id` | VARCHAR(100) | NOT NULL, INDEX | |
| `user_id` | UUID | FK → users.id | User who performed action (NULL for system) |
| `entity_type` | VARCHAR(50) | NOT NULL | Entity type |
| `entity_id` | UUID | NOT NULL | Entity ID |
| `action` | VARCHAR(50) | NOT NULL | Action: CREATE, UPDATE, DELETE, APPROVE, SUBMIT |
| `old_values` | JSONB | | Previous values (for UPDATE/DELETE) |
| `new_values` | JSONB | | New values (for CREATE/UPDATE) |
| `ip_address` | INET | | Client IP address |
| `user_agent` | TEXT | | Client user agent |
| `request_id` | UUID | | Request correlation ID |
| `timestamp` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | |
| `checksum` | VARCHAR(64) | | SHA-256 checksum for tamper detection |

**Indexes:**
```sql
CREATE INDEX idx_audit_logs_tenant ON audit_logs(tenant_id, timestamp DESC);
CREATE INDEX idx_audit_logs_entity ON audit_logs(entity_type, entity_id);
CREATE INDEX idx_audit_logs_user ON audit_logs(user_id, timestamp DESC);
CREATE INDEX idx_audit_logs_action ON audit_logs(action, timestamp DESC);
-- Partitioning by month recommended for large audit tables
```

---

## 7. Integration & System Entities

### 7.1 `users`

Local user cache (synced from auth-service).

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | User ID from auth-service |
| `tenant_id` | VARCHAR(100) | NOT NULL, INDEX | |
| `email` | VARCHAR(255) | NOT NULL | |
| `full_name` | VARCHAR(255) | NOT NULL | |
| `avatar_url` | TEXT | | Profile picture URL |
| `is_active` | BOOLEAN | DEFAULT true | |
| `last_synced_at` | TIMESTAMPTZ | | Last sync from auth-service |
| `created_at` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | |
| `updated_at` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | |

**Indexes:**
```sql
CREATE UNIQUE INDEX idx_users_email ON users(email, tenant_id);
CREATE INDEX idx_users_tenant ON users(tenant_id, is_active);
```

---

### 7.2 `permissions`

Permission definitions.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | |
| `code` | VARCHAR(100) | UNIQUE NOT NULL | Permission code (e.g., 'projects:write') |
| `name` | VARCHAR(100) | NOT NULL | Display name |
| `description` | TEXT | | Description |
| `resource` | VARCHAR(50) | NOT NULL | Resource type |
| `action` | VARCHAR(50) | NOT NULL | Action type |
| `is_system` | BOOLEAN | DEFAULT true | System vs custom permission |

**Indexes:**
```sql
CREATE UNIQUE INDEX idx_permissions_code ON permissions(code);
CREATE INDEX idx_permissions_resource ON permissions(resource, action);
```

---

### 7.3 `role_permissions`

Role-permission assignments.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | |
| `role_id` | UUID | FK → project_roles.id, NOT NULL | |
| `permission_id` | UUID | FK → permissions.id, NOT NULL | |

**Indexes:**
```sql
CREATE UNIQUE INDEX idx_role_perm_unique ON role_permissions(role_id, permission_id);
CREATE INDEX idx_role_permissions_role ON role_permissions(role_id);
```

---

### 7.4 `integrations`

External integration configurations.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | |
| `tenant_id` | VARCHAR(100) | NOT NULL, INDEX | |
| `integration_type` | VARCHAR(50) | NOT NULL | Type: google_workspace, microsoft_365, slack, jira, github |
| `name` | VARCHAR(255) | NOT NULL | Display name |
| `is_enabled` | BOOLEAN | DEFAULT true | Active status |
| `config` | JSONB | NOT NULL | Configuration (encrypted credentials) |
| `oauth_token` | TEXT | | OAuth access token (encrypted) |
| `oauth_refresh_token` | TEXT | | OAuth refresh token (encrypted) |
| `token_expires_at` | TIMESTAMPTZ | | Token expiration |
| `last_sync_at` | TIMESTAMPTZ | | Last successful sync |
| `sync_status` | VARCHAR(50) | | Status: connected, error, pending |
| `error_message` | TEXT | | Last error message |
| `created_at` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | |
| `created_by` | UUID | FK → users.id | |

**Indexes:**
```sql
CREATE INDEX idx_integrations_tenant ON integrations(tenant_id, integration_type);
CREATE INDEX idx_integrations_enabled ON integrations(is_enabled, tenant_id);
```

---

### 7.5 `webhooks`

Outgoing webhook configurations.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | |
| `tenant_id` | VARCHAR(100) | NOT NULL, INDEX | |
| `name` | VARCHAR(255) | NOT NULL | Webhook name |
| `url` | TEXT | NOT NULL | Webhook endpoint URL |
| `secret` | VARCHAR(255) | | Webhook secret for signature validation |
| `events` | VARCHAR(100)[] | ARRAY | Array of event types to trigger on |
| `is_active` | BOOLEAN | DEFAULT true | Active status |
| `headers` | JSONB | | Custom HTTP headers |
| `created_at` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | |
| `created_by` | UUID | FK → users.id | |

**Indexes:**
```sql
CREATE INDEX idx_webhooks_tenant ON webhooks(tenant_id, is_active);
```

---

### 7.6 `webhook_logs`

Webhook delivery logs.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | |
| `webhook_id` | UUID | FK → webhooks.id, NOT NULL | |
| `event_type` | VARCHAR(100) | NOT NULL | Event that triggered webhook |
| `payload` | JSONB | NOT NULL | Event payload |
| `http_status` | INTEGER | | Response HTTP status |
| `response_body` | TEXT | | Response body |
| `error_message` | TEXT | | Error if delivery failed |
| `attempts` | INTEGER | DEFAULT 1 | Delivery attempt count |
| `delivered_at` | TIMESTAMPTZ | | Successful delivery timestamp |
| `created_at` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | |

**Indexes:**
```sql
CREATE INDEX idx_webhook_logs_webhook ON webhook_logs(webhook_id, created_at DESC);
CREATE INDEX idx_webhook_logs_event ON webhook_logs(event_type);
```

---

### 7.7 `reports`

Saved report configurations.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | |
| `tenant_id` | VARCHAR(100) | NOT NULL, INDEX | |
| `name` | VARCHAR(255) | NOT NULL | Report name |
| `description` | TEXT | | Report description |
| `report_type` | VARCHAR(50) | NOT NULL | Type: standard, custom |
| `category` | VARCHAR(50) | | Category: project, financial, resource, governance |
| `template` | VARCHAR(50) | | Template name for standard reports |
| `filters` | JSONB | | Report filters (project IDs, date ranges, etc.) |
| `schedule` | JSONB | | Schedule config for automated reports |
| `recipients` | UUID[] | ARRAY | Recipients for scheduled reports |
| `format` | VARCHAR(20) | DEFAULT 'pdf' | Format: pdf, xlsx, csv, html |
| `is_public` | BOOLEAN | DEFAULT false | Public or private |
| `created_at` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | |
| `created_by` | UUID | FK → users.id | |

**Indexes:**
```sql
CREATE INDEX idx_reports_tenant ON reports(tenant_id, report_type);
CREATE INDEX idx_reports_created_by ON reports(created_by);
```

---

## 8. Client & Issue Management Entities

### 8.1 `clients`

External clients/organizations.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | |
| `tenant_id` | VARCHAR(100) | NOT NULL, INDEX | |
| `name` | VARCHAR(255) | NOT NULL | |
| `industry` | VARCHAR(100) | | |
| `website` | VARCHAR(255) | | |
| `address` | TEXT | | |
| `contact_person` | VARCHAR(255) | | |
| `contact_email` | VARCHAR(255) | | |
| `contact_phone` | VARCHAR(50) | | |
| `metadata` | JSONB | | |
| `created_at` | TIMESTAMPTZ | DEFAULT NOW() | |

---

### 8.2 `client_users`

Users belonging to client organizations (for portal access).

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | |
| `tenant_id` | VARCHAR(100) | NOT NULL, INDEX | |
| `client_id` | UUID | FK → clients.id, NOT NULL | |
| `user_id` | UUID | FK → users.id, NOT NULL | Link to auth-service user |
| `role` | VARCHAR(50) | | Admin, Viewer, Approver |
| `is_active` | BOOLEAN | DEFAULT true | |
| `created_at` | TIMESTAMPTZ | DEFAULT NOW() | |

---

### 8.3 `issues`

Project issues, bugs, or risks.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | |
| `tenant_id` | VARCHAR(100) | NOT NULL, INDEX | |
| `project_id` | UUID | FK → projects.id, NOT NULL | |
| `task_id` | UUID | FK → tasks.id | Optional link to task |
| `title` | VARCHAR(500) | NOT NULL | |
| `description` | TEXT | | |
| `issue_type` | VARCHAR(50) | | Bug, Issue, Risk, Impediment |
| `priority` | VARCHAR(20) | | Critical, High, Medium, Low |
| `severity` | VARCHAR(20) | | Blocker, Major, Minor, Trivial |
| `status` | VARCHAR(50) | | Open, In Progress, Resolved, Closed, Won't Fix |
| `reporter_id` | UUID | FK → users.id, NOT NULL | |
| `assignee_id` | UUID | FK → users.id | |
| `due_date` | DATE | | |
| `resolved_at` | TIMESTAMPTZ | | |
| `resolution_notes` | TEXT | | |
| `metadata` | JSONB | | |
| `created_at` | TIMESTAMPTZ | DEFAULT NOW() | |
| `updated_at` | TIMESTAMPTZ | DEFAULT NOW() | |

---

### 8.4 `issue_comments`

Comments specifically for issues.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | |
| `tenant_id` | VARCHAR(100) | NOT NULL, INDEX | |
| `issue_id` | UUID | FK → issues.id, NOT NULL | |
| `author_id` | UUID | FK → users.id, NOT NULL | |
| `content` | TEXT | NOT NULL | |
| `created_at` | TIMESTAMPTZ | DEFAULT NOW() | |

---

## 9. Activity Management Entities

### 8.1 `activities`

Specific project activities like workshops, site visits, etc.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | |
| `tenant_id` | VARCHAR(100) | NOT NULL, INDEX | |
| `project_id` | UUID | FK → projects.id, NOT NULL | |
| `output_id` | UUID | FK → activity_outputs.id | Link to workplan output |
| `sub_output_id` | UUID | FK → activity_sub_outputs.id | Link to workplan sub-output |
| `title` | VARCHAR(500) | NOT NULL | Activity title |
| `description` | TEXT | | Detailed description |
| `mode_of_delivery` | VARCHAR(50) | NOT NULL | Field Visit, Meeting, Workshop, Virtual, LOE |
| `activity_type` | VARCHAR(50) | DEFAULT 'general' | General, CPU, etc. |
| `funding_source_id` | UUID | FK → funding_sources.id | |
| `person_responsible_id` | UUID | FK → users.id | |
| `start_date` | DATE | | |
| `end_date` | DATE | | |
| `pax` | INTEGER | DEFAULT 0 | Number of participants |
| `days` | INTEGER | DEFAULT 0 | Duration in days |
| `frequency` | INTEGER | DEFAULT 1 | How often it occurs |
| `status` | VARCHAR(50) | DEFAULT 'not_started' | Not Started, Started, Completed |
| `deliverables` | TEXT | | Expected deliverables |
| `q1`, `q2`, `q3`, `q4` | BOOLEAN | DEFAULT false | Quarterly planning flags |
| `metadata` | JSONB | | |
| `created_at` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | |
| `updated_at` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | |

**Indexes:**
```sql
CREATE INDEX idx_activities_project ON activities(project_id, tenant_id);
CREATE INDEX idx_activities_status ON activities(status, project_id);
```

---

### 8.2 `activity_budgets`

Detailed budget breakdown for a specific activity.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | |
| `tenant_id` | VARCHAR(100) | NOT NULL, INDEX | |
| `activity_id` | UUID | FK → activities.id, NOT NULL | |
| `budget_line_id` | UUID | FK → budget_lines.id | Link to main project budget line |
| `conference_budget` | DECIMAL(15,2) | DEFAULT 0 | Venue/Conference costs |
| `mie_budget` | DECIMAL(15,2) | DEFAULT 0 | Meals, Incidental & Expenses |
| `transport_budget` | DECIMAL(15,2) | DEFAULT 0 | Ground transport |
| `air_travel_budget` | DECIMAL(15,2) | DEFAULT 0 | |
| `internet_budget` | DECIMAL(15,2) | DEFAULT 0 | |
| `airtime_budget` | DECIMAL(15,2) | DEFAULT 0 | |
| `equipment_budget` | DECIMAL(15,2) | DEFAULT 0 | Projector, PA, etc. |
| `total_budget` | DECIMAL(15,2) | NOT NULL | Sum of all categories |
| `currency` | VARCHAR(3) | DEFAULT 'KES' | |
| `created_at` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | |

**Indexes:**
```sql
CREATE INDEX idx_activity_budgets_activity ON activity_budgets(activity_id);
```

---

### 8.3 `activity_reports`

Reports submitted after activity completion.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | |
| `tenant_id` | VARCHAR(100) | NOT NULL, INDEX | |
| `activity_id` | UUID | FK → activities.id, NOT NULL | |
| `submitted_by` | UUID | FK → users.id, NOT NULL | |
| `approver_id` | UUID | FK → users.id | |
| `status` | VARCHAR(50) | DEFAULT 'pending' | Pending, Approved, Update Requested |
| `title` | VARCHAR(255) | | |
| `description` | TEXT | | |
| `submission_date` | TIMESTAMPTZ | DEFAULT NOW() | |
| `updated_at` | TIMESTAMPTZ | DEFAULT NOW() | |

**Indexes:**
```sql
CREATE INDEX idx_activity_reports_activity ON activity_reports(activity_id);
CREATE INDEX idx_activity_reports_status ON activity_reports(status);
```

---

### 8.4 `activity_report_reviews`

Reviews and feedback on activity reports.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | |
| `tenant_id` | VARCHAR(100) | NOT NULL, INDEX | |
| `activity_report_id` | UUID | FK → activity_reports.id, NOT NULL | |
| `reviewer_id` | UUID | FK → users.id, NOT NULL | |
| `status` | VARCHAR(50) | | Approved, Pending, Update Requested |
| `comment` | TEXT | | |
| `review_file_path` | TEXT | | Path to review document |
| `created_at` | TIMESTAMPTZ | DEFAULT NOW() | |

---

### 8.5 `activity_signing_sheets`

Digital attendance/signing sheets for activities.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| `id` | UUID | PRIMARY KEY | |
| `tenant_id` | VARCHAR(100) | NOT NULL, INDEX | |
| `activity_id` | UUID | FK → activities.id, NOT NULL | |
| `participant_name` | VARCHAR(255) | NOT NULL | |
| `participant_email` | VARCHAR(255) | | |
| `participant_phone` | VARCHAR(50) | | |
| `organization` | VARCHAR(255) | | |
| `designation` | VARCHAR(255) | | |
| `signature_path` | TEXT | | Path to digital signature image |
| `signed_at` | TIMESTAMPTZ | DEFAULT NOW() | |
| `verified_by` | UUID | FK → users.id | |

---

### 8.6 `activity_lookup_tables`

Supporting tables for activity classification.

#### `activity_outputs`
- `id`, `tenant_id`, `title`, `created_at`

#### `activity_sub_outputs`
- `id`, `tenant_id`, `output_id` (FK), `title`, `created_at`

#### `activity_sub_activities`
- `id`, `tenant_id`, `title`, `created_at`

#### `funding_sources`
- `id`, `tenant_id`, `name`, `description`, `created_at`

---

## Summary Statistics

### Total Entities: 59

| Category | Entity Count |
|----------|--------------|
| Tender Management | 8 |
| Project Management | 3 |
| Task Management | 6 |
| Resource & Budget | 7 |
| Collaboration | 5 |
| Governance & Compliance | 6 |
| Client & Issue Management | 4 |
| Integration & System | 7 |
| Activity Management | 13 |

### Vector Columns Summary

| Entity | Column | Vector Dimensions | Purpose |
|--------|--------|-------------------|---------|
| `tenders` | `description_embedding` | 1536 | Semantic search of tenders |
| `tender_documents` | `content_embedding` | 1536 | Document content search |
| `projects` | `description_embedding` | 1536 | Semantic search of projects |
| `tasks` | `description_embedding` | 768 | Task similarity and search |

### Key Relationships

- **Tender → Project**: One-to-one conversion when tender is awarded
- **Project → Tasks**: One-to-many (hierarchical with subtasks)
- **Project → Milestones → Deliverables**: Hierarchical structure
- **Project → Budget → Budget Lines**: Hierarchical budget allocation
- **Project → Expenses/Vouchers**: Many-to-many via budget lines
- **Task → Dependencies**: Many-to-many self-referential
- **User → Multiple Entities**: One-to-many for ownership and assignments

---

**Document Version**: 1.0  
**Last Updated**: 2024-12-05  
**Maintained By**: Projects Service Team
