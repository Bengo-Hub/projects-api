# Projects Service - Integration Matrix

## Overview

This document provides a comprehensive matrix of all services and platforms that the Projects Service integrates with, including the integration methods, data flows, and use cases.

---

## Internal Codevertex Microservices Integration Matrix

| Service | Location | Status | Integration Type | Data Flow | Key Use Cases |
|---------|----------|--------|------------------|-----------|---------------|
| **Auth Service** | `auth-service/` | ✅ Production | REST API + Events | Bi-directional | • JWT validation (JWKS)<br>• User synchronization<br>• Tenant management<br>• SSO authentication |
| **HRM (ERP)** | `erp/erp-api/` | ✅ Production | REST API + Events | Projects → HRM (read)<br>HRM → Projects (events) | • Employee directory<br>• Organizational structure<br>• Leave calendar<br>• Skill matrix<br>• Performance linking |
| **Treasury** | `treasury-api/` | ✅ Production | REST API + Events | Bi-directional | • Budget approval<br>• Voucher processing<br>• Payment tracking<br>• Expense recording<br>• Cost center allocation |
| **Finance (ERP)** | `erp/erp-api/` | ✅ Production | REST API | Projects → Finance (read) | • Invoice management<br>• Cost center lookup<br>• Financial reporting<br>• Budget integration |
| **CRM (ERP)** | `erp/erp-api/` | ✅ Production | REST API + Events | Bi-directional | • Client information<br>• Opportunity tracking<br>• Contact management<br>• Client portal<br>• Feedback collection |
| **Notifications** | `notifications-service/notifications-api/` | ✅ Production | Events (NATS) | Projects → Notifications | • Task assignments<br>• Deadline reminders<br>• Status changes<br>• Comment mentions<br>• Meeting reminders<br>• Budget alerts |
| **Logistics** | `logistics-service/` | ⚙️ Development | REST API + Events | Bi-directional | • Delivery coordination<br>• Resource transportation<br>• Site logistics<br>• Proof of delivery |
| **Inventory** | `inventory-service/` | ⚙️ Development | REST API + Events | Projects → Inventory | • Material requisition<br>• Equipment allocation<br>• Stock tracking<br>• Equipment reservation |
| **IoT** | `iot-service/` | ⚙️ Development | Events (NATS) | IoT → Projects | • Site monitoring<br>• Equipment telemetry<br>• Environmental monitoring<br>• Security & access control |
| **POS** | `pos-service/` | 📋 Planned | REST API | Projects → POS (optional) | • Project-specific POS transactions<br>• Client payments |

---

## External Third-Party Platform Integration Matrix

### Collaboration & Communication Platforms

| Platform | Status | Auth Method | Integration Type | Key Features |
|----------|--------|-------------|------------------|--------------|
| **Google Workspace** | 📋 Planned | OAuth 2.0 | REST API | • **Google Drive**: File storage, sharing, sync<br>• **Google Docs**: Collaborative editing<br>• **Google Meet**: Video conferencing<br>• **Google Calendar**: Meeting scheduling |
| **Microsoft 365** | 📋 Planned | OAuth 2.0 (Microsoft Identity) | REST API | • **OneDrive**: File storage<br>• **Office Online**: Document collaboration<br>• **Microsoft Teams**: Meetings, chat, files<br>• **Outlook Calendar**: Calendar sync |
| **Slack** | 📋 Planned | OAuth 2.0 | REST API + Webhooks | • Post project updates<br>• Task notifications<br>• Slash commands<br>• Bot integration |
| **Zoom** | 📋 Planned | OAuth 2.0 / API Key | REST API | • Video conferencing<br>• Meeting scheduling<br>• Recording storage |
| **Zoho Meeting** | 📋 Planned | OAuth 2.0 / API Key | REST API | • Video conferencing<br>• Meeting scheduling |
| **Webex** | 📋 Planned | OAuth 2.0 / API Key | REST API | • Video conferencing<br>• Meeting scheduling |

### Project Management & Development Tools

