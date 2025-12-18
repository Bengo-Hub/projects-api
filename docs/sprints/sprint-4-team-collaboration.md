# Sprint 4: Team & Collaboration

**Duration**: 3 weeks  
**Sprint Goal**: Implement team management, real-time collaboration, and activity tracking.  
**Team Size**: x developers  
**Prerequisites**: Sprint 3 completed

---

## Sprint Objectives

1. Implement project-level team management and RBAC
2. Develop task-level discussions with @mentions
3. Implement a project-wide activity feed
4. Enable file attachments for tasks and projects
5. Integrate with WebSockets for real-time updates

---

## User Stories

### Epic 1: Team Management

#### US-4.1: Project Team & Roles
**As a** project manager  
**I want to** assign team members to my project with specific roles  
**So that** I can manage permissions and responsibilities

**Acceptance Criteria**:
- Can add/remove users from the project team
- Support for roles: Owner, Admin, Manager, Member, Viewer, Guest
- Permissions are enforced across all project APIs

**Story Points**: 8  
**Tasks**:
- Implement project_members entity with roles
- Add RBAC middleware for project-level access
- Create team management APIs
- Integrate with auth-service for user lookup

---

### Epic 2: Collaboration

#### US-4.2: Task Comments & Mentions
**As a** team member  
**I want to** comment on tasks and mention colleagues  
**So that** we can collaborate effectively in context

**Acceptance Criteria**:
- Threaded comments on tasks
- @mention support with automatic notifications
- Markdown support in comment text

**Story Points**: 8  
**Tasks**:
- Create comment entity
- Implement comment CRUD APIs
- Add @mention parsing and notification triggers
- Integrate with notifications-service

---

#### US-4.3: Project Activity Feed
**As a** project manager  
**I want to** see a feed of all activities within the project  
**So that** I can stay updated on progress and changes

**Acceptance Criteria**:
- Feed tracks: task updates, status changes, comments, file uploads
- Filterable by user, date, and activity type
- Real-time updates via WebSockets

**Story Points**: 5  
**Tasks**:
- Implement activity logging service
- Create activity feed API
- Implement WebSocket hub for real-time event broadcasting
