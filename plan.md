# Projects Service

## Executive Summary

The Projects Service is a comprehensive, enterprise-grade project management and tender management platform designed for the BengoBox ecosystem. It provides end-to-end lifecycle management from tender identification through project completion, integrating seamlessly with all internal microservices and popular third-party collaboration tools.

### Vision

To deliver a unified platform that rivals Zoho Projects, Microsoft Project, and Jira, while providing specialized tender management capabilities and deep integration with the BengoBox ecosystem.

---

## 1. Core Features Overview

### 1.1 Tender Management Module (Pre-Project Phase)

#### A. Tender Discovery & Logging
- **Opportunity Identification**: Dedicated interface for business development teams to log discovered tenders/RFPs
- **Tender Repository**: Centralized database of all identified opportunities
- **Source Tracking**: Track tender sources (government portals, industry websites, referrals)
- **Automatic Imports**: Integration with tender notification services
- **Document Storage**: Upload and manage tender documents, requirements, evaluation criteria
- **Deadline Tracking**: Submission deadlines with visual countdown and alerts

#### B. Tender Evaluation & Selection
- **Committee Management**: Form dedicated tender evaluation committees
- **Meeting Scheduling**: Integration with Google Meet, Microsoft Teams, Zoho Meet
  - Virtual meeting room creation
  - Automatic calendar invites
  - Meeting notes and recordings storage
- **Evaluation Criteria**: Customizable scoring matrices for tender assessment
- **Decision Workflow**: Multi-stage approval process (Initial Review → Detailed Analysis → Go/No-Go Decision)
- **Financial Analysis**: Cost-benefit analysis, resource requirements, profit projections
- **Risk Assessment**: SWOT analysis, risk registers, compliance checks
- **Voting System**: Committee members can vote and provide comments
- **Audit Trail**: Complete history of tender evaluation decisions

#### C. Tender Preparation & Document Management
- **Team Assignment**: Assign dedicated teams (internal/external consultants) to winning bids
- **Task Decomposition**: Break tender document into sections with individual assignments
- **Internal Deadlines**: Set milestones ahead of official submission deadline
- **Progress Tracking**: Real-time visibility of section completion status
- **Version Control**: Track document revisions and changes
- **Review Workflow**: Multi-level review process with approval gates
  - Section-level review
  - Technical review
  - Financial review
  - Legal compliance review
  - Final review
- **Comments & Annotations**: Reviewer feedback system with tracked resolution
- **Template Library**: Reusable tender response templates
- **Compliance Checklist**: Automated verification of tender requirements

#### D. Tender Submission
- **Submission Modes**: Support for physical and electronic submissions
- **Email Integration**: Automated email submission to specified recipient
- **Document Packaging**: Automatic compilation of final tender document
- **Submission Proof**: Digital receipts, tracking numbers, confirmation emails
- **Calendar Integration**: Submission deadline reminders and alerts
- **Last-minute Updates**: Emergency update workflow before submission

#### E. Post-Submission Tracking
- **Status Updates**: Track tender status (Submitted → Under Review → Shortlisted → Interview → Awarded/Lost)
- **Interview Management**: Schedule and track tender interviews/presentations
- **Follow-up Activities**: Manage clarification requests, presentations, site visits
- **Competitor Intelligence**: Track competitor participation (where available)
- **Lessons Learned**: Post-mortem analysis for both won and lost tenders
- **Success Metrics**: Win rate, average bid value, time-to-submission analytics

#### F. Automatic Project Conversion
- **Seamless Transition**: Awarded tenders automatically convert to projects
- **Data Migration**: Transfer all relevant tender data to project
  - Tender team becomes initial project team
  - Budget and timeline from tender
  - Deliverables and milestones
  - Client information and contacts
- **Kickoff Automation**: Generate project charter, kickoff meeting agenda, stakeholder register

---

### 1.2 Project Management Module

#### A. Project Planning
- **Project Charter**: Define vision, objectives, scope, success criteria
- **Work Breakdown Structure (WBS)**: Hierarchical task decomposition
- **Project Templates**: Industry-specific and custom templates
- **Gantt Charts**: Visual timeline with dependencies
- **Critical Path Analysis**: Identify critical tasks and bottlenecks
- **Resource Planning**: Resource allocation and leveling
- **Budget Planning**: Detailed cost estimates and budget allocation
- **Risk Management**: Risk identification, assessment, and mitigation planning
- **Stakeholder Management**: Stakeholder register, communication plan, engagement strategies