| Platform | Status | Auth Method | Integration Type | Key Features |
|----------|--------|-------------|------------------|--------------|
| **Jira** | 📋 Planned | OAuth 2.0 / API Token | REST API + Webhooks | • Bi-directional task sync<br>• Issue linking<br>• Webhook for updates<br>• Status synchronization |
| **Trello** | 📋 Planned | OAuth 2.0 / API Key | REST API | • Board import<br>• Card sync<br>• Checklist import |
| **Asana** | 📋 Planned | OAuth 2.0 / Personal Access Token | REST API | • Task import<br>• Project sync |
| **GitHub** | 📋 Planned | OAuth 2.0 / Personal Access Token | REST API + Webhooks | • Repository linking<br>• Commit tracking<br>• PR notifications<br>• Issue linking |
| **GitLab** | 📋 Planned | OAuth 2.0 / Personal Access Token | REST API + Webhooks | • Repository linking<br>• Commit tracking<br>• MR notifications<br>• Issue linking |
| **Bitbucket** | 📋 Planned | OAuth 2.0 / API Token | REST API + Webhooks | • Repository linking<br>• Commit tracking |

### Storage & File Management

| Platform | Status | Auth Method | Integration Type | Key Features |
|----------|--------|-------------|------------------|--------------|
| **AWS S3** | ✅ Production | API Key (Access Key + Secret) | REST API (S3) | • Primary object storage<br>• Document storage<br>• File versioning<br>• Presigned URLs |
| **MinIO** | ✅ Production | API Key | S3-compatible API | • Self-hosted object storage<br>• S3-compatible<br>• Local development |
| **Dropbox** | 📋 Planned | OAuth 2.0 | REST API | • File storage<br>• File sharing |
| **Google Drive** | 📋 Planned | OAuth 2.0 | REST API | • File storage<br>• Collaborative editing<br>• File sync |
| **OneDrive** | 📋 Planned | OAuth 2.0 | REST API | • File storage<br>• Office integration |

### Business Intelligence & Reporting

| Platform | Status | Auth Method | Integration Type | Key Features |
|----------|--------|-------------|------------------|--------------|
| **Apache Superset** | 📋 Planned | JWT + Database Connection | Direct DB + REST API | • Pre-built dashboards<br>• Custom reports<br>• Embedded dashboards<br>• Scheduled reports<br>• Row-level security |

### Time Tracking

| Platform | Status | Auth Method | Integration Type | Key Features |
|----------|--------|-------------|------------------|--------------|
| **Toggl** | 📋 Planned | API Token | REST API | • Time entry import<br>• Project sync |
| **Harvest** | 📋 Planned | OAuth 2.0 / API Token | REST API | • Time & expense sync |

---

## Integration Architecture Patterns

### 1. Event-Driven Integration (NATS JetStream)

**Suitable For**: Asynchronous, non-blocking operations

**Services Using This Pattern**:
- Auth Service (user events)
- HRM (employee updates, leave approvals)
- Notifications (all notification triggers)
- IoT (telemetry data, alerts)
- Treasury (payment confirmations)
- Logistics (delivery updates)

**Event Format**:
```json
{
  "event_id": "uuid",
  "event_type": "projects.task.assigned",
  "tenant_id": "tenant-slug",
  "timestamp": "2024-12-05T10:30:00Z",
  "version": "1.0",
  "source": "projects-service",
  "data": { /* event payload */ },
  "metadata": {
    "user_id": "uuid",
    "trace_id": "uuid"
  }
}
```

---

### 2. Synchronous REST API Integration

**Suitable For**: Real-time data retrieval, immediate responses

**Services Using This Pattern**:
- Auth Service (user lookup)
- HRM (employee data, org chart)
- Treasury (voucher creation)
- Finance (cost centers, invoices)
- CRM (client information)
- Inventory (material requisition)
- All external platforms

**Request Pattern**:
```go
// With circuit breaker, retry, and timeout
ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
defer cancel()

result, err := circuitBreaker.Execute(func() (interface{}, error) {
    return httpClient.Get(ctx, url)
})
```

---

### 3. Webhook Integration

**Suitable For**: External platform notifications

**Platforms Using This Pattern**:
- GitHub/GitLab (commit notifications)
- Jira (issue updates)
- Slack (slash commands)
- Google Drive (file changes)

