package gatekeeper

type Permission string

const (
	PermPatientRead   Permission = "patient:read"
	PermPatientWrite  Permission = "patient:write"
	PermPatientDelete Permission = "patient:delete"
	PermAuditRead     Permission = "audit:read"
	PermAdmin         Permission = "admin:all"
	PermSystem        Permission = "system:all"
)

type Role string

const (
	RoleAdmin   Role = "admin"
	RoleNurse   Role = "nurse"
	RoleSystem  Role = "system"
	RoleAuditor Role = "auditor"
)

var rolePermissions = map[Role][]Permission{
	RoleAdmin:   {PermPatientRead, PermPatientWrite, PermPatientDelete, PermAuditRead, PermAdmin},
	RoleNurse:   {PermPatientRead, PermPatientWrite},
	RoleSystem:  {PermPatientRead, PermPatientWrite, PermPatientDelete, PermSystem},
	RoleAuditor: {PermAuditRead},
}

func HasPermission(role Role, perm Permission) bool {
	perms, ok := rolePermissions[role]
	if !ok {
		return false
	}
	for _, p := range perms {
		if p == perm {
			return true
		}
	}
	return false
}

func ValidRole(role string) bool {
	switch Role(role) {
	case RoleAdmin, RoleNurse, RoleSystem, RoleAuditor:
		return true
	}
	return false
}
