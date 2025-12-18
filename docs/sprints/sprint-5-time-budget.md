# Sprint 5: Time & Budget

**Duration**: 3 weeks  
**Sprint Goal**: Implement time tracking, budget management, and integration with the Treasury Service.  
**Team Size**: x developers  
**Prerequisites**: Sprint 4 completed

---

## Sprint Objectives

1. Implement time logging, task timers, and digital Signing Sheets (Attendance)
2. Develop project budget allocation and tracking
3. Implement expense recording and voucher management
4. Integrate with Treasury Service (Finance Service) for payment processing

---

## User Stories

### Epic 1: Time Tracking & Attendance

#### US-5.1: Log Time & Digital Signing Sheets
**As a** team member  
**I want to** log time spent on tasks and sign digital attendance sheets  
**So that** my effort and presence are accurately recorded for payroll and project tracking

**Acceptance Criteria**:
- Start/stop timer on tasks
- Manual time entry with descriptions
- Digital signing sheets for daily/weekly attendance
- Weekly timesheet view and submission for approval

**Story Points**: 8  
**Tasks**:
- Create time_log and attendance_sheet entities
- Implement timer and manual log APIs
- Create digital signing sheet APIs
- Create timesheet aggregation and approval APIs
- Add validation for overlapping time logs

---

### Epic 2: Budget & Expenses

#### US-5.2: Manage Project Budget
**As a** project manager  
**I want to** define and track the project budget  
**So that** I can ensure the project remains financially viable

**Acceptance Criteria**:
- Allocate budget by category (Labor, Materials, etc.)
- Real-time "Budget vs Actual" tracking
- Alerts for budget overruns

**Story Points**: 8  
**Tasks**:
- Create budget and budget_item entities
- Implement budget management APIs
- Add budget variance calculation logic
- Integrate with notifications-service for alerts

---

#### US-5.3: Expense Vouchers & Treasury (Finance) Integration
**As a** project manager  
**I want to** raise vouchers for project expenses and have them processed by the Treasury Service  
**So that** vendors and team members are paid correctly and costs are reflected in the Finance Service

**Acceptance Criteria**:
- Create vouchers with supporting documents
- Approval workflow for vouchers
- Integration with Treasury Service for payment execution
- Automatic sync of payment status back to the project expense log

**Story Points**: 13  
**Tasks**:
- Create voucher entity
- Implement voucher CRUD and approval APIs
- Implement Treasury Service (Finance) integration client
- Add logic to update project actuals from approved vouchers
- Implement webhook/event handler for treasury payment confirmations

---

### Epic 3: Activity Budgeting

#### US-5.4: Detailed Activity Budgeting
**As a** project coordinator  
**I want to** create detailed budgets for specific activities (M,I&E, transport, etc.)  
**So that** I can accurately estimate and track costs for field work and workshops.

**Acceptance Criteria**:
- Breakdown budget into: Conference, MIE, Transport, Air Travel, Internet, Airtime, and Equipment
- Link activity budgets to main project budget lines
- Automatic calculation of `total_budget` for the activity
- Validation against the parent project budget line limit

**Story Points**: 5  
**Tasks**:
- Create `activity_budgets` entity
- Implement Activity Budget management APIs
- Add logic to roll up activity budgets to project budget lines
- Implement budget validation logic (Activity Budget <= Project Budget Line)
