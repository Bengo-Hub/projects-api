# Projects Service Documentation

Welcome to the Projects Service documentation. This service provides world-class project management and tender management capabilities for the Codevertex ecosystem.

## 📚 Documentation Index

### Core Planning Documents

| Document | Description |
|----------|-------------|
| [**plan.md**](../plan.md) | Comprehensive service delivery plan with all features, technical architecture, and roadmap |
| [**erd.md**](erd.md) | Complete Entity Relationship Diagram with all database entities, relationships, and pgvector columns |
| [**integrations.md**](integrations.md) | Detailed integration architecture with internal microservices and external platforms |
| [**superset-setup.md**](superset-setup.md) | Apache Superset setup, configuration, and dashboard creation guide |

### Sprint Planning

| Sprint | Duration | Focus | Document |
|--------|----------|-------|----------|
| **Sprint 0** | 2 weeks | ✅ Foundations (COMPLETED) | - |
| **Sprint 1** | 4 weeks | Tender Management | [sprint-1-tender-management.md](sprints/sprint-1-tender-management.md) |
| **Sprint 2** | 2 weeks | Tender to Project Conversion | Coming soon |
| **Sprint 3** | 4 weeks | Project Planning | Coming soon |
| **Sprint 4** | 3 weeks | Team & Collaboration | Coming soon |
| **Sprint 5** | 3 weeks | Time & Budget | Coming soon |
| **Sprint 6** | 2 weeks | Resource Management | Coming soon |
| **Sprint 7** | 3 weeks | Governance & Reporting | Coming soon |
| **Sprint 8** | 2 weeks | Apache Superset Integration | Coming soon |
| **Sprint 9** | 4 weeks | External Integrations | Coming soon |
| **Sprint 10** | 3 weeks | AI & Advanced Features | Coming soon |
| **Sprint 11** | 2 weeks | Polish & Production Readiness | Coming soon |

**Total Estimated Duration**: ~32 weeks (~8 months)

---

## 🎯 Quick Start

### For Developers

1. **Read the Plan**: Start with [plan.md](../plan.md) to understand the overall vision and features
2. **Review the ERD**: Check [erd.md](erd.md) for database schema and entity relationships
3. **Current Sprint**: See [sprints/sprint-1-tender-management.md](sprints/sprint-1-tender-management.md) for current work
4. **Setup Environment**: Follow the setup instructions in the main [README.md](../README.md)

### For Project Managers

1. **Features Overview**: Review [plan.md](../plan.md) sections 1-2 for feature descriptions
2. **Roadmap**: See section 10 of [plan.md](../plan.md) for delivery timeline
3. **Sprint Progress**: Check individual sprint documents in [sprints/](sprints/) folder

### For Stakeholders

1. **Executive Summary**: Read sections 1-2 of [plan.md](../plan.md)
2. **Integration Points**: See [integrations.md](integrations.md) for how this service connects with other systems
3. **Reporting**: Check [superset-setup.md](superset-setup.md) for BI dashboard capabilities

---

## 🏗️ Architecture Overview

### Technology Stack

- **Language**: Go 1.24+
- **Database**: PostgreSQL 15+ with pgvector extension
- **Cache**: Redis 7+
- **Message Bus**: NATS JetStream
- **ORM**: Ent (schema-as-code)
- **API**: RESTful with Chi router
- **BI Platform**: Apache Superset

### Key Features

#### 1. Tender Management
- Opportunity logging and tracking
- Committee-based evaluation
- Meeting scheduling (Google Meet, Teams, Zoom)
- Document preparation with task assignment
- Email and physical submission tracking
- Win/loss analysis and lessons learned

#### 2. Project Management
- Complete project lifecycle management
- Gantt charts and dependencies
- Milestone and deliverable tracking
- Team collaboration features
- Real-time activity feeds
- Budget and expense management

#### 3. Resource Management
- Resource pool management
- Capacity planning and allocation
- Resource utilization tracking
- Skill-based resource matching

#### 4. Financial Management
- Project budgeting
- Expense tracking with receipts
- Voucher system for payments
- Integration with Treasury service
- Cost allocation to cost centers

#### 5. Reporting & Analytics
- Pre-built dashboards (Executive, Project Manager, Financial, Tender, Resource)
- Custom report builder
- Apache Superset integration
- Embedded dashboards in UI
- Scheduled reports via email

#### 6. Integrations
- **Internal**: Auth, HRM, Treasury, CRM, Notifications, Logistics, Inventory, IoT
- **External**: Google Workspace, Microsoft 365, Slack, Jira, GitHub, Zoom, Teams

---

## 📊 Database Schema Highlights

### Total Entities: 45

- **Tender Management**: 8 entities
- **Project Management**: 3 entities
- **Task Management**: 6 entities
- **Resource & Budget**: 7 entities
- **Collaboration**: 5 entities
- **Governance & Compliance**: 6 entities
- **Integration & System**: 7 entities

### Vector Embeddings (pgvector)

| Entity | Column | Dimensions | Purpose |
|--------|--------|------------|---------|
| `tenders` | `description_embedding` | 1536 | Semantic search |
| `tender_documents` | `content_embedding` | 1536 | Document search |
| `projects` | `description_embedding` | 1536 | Project search |
| `tasks` | `description_embedding` | 768 | Task similarity |

---

## 🔗 Integration Architecture

### Internal Microservices

| Service | Integration Type | Purpose |
|---------|------------------|---------|
| **Auth Service** | REST + Events | SSO, user sync, tenant management |
| **HRM** | REST + Events | Employee data, org chart, leave calendar |
| **Treasury** | REST + Events | Budget approval, voucher processing |
| **Finance** | REST | Invoice linking, cost centers |
| **CRM** | REST + Events | Client data, opportunities |
| **Notifications** | Events | Multi-channel notifications |
| **Logistics** | REST + Events | Delivery tracking |
| **Inventory** | REST | Material requisition |
| **IoT** | Events | Site monitoring, telemetry |

