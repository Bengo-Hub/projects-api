# Projects Service - Apache Superset Integration

## Overview

The Projects service integrates with the centralized Apache Superset instance for BI dashboards, analytics, and reporting. Superset is deployed as a centralized service accessible to all Codevertex services.

---

## Architecture

### Service Configuration

**Environment Variables**:
- `SUPERSET_BASE_URL` - Superset service URL
- `SUPERSET_ADMIN_USERNAME` - Admin username (K8s secret)
- `SUPERSET_ADMIN_PASSWORD` - Admin password (K8s secret)
- `SUPERSET_API_VERSION` - API version (default: v1)

**Authentication**:
- Admin credentials used for backend-to-Superset communication
- User authentication via JWT tokens passed to Superset for SSO
- Guest tokens generated for embedded dashboards

---

## Integration Methods

### 1. REST API Client

Backend uses Go HTTP client configured for Superset REST API calls.

**Base Configuration**:
- Base URL: `SUPERSET_BASE_URL/api/v1`
- Default headers: `Content-Type: application/json`
- Authentication: Bearer token from Superset login endpoint
- Retry policy: Exponential backoff (3 retries)
- Circuit breaker: Opens after 5 consecutive failures

**HTTP Client Setup**:
- Go HTTP client with 30-second timeout
- Token management with expiration tracking
- Base URL configuration from environment
- Automatic token refresh before expiry

**Key API Endpoints**:

**Authentication**:
- `POST /api/v1/security/login` - Login with admin credentials
- `POST /api/v1/security/refresh` - Refresh access token
- `POST /api/v1/security/guest_token/` - Generate guest token for embedding

**Data Sources**:
- `GET /api/v1/database/` - List all data sources
- `POST /api/v1/database/` - Create new data source
- `PUT /api/v1/database/{id}` - Update data source
- `DELETE /api/v1/database/{id}` - Delete data source

**Dashboards**:
- `GET /api/v1/dashboard/` - List all dashboards
- `POST /api/v1/dashboard/` - Create new dashboard
- `PUT /api/v1/dashboard/{id}` - Update dashboard
- `GET /api/v1/dashboard/{id}` - Get dashboard details

**Charts**:
- `GET /api/v1/chart/` - List all charts
- `POST /api/v1/chart/` - Create new chart
- `PUT /api/v1/chart/{id}` - Update chart

**Datasets**:
- `GET /api/v1/dataset/` - List all datasets
- `POST /api/v1/dataset/` - Create new dataset
- `PUT /api/v1/dataset/{id}` - Update dataset

### 2. Database Direct Connection

Superset connects directly to PostgreSQL database via read-only user for data access.

**Connection Configuration**:
- Database type: PostgreSQL with pgvector extension
- Connection string: Provided to Superset via data source API
- Read-only user: `superset_readonly` (created in PostgreSQL)
- Permissions: SELECT only on all tables, no write access
- SSL: Required for production connections

**Read-Only User Setup**:
- Create `superset_readonly` role in PostgreSQL
- Grant CONNECT on database
- Grant USAGE on schema
- Grant SELECT on all tables
- Set default privileges for future tables

**Connection String** (for Superset):
```
postgresql://superset_readonly:password@postgresql.infra.svc.cluster.local:5432/projects_db?sslmode=require
```

**Data Source Creation**:
- Data source created programmatically on application startup
- Connection tested before marking as active
- Data source updated if connection parameters change

---

## Pre-Built Dashboards

### 1. Executive Overview Dashboard

**Charts**:
- Total active projects (metric)
- Total budget allocated (metric)
- Total budget spent (metric)
- Average project health score (metric)
- Tender win rate (metric)
- Project status distribution (pie chart)
- Project health trend (line chart)
- Budget utilization (bar chart)
- Top projects by budget (table)
- Tender pipeline funnel (funnel chart)

**Filters**:
- Date range
- Tenant selection
- Project status

**Data Source**: `projects`, `tasks`, `tenders`, `budgets` tables

### 2. Project Manager Dashboard