**Webhook Handler**:
```go
func (h *WebhookHandler) HandleWebhook(w http.ResponseWriter, r *http.Request) {
    // Validate signature
    if !validateSignature(r) {
        http.Error(w, "Invalid signature", 401)
        return
    }
    
    // Process webhook
    // ...
    
    w.WriteHeader(200)
}
```

---

## Data Flow Diagrams

### Tender Creation with Notifications

```
User (Frontend)
    │
    ├─► POST /api/v1/{tenant}/tenders
    │
    ▼
Projects Service
    │
    ├─► Insert into `tenders` table
    │
    ├─► Publish: projects.tender.created
    │
    ▼
NATS JetStream
    │
    ├─► Consumed by: Notifications Service
    │
    ▼
Notifications Service
    │
    ├─► Send Email to committee members
    │
    └─► Send Push notification
```

---

### Voucher Creation with Treasury

```
Project Manager
    │
    ├─► POST /api/v1/{tenant}/projects/{id}/vouchers
    │
    ▼
Projects Service
    │
    ├─► Insert into `vouchers` table
    │
    ├─► POST /api/v1/vouchers (Treasury API)
    │
    ▼
Treasury Service
    │
    ├─► Create payment record
    │
    ├─► Initiate approval workflow
    │
    ├─► Publish: treasury.payment.completed
    │
    ▼
NATS JetStream
    │
    └─► Consumed by: Projects Service
          │
          └─► Update voucher status to "paid"
```

---

### Employee Assignment with HRM

```
Project Manager
    │
    ├─► POST /api/v1/{tenant}/projects/{id}/members
    │
    ▼
Projects Service
    │
    ├─► GET /api/v1/employees/{id} (HRM API)
    │
    ▼
HRM Service
    │
    ├─► Return employee details
    │
    ▼
Projects Service
    │
    ├─► Cache employee data in local `users` table
    │
    ├─► Insert into `project_members` table
    │
    └─► Publish: projects.team.member_added
```

---

### Google Meet Integration

```
Committee Chair
    │
    ├─► POST /api/v1/{tenant}/tenders/{id}/meetings
    │
    ▼
Projects Service
    │
    ├─► Validate user OAuth token
    │
    ├─► POST /calendar/v3/events (Google Calendar API)
    │
    ▼
Google Calendar API
    │
    ├─► Create calendar event with Google Meet
    │
    ├─► Return meeting link
    │
    ▼
Projects Service
    │
    ├─► Insert into `tender_meetings` table
    │
    ├─► Publish: projects.tender.meeting.scheduled
    │
    ▼
Notifications Service
    │
    └─► Send calendar invites to attendees
```

---

## Event Catalog

### Events Published by Projects Service

| Event Type | Trigger | Consumed By | Payload |
|------------|---------|-------------|---------|
| `projects.tender.created` | Tender logged | Notifications | Tender details |
| `projects.tender.evaluated` | Evaluation submitted | Notifications | Evaluation summary |
| `projects.tender.submitted` | Tender submitted | CRM, Notifications | Submission details |
| `projects.tender.awarded` | Tender won | CRM, Finance, Notifications | Award details |
| `projects.tender.converted` | Tender → Project | Finance, HRM | Project details |
| `projects.project.created` | Project created | Finance, HRM, Notifications | Project details |
| `projects.project.updated` | Project updated | Notifications | Changed fields |
| `projects.project.completed` | Project completed | Finance, CRM | Completion summary |
| `projects.task.assigned` | Task assigned | Notifications | Task + assignee |
| `projects.task.completed` | Task completed | Notifications | Task details |
| `projects.deadline.approaching` | 24h/1h before deadline | Notifications | Task/project + deadline |
| `projects.budget.threshold` | Budget >80% spent | Notifications, Finance | Budget details |
| `projects.milestone.completed` | Milestone done | Notifications, CRM | Milestone details |
| `projects.voucher.created` | Voucher raised | Treasury, Notifications | Voucher details |
| `projects.expense.recorded` | Expense logged | Treasury, Finance | Expense details |
| `projects.document.uploaded` | File uploaded | Notifications | Document details |
| `projects.comment.mentioned` | User @mentioned | Notifications | Comment + mention |

### Events Consumed by Projects Service