#### B. Task Management
- **Task Creation**: Rich task details (description, assignees, priority, labels, custom fields)
- **Subtasks**: Unlimited nesting of subtasks
- **Dependencies**: Task dependencies (Finish-to-Start, Start-to-Start, Finish-to-Finish, Start-to-Finish)
- **Task Templates**: Reusable task templates for common activities
- **Recurring Tasks**: Automated creation of recurring tasks
- **Task Checklists**: Subtask checklists within tasks
- **Time Estimates**: Estimated vs actual time tracking
- **Task Priorities**: P0 (Critical) to P4 (Low) with visual indicators
- **Task Status Workflow**: Customizable status transitions
- **Bulk Operations**: Bulk task updates, assignments, status changes

#### C. Team Collaboration
- **Team Management**: Add/remove team members, define roles
- **Role-Based Access**: Granular permissions (Owner, Admin, Manager, Member, Viewer, Guest)
- **Task Comments**: Threaded discussions on tasks
- **@Mentions**: Tag team members in comments and descriptions
- **Activity Feed**: Real-time feed of project activities
- **File Attachments**: Upload files to projects, tasks, comments (integration with Google Drive, OneDrive, Dropbox)
- **Screen Sharing**: Integration with collaboration tools for virtual collaboration
- **Document Collaboration**: Real-time collaborative editing via Google Docs/Microsoft 365 integration

#### D. Time Tracking
- **Time Logs**: Manual time entry by task
- **Timer**: Built-in timer for real-time tracking
- **Timesheet Management**: Weekly/monthly timesheets with approval workflow
- **Billable vs Non-billable**: Track billable hours for client invoicing
- **Overtime Tracking**: Identify and manage overtime
- **Integration**: Sync with HRM for payroll processing

#### E. Milestone & Deliverable Management
- **Milestone Definition**: Key project checkpoints with dates
- **Deliverable Registry**: Track all project deliverables
- **Acceptance Criteria**: Define clear acceptance criteria for deliverables
- **Approval Workflow**: Client/stakeholder approval process
- **Version Control**: Track deliverable versions and revisions
- **Milestone Reports**: Progress against milestones

#### F. Budget & Expense Management
- **Budget Allocation**: Project budget by category (Labor, Materials, Equipment, Overhead)
- **Expense Tracking**: Record project expenses in real-time
- **Voucher System**: Raise vouchers for payments (consultants, casuals, vendors)
  - Integration with Treasury Service for payment processing
  - Approval workflow for vouchers
  - Payment status tracking
- **Budget vs Actual**: Real-time variance analysis
- **Cost Forecasting**: Predict final project cost based on current burn rate
- **Invoice Integration**: Link invoices to projects (Finance integration)
- **Cost Allocation**: Allocate indirect costs across projects

#### G. Resource Management
- **Resource Pool**: Centralized view of all organizational resources
- **Resource Allocation**: Assign resources to projects/tasks
- **Capacity Planning**: Track resource utilization and availability
- **Resource Conflicts**: Identify over-allocation and conflicts
- **Skill Management**: Track resource skills and competencies (integration with HRM)
- **Resource Requests**: Request additional resources with approval workflow

#### H. Project Governance
- **Governance Structure**: Define governance team and hierarchy
  - Steering Committee
  - Project Sponsor
  - Project Manager
  - Technical Lead
  - Team Leads
- **Organizational Chart**: Visual hierarchy of project team
- **Roles & Responsibilities (RACI)**: RACI matrix for activities
- **Governance Meetings**: Schedule and track governance meetings
- **Decision Log**: Track key project decisions
- **Change Control**: Formal change request and approval process

#### I. Reporting & Analytics
- **Project Dashboards**: Customizable dashboards with key metrics
- **Standard Reports**:
  - Project Status Report
  - Progress Report (by time period)
  - Resource Utilization Report
  - Budget Report
  - Milestone Report
  - Risk Report
  - Time & Expense Report
  - Team Performance Report