### External Platforms

- **Google Workspace**: Drive, Docs, Meet, Calendar
- **Microsoft 365**: OneDrive, Teams, Office Online
- **Slack**: Chat, notifications, bot
- **Jira**: Development task sync
- **GitHub/GitLab**: Repository linking
- **Zoom/Webex**: Video conferencing

---

## 📈 Apache Superset Dashboards

### 1. Executive Overview
- Total active projects
- Budget utilization
- Project health distribution
- Tender win rate
- Top projects by budget

### 2. Project Manager Dashboard
- My projects
- Upcoming deadlines
- Team workload
- Task completion rate
- Budget variance

### 3. Financial Dashboard
- Budget vs actual
- Expense breakdown
- Cash flow projection
- Voucher status
- Top cost projects

### 4. Tender Dashboard
- Tender pipeline funnel
- Win/loss analysis
- Evaluation scores
- Tender sources performance
- Time to submission

### 5. Resource Utilization
- Allocation overview
- Resource heatmap
- Overallocated resources
- Available resources
- Team composition

---

## 🚀 Development Workflow

### 1. Start New Sprint
```bash
# Review sprint document
cat docs/sprints/sprint-X-*.md

# Update your local branch
git checkout main
git pull origin main
git checkout -b sprint-X/feature-name

# Start development
go mod tidy
make run
```

### 2. Database Changes
```bash
# Create new Ent schema
touch internal/ent/schema/new_entity.go

# Generate Ent code
go generate ./internal/ent

# Create migration
go run ./cmd/migrate

# Seed data
go run ./cmd/seed
```

### 3. Testing
```bash
# Unit tests
go test ./...

# Integration tests
go test ./... -tags=integration

# Coverage report
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out
```

### 4. Commit & Push
```bash
git add .
git commit -m "feat: implement feature X"
git push origin sprint-X/feature-name

# Create pull request
gh pr create --title "feat: implement feature X" --body "Description..."
```

---

## 📝 API Documentation

Once the service is running, access API documentation at:

```
http://localhost:4005/v1/docs/
```

### Key API Patterns

- **Base URL**: `/api/v1/{tenantID}/...`
- **Authentication**: JWT Bearer token from auth-service
- **Pagination**: Cursor-based pagination for lists
- **Filtering**: Query parameters for filtering
- **Sorting**: `?sort=field:asc|desc`

### Example Endpoints

```
# Tenders
GET    /api/v1/{tenantID}/tenders
POST   /api/v1/{tenantID}/tenders
GET    /api/v1/{tenantID}/tenders/{id}
POST   /api/v1/{tenantID}/tenders/{id}/evaluate
POST   /api/v1/{tenantID}/tenders/{id}/submit

# Projects
GET    /api/v1/{tenantID}/projects
POST   /api/v1/{tenantID}/projects
GET    /api/v1/{tenantID}/projects/{id}
GET    /api/v1/{tenantID}/projects/{id}/dashboard

# Tasks
GET    /api/v1/{tenantID}/tasks
POST   /api/v1/{tenantID}/tasks
PUT    /api/v1/{tenantID}/tasks/{id}

# Reports
GET    /api/v1/{tenantID}/reports/{reportType}
POST   /api/v1/{tenantID}/reports/custom
```

---

## 🔐 Security Considerations

### Authentication & Authorization
- JWT validation via auth-service JWKS
- Role-Based Access Control (RBAC)
- Tenant isolation with `tenant_id`
- API rate limiting per tenant/user

### Data Security
- Encryption at rest (PostgreSQL TDE)
- Encryption in transit (TLS 1.3)
- Sensitive field encryption (pgcrypto)
- Secrets in Kubernetes Secrets/Vault

### Audit Logging
- Complete audit trail in `audit_logs` table
- Immutable audit logs with checksums
- 7-year retention for compliance
- Track all CRUD operations with old/new values

---

## 🧪 Testing Strategy

### Unit Tests (70%)
- Service layer business logic
- Input validation
- Status transitions
- Calculations and scoring

### Integration Tests (20%)
- API endpoint tests with Testcontainers
- Database interactions
- Event publishing/consuming
- External API integrations (mocked)

### E2E Tests (10%)
- Complete user workflows
- Tender lifecycle
- Project lifecycle

**Target Coverage**: 80%+

---

## 📦 Deployment

### Local Development
```bash
# Using Docker Compose
docker-compose up -d postgres redis nats
go run ./cmd/api
```

### Staging/Production
```bash
# Using Helm
helm upgrade --install projects-service \
  ./charts/app \
  -f devops-k8s/apps/projects-service/values.yaml \
  -n projects \
  --create-namespace
```

---

## 🤝 Contributing

1. Read [CONTRIBUTING.md](../CONTRIBUTING.md)
2. Pick a task from current sprint
3. Create feature branch
4. Implement with tests
5. Submit pull request
6. Code review
7. Merge to main

---

## 📞 Support

- **Technical Issues**: Create GitHub issue
- **Questions**: Check [SUPPORT.md](../SUPPORT.md)
- **Security**: See [SECURITY.md](../SECURITY.md)

---

## 📅 Change Log

See [CHANGELOG.md](../CHANGELOG.md) for version history and changes.

---

## 📄 License

See [LICENSE](../LICENSE) for license information.

---

**Last Updated**: 2024-12-05  
**Maintained By**: Projects Service Team  
**Version**: 1.0.0