**Charts**:
- My projects (table)
- Upcoming deadlines (timeline)
- Team workload (heatmap)
- Task completion rate (line chart)
- Budget variance (waterfall chart)
- Time logs by project (stacked bar chart)

**Filters**:
- Date range
- Project manager selection
- Project selection

**Data Source**: `projects`, `tasks`, `project_members`, `time_logs` tables

### 3. Financial Dashboard

**Charts**:
- Total project value (metric)
- Budget vs actual (grouped bar chart)
- Expense breakdown (pie chart)
- Cash flow projection (line chart)
- Voucher status (Sankey diagram)
- Top cost projects (bar chart)

**Filters**:
- Date range
- Project selection
- Expense category

**Data Source**: `projects`, `budgets`, `expenses`, `vouchers` tables

### 4. Tender Dashboard

**Charts**:
- Tender pipeline (funnel chart)
- Win/loss rate (metric)
- Tender value by status (stacked area chart)
- Average evaluation score (gauge)
- Tender sources performance (bar chart)
- Time to submission (box plot)

**Filters**:
- Date range
- Tender status
- Tender category

**Data Source**: `tenders`, `tender_evaluations`, `tender_sections` tables

### 5. Resource Utilization Dashboard

**Charts**:
- Resource allocation overview (gauge)
- Resource allocation heatmap (heatmap)
- Overallocated resources (table)
- Available resources (table)
- Project team composition (sunburst chart)

**Filters**:
- Date range
- Resource type
- Utilization status

**Data Source**: `resources`, `resource_allocations`, `project_members` tables

---

## Materialized Views

### project_metrics_mv

**Purpose**: Aggregated project metrics for performance.

**Key Columns**:
- `project_id`, `project_number`, `project_name`, `tenant_id`
- `status`, `health`, `priority`, `project_manager_id`, `project_manager_name`
- `start_date`, `end_date`, `completion_percentage`
- `budget_total`, `budget_spent`, `budget_remaining`, `budget_utilization_pct`
- `total_tasks`, `completed_tasks`, `pending_tasks`, `in_progress_tasks`
- `team_size`, `total_hours_logged`, `total_expenses`

**Refresh Strategy**: Hourly via cron job

### tender_pipeline_mv

**Purpose**: Tender pipeline metrics and status tracking.

**Key Columns**:
- `tender_id`, `tender_number`, `title`, `tenant_id`
- `status`, `priority`, `category`, `client_name`
- `estimated_value`, `currency`, `submission_deadline`, `internal_deadline`
- `deadline_status`, `win_probability`
- `discovered_by_name`, `assigned_to_name`
- `go_no_go_decision`, `awarded_date`, `award_value`
- `evaluation_count`, `avg_evaluation_score`
- `total_sections`, `approved_sections`

**Refresh Strategy**: Hourly via cron job

### resource_utilization_mv

**Purpose**: Resource allocation and capacity planning metrics.

**Key Columns**:
- `resource_id`, `resource_name`, `resource_type`, `tenant_id`
- `capacity_hours_per_week`
- `active_projects`, `total_allocation_percentage`
- `utilization_status` (overallocated, optimal, underutilized, available)
- `allocation_count`

**Refresh Strategy**: Hourly via cron job

---

## Implementation Details

### Initialization Process

1. Authenticate with Superset using admin credentials
2. Create/update data source pointing to PostgreSQL with pgvector
3. Create/update dashboards for each module:
   - Executive Overview Dashboard
   - Project Manager Dashboard
   - Financial Dashboard
   - Tender Dashboard
   - Resource Utilization Dashboard
4. Log warnings for dashboard creation failures (non-blocking)

### Dashboard Bootstrap

**Backend Endpoint**: `GET /api/v1/dashboards/{module}/embed`

**Process**:
1. Extract tenant ID from context
2. Get dashboard ID for module from Superset
3. Generate guest token with Row-Level Security (RLS) clause filtering by tenant_id
4. Construct embed URL with dashboard ID and guest token
5. Return embed URL with expiration time (5 minutes)