- **Custom Reports**: Build custom reports with filters and visualizations
- **Report Scheduling**: Automated report generation and distribution
- **Export Options**: PDF, Excel, CSV, PowerPoint
- **Apache Superset Integration**: Advanced BI and data visualization
  - Pre-built dashboards
  - Ad-hoc analysis
  - SQL-based custom queries

#### J. Project Roadmap
- **Visual Roadmap**: Timeline view of project phases and milestones
- **Portfolio View**: Multi-project roadmap
- **Scenario Planning**: What-if analysis for timeline changes
- **Dependency Mapping**: Cross-project dependencies

#### K. Quality Management
- **Quality Standards**: Define quality metrics and standards
- **Quality Checklists**: Inspection and review checklists
- **Defect Tracking**: Log and manage defects/issues
- **Quality Audits**: Schedule and track quality audits

#### L. Communication Management
- **Communication Plan**: Define communication strategy
- **Meeting Management**: Schedule, track, and document meetings
- **Status Updates**: Regular status update workflow
- **Stakeholder Communication**: Targeted communication to stakeholder groups
- **Notification Center**: Centralized notification management
- **Email Digest**: Configurable email digests of project activities

---

### 1.3 Integration Capabilities

#### A. Internal BengoBox Microservices

##### Auth Service Integration
- **Single Sign-On (SSO)**: Seamless authentication via JWT
- **User Synchronization**: Real-time user data sync
- **Role Mapping**: Map auth roles to project roles
- **Tenant Management**: Multi-tenant support with tenant isolation

##### HRM Integration
- **Employee Directory**: Access employee information for team assignment
- **Organizational Structure**: Import org chart for governance structure
- **Leave Calendar**: View employee leave schedules for planning
- **Performance Data**: Link project performance to employee appraisals
- **Skill Matrix**: Access employee skills for resource allocation

##### Treasury/Finance Integration
- **Budget Approval**: Submit project budgets for finance approval
- **Voucher Processing**: Create and track payment vouchers
- **Expense Recording**: Sync project expenses with financial system
- **Invoice Management**: Link project milestones to client invoices
- **Cost Center Allocation**: Allocate costs to proper cost centers
- **Financial Reports**: Pull financial data for project reports

##### CRM Integration
- **Client Information**: Access client data for project setup
- **Opportunity Tracking**: Link projects to CRM opportunities
- **Contact Management**: Access client contacts for project communication
- **Client Portal**: Provide clients with project visibility
- **Customer Feedback**: Collect and link client feedback to projects

##### Notifications Service Integration
- **Multi-Channel Notifications**: Email, SMS, push notifications
- **Event-Driven Alerts**:
  - Task assignments
  - Deadline reminders (24h, 1h before)
  - Status changes
  - Budget thresholds
  - Milestone completion
  - Comment mentions
  - Document uploads
  - Meeting reminders
- **Notification Preferences**: User-configurable notification settings
- **Escalation Notifications**: Automated escalations for overdue items

##### Logistics Service Integration
- **Project Deliveries**: Track logistics for project deliveries
- **Resource Transportation**: Coordinate transportation of equipment/materials
- **Site Logistics**: Manage on-site logistics for field projects
- **Proof of Delivery**: Link delivery confirmations to project tasks

##### Inventory Service Integration
- **Material Requisition**: Request materials from inventory
- **Equipment Allocation**: Reserve and allocate equipment to projects
- **Stock Tracking**: Track material consumption by project
- **Inventory Reports**: Material usage reports by project

##### IoT Service Integration
- **Site Monitoring**: IoT device data for construction/field projects
- **Equipment Telemetry**: Track equipment usage and performance
- **Environmental Monitoring**: Temperature, humidity, etc. for sensitive projects
- **Security & Access Control**: Site access logs and security monitoring

#### B. Third-Party Integrations

##### Collaboration Tools
- **Google Workspace**:
  - Google Drive: File storage and sharing
  - Google Docs/Sheets/Slides: Collaborative editing
  - Google Meet: Video conferencing
  - Google Calendar: Calendar integration
- **Microsoft 365**:
  - OneDrive: File storage
  - Office Online: Document collaboration
  - Microsoft Teams: Chat, meetings, file sharing
  - Outlook Calendar: Calendar sync
- **Zoho**:
  - Zoho Meeting: Video conferencing
  - Zoho Docs: Document management
  - Zoho Cliq: Team chat
