package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	tc "github.com/sk1fy/team-os-backend/services/company/internal/domain/taskcontrol"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

type TaskControlReferenceCore interface {
	References(context.Context, uuid.UUID, string) (tc.ReferenceSnapshot, error)
}
type TaskControlReferenceOptions struct {
	Search              string
	DepartmentID        *uuid.UUID
	WithoutDepartment   bool
	Offset, Limit       int
	SelectedEmployeeIDs []uuid.UUID
}
type TaskControlReferenceEmployee struct {
	EmployeeID       uuid.UUID   `json:"employeeId"`
	Name             string      `json:"name"`
	PositionName     string      `json:"positionName"`
	DepartmentIDs    []uuid.UUID `json:"departmentIds"`
	CRMUserID        *string     `json:"crmUserId"`
	Eligible         bool        `json:"eligible"`
	IneligibleReason *string     `json:"ineligibleReason"`
}
type TaskControlReferenceDepartment struct {
	ID       uuid.UUID  `json:"id"`
	Name     string     `json:"name"`
	ParentID *uuid.UUID `json:"parentId"`
}
type TaskControlReferences struct {
	Scope                TaskControlScope                 `json:"scope"`
	State                string                           `json:"state"`
	CRM                  *tc.ReferenceSnapshot            `json:"crm"`
	OrganizationRevision int64                            `json:"organizationRevision"`
	MappingRevision      int64                            `json:"mappingRevision"`
	Employees            []TaskControlReferenceEmployee   `json:"employees"`
	Departments          []TaskControlReferenceDepartment `json:"departments"`
	Issues               []tc.Issue                       `json:"issues"`
	Total                int                              `json:"total"`
	Offset               int                              `json:"offset"`
	Limit                int                              `json:"limit"`
	SelectedEmployees    []TaskControlReferenceEmployee   `json:"selectedEmployees"`
}
type TaskControlReferenceRefresh struct {
	RefreshID   uuid.UUID  `json:"refreshId"`
	State       string     `json:"state"`
	ErrorCode   *string    `json:"errorCode"`
	CreatedAt   time.Time  `json:"createdAt"`
	CompletedAt *time.Time `json:"completedAt"`
}

func referenceScope(c db.TaskControlConnection) tc.ReferenceScope {
	return tc.ReferenceScope{CompanyID: c.CompanyID, AccountID: c.AccountID, IntegrationID: c.IntegrationID, InstallationID: c.InstallationID, Generation: c.Generation, Product: tc.Product}
}
func referenceScopeKey(c db.TaskControlConnection) string {
	return fmt.Sprintf("%s/%s/%s/%d/%d/%d/%s/1", c.AccountID, c.IntegrationID, c.InstallationID, c.Generation, c.BindingRevision, c.ModeEpoch, tc.Product)
}
func (s *Service) referenceConnection(ctx context.Context, q *db.Queries, a Actor, id uuid.UUID, manage bool) (db.TaskControlConnection, error) {
	var c db.TaskControlConnection
	if _, e := s.taskControlActor(ctx, q, a, manage); e != nil {
		return c, e
	}
	c, e := q.GetTaskControlConnection(ctx, db.GetTaskControlConnectionParams{CompanyID: a.CompanyID, ID: id})
	if e != nil {
		return c, notFound("Подключение")
	}
	if c.State != "active" {
		return c, forbidden("Подключение отозвано")
	}
	if e = s.taskControlAccount(ctx, q, a.CompanyID, c.AccountID); e != nil {
		return c, e
	}
	return c, nil
}
func (s *Service) taskControlFreshReferencesTx(ctx context.Context, q *db.Queries, c db.TaskControlConnection) (tc.ReferenceSnapshot, error) {
	var out tc.ReferenceSnapshot
	if c.State != "active" {
		return out, forbidden("Подключение отозвано")
	}
	if e := s.currentTaskControlInstallation(ctx, q, c.CompanyID, c.AccountID, c.InstallationID, c.IntegrationID, c.Generation); e != nil {
		return out, e
	}
	row, e := q.GetTaskControlReferenceCache(ctx, db.GetTaskControlReferenceCacheParams{CompanyID: c.CompanyID, BindingID: c.ID, ScopeKey: referenceScopeKey(c)})
	if e != nil || (row.LastError.Valid && referenceAccessInvalid(row.LastError.String)) || json.Unmarshal(row.Payload, &out) != nil || out.Validate(referenceScope(c), s.now()) != nil || !out.FreshUntil.After(s.now()) {
		return tc.ReferenceSnapshot{}, coded(ErrorUpstream, "references_unavailable", "Требуется свежий справочник CRM")
	}
	return out, nil
}
func referenceRefreshDTO(r db.TaskControlReferenceRefresh) TaskControlReferenceRefresh {
	out := TaskControlReferenceRefresh{RefreshID: r.ID, State: r.State, CreatedAt: r.CreatedAt}
	if out.State == "running" {
		out.State = "pending"
	}
	if r.ErrorCode.Valid {
		out.ErrorCode = &r.ErrorCode.String
	}
	if r.CompletedAt.Valid {
		out.CompletedAt = &r.CompletedAt.Time
	}
	return out
}
func (s *Service) queueReferenceRefresh(ctx context.Context, q *db.Queries, c db.TaskControlConnection) (TaskControlReferenceRefresh, error) {
	r, e := q.QueueTaskControlReferenceRefresh(ctx, db.QueueTaskControlReferenceRefreshParams{ID: uuid.New(), CompanyID: c.CompanyID, BindingID: c.ID, ScopeKey: referenceScopeKey(c)})
	return referenceRefreshDTO(r), e
}
func (s *Service) RequestTaskControlReferencesRefresh(ctx context.Context, a Actor, id uuid.UUID) (TaskControlReferenceRefresh, error) {
	q := db.New(s.pool)
	c, e := s.referenceConnection(ctx, q, a, id, true)
	if e != nil {
		return TaskControlReferenceRefresh{}, e
	}
	// Installation verification is performed by the bounded worker, not the request.
	return s.queueReferenceRefresh(ctx, q, c)
}
func (s *Service) GetTaskControlReferencesRefresh(ctx context.Context, a Actor, id, refreshID uuid.UUID) (TaskControlReferenceRefresh, error) {
	q := db.New(s.pool)
	c, e := s.referenceConnection(ctx, q, a, id, false)
	if e != nil {
		return TaskControlReferenceRefresh{}, e
	}
	r, e := q.GetTaskControlReferenceRefresh(ctx, db.GetTaskControlReferenceRefreshParams{CompanyID: a.CompanyID, BindingID: id, ScopeKey: referenceScopeKey(c), ID: refreshID})
	if e != nil {
		return TaskControlReferenceRefresh{}, notFound("Обновление справочников")
	}
	return referenceRefreshDTO(r), nil
}