**Frontend Request**:
- Frontend requests dashboard URL from backend
- Backend generates secure embedded URL with user authentication token
- URL includes dashboard ID, filters, and time range
- Frontend renders dashboard using Superset SDK iframe

### Row-Level Security (RLS)

**Implementation**:
- Guest tokens include RLS clauses
- RLS filters data by `tenant_id`
- Each tenant sees only their data

**RLS Configuration**:
- RLS clause filters data by `tenant_id`
- Each tenant sees only their data
- Guest token includes RLS configuration

---

## Custom Metrics

### Budget Utilization Percentage

**Calculation**: `(SUM(budget_spent) / NULLIF(SUM(budget_total), 0)) * 100`

**Use Case**: Track budget usage across projects

### Average Task Completion Time

**Calculation**: `AVG(EXTRACT(EPOCH FROM (completed_at - created_at)) / 86400)`

**Use Case**: Measure team productivity

### Tender Win Rate

**Calculation**: `COUNT(CASE WHEN status = 'awarded' THEN 1 END) * 100.0 / NULLIF(COUNT(CASE WHEN status IN ('awarded', 'lost') THEN 1 END), 0)`

**Use Case**: Track tender success rate

### Project Health Score

**Calculation**: Maps health status (green=100, yellow=60, red=20) to numeric score

**Use Case**: Aggregate project health metrics

### Team Productivity Score

**Calculation**: `(Completed tasks in last 30 days * 100) / Team size`

**Use Case**: Measure team efficiency

---

## Error Handling

### Retry Logic

**Retry Policy**:
- Maximum 3 retry attempts
- Exponential backoff (1s, 2s, 4s delays)
- Retry on 5xx errors or network failures
- Return response on success or after max retries

### Circuit Breaker

**Implementation**:
- Opens after 5 consecutive failures
- Half-open after 60 seconds
- Closes on successful request

### Fallback Strategies

**Superset Unavailable**:
- Return cached dashboard URLs (if available)
- Show static dashboard images
- Log error for monitoring
- Alert operations team

---

## Performance Optimization

### Materialized View Refresh

**Strategy**: Refresh materialized views hourly via cron job

**Views to Refresh**:
- `project_metrics_mv`
- `tender_pipeline_mv`
- `resource_utilization_mv`

### Query Caching

**Configuration**: Superset automatically caches query results

**Cache TTL**: 1 hour (configurable)

**Warm Cache**: Pre-warm frequently accessed dashboards

### Database Indexes

**Key Indexes**:
- `projects`: `(tenant_id, status)`, `project_manager_id`, `(start_date, end_date)`
- `tasks`: `(project_id, status)`, `(assigned_to, status)`, `due_date`
- `expenses`: `(project_id, status)`, `expense_date`

---

## Monitoring

### Metrics

**Integration-Specific Metrics**:
- Superset API call latency (p50, p95, p99)
- Dashboard creation/update success rates
- Guest token generation latency
- Data source connection health
- Materialized view refresh duration

**Prometheus Metrics**:
- `superset_api_call_duration_seconds` - Histogram of API call durations (labeled by endpoint, status)
- `superset_dashboard_views_total` - Counter of dashboard views (labeled by dashboard, tenant)
- `superset_materialized_view_refresh_duration_seconds` - Histogram of view refresh durations

### Alerts

**Alert Conditions**:
- Superset service unavailability
- High API call failure rate (>5%)
- Dashboard creation failures
- Data source connection failures
- Materialized view refresh failures

---

## Security Considerations

### Authentication & Authorization

- Admin credentials stored in K8s secrets
- Guest tokens expire after 5 minutes
- RLS ensures tenant data isolation
- JWT tokens validated for SSO

### Data Privacy

- Read-only database user
- RLS filters enforce tenant isolation
- Sensitive data masked in logs
- PII data excluded from dashboards (if applicable)

---

## References

- [Apache Superset REST API Documentation](https://superset.apache.org/docs/api)
- [Superset Deployment Guide](../../devops-k8s/docs/superset-deployment.md)
- [Ordering Service Superset Integration](../../../ordering-service/ordering-backend/docs/superset-integration.md)