- **Slack**: Team chat, notifications, bot integrations
- **Dropbox**: File storage and sharing

##### Project Management Tools
- **Jira**: Bi-directional sync for development tasks
- **Trello**: Import boards and cards
- **Asana**: Task import and sync

##### Development Tools
- **GitHub/GitLab/Bitbucket**: Link code repositories to projects
- **CI/CD Pipelines**: Track deployment pipeline status

##### Time Tracking Tools
- **Toggl**: Import time entries
- **Harvest**: Sync time and expenses

##### Communication Tools
- **Zoom**: Video conferencing
- **Webex**: Video conferencing

##### Storage & Backup
- **AWS S3**: Object storage for documents
- **MinIO**: Self-hosted object storage
- **Azure Blob Storage**: Cloud storage

##### AI & Automation
- **Natural Language Processing**: AI-powered task creation from text
- **Predictive Analytics**: Project outcome predictions based on historical data
- **Smart Recommendations**: Suggest optimal resource allocation, timelines
- **Automated Status Updates**: Generate status reports from activity data

---

## 2. Technical Architecture

### 2.1 Technology Stack

- **Language**: Go 1.24+
- **Framework**: Chi router for REST APIs
- **Database**: PostgreSQL 15+ with pgvector extension
- **Cache**: Redis 7+ (caching, rate limiting, idempotency)
- **Message Bus**: NATS JetStream (event-driven architecture)
- **ORM**: Ent (schema-as-code, type-safe)
- **Authentication**: JWT validation via auth-service JWKS
- **Object Storage**: S3-compatible (AWS S3, MinIO) for file storage
- **Search**: PostgreSQL full-text search + pgvector for semantic search
- **Analytics**: Apache Superset for BI and reporting
- **Observability**:
  - Logging: Zap structured logging
  - Metrics: Prometheus
  - Tracing: OpenTelemetry
  - APM: Grafana, Jaeger

### 2.2 Architecture Principles

- **Clean Architecture**: Domain-driven design with clear separation of concerns
- **Multi-Tenancy**: Row-level tenant isolation with `tenant_id` column
- **Event-Driven**: Publish events to NATS for decoupled service communication
- **API-First**: RESTful APIs with OpenAPI/Swagger documentation
- **Idempotency**: Redis-based idempotency keys for safe retries
- **Audit Logging**: Complete audit trail of all operations
- **RBAC**: Fine-grained role-based access control
- **Scalability**: Horizontal scaling with stateless services
- **Resilience**: Circuit breakers, retries, fallbacks for external integrations

### 2.3 Database Design

#### PostgreSQL Extensions
```sql
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pgcrypto";
CREATE EXTENSION IF NOT EXISTS "vector";
CREATE EXTENSION IF NOT EXISTS "pg_trgm"; -- for fuzzy search
CREATE EXTENSION IF NOT EXISTS "btree_gist"; -- for advanced indexing
```

#### pgvector Usage
- **Document Embeddings**: Store vector embeddings for tender documents, project descriptions, task descriptions
- **Semantic Search**: Find similar tenders, projects, tasks using vector similarity
- **AI Recommendations**: Use embeddings for intelligent project matching, resource recommendations
- **Knowledge Base**: Build a searchable knowledge base of past projects and tenders

**Vector Columns**:
- `tenders.description_embedding` - vector(1536) for tender description
- `projects.description_embedding` - vector(1536) for project description  
- `tasks.description_embedding` - vector(768) for task description
- `documents.content_embedding` - vector(1536) for document content

### 2.4 API Design

#### Base URL Structure
```
/api/v1/{tenantID}/...
```

#### Authentication
- **JWT Bearer Token**: All requests require valid JWT from auth-service
- **API Key**: Optional service-to-service authentication

#### Versioning
- URL-based versioning: `/api/v1/`, `/api/v2/`
- Maintain backward compatibility for at least 2 versions

#### Key Endpoints (see api-endpoints.md for full list)

