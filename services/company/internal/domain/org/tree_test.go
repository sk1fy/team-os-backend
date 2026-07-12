package org

import (
	"reflect"
	"testing"
)

func TestBuildDepartmentTree(t *testing.T) {
	t.Run("builds nested tree and sorts siblings", func(t *testing.T) {
		tree := BuildDepartmentTree(testDepartments())
		if len(tree) != 1 {
			t.Fatalf("root count = %d, want 1", len(tree))
		}
		if tree[0].ID != "root" {
			t.Fatalf("root ID = %q, want root", tree[0].ID)
		}

		if got, want := nodeIDs(tree[0].Children), []ID{"sales", "marketing", "dev"}; !reflect.DeepEqual(got, want) {
			t.Errorf("root children = %v, want %v", got, want)
		}
		if got, want := nodeIDs(tree[0].Children[1].Children), []ID{"content"}; !reflect.DeepEqual(got, want) {
			t.Errorf("marketing children = %v, want %v", got, want)
		}
	})

	t.Run("sorts reversed input by order", func(t *testing.T) {
		departments := testDepartments()
		for left, right := 0, len(departments)-1; left < right; left, right = left+1, right-1 {
			departments[left], departments[right] = departments[right], departments[left]
		}

		tree := BuildDepartmentTree(departments)
		if got, want := nodeIDs(tree[0].Children), []ID{"sales", "marketing", "dev"}; !reflect.DeepEqual(got, want) {
			t.Errorf("root children = %v, want %v", got, want)
		}
	})

	t.Run("keeps an orphan as a root", func(t *testing.T) {
		departments := append(testDepartments(), Department{
			ID:       "orphan",
			Name:     "Сирота",
			ParentID: idPointer("ghost"),
			Order:    0,
		})

		tree := BuildDepartmentTree(departments)
		if !containsID(nodeIDs(tree), "orphan") {
			t.Errorf("root IDs = %v, want orphan to be retained", nodeIDs(tree))
		}
	})
}

func TestGetDescendantIDs(t *testing.T) {
	tests := []struct {
		name string
		id   ID
		want []ID
	}{
		{
			name: "returns children and grandchildren",
			id:   "root",
			want: []ID{"sales", "marketing", "dev", "content"},
		},
		{
			name: "returns an empty set for a leaf",
			id:   "content",
			want: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := GetDescendantIDs(testDepartments(), test.id)
			if len(got) != len(test.want) {
				t.Fatalf("descendant count = %d, want %d; got %v", len(got), len(test.want), got)
			}
			for _, id := range test.want {
				if _, exists := got[id]; !exists {
					t.Errorf("descendants = %v, want ID %q", got, id)
				}
			}
		})
	}
}

func TestCanMoveDepartment(t *testing.T) {
	tests := []struct {
		name           string
		sourceID       ID
		targetParentID *ID
		want           MoveValidation
	}{
		{
			name:           "allows a regular move",
			sourceID:       "content",
			targetParentID: idPointer("dev"),
			want:           MoveValidation{Allowed: true},
		},
		{
			name:           "allows a move to root",
			sourceID:       "content",
			targetParentID: nil,
			want:           MoveValidation{Allowed: true},
		},
		{
			name:           "rejects moving into itself",
			sourceID:       "marketing",
			targetParentID: idPointer("marketing"),
			want:           MoveValidation{Reason: ReasonMoveDepartmentIntoSelf},
		},
		{
			name:           "rejects moving into a direct descendant",
			sourceID:       "marketing",
			targetParentID: idPointer("content"),
			want:           MoveValidation{Reason: ReasonMoveDepartmentIntoChild},
		},
		{
			name:           "rejects moving root into a grandchild",
			sourceID:       "root",
			targetParentID: idPointer("content"),
			want:           MoveValidation{Reason: ReasonMoveDepartmentIntoChild},
		},
		{
			name:           "returns a reasonless no-op for the current parent",
			sourceID:       "sales",
			targetParentID: idPointer("root"),
			want:           MoveValidation{},
		},
		{
			name:           "rejects a missing source department",
			sourceID:       "ghost",
			targetParentID: idPointer("root"),
			want:           MoveValidation{Reason: ReasonDepartmentNotFound},
		},
		{
			name:           "rejects a missing target department",
			sourceID:       "content",
			targetParentID: idPointer("ghost"),
			want:           MoveValidation{Reason: ReasonTargetDepartmentNotFound},
		},
	}

	departments := testDepartments()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := CanMoveDepartment(departments, test.sourceID, test.targetParentID)
			if got != test.want {
				t.Errorf("CanMoveDepartment() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func testDepartments() []Department {
	return []Department{
		{ID: "root", Name: "Компания", ParentID: nil, Order: 0},
		{ID: "sales", Name: "Продажи", ParentID: idPointer("root"), Order: 0},
		{ID: "marketing", Name: "Маркетинг", ParentID: idPointer("root"), Order: 1},
		{ID: "dev", Name: "Разработка", ParentID: idPointer("root"), Order: 2},
		{ID: "content", Name: "Контент", ParentID: idPointer("marketing"), Order: 0},
	}
}

func idPointer(id ID) *ID {
	return &id
}

func nodeIDs(nodes []*DepartmentTreeNode) []ID {
	ids := make([]ID, len(nodes))
	for index, node := range nodes {
		ids[index] = node.ID
	}
	return ids
}

func containsID(ids []ID, target ID) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}