| Event Type | Source | Action | Updates |
|------------|--------|--------|---------|
| `auth.user.created` | Auth Service | Sync user to local cache | `users` table |
| `auth.user.updated` | Auth Service | Update cached user | `users` table |
| `auth.user.deactivated` | Auth Service | Mark user inactive | `users` table |
| `hrm.employee.updated` | HRM | Update employee cache | `users` table |
| `hrm.leave.approved` | HRM | Update resource availability | `resource_allocations` |
| `treasury.payment.completed` | Treasury | Update voucher status | `vouchers` table |
| `treasury.budget.approved` | Treasury | Update budget status | `budgets` table |
| `logistics.delivery.completed` | Logistics | Update deliverable status | `deliverables` table |
| `inventory.equipment.allocated` | Inventory | Update resource allocation | `resource_allocations` |
| `iot.alert.triggered` | IoT | Log project alert | `activities` table |

---

## API Endpoint Summary

### Projects Service Exposes

**Base URL**: `/api/v1/{tenantID}/`

| Resource | Endpoints | Methods |
|----------|-----------|---------|
| **Tenders** | `/tenders`, `/tenders/{id}` | GET, POST, PUT, DELETE |
| **Committees** | `/tenders/{id}/committees` | GET, POST |
| **Evaluations** | `/tenders/{id}/evaluations` | GET, POST |
| **Meetings** | `/tenders/{id}/meetings` | GET, POST, PUT |
| **Sections** | `/tenders/{id}/sections` | GET, POST, PUT |
| **Projects** | `/projects`, `/projects/{id}` | GET, POST, PUT, DELETE |
| **Tasks** | `/tasks`, `/tasks/{id}` | GET, POST, PUT, DELETE |
| **Milestones** | `/projects/{id}/milestones` | GET, POST, PUT, DELETE |
| **Deliverables** | `/projects/{id}/deliverables` | GET, POST, PUT |
| **Budget** | `/projects/{id}/budgets` | GET, POST, PUT |
| **Expenses** | `/projects/{id}/expenses` | GET, POST, PUT |
| **Vouchers** | `/projects/{id}/vouchers` | GET, POST, PUT |
| **Time Logs** | `/projects/{id}/time-logs` | GET, POST, PUT |
| **Team Members** | `/projects/{id}/members` | GET, POST, DELETE |
| **Comments** | `/{entity}/{id}/comments` | GET, POST, PUT, DELETE |
| **Attachments** | `/{entity}/{id}/attachments` | GET, POST, DELETE |
| **Reports** | `/reports/{type}` | GET, POST |
| **Dashboard** | `/projects/{id}/dashboard` | GET |

---

## Security & Authentication

### Internal Service Authentication

| Service | Method | Details |
|---------|--------|---------|
| Auth Service | JWKS | JWT validation via public keys |
| HRM | Service Token | API key in header |
| Treasury | Service Token | API key in header |
| Finance | Service Token | API key in header |
| CRM | Service Token | API key in header |
| Notifications | NATS Auth | NATS credentials |
| Logistics | Service Token | API key in header |
| Inventory | Service Token | API key in header |
| IoT | NATS Auth | NATS credentials |

### External Platform Authentication

| Platform | Method | Storage | Refresh |
|----------|--------|---------|---------|
| Google Workspace | OAuth 2.0 | Encrypted in `integrations` table | Auto-refresh with refresh token |
| Microsoft 365 | OAuth 2.0 | Encrypted in `integrations` table | Auto-refresh with refresh token |
| Slack | OAuth 2.0 | Encrypted in `integrations` table | Auto-refresh with refresh token |
| Jira | OAuth 2.0 / API Token | Encrypted in `integrations` table | Token-based (no refresh) |
| GitHub | OAuth 2.0 / PAT | Encrypted in `integrations` table | Token-based (no refresh) |
| Zoom | OAuth 2.0 / API Key | Encrypted in `integrations` table | Auto-refresh with refresh token |

---

## Integration Testing Strategy

### Mock Services for Unit Tests

