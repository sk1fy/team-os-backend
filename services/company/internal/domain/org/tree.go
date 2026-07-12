package org

import "sort"

const (
	ReasonDepartmentNotFound       = "Отдел не найден"
	ReasonMoveDepartmentIntoSelf   = "Нельзя переместить отдел в самого себя"
	ReasonTargetDepartmentNotFound = "Целевой отдел не найден"
	ReasonMoveDepartmentIntoChild  = "Нельзя переместить отдел внутрь его собственного подотдела"
)

// DepartmentTreeNode is a department with its nested child departments.
type DepartmentTreeNode struct {
	Department
	Children []*DepartmentTreeNode
}

// BuildDepartmentTree converts a flat department collection into a forest.
// Siblings are sorted by Order. A department whose parent is absent is kept as
// a root rather than being discarded.
func BuildDepartmentTree(departments []Department) []*DepartmentTreeNode {
	nodes := make(map[ID]*DepartmentTreeNode, len(departments))
	ids := make([]ID, 0, len(departments))

	for _, department := range departments {
		if _, exists := nodes[department.ID]; !exists {
			ids = append(ids, department.ID)
		}

		departmentCopy := department
		nodes[department.ID] = &DepartmentTreeNode{
			Department: departmentCopy,
			Children:   make([]*DepartmentTreeNode, 0),
		}
	}

	roots := make([]*DepartmentTreeNode, 0)
	for _, id := range ids {
		node := nodes[id]
		if node.ParentID != nil {
			if parent, exists := nodes[*node.ParentID]; exists {
				parent.Children = append(parent.Children, node)
				continue
			}
		}

		roots = append(roots, node)
	}

	sortDepartmentBranch(roots)

	return roots
}

func sortDepartmentBranch(nodes []*DepartmentTreeNode) {
	sort.SliceStable(nodes, func(i, j int) bool {
		return nodes[i].Order < nodes[j].Order
	})

	for _, node := range nodes {
		sortDepartmentBranch(node.Children)
	}
}

// GetDescendantIDs returns all direct and indirect descendants of id.
func GetDescendantIDs(departments []Department, id ID) map[ID]struct{} {
	childrenByParent := make(map[ID][]ID)
	for _, department := range departments {
		if department.ParentID == nil {
			continue
		}

		parentID := *department.ParentID
		childrenByParent[parentID] = append(childrenByParent[parentID], department.ID)
	}

	descendants := make(map[ID]struct{})
	queue := append([]ID(nil), childrenByParent[id]...)
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if _, visited := descendants[current]; visited {
			continue
		}

		descendants[current] = struct{}{}
		queue = append(queue, childrenByParent[current]...)
	}

	return descendants
}

// MoveValidation describes whether a department move is allowed. An empty
// Reason with Allowed=false denotes a no-op move to the current parent.
type MoveValidation struct {
	Allowed bool
	Reason  string
}

// CanMoveDepartment validates moving sourceID under targetParentID. A nil
// targetParentID means moving the department to the root level.
func CanMoveDepartment(
	departments []Department,
	sourceID ID,
	targetParentID *ID,
) MoveValidation {
	source, found := findDepartment(departments, sourceID)
	if !found {
		return MoveValidation{Reason: ReasonDepartmentNotFound}
	}

	if targetParentID != nil && *targetParentID == sourceID {
		return MoveValidation{Reason: ReasonMoveDepartmentIntoSelf}
	}

	if targetParentID != nil {
		if _, targetFound := findDepartment(departments, *targetParentID); !targetFound {
			return MoveValidation{Reason: ReasonTargetDepartmentNotFound}
		}
	}

	if equalOptionalIDs(source.ParentID, targetParentID) {
		return MoveValidation{}
	}

	if targetParentID != nil {
		if _, isDescendant := GetDescendantIDs(departments, sourceID)[*targetParentID]; isDescendant {
			return MoveValidation{Reason: ReasonMoveDepartmentIntoChild}
		}
	}

	return MoveValidation{Allowed: true}
}

func findDepartment(departments []Department, id ID) (Department, bool) {
	for _, department := range departments {
		if department.ID == id {
			return department, true
		}
	}

	return Department{}, false
}

func equalOptionalIDs(left, right *ID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}

	return *left == *right
}