**Tender Management**
- `POST /api/v1/{tenantID}/tenders` - Create tender
- `GET /api/v1/{tenantID}/tenders` - List tenders
- `GET /api/v1/{tenantID}/tenders/{id}` - Get tender details
- `PUT /api/v1/{tenantID}/tenders/{id}` - Update tender
- `POST /api/v1/{tenantID}/tenders/{id}/evaluate` - Submit evaluation
- `POST /api/v1/{tenantID}/tenders/{id}/submit` - Submit tender
- `POST /api/v1/{tenantID}/tenders/{id}/convert-to-project` - Convert to project

**Project Management**
- `POST /api/v1/{tenantID}/projects` - Create project
- `GET /api/v1/{tenantID}/projects` - List projects
- `GET /api/v1/{tenantID}/projects/{id}` - Get project details
- `PUT /api/v1/{tenantID}/projects/{id}` - Update project
- `GET /api/v1/{tenantID}/projects/{id}/dashboard` - Project dashboard

**Task Management**
- `POST /api/v1/{tenantID}/tasks` - Create task
- `GET /api/v1/{tenantID}/tasks` - List tasks
- `PUT /api/v1/{tenantID}/tasks/{id}` - Update task
- `POST /api/v1/{tenantID}/tasks/{id}/time-logs` - Log time

**Budget Management**
- `POST /api/v1/{tenantID}/projects/{id}/vouchers` - Create voucher
- `POST /api/v1/{tenantID}/projects/{id}/expenses` - Record expense

**Reporting**
- `GET /api/v1/{tenantID}/reports/{reportType}` - Generate report
- `POST /api/v1/{tenantID}/reports/custom` - Custom report

### 2.5 Event Architecture

#### Published Events (to NATS)
```
projects.tender.created
projects.tender.evaluated
projects.tender.submitted
projects.tender.awarded
projects.tender.converted
projects.project.created
projects.project.updated
projects.project.completed
projects.task.created
projects.task.assigned
projects.task.completed
projects.milestone.completed
projects.budget.threshold_exceeded
projects.deadline.approaching
projects.team.member_added
projects.document.uploaded
projects.expense.created
projects.voucher.created
```

#### Consumed Events
```
auth.user.created
auth.user.updated
treasury.payment.completed
notifications.email.delivered
logistics.delivery.completed
```

---

## 3. Data Model (Entity Relationship)

See `docs/erd.md` for complete ERD with all entities, relationships, and vector columns.

### Key Entities

#### Tender Management
- `tenders` - Tender opportunities
- `tender_documents` - Tender document library
- `tender_evaluations` - Evaluation scores and comments
- `tender_committees` - Evaluation committees
- `tender_meetings` - Meeting records
- `tender_sections` - Document sections for assignment
- `tender_submissions` - Submission records

#### Project Management
- `projects` - Core project data
- `project_members` - Team assignments
- `project_roles` - Custom project roles
- `tasks` - Task details
- `task_assignments` - Task assignees
- `task_dependencies` - Task relationships
- `milestones` - Project milestones
- `deliverables` - Project deliverables

#### Resource & Budget
- `resources` - Resource pool
- `resource_allocations` - Resource assignments
- `budgets` - Project budgets
- `budget_lines` - Budget line items
- `expenses` - Project expenses
- `vouchers` - Payment vouchers
- `time_logs` - Time tracking

#### Collaboration
- `comments` - Task/project comments
- `attachments` - File attachments
- `activities` - Activity feed
- `notifications` - User notifications
- `meetings` - Meeting records

#### Governance & Reporting
- `governance_teams` - Governance structure
- `decisions` - Decision log
- `change_requests` - Change control
- `risks` - Risk register
- `quality_checks` - Quality tracking
- `reports` - Report metadata

---

## 4. Integration Architecture

### 4.1 Integration Patterns

#### A. Event-Driven Integration (via NATS)
**Use Case**: Asynchronous, non-blocking communication
**Examples**:
- Notify users when assigned to tasks
- Trigger budget alerts when thresholds exceeded
- Sync user changes from auth-service

#### B. REST API Integration
**Use Case**: Synchronous data retrieval/manipulation
**Examples**:
- Fetch employee data from HRM
- Create vouchers in Treasury
- Retrieve client info from CRM

#### C. Webhook Integration
**Use Case**: External system notifications
**Examples**:
- Jira webhook for issue updates
- GitHub webhook for commit notifications
- Google Drive webhook for file changes

### 4.2 Integration Catalog

See `docs/integrations.md` for detailed integration specifications.