type referenceEmployeeRow struct {
	EmployeeID    uuid.UUID   `json:"employeeId"`
	Name          string      `json:"name"`
	PositionName  string      `json:"positionName"`
	DepartmentIDs []uuid.UUID `json:"departmentIds"`
	CRMUserIDs    []string    `json:"crmUserIds"`
	Active        bool        `json:"active"`
}

func referenceEmployees(rows []referenceEmployeeRow, snapshot *tc.ReferenceSnapshot) []TaskControlReferenceEmployee {
	users := map[string]bool{}
	if snapshot != nil {
		for _, u := range snapshot.Users {
			users[u.ID] = u.IsActive
		}
	}
	// A CRM identity assigned to multiple employees is ambiguous for each employee.
	counts := map[string]int{}
	for _, r := range rows {
		for _, id := range r.CRMUserIDs {
			counts[id]++
		}
	}
	out := make([]TaskControlReferenceEmployee, 0, len(rows))
	for _, r := range rows {
		e := TaskControlReferenceEmployee{EmployeeID: r.EmployeeID, Name: r.Name, PositionName: r.PositionName, DepartmentIDs: r.DepartmentIDs}
		reason := ""
		switch {
		case !r.Active:
			reason = "employee_inactive"
		case len(r.CRMUserIDs) == 0:
			reason = "mapping_missing"
		case len(r.CRMUserIDs) != 1:
			reason = "mapping_ambiguous"
		default:
			id := r.CRMUserIDs[0]
			e.CRMUserID = &id
			switch {
			case counts[id] != 1:
				reason = "mapping_ambiguous"
			case snapshot == nil:
				reason = "references_unavailable"
			case !users[id]:
				if _, ok := users[id]; ok {
					reason = "crm_user_inactive"
				} else {
					reason = "crm_user_missing"
				}
			}
		}
		e.Eligible = reason == ""
		if reason != "" {
			e.IneligibleReason = &reason
		}
		out = append(out, e)
	}
	return out
}
func (s *Service) GetTaskControlReferences(ctx context.Context, a Actor, id uuid.UUID, opts TaskControlReferenceOptions) (TaskControlReferences, error) {
	var out TaskControlReferences
	if opts.Limit == 0 {
		opts.Limit = 100
	}
	if opts.Limit < 1 || opts.Limit > 1000 || opts.Offset < 0 || opts.Offset > 1000000 || len(opts.SelectedEmployeeIDs) > 1000 || utf8.RuneCountInString(opts.Search) > 200 || (opts.WithoutDepartment && opts.DepartmentID != nil) {
		return out, validation("Некорректные параметры справочника")
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if e != nil {
		return out, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	c, e := s.referenceConnection(ctx, q, a, id, false)
	if e != nil {
		return out, e
	}
	out.Scope = taskControlScope(c)
	out.State = "unavailable"
	out.Issues = []tc.Issue{}
	out.Employees = []TaskControlReferenceEmployee{}
	out.SelectedEmployees = []TaskControlReferenceEmployee{}
	out.Departments = []TaskControlReferenceDepartment{}
	out.Offset = opts.Offset
	out.Limit = opts.Limit
	row, e := q.GetTaskControlReferenceCache(ctx, db.GetTaskControlReferenceCacheParams{CompanyID: a.CompanyID, BindingID: id, ScopeKey: referenceScopeKey(c)})
	if e != nil && !isNoRows(e) {
		return out, e
	}
	if e == nil {
		var snap tc.ReferenceSnapshot
		if json.Unmarshal(row.Payload, &snap) == nil && snap.Validate(referenceScope(c), s.now()) == nil {
			out.CRM = &snap
			out.State = "stale"
			if snap.FreshUntil.After(s.now()) {
				out.State = "fresh"
			}
		}
		if row.LastError.Valid {
			out.Issues = append(out.Issues, tc.Issue{Path: "crm", Code: row.LastError.String, Message: "Последнее обновление справочников завершилось ошибкой"})
			out.State = "error"
			if referenceAccessInvalid(row.LastError.String) {
				out.CRM = nil
			}
		}
	}
	// A cached snapshot does not grant access after installation invalidation.
	if e = s.currentTaskControlInstallation(ctx, q, c.CompanyID, c.AccountID, c.InstallationID, c.IntegrationID, c.Generation); e != nil {
		var appErr *Error
		if errors.As(e, &appErr) && (appErr.Kind == ErrorForbidden || appErr.Kind == ErrorNotFound) {
			return TaskControlReferences{}, e
		}
		out.CRM = nil
		out.State = "unavailable"
	}
	counts, e := q.TaskControlReferenceOrganizationCounts(ctx, a.CompanyID)
	if e != nil {
		return out, e
	}
	if counts.Employees > 10000 || counts.Departments > 1000 {
		return out, coded(ErrorPayloadTooLarge, "references_too_large", "Организация превышает лимит справочника")
	}
	revisions, e := q.GetTaskControlReferenceRevisions(ctx, a.CompanyID)
	if e != nil {
		return out, e
	}
	out.OrganizationRevision = revisions.OrganizationRevision
	out.MappingRevision = revisions.MappingRevision
	employees, e := q.TaskControlReferenceEmployees(ctx, db.TaskControlReferenceEmployeesParams{CompanyID: a.CompanyID, AccountID: c.AccountID})
	if e != nil {
		return out, e
	}
	var employeeRows []referenceEmployeeRow
	if e = json.Unmarshal(employees, &employeeRows); e != nil {
		return out, e
	}
	departments, e := q.TaskControlReferenceDepartments(ctx, a.CompanyID)
	if e != nil {
		return out, e
	}
	if e = json.Unmarshal(departments, &out.Departments); e != nil {
		return out, e
	}
	selected := map[uuid.UUID]bool{}
	for _, eid := range opts.SelectedEmployeeIDs {
		selected[eid] = true
	}
	needle := strings.ToLower(strings.TrimSpace(opts.Search))
	all := referenceEmployees(employeeRows, out.CRM)
	for _, employee := range all {
		if selected[employee.EmployeeID] {
			out.SelectedEmployees = append(out.SelectedEmployees, employee)
			delete(selected, employee.EmployeeID)
		}
		match := needle == "" || strings.Contains(strings.ToLower(employee.Name+" "+employee.PositionName), needle)
		if opts.WithoutDepartment && len(employee.DepartmentIDs) != 0 {
			match = false
		}
		if opts.DepartmentID != nil {
			found := false
			for _, dep := range employee.DepartmentIDs {
				found = found || dep == *opts.DepartmentID
			}
			match = match && found
		}
		if match {
			if out.Total >= opts.Offset && len(out.Employees) < opts.Limit {
				out.Employees = append(out.Employees, employee)
			}
			out.Total++
		}
	}
	for _, eid := range opts.SelectedEmployeeIDs {
		if selected[eid] {
			reason := "employee_missing"
			out.SelectedEmployees = append(out.SelectedEmployees, TaskControlReferenceEmployee{EmployeeID: eid, Name: "Недоступный сотрудник", DepartmentIDs: []uuid.UUID{}, IneligibleReason: &reason})
			delete(selected, eid)
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return out, e
	}
	current, e := s.referenceConnection(ctx, db.New(s.pool), a, id, false)
	if e != nil {
		return TaskControlReferences{}, e
	}
	if err := s.currentTaskControlInstallation(ctx, db.New(s.pool), current.CompanyID, current.AccountID, current.InstallationID, current.IntegrationID, current.Generation); err != nil {
		var appErr *Error
		if errors.As(err, &appErr) && (appErr.Kind == ErrorForbidden || appErr.Kind == ErrorNotFound) {
			return TaskControlReferences{}, err
		}
	}
	if referenceScopeKey(current) != referenceScopeKey(c) {
		return TaskControlReferences{}, conflict("Подключение изменилось")
	}
	latest, latestErr := db.New(s.pool).LatestTaskControlReferenceRefresh(ctx, db.LatestTaskControlReferenceRefreshParams{CompanyID: a.CompanyID, BindingID: id, ScopeKey: referenceScopeKey(current)})
	if latestErr != nil && !isNoRows(latestErr) {
		return TaskControlReferences{}, latestErr
	}
	pending := latestErr == nil && (latest.State == "pending" || latest.State == "running")
	retry := latestErr != nil || latest.State != "failed" || s.now().Sub(latest.CompletedAt.Time) >= 30*time.Second
	if out.State != "fresh" && retry && !pending {
		if _, e = s.queueReferenceRefresh(ctx, db.New(s.pool), current); e != nil {
			return TaskControlReferences{}, e
		}
		pending = true
	}
	if pending {
		out.State = "refreshing"
	}

	if e = taskControlReferenceViewSize(out); e != nil {
		return TaskControlReferences{}, e
	}
	return out, nil
}

// RunTaskControlReferences processes durable refreshes immediately at startup and
// periodically thereafter. Reads never wait for CRM, and crashed leases recover.
func (s *Service) RunTaskControlReferences(ctx context.Context) error {
	// Keep scheduling independent of slow CRM accounts. A fixed worker count
	// bounds network concurrency and prevents one timeout from delaying the sweep.
	s.queueReferenceSweep(ctx)
	var workers sync.WaitGroup
	for n := 0; n < 4; n++ {
		workers.Add(1)
		go func() { defer workers.Done(); s.runReferenceWorker(ctx) }()
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			workers.Wait()
			return ctx.Err()
		case <-ticker.C:
			s.queueReferenceSweep(ctx)
		}
	}
}
func (s *Service) runReferenceWorker(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		job, e := db.New(s.pool).ClaimTaskControlReferenceRefresh(ctx)
		if e == nil {
			s.runReferenceRefresh(ctx, job)
			continue
		}
		if ctx.Err() != nil {
			return
		}
		if !isNoRows(e) {
			s.logger.WarnContext(ctx, "Не удалось получить обновление справочников", "error", e)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) queueReferenceSweep(ctx context.Context) {
	q := db.New(s.pool)
	rows, e := q.TaskControlReferenceActiveConnections(ctx)
	if e != nil {
		return
	}
	for _, c := range rows {
		if s.taskControlAccount(ctx, q, c.CompanyID, c.AccountID) != nil {
			continue
		}
		row, err := q.GetTaskControlReferenceCache(ctx, db.GetTaskControlReferenceCacheParams{CompanyID: c.CompanyID, BindingID: c.ID, ScopeKey: referenceScopeKey(c)})
		var snap tc.ReferenceSnapshot
		if err == nil && json.Unmarshal(row.Payload, &snap) == nil && snap.Validate(referenceScope(c), s.now()) == nil && s.now().Sub(snap.Provenance.FetchStartedAt) < 120*time.Second {
			continue
		}
		_, _ = s.queueReferenceRefresh(ctx, q, c)
	}
	_ = q.PruneTaskControlReferenceRefreshes(ctx)
}
func (s *Service) runReferenceRefresh(ctx context.Context, job db.TaskControlReferenceRefresh) {
	q := db.New(s.pool)
	code := "references_unavailable"
	state := "failed"
	var payload []byte
	c, e := q.GetTaskControlConnection(ctx, db.GetTaskControlConnectionParams{CompanyID: job.CompanyID, ID: job.BindingID})
	if e == nil && c.State == "active" && referenceScopeKey(c) == job.ScopeKey && s.taskControlAccount(ctx, q, c.CompanyID, c.AccountID) == nil {
		st, statusErr := s.taskControlStatus(ctx, c.CompanyID, c.AccountID, true)
		core, ok := s.taskControlCore.(TaskControlReferenceCore)
		if ok && statusErr == nil && taskControlInstalled(st, c.InstallationID, c.IntegrationID, c.Generation, s.now()) == nil {
			bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
			snap, err := core.References(bounded, c.CompanyID, c.AccountID)
			cancel()
			if err != nil {
				var codedError interface{ ReferenceCode() string }
				if errors.As(err, &codedError) {
					code = codedError.ReferenceCode()
				}
			} else if snap.Scope != referenceScope(c) {
				code = "references_scope_mismatch"
			} else if snap.Validate(referenceScope(c), s.now()) != nil {
				code = "references_incomplete"
			}
			if err == nil && snap.Validate(referenceScope(c), s.now()) == nil && snap.FreshUntil.After(s.now()) {
				payload, err = json.Marshal(snap)
				if err == nil && len(payload) <= tc.ReferenceMaxBytes {
					state = "completed"
					code = ""
				} else {
					payload = nil
					code = "references_too_large"
				}
			}
		}
	}
	// Recheck account, binding and generation after slow I/O under the same lock
	// used by revoke/mode transitions. Never revive a departed scope.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q = db.New(tx)
	leased, leaseErr := q.LockTaskControlReferenceRefresh(ctx, job.ID)
	if leaseErr != nil || leased.State != "running" || !leased.LeaseUntil.Valid || !job.LeaseUntil.Valid || !leased.LeaseUntil.Time.Equal(job.LeaseUntil.Time) {
		return
	}
	if e != nil {
		state = "failed"
		payload = nil
	} else if err = q.LockTaskControlAccount(ctx, c.AccountID); err != nil {
		return
	}
	current, err := q.GetTaskControlConnection(ctx, db.GetTaskControlConnectionParams{CompanyID: job.CompanyID, ID: job.BindingID})
	if err != nil || current.State != "active" || referenceScopeKey(current) != job.ScopeKey || s.taskControlAccount(ctx, q, job.CompanyID, current.AccountID) != nil {
		payload = nil
		state = "failed"
		code = "references_scope_changed"
	}
	if payload != nil {
		// Refresh installation status independently: a successful references adapter
		// validates Core grant, but company still maintains its installation fence.
		if s.currentTaskControlInstallation(ctx, q, current.CompanyID, current.AccountID, current.InstallationID, current.IntegrationID, current.Generation) != nil {
			payload = nil
			state = "failed"
			code = "references_installation_unconfirmed"
		}
	}
	if current.State == "active" && referenceScopeKey(current) == job.ScopeKey {
		if referenceAccessInvalid(code) {
			if err = q.InvalidateTaskControlReferenceCache(ctx, db.InvalidateTaskControlReferenceCacheParams{CompanyID: job.CompanyID, BindingID: job.BindingID, ScopeKey: job.ScopeKey, LastError: pgtype.Text{String: code, Valid: true}}); err != nil {
				return
			}
		}
		if err = q.SaveTaskControlReferenceCache(ctx, db.SaveTaskControlReferenceCacheParams{CompanyID: job.CompanyID, BindingID: job.BindingID, ScopeKey: job.ScopeKey, Payload: payload, LastError: pgtype.Text{String: code, Valid: code != ""}}); err != nil {
			return
		}
	}
	if err = q.CompleteTaskControlReferenceRefresh(ctx, db.CompleteTaskControlReferenceRefreshParams{ID: job.ID, State: state, ErrorCode: pgtype.Text{String: code, Valid: code != ""}}); err != nil {
		return
	}
	_ = tx.Commit(ctx)
}

func referenceAccessInvalid(code string) bool {
	return code == "references_denied" || code == "invalid_signature" || code == "references_scope_mismatch"
}

// Leave transport headroom below the company gRPC 8 MiB response limit.
const taskControlReferenceViewMaxBytes = 7 << 20

func taskControlReferenceViewSize(view TaskControlReferences) error {
	encoded, e := json.Marshal(view)
	if e != nil {
		return internal("Не удалось подготовить справочник", e)
	}
	if len(encoded) > taskControlReferenceViewMaxBytes {
		return coded(ErrorPayloadTooLarge, "references_too_large", "Справочник превышает лимит ответа")
	}
	return nil
}
