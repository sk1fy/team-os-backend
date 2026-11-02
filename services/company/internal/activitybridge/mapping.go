package activitybridge

import (
	"time"

	"github.com/google/uuid"
	dto "github.com/sk1fy/team-os-backend/services/company/internal/activitycontract"
)

type thresholds struct {
	DialingSeconds      int64 `json:"dialing_seconds"`
	ConversationSeconds int64 `json:"conversation_seconds"`
	ExceedingSeconds    int64 `json:"exceeding_seconds"`
}

func (v thresholds) public() dto.ActivityThresholds {
	out := dto.ActivityThresholds{}
	out.DialingSeconds = v.DialingSeconds
	out.ConversationSeconds = v.ConversationSeconds
	out.ExceedingSeconds = v.ExceedingSeconds
	return out
}
func thresholdsInput(v dto.ActivityThresholds) thresholds {
	out := thresholds{}
	out.DialingSeconds = v.DialingSeconds
	out.ConversationSeconds = v.ConversationSeconds
	out.ExceedingSeconds = v.ExceedingSeconds
	return out
}

type operator struct {
	EmployeeId int64      `json:"employee_id"`
	Enabled    bool       `json:"enabled"`
	Plan       int64      `json:"plan"`
	Thresholds thresholds `json:"thresholds"`
}

func (v operator) public() dto.ActivityOperator {
	out := dto.ActivityOperator{}
	out.EmployeeId = v.EmployeeId
	out.Enabled = v.Enabled
	out.Plan = v.Plan
	out.Thresholds = v.Thresholds.public()
	return out
}
func operatorInput(v dto.ActivityOperator) operator {
	out := operator{}
	out.EmployeeId = v.EmployeeId
	out.Enabled = v.Enabled
	out.Plan = v.Plan
	out.Thresholds = thresholdsInput(v.Thresholds)
	return out
}

type department struct {
	GroupId    int64      `json:"group_id"`
	Thresholds thresholds `json:"thresholds"`
}

func (v department) public() dto.ActivityDepartment {
	out := dto.ActivityDepartment{}
	out.GroupId = v.GroupId
	out.Thresholds = v.Thresholds.public()
	return out
}
func departmentInput(v dto.ActivityDepartment) department {
	out := department{}
	out.GroupId = v.GroupId
	out.Thresholds = thresholdsInput(v.Thresholds)
	return out
}

type options struct {
	PipelineIds []int64      `json:"pipeline_ids"`
	Operators   []operator   `json:"operators"`
	Departments []department `json:"departments"`
}

func (v options) public() dto.ActivityOptions {
	out := dto.ActivityOptions{}
	out.PipelineIds = append([]int64{}, v.PipelineIds...)
	out.Operators = make([]dto.ActivityOperator, 0, len(v.Operators))
	for _, x := range v.Operators {
		out.Operators = append(out.Operators, x.public())
	}
	out.Departments = make([]dto.ActivityDepartment, 0, len(v.Departments))
	for _, x := range v.Departments {
		out.Departments = append(out.Departments, x.public())
	}
	return out
}
func optionsInput(v dto.ActivityOptions) options {
	out := options{}
	out.PipelineIds = append([]int64{}, v.PipelineIds...)
	out.Operators = make([]operator, 0, len(v.Operators))
	for _, x := range v.Operators {
		out.Operators = append(out.Operators, operatorInput(x))
	}
	out.Departments = make([]department, 0, len(v.Departments))
	for _, x := range v.Departments {
		out.Departments = append(out.Departments, departmentInput(x))
	}
	return out
}

type panel struct {
	Name           string             `json:"name"`
	EmployeeIds    []int64            `json:"employee_ids"`
	DisplayWindow  dto.ActivityWindow `json:"display_window"`
	Enabled        bool               `json:"enabled"`
	Options        options            `json:"options"`
	Id             uuid.UUID          `json:"id"`
	Revision       int64              `json:"revision"`
	Timezone       string             `json:"timezone"`
	UpdatedAt      time.Time          `json:"updated_at"`
	ShareUrlIssued bool               `json:"share_url_issued"`
	ShareUrl       *string            `json:"share_url,omitempty"`
}