#### Internal Services Integration Matrix

| Service | Integration Type | Use Cases |
|---------|------------------|-----------|
| Auth Service | REST + Events | SSO, user sync, tenant management |
| HRM | REST + Events | Employee data, org chart, leave calendar |
| Treasury | REST + Events | Budget approval, voucher processing, expenses |
| Finance | REST | Invoice linking, cost center allocation |
| CRM | REST + Events | Client data, opportunities, contacts |
| Notifications | Events | Multi-channel notifications |
| Logistics | REST + Events | Delivery tracking, resource transport |
| Inventory | REST | Material requisition, equipment allocation |
| IoT | Events | Site monitoring, equipment telemetry |

#### External Services Integration

| Service | Protocol | Purpose |
|---------|----------|---------|
| Google Workspace | OAuth 2.0 + REST | Drive, Docs, Meet, Calendar |
| Microsoft 365 | OAuth 2.0 + REST | OneDrive, Teams, Outlook |
| Zoho | OAuth 2.0 + REST | Meeting, Docs, Cliq |
| Slack | OAuth 2.0 + Webhooks | Chat, notifications |
| Jira | OAuth 2.0 + REST + Webhooks | Task sync |
| GitHub | OAuth 2.0 + Webhooks | Repository linking |
| Apache Superset | REST | BI dashboards |

---

## 5. Apache Superset Integration

### 5.1 Architecture

```
Projects Service (PostgreSQL)
    ↓
Apache Superset
    ↓ (SQL queries)
PostgreSQL (read replica or direct connection)
    ↓
Dashboards & Reports
```

### 5.2 Data Pipeline

1. **Database Connection**: Superset connects to projects PostgreSQL database
2. **Datasets**: Define virtual datasets with pre-joined tables
3. **Metrics**: Create reusable metrics (e.g., project count, budget utilization, task completion rate)
4. **Charts**: Build visualizations (time series, bar charts, pie charts, gauges)
5. **Dashboards**: Compose dashboards from charts
6. **Access Control**: Row-level security to enforce tenant isolation

### 5.3 Pre-built Dashboards

#### Executive Dashboard
- Total active projects
- Total project budget vs spent
- Project success rate
- Tender win rate
- Resource utilization
- Revenue by project

#### Project Manager Dashboard
- My projects status
- Upcoming deadlines
- Overdue tasks
- Budget variance by project
- Team workload
- Milestone progress

#### Portfolio Dashboard
- Project pipeline (tender → active → completed)
- Project distribution by department/region
- Budget allocation by project
- Timeline view of all projects
- Resource allocation heatmap

#### Financial Dashboard
- Project profitability
- Cost breakdown by category
- Budget vs actual by project
- Cash flow projection
- Voucher and expense tracking

#### Tender Dashboard
- Tender pipeline (opportunity → evaluation → submission → award)
- Win/loss analysis
- Average bid value
- Time to submission metrics
- Tender source effectiveness

### 5.4 Custom SQL Queries

Superset allows power users to write custom SQL queries for ad-hoc analysis:

```sql
-- Example: Project performance by department
SELECT 
    d.name AS department,
    COUNT(p.id) AS project_count,
    AVG(p.completion_percentage) AS avg_completion,
    SUM(b.total_amount) AS total_budget,
    SUM(e.amount) AS total_spent,
    (SUM(b.total_amount) - SUM(e.amount)) / NULLIF(SUM(b.total_amount), 0) * 100 AS budget_remaining_pct
FROM projects p
LEFT JOIN departments d ON p.department_id = d.id
LEFT JOIN budgets b ON p.id = b.project_id
LEFT JOIN expenses e ON p.id = e.project_id
WHERE p.tenant_id = :tenant_id
    AND p.status NOT IN ('cancelled', 'on_hold')
GROUP BY d.name
ORDER BY project_count DESC;
```

### 5.5 Embedding Superset Dashboards

Superset dashboards can be embedded into the Projects Service UI using iframe embedding with JWT authentication.

```html
<iframe 
    src="https://superset.bengobox.com/superset/dashboard/1/?standalone=2&jwt={token}"
    width="100%" 
    height="800px"
    frameborder="0"
></iframe>
```

---

## 6. Deployment & DevOps

### 6.1 Containerization

