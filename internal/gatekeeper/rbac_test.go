package gatekeeper

import "testing"

type permTestCase struct {
	role Role
	perm Permission
	want bool
}

func TestAdminPermissions(t *testing.T) {
	tests := []permTestCase{
		{RoleAdmin, PermPatientRead, true},
		{RoleAdmin, PermPatientWrite, true},
		{RoleAdmin, PermPatientDelete, true},
		{RoleAdmin, PermAuditRead, true},
		{RoleAdmin, PermAdmin, true},
		{RoleAdmin, PermSystem, false},
	}
	for _, tt := range tests {
		got := HasPermission(tt.role, tt.perm)
		if got != tt.want {
			t.Errorf("HasPermission(%q, %q) = %v, want %v", tt.role, tt.perm, got, tt.want)
		}
	}
}

func TestNursePermissions(t *testing.T) {
	tests := []permTestCase{
		{RoleNurse, PermPatientRead, true},
		{RoleNurse, PermPatientWrite, true},
		{RoleNurse, PermPatientDelete, false},
		{RoleNurse, PermAuditRead, false},
		{RoleNurse, PermAdmin, false},
		{RoleNurse, PermSystem, false},
	}
	for _, tt := range tests {
		got := HasPermission(tt.role, tt.perm)
		if got != tt.want {
			t.Errorf("HasPermission(%q, %q) = %v, want %v", tt.role, tt.perm, got, tt.want)
		}
	}
}

func TestAuditorPermissions(t *testing.T) {
	tests := []permTestCase{
		{RoleAuditor, PermPatientRead, false},
		{RoleAuditor, PermPatientWrite, false},
		{RoleAuditor, PermPatientDelete, false},
		{RoleAuditor, PermAuditRead, true},
		{RoleAuditor, PermAdmin, false},
		{RoleAuditor, PermSystem, false},
	}
	for _, tt := range tests {
		got := HasPermission(tt.role, tt.perm)
		if got != tt.want {
			t.Errorf("HasPermission(%q, %q) = %v, want %v", tt.role, tt.perm, got, tt.want)
		}
	}
}

func TestSystemPermissions(t *testing.T) {
	tests := []permTestCase{
		{RoleSystem, PermPatientRead, true},
		{RoleSystem, PermPatientWrite, true},
		{RoleSystem, PermPatientDelete, true},
		{RoleSystem, PermAuditRead, false},
		{RoleSystem, PermAdmin, false},
		{RoleSystem, PermSystem, true},
	}
	for _, tt := range tests {
		got := HasPermission(tt.role, tt.perm)
		if got != tt.want {
			t.Errorf("HasPermission(%q, %q) = %v, want %v", tt.role, tt.perm, got, tt.want)
		}
	}
}

func TestInvalidRolePermissions(t *testing.T) {
	tests := []permTestCase{
		{Role("doctor"), PermPatientRead, false},
		{Role(""), PermPatientRead, false},
		{Role("unknown"), PermPatientWrite, false},
	}
	for _, tt := range tests {
		got := HasPermission(tt.role, tt.perm)
		if got != tt.want {
			t.Errorf("HasPermission(%q, %q) = %v, want %v", tt.role, tt.perm, got, tt.want)
		}
	}
}

func TestValidRole(t *testing.T) {
	valid := []string{"admin", "nurse", "system", "auditor"}
	for _, r := range valid {
		if !ValidRole(r) {
			t.Errorf("ValidRole(%q) should be true", r)
		}
	}
	invalid := []string{"doctor", "", "admin-1", "ADMIN", "root"}
	for _, r := range invalid {
		if ValidRole(r) {
			t.Errorf("ValidRole(%q) should be false", r)
		}
	}
}

func TestRoleDefinition(t *testing.T) {
	roles := []Role{RoleAdmin, RoleNurse, RoleSystem, RoleAuditor}
	for _, role := range roles {
		if !ValidRole(string(role)) {
			t.Errorf("ValidRole(%q) should be true for defined role", role)
		}
	}
}

func TestHasPermissionAllRolesAllPermissions(t *testing.T) {
	allRoles := []Role{RoleAdmin, RoleNurse, RoleSystem, RoleAuditor}
	allPerms := []Permission{PermPatientRead, PermPatientWrite, PermPatientDelete, PermAuditRead, PermAdmin, PermSystem}

	matrix := map[Role][]Permission{
		RoleAdmin:   {PermPatientRead, PermPatientWrite, PermPatientDelete, PermAuditRead, PermAdmin},
		RoleNurse:   {PermPatientRead, PermPatientWrite},
		RoleSystem:  {PermPatientRead, PermPatientWrite, PermPatientDelete, PermSystem},
		RoleAuditor: {PermAuditRead},
	}

	for _, role := range allRoles {
		for _, perm := range allPerms {
			got := HasPermission(role, perm)
			want := false
			if perms, ok := matrix[role]; ok {
				for _, p := range perms {
					if p == perm {
						want = true
						break
					}
				}
			}
			if got != want {
				t.Errorf("HasPermission(%q, %q) = %v, want %v", role, perm, got, want)
			}
		}
	}
}
