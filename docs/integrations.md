# Projects Service - Integration Guide

## Overview

This document provides detailed integration information for all external services and systems integrated with the Projects service, including internal Codevertex microservices and external third-party services.

---

## Table of Contents

1. [Internal Codevertex Service Integrations](#internal-codevertex-service-integrations)
2. [External Third-Party Integrations](#external-third-party-integrations)
3. [Integration Patterns](#integration-patterns)
4. [Two-Tier Configuration Management](#two-tier-configuration-management)
5. [Event-Driven Architecture](#event-driven-architecture)
6. [Integration Security](#integration-security)
7. [Error Handling & Resilience](#error-handling--resilience)

---

## Internal Codevertex Service Integrations

### Auth Service

**Integration Type**: OAuth2/OIDC + Events + REST

**Use Cases**:
- User authentication and authorization
- JWT token validation
- User identity synchronization
- Tenant/outlet discovery

**Architecture**:
- Uses `shared/auth-client` v0.1.0 library for JWT validation
- All protected `/v1/{tenant}` routes require valid Bearer tokens
- Local user table synced with auth-service (app-specific data only)

**REST API Usage**:
- `GET /api/v1/users/{id}` - Get user details (for identity sync)
- `GET /api/v1/tenants/{id}` - Get tenant details
- `GET /api/v1/tenants/by-slug/{slug}` - Get tenant by slug
- JWT validation on each request using JWKS

**Events Consumed**:
- `auth.user.created` - Create local user with app-specific defaults
- `auth.user.updated` - Update local user identity fields
- `auth.user.deactivated` - Deactivate local user
- `auth.tenant.created` - Initialize tenant in projects system
- `auth.tenant.updated` - Update tenant metadata

**Data Synchronization**:
- User data cached locally in `users` table
- Sync triggered by auth events
- Fallback to API call if user not in cache
- Refresh stale data (older than 24 hours)

### Treasury App

**Integration Type**: REST API + Events (NATS)

**Use Cases**:
- Budget submission and approval
- Voucher creation for payments
- Expense recording
- Payment tracking
- Cost allocation

**REST API Usage**:
- `POST /api/v1/budgets` - Submit budget for approval
- `GET /api/v1/budgets/{id}` - Get budget status
- `POST /api/v1/vouchers` - Create payment voucher
- `GET /api/v1/vouchers/{id}` - Get voucher status
- `POST /api/v1/expenses` - Record expense
- `GET /api/v1/cost-centers` - Get cost centers
- `POST /api/v1/payments` - Initiate payment

**Events Published**:
- `projects.voucher.created` - Voucher raised from project
- `projects.budget.submitted` - Project budget submitted for approval
- `projects.expense.recorded` - Project expense recorded

**Events Consumed**:
- `treasury.payment.completed` - Payment processed successfully
- `treasury.payment.failed` - Payment failed
- `treasury.budget.approved` - Budget approved by finance
- `treasury.budget.rejected` - Budget rejected
- `treasury.voucher.approved` - Voucher approved

### Notifications Service

**Integration Type**: Events (NATS) + REST API

**Use Cases**:
- Task assignment notifications
- Deadline reminders
- Status change notifications
- Comment mentions
- Document upload notifications
- Meeting scheduling
- Budget threshold alerts
- Milestone completion notifications

**REST API Usage**:
- `POST /v1/{tenantId}/notifications/messages` - Send notification

**Events Published**:
- `projects.notification.send` - Generic notification request
- `projects.task.assigned` - Task assigned to user
- `projects.deadline.approaching` - Deadline reminder
- `projects.status.changed` - Status changed
- `projects.comment.mentioned` - User mentioned in comment
- `projects.document.uploaded` - Document uploaded
- `projects.meeting.scheduled` - Meeting scheduled
- `projects.budget.threshold` - Budget threshold exceeded
- `projects.milestone.completed` - Milestone completed

**Notification Preferences**:
- Users configure notification preferences
- Store preferences in projects service
- Filter events based on user preferences before publishing

### Logistics Service

**Integration Type**: REST API + Events (NATS)

**Use Cases**:
- Project deliveries
- Resource transportation
- Site logistics
- Proof of delivery

**REST API Usage**:
- `POST /v1/{tenant}/tasks` - Create delivery task
- `GET /v1/{tenant}/tasks/{id}` - Get delivery status
- `POST /v1/{tenant}/vehicles/reserve` - Reserve vehicle
- `GET /v1/{tenant}/deliveries/{id}/proof` - Get proof of delivery

**Events Published**:
- `projects.delivery.requested` - Delivery request created
- `projects.resource.transport` - Resource transport needed

**Events Consumed**:
- `logistics.delivery.completed` - Delivery completed
- `logistics.delivery.failed` - Delivery failed

### Inventory Service

**Integration Type**: REST API + Events (NATS)

**Use Cases**:
- Material requisition
- Equipment allocation
- Stock tracking

**REST API Usage**:
- `POST /v1/{tenant}/inventory/requisitions` - Create material requisition
- `GET /v1/{tenant}/inventory/equipment/available` - Check equipment availability
- `POST /v1/{tenant}/inventory/equipment/reserve` - Reserve equipment
- `GET /v1/{tenant}/inventory/stock/{item}` - Check stock levels

**Events Published**:
- `projects.material.requested` - Material requisition created
- `projects.equipment.needed` - Equipment allocation request

**Events Consumed**:
- `inventory.requisition.fulfilled` - Requisition fulfilled
- `inventory.equipment.allocated` - Equipment allocated
- `inventory.stock.low` - Stock level low (affects project planning)

### IoT Service

**Integration Type**: Events (NATS)

**Use Cases**:
- Site monitoring
- Equipment telemetry
- Environmental data
- Security alerts

**Events Consumed**:
- `iot.device.telemetry` - Device telemetry data
- `iot.alert.triggered` - Alert triggered (temperature, security)
- `iot.device.offline` - Device went offline

---

## External Third-Party Integrations

### Google Workspace

#### Google Drive

**Purpose**: File storage and document collaboration

**Configuration** (Tier 1):
- OAuth Client ID: Stored encrypted at rest
- OAuth Client Secret: Stored encrypted at rest
- Refresh Token: Stored encrypted at rest

**Use Cases**:
- Store tender documents in Google Drive
- Collaborative editing of project documents
- Share files with team members
- Sync project files from Drive to S3

**API Endpoints**:
- Files API for upload/download
- Permissions API for sharing
- Webhooks for file change notifications

#### Google Meet

**Purpose**: Video conferencing for meetings

**Configuration** (Tier 1):
- OAuth credentials: Stored encrypted

**Use Cases**:
- Create meeting links for tender evaluation
- Schedule project meetings
- Automatic calendar invites

### Microsoft 365

#### OneDrive

**Purpose**: File storage and collaboration

**Configuration** (Tier 1):
- OAuth credentials: Stored encrypted

**Use Cases**: Similar to Google Drive

#### Microsoft Teams

**Purpose**: Team collaboration, meetings, chat

**Configuration** (Tier 1):
- OAuth credentials: Stored encrypted

**Use Cases**:
- Create Teams meetings
- Post project updates to Teams channels
- Team chat integration

### Slack

**Purpose**: Team communication, notifications, bot commands

**Configuration** (Tier 1):
- OAuth credentials: Stored encrypted
- Webhook signing secret: Stored encrypted

**Use Cases**:
- Post project updates to Slack channels
- Send task notifications
- Slack bot for project queries
- Slash commands for quick actions

### Jira

**Purpose**: Development task sync, issue tracking

**Configuration** (Tier 1):
- API Token: Stored encrypted
- Webhook Secret: Stored encrypted

**Use Cases**:
- Bi-directional sync of development tasks
- Link Jira issues to project tasks
- Webhook for issue updates
- Status sync

### GitHub/GitLab

**Purpose**: Link code repositories, track commits, pull requests

**Configuration** (Tier 1):
- Personal Access Token: Stored encrypted
- Webhook Secret: Stored encrypted

**Use Cases**:
- Link repositories to projects
- Show commit activity in project feed
- Webhook for push notifications
- Link commits to tasks

### Zoho Meet

**Purpose**: Video conferencing alternative

**Configuration** (Tier 1):
- OAuth credentials: Stored encrypted

**Use Cases**: Similar to Google Meet

---

## Integration Patterns

### 1. REST API Pattern (Synchronous)

**Use Case**: Real-time data retrieval, operations requiring immediate response

**Implementation**:
- HTTP client with retry logic
- Circuit breaker pattern
- Request timeout (5 seconds default)
- Idempotency keys for mutations

**When to Use**:
- Fetching user/employee data
- Creating vouchers in treasury
- Retrieving client information from CRM
- Operations requiring immediate feedback

### 2. Event-Driven Pattern (Asynchronous)

**Use Case**: Asynchronous, decoupled communication for non-blocking operations

**Transport**: NATS JetStream

**Flow**:
1. Service publishes event to NATS
2. Subscriber services consume event
3. Process event and update local state
4. Publish response events if needed

**Reliability**:
- At-least-once delivery
- Event deduplication via event_id
- Retry on failure
- Dead letter queue for failed events

**When to Use**:
- Notifications
- Audit logging
- User synchronization
- Non-critical updates that can tolerate eventual consistency

### 3. Webhook Pattern (Callbacks)

**Use Case**: Receiving notifications from external systems

**Implementation**:
- Webhook endpoints in projects service
- Signature verification (HMAC-SHA256)
- Retry logic for failed deliveries
- Idempotency handling

**When to Use**:
- GitHub/GitLab commit notifications
- Jira issue updates
- Google Drive file changes
- External calendar updates

---

## Two-Tier Configuration Management

### Tier 1: Developer/Superuser Configuration

**Visibility**: Only developers and superusers

**Configuration Items**:
- OAuth client IDs and secrets (Google, Microsoft, Slack)
- API tokens (Jira, GitHub, GitLab)
- Webhook signing secrets
- Database credentials
- Encryption keys

**Storage**:
- Encrypted at rest in database (AES-256-GCM)
- K8s secrets for runtime
- Vault for production secrets

**Management**:
- Admin API endpoints (superuser only)
- Key rotation every 90 days

### Tier 2: Business User Configuration

**Visibility**: Normal system users (tenant admins)

**Configuration Items**:
- Integration enable/disable toggles
- Notification preferences
- Default meeting provider (Google Meet, Teams, Zoho Meet)
- File storage preferences (Drive, OneDrive)

**Storage**:
- Plain text in database (non-sensitive)
- Tenant-specific configuration tables

---

## Event-Driven Architecture

### Event Catalog

#### Outbound Events (Published by Projects Service)

**projects.project.created**
```json
{
  "event_id": "uuid",
  "event_type": "projects.project.created",
  "tenant_id": "tenant-uuid",
  "timestamp": "2024-12-05T10:30:00Z",
  "data": {
    "project_id": "project-uuid",
    "project_name": "Project Name",
    "manager_id": "user-uuid",
    "client_id": "client-uuid"
  }
}
```

**projects.tender.awarded**
```json
{
  "event_id": "uuid",
  "event_type": "projects.tender.awarded",
  "tenant_id": "tenant-uuid",
  "timestamp": "2024-12-05T10:30:00Z",
  "data": {
    "tender_id": "tender-uuid",
    "project_id": "project-uuid",
    "award_value": 500000.00
  }
}
```

#### Inbound Events (Consumed by Projects Service)

**treasury.budget.approved**
```json
{
  "event_id": "uuid",
  "event_type": "treasury.budget.approved",
  "tenant_id": "tenant-uuid",
  "timestamp": "2024-12-05T10:30:00Z",
  "data": {
    "budget_id": "budget-uuid",
    "project_id": "project-uuid",
    "approved_amount": 100000.00
  }
}
```

---

## Integration Security

### Authentication

**JWT Tokens**:
- Validated via `shared/auth-client` library
- JWKS from auth-service
- Token claims include tenant_id for scoping

**OAuth 2.0**:
- Tokens stored encrypted at rest
- Automatic token refresh before expiry
- Token rotation every 90 days

**API Keys** (Service-to-Service):
- Stored in K8s secrets
- Rotated quarterly

### Authorization

**Tenant Isolation**:
- All requests scoped by tenant_id
- Provider credentials isolated per tenant
- Data isolation enforced at database level

### Secrets Management

**Encryption**:
- Secrets encrypted at rest (AES-256-GCM)
- Decrypted only when used
- Key rotation every 90 days

**Storage**:
- OAuth tokens: Encrypted in database
- API keys: K8s secrets or Vault
- Webhook secrets: Encrypted in database

### Webhook Security

**Signature Verification**:
- HMAC-SHA256 signatures
- Secret shared via K8s secret
- Timestamp validation (5-minute window)
- Nonce validation (prevent replay attacks)

---

## Error Handling & Resilience

### Retry Policies

**Exponential Backoff**:
- Initial delay: 1 second
- Max delay: 30 seconds
- Max retries: 3

### Circuit Breaker

**Implementation**:
- Opens after 5 consecutive failures
- Half-open after 60 seconds
- Closes on successful request

### Graceful Degradation

**Fallback Strategies**:
- Use cached data if external service unavailable
- Return basic/default data if both service and cache fail
- Log warnings for monitoring
- Alert operations team

### Monitoring

**Metrics**:
- API call latency (p50, p95, p99)
- API call success/failure rates
- Event publishing success rates
- Integration health status

**Alerts**:
- High failure rate (>5%)
- Service unavailability
- Event delivery failures
- Circuit breaker opened

---

## References

- [Auth Service Integration](../auth-service/auth-service/docs/integrations.md)
- [Treasury App Integration](../finance-service/treasury-api/docs/integrations.md)
- [Logistics Service Integration](../logistics-service/logistics-api/docs/integrations.md)
- [Notifications Service Integration](../notifications-service/notifications-api/docs/integrations.md)