func (v panel) public() dto.ActivityPanel {
	out := dto.ActivityPanel{}
	out.Name = v.Name
	out.EmployeeIds = append([]int64{}, v.EmployeeIds...)
	out.DisplayWindow = v.DisplayWindow
	out.Enabled = v.Enabled
	out.Options = v.Options.public()
	out.Id = v.Id
	out.Revision = v.Revision
	out.Timezone = v.Timezone
	out.UpdatedAt = v.UpdatedAt
	out.ShareUrlIssued = v.ShareUrlIssued
	out.ShareUrl = v.ShareUrl
	return out
}

type employee struct {
	Id        int64  `json:"id"`
	Name      string `json:"name"`
	GroupId   int64  `json:"group_id"`
	GroupName string `json:"group_name"`
}

func (v employee) public() dto.ActivityEmployee {
	out := dto.ActivityEmployee{}
	out.Id = v.Id
	out.Name = v.Name
	out.GroupId = v.GroupId
	out.GroupName = v.GroupName
	return out
}

type directory struct {
	Users     []employee             `json:"users"`
	Pipelines []dto.ActivityPipeline `json:"pipelines"`
	Timezone  string                 `json:"timezone"`
}

func (v directory) public() dto.ActivityDirectory {
	out := dto.ActivityDirectory{}
	out.Users = make([]dto.ActivityEmployee, 0, len(v.Users))
	for _, x := range v.Users {
		out.Users = append(out.Users, x.public())
	}
	out.Pipelines = append([]dto.ActivityPipeline{}, v.Pipelines...)
	out.Timezone = v.Timezone
	return out
}

type taskPanel struct {
	Enabled             bool     `json:"enabled"`
	EmployeeIds         []int64  `json:"employee_ids"`
	ConsiderFutureTasks bool     `json:"consider_future_tasks"`
	IncludeRelatedDeals bool     `json:"include_related_deals"`
	PriorityKeys        []string `json:"priority_keys"`
	Revision            int64    `json:"revision"`
}

func (v taskPanel) public() dto.ActivityTaskPanel {
	out := dto.ActivityTaskPanel{}
	out.Enabled = v.Enabled
	out.EmployeeIds = append([]int64{}, v.EmployeeIds...)
	out.ConsiderFutureTasks = v.ConsiderFutureTasks
	out.IncludeRelatedDeals = v.IncludeRelatedDeals
	out.PriorityKeys = append([]string{}, v.PriorityKeys...)
	out.Revision = v.Revision
	return out
}
func taskPanelInput(v dto.ActivityTaskPanel) taskPanel {
	out := taskPanel{}
	out.Enabled = v.Enabled
	out.EmployeeIds = append([]int64{}, v.EmployeeIds...)
	out.ConsiderFutureTasks = v.ConsiderFutureTasks
	out.IncludeRelatedDeals = v.IncludeRelatedDeals
	out.PriorityKeys = append([]string{}, v.PriorityKeys...)
	out.Revision = v.Revision
	return out
}
func panelInput(v dto.ActivityPanelInput) panelWrite {
	return panelWrite{Name: v.Name, EmployeeIds: v.EmployeeIds, DisplayWindow: v.DisplayWindow, Enabled: v.Enabled, Options: optionsInput(v.Options)}
}

type panelWrite struct {
	Name          string             `json:"name"`
	EmployeeIds   []int64            `json:"employee_ids"`
	DisplayWindow dto.ActivityWindow `json:"display_window"`
	Enabled       bool               `json:"enabled"`
	Options       options            `json:"options"`
	Revision      int64              `json:"revision,omitempty"`
}