**Dockerfile**:
- Multi-stage build
- Minimal Alpine-based image
- Non-root user
- Health check endpoint

### 6.2 Kubernetes Deployment

**Helm Chart**: `devops-k8s/charts/app` (reusable chart)
**Values File**: `devops-k8s/apps/projects-service/values.yaml`

**Resources**:
- Deployment (API pods)
- Deployment (Worker pods for background jobs)
- Service (ClusterIP)
- Ingress (HTTPS with TLS)
- HPA (Horizontal Pod Autoscaler)
- PDB (Pod Disruption Budget)
- ConfigMap (non-secret config)
- Secret (DB credentials, API keys)
- ServiceMonitor (Prometheus scraping)

### 6.3 Database Management

**Migrations**: Ent migrations via `cmd/migrate`
**Backups**: Automated PostgreSQL backups with point-in-time recovery
**Read Replicas**: For Superset and reporting queries

### 6.4 CI/CD Pipeline

**Pipeline Stages**:
1. Code Quality: golangci-lint, gofmt
2. Unit Tests: `go test ./...`
3. Integration Tests: Testcontainers with real PostgreSQL, Redis, NATS
4. Build: Docker image build
5. Push: Push to container registry
6. Deploy: Helm upgrade to Kubernetes
7. Smoke Tests: Health check, basic API tests
8. Notifications: Slack/email notification on success/failure

---

## 7. Security & Compliance

### 7.1 Authentication & Authorization

- **JWT Validation**: JWKS-based validation via auth-service
- **RBAC**: Fine-grained permissions (projects:read, projects:write, etc.)
- **Tenant Isolation**: Strict tenant_id filtering in all queries
- **API Rate Limiting**: Redis-based rate limiting per tenant/user

### 7.2 Data Security

- **Encryption at Rest**: PostgreSQL TDE (Transparent Data Encryption)
- **Encryption in Transit**: TLS 1.3 for all communications
- **Sensitive Data**: Encrypt sensitive fields (PII, financial data) using pgcrypto
- **Secrets Management**: Kubernetes Secrets, HashiCorp Vault for production

### 7.3 Audit Logging

- **Audit Table**: `audit_logs` table with:
  - User ID, tenant ID
  - Action (CREATE, UPDATE, DELETE, etc.)
  - Entity type and ID
  - Old and new values (JSON)
  - IP address, user agent
  - Timestamp
- **Retention Policy**: 7 years for compliance
- **Tamper-Proof**: Write-only audit log with checksums

### 7.4 Compliance

- **GDPR**: Data export, right to be forgotten
- **SOC 2**: Access controls, audit logging, encryption
- **ISO 27001**: Information security management

---

## 8. Performance & Scalability

### 8.1 Performance Targets

- **API Response Time**: p95 < 200ms, p99 < 500ms
- **Database Queries**: < 50ms for most queries
- **Concurrent Users**: Support 10,000+ concurrent users per tenant
- **Throughput**: 10,000+ requests/second

### 8.2 Optimization Strategies

- **Database Indexing**: Strategic indexes on frequently queried columns
- **Query Optimization**: Use EXPLAIN ANALYZE, avoid N+1 queries
- **Caching**: Redis caching for frequently accessed data (TTL-based)
- **Pagination**: Cursor-based pagination for large datasets
- **Lazy Loading**: Load related data on-demand
- **Background Jobs**: Offload heavy operations (report generation, email sending) to workers
- **CDN**: Serve static assets (documents, images) via CDN

### 8.3 Scalability

- **Horizontal Scaling**: Stateless API pods, scale based on CPU/memory
- **Database Scaling**: Read replicas for reporting, connection pooling
- **Cache Scaling**: Redis Cluster for high availability
- **Message Bus Scaling**: NATS JetStream clustering

---

## 9. Testing Strategy

### 9.1 Test Pyramid

- **Unit Tests (70%)**: Test individual functions, business logic
- **Integration Tests (20%)**: Test API endpoints, database interactions
- **E2E Tests (10%)**: Test complete user workflows

### 9.2 Test Coverage

- **Target**: 80%+ code coverage
- **Critical Paths**: 100% coverage for payment, budget, tender workflows

### 9.3 Test Tools