```go
// Mock HRM client
type MockHRMClient struct {
    mock.Mock
}

func (m *MockHRMClient) GetEmployee(ctx context.Context, id string) (*Employee, error) {
    args := m.Called(ctx, id)
    return args.Get(0).(*Employee), args.Error(1)
}

// Usage in tests
func TestAssignTeamMember(t *testing.T) {
    mockHRM := new(MockHRMClient)
    mockHRM.On("GetEmployee", mock.Anything, "emp-123").Return(&Employee{
        ID: "emp-123",
        Name: "John Doe",
    }, nil)
    
    service := NewProjectService(mockHRM)
    err := service.AssignTeamMember("proj-1", "emp-123")
    
    assert.NoError(t, err)
}
```

### Integration Tests with Testcontainers

```go
func TestIntegration_TreasuryVoucher(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping integration test")
    }
    
    // Start treasury service in container
    treasuryContainer := startTreasuryContainer(t)
    defer treasuryContainer.Terminate(context.Background())
    
    client := NewTreasuryClient(treasuryContainer.Endpoint)
    
    // Test voucher creation
    voucherID, err := client.CreateVoucher(ctx, &CreateVoucherRequest{
        ProjectID: "test-proj",
        Amount: 1000.00,
    })
    
    assert.NoError(t, err)
    assert.NotEmpty(t, voucherID)
}
```

---

## Performance Considerations

### Caching Strategy

| Data Type | Cache Duration | Invalidation Trigger |
|-----------|----------------|----------------------|
| User data | 1 hour | `auth.user.updated` event |
| Employee data | 1 hour | `hrm.employee.updated` event |
| Client data | 30 minutes | `crm.client.updated` event |
| Cost centers | 24 hours | Manual refresh |
| Integration tokens | Until expiry | Token refresh |

### Rate Limiting

| External Platform | Rate Limit | Strategy |
|-------------------|------------|----------|
| Google APIs | 100 req/min | Token bucket with Redis |
| Microsoft Graph | 120 req/min | Token bucket with Redis |
| Slack API | 100 req/min | Token bucket with Redis |
| Jira API | 100 req/min | Token bucket with Redis |
| GitHub API | 5000 req/hour | Token bucket with Redis |

---

## Monitoring & Observability

### Integration Metrics

```go
// Prometheus metrics
var (
    integrationCallsTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "projects_integration_calls_total",
            Help: "Total integration calls by service and status",
        },
        []string{"service", "method", "status"},
    )
    
    integrationCallDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name: "projects_integration_call_duration_seconds",
            Help: "Integration call duration",
        },
        []string{"service", "method"},
    )
)
```

### Health Checks

Each integration has a health check:

```
GET /health
{
  "status": "healthy",
  "integrations": {
    "auth_service": { "status": "up", "latency_ms": 45 },
    "hrm_service": { "status": "up", "latency_ms": 82 },
    "treasury_service": { "status": "up", "latency_ms": 67 },
    "postgres": { "status": "up", "latency_ms": 12 },
    "redis": { "status": "up", "latency_ms": 3 },
    "nats": { "status": "up", "latency_ms": 5 }
  }
}
```

---

## Integration Roadmap

### Phase 1: Core Internal Services (Sprint 1-2)
- ✅ Auth Service (COMPLETED)
- ✅ Notifications Service (COMPLETED)
- ⚙️ HRM Service (In Progress)
- ⚙️ Treasury Service (In Progress)

### Phase 2: Extended Internal Services (Sprint 3-4)
- 📋 CRM Service
- 📋 Finance Module
- 📋 Logistics Service
- 📋 Inventory Service

### Phase 3: Collaboration Platforms (Sprint 9)
- 📋 Google Workspace (Meet, Drive, Calendar)
- 📋 Microsoft 365 (Teams, OneDrive)
- 📋 Slack
- 📋 Zoom

### Phase 4: Development Tools (Sprint 9)
- 📋 Jira
- 📋 GitHub/GitLab
- 📋 Trello/Asana

### Phase 5: BI & Analytics (Sprint 8)
- 📋 Apache Superset

---

## Status Legend

- ✅ **Production**: Fully implemented and tested
- ⚙️ **Development**: Currently being implemented
- 📋 **Planned**: Scheduled for future sprints
- ❌ **Deprecated**: No longer supported

---

**Document Version**: 1.0  
**Last Updated**: 2024-12-05  
**Maintained By**: Projects Service Team