- **Go Testing**: Standard `testing` package
- **Testify**: Assertions and mocking
- **Testcontainers**: Real PostgreSQL, Redis, NATS in tests
- **HTTPTest**: HTTP handler testing

---

## 10. Delivery Roadmap

See `docs/sprints/` for detailed sprint planning.

### Sprint 0: Foundations ✅ (COMPLETED)
- Project scaffold, config, logging
- Auth-service integration
- Basic RBAC service
- Health check endpoints

### Sprint 1: Tender Management (4 weeks)
- Tender CRUD operations
- Tender evaluation workflow
- Committee management
- Meeting scheduling integration
- Document management
- Submission workflow

### Sprint 2: Tender to Project Conversion (2 weeks)
- Conversion logic
- Data migration
- Kickoff automation
- Status tracking

### Sprint 3: Project Planning (4 weeks)
- Project CRUD operations
- Task management with dependencies
- Gantt chart data
- Milestone management
- Deliverable tracking

### Sprint 4: Team & Collaboration (3 weeks)
- Team management
- Comments & mentions
- Activity feed
- File attachments
- Real-time updates (WebSockets)

### Sprint 5: Time & Budget (3 weeks)
- Time tracking
- Budget management
- Expense recording
- Voucher system
- Treasury integration

### Sprint 6: Resource Management (2 weeks)
- Resource pool
- Resource allocation
- Capacity planning
- HRM integration

### Sprint 7: Governance & Reporting (3 weeks)
- Governance structure
- Decision log
- Change control
- Standard reports
- Dashboard APIs

### Sprint 8: Apache Superset Integration (2 weeks)
- Superset setup
- Dataset configuration
- Dashboard creation
- Embedding setup

### Sprint 9: External Integrations (4 weeks)
- Google Workspace
- Microsoft 365
- Slack
- Jira
- GitHub

### Sprint 10: AI & Advanced Features (3 weeks)
- pgvector setup
- Semantic search
- AI recommendations
- Predictive analytics

### Sprint 11: Polish & Production Readiness (2 weeks)
- Performance optimization
- Security hardening
- Documentation
- User acceptance testing

**Total Duration**: ~32 weeks (~8 months)

---

## 11. Success Metrics

### Business Metrics
- **Tender Win Rate**: % of tenders won vs submitted
- **Project Success Rate**: % of projects completed on time and within budget
- **User Adoption**: % of active users vs total users
- **Time Savings**: Reduction in project setup and reporting time

### Technical Metrics
- **System Uptime**: 99.9% availability
- **API Response Time**: p95 < 200ms
- **Error Rate**: < 0.1%
- **Test Coverage**: > 80%

### User Satisfaction
- **NPS Score**: Net Promoter Score > 50
- **Feature Usage**: Track most-used features
- **Support Tickets**: < 1% of active users per month

---

## 12. Documentation Index

- [`docs/erd.md`](docs/erd.md) - Entity Relationship Diagram
- [`docs/api-endpoints.md`](docs/api-endpoints.md) - Complete API reference
- [`docs/integrations.md`](docs/integrations.md) - Integration specifications
- [`docs/superset-setup.md`](docs/superset-setup.md) - Apache Superset configuration
- [`docs/deployment.md`](docs/deployment.md) - Deployment guide
- [`docs/security.md`](docs/security.md) - Security best practices
- [`docs/sprints/sprint-1.md`](docs/sprints/sprint-1.md) - Sprint 1 plan
- [`docs/sprints/sprint-2.md`](docs/sprints/sprint-2.md) - Sprint 2 plan
- ...

---

## 13. Appendices

### A. Glossary
- **Tender**: A formal invitation to bid for a project
- **RFP**: Request for Proposal
- **WBS**: Work Breakdown Structure
- **RACI**: Responsible, Accountable, Consulted, Informed
- **Gantt Chart**: Timeline visualization with task dependencies

### B. References
- Zoho Projects: https://www.zoho.com/projects/
- Microsoft Project: https://www.microsoft.com/en-us/microsoft-365/project
- Jira: https://www.atlassian.com/software/jira
- Apache Superset: https://superset.apache.org/

### C. Change Log
- 2024-12-05: Initial comprehensive plan created
- ...

---

**Document Version**: 1.0  
**Last Updated**: 2024-12-05  
**Maintained By**: Projects Service Team
